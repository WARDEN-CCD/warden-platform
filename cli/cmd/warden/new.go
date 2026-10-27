package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/WARDEN-CCD/warden-platform/cli/internal/gh"
	"github.com/WARDEN-CCD/warden-platform/cli/internal/registry"
	"github.com/WARDEN-CCD/warden-platform/cli/internal/render"
	"github.com/WARDEN-CCD/warden-platform/templates"
)

// timing records each step of `warden new` for evaluation E1 (hub EVALUATION.md).
type timing struct {
	Service     string       `json:"service"`
	StartedUTC  string       `json:"started_utc"`
	Steps       []timingStep `json:"steps"`
	TotalS      float64      `json:"total_s"`
	ManualSteps int          `json:"manual_steps"`
	Resumed     bool         `json:"resumed,omitempty"`
	Result      string       `json:"result"`
}

type timingStep struct {
	Name    string  `json:"name"`
	Seconds float64 `json:"seconds"`
	Manual  bool    `json:"manual,omitempty"`
	Note    string  `json:"note,omitempty"`
}

// scaffold carries the state of one `warden new` run.
type scaffold struct {
	t        *timing
	v        render.Values
	full     string // WARDEN-CCD/svc-<name>
	sha      string // commit the first image is built from
	work     string // temporary working directory
	deadline time.Time
}

// step runs fn, prints its outcome and records its duration.
func (s *scaffold) step(name string, fn func() (string, error)) error {
	fmt.Printf("▶ %s\n", name)
	start := time.Now()
	note, err := fn()
	ts := timingStep{Name: name, Seconds: round(time.Since(start).Seconds()), Note: note}
	s.t.Steps = append(s.t.Steps, ts)
	if err != nil {
		fmt.Printf("  ✘ %v\n", err)
		return err
	}
	fmt.Printf("  ✔ %.0fs %s\n", ts.Seconds, note)
	return nil
}

func cmdNew(args []string) error {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	owner := fs.String("owner", "warden-maintainers", "owning GitHub team")
	ref := fs.String("platform-ref", "v0.1.2", "warden-platform workflow tag the service builds with")
	dryRun := fs.Bool("dry-run", false, "render into a temporary directory and stop")
	noMerge := fs.Bool("no-merge", false, "leave the warden-gitops PR for human review instead of merging it")
	resume := fs.Bool("resume", false, "continue with an existing svc-<name> repo (e.g. after fixing a failed first build)")
	merge := fs.Bool("merge", false, "with --resume: merge the warden-gitops PR without review (resume defaults to review)")
	evidence := fs.String("evidence", "", "directory for the timing JSON (default: <hub>/evidence/scaffold-timings)")
	timeout := fs.Duration("timeout", 45*time.Minute, "overall timeout")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("usage: warden new <name> [--owner TEAM] [--platform-ref vX.Y.Z] [--resume] [--dry-run]")
	}
	v, err := render.NewValues(pos[0], *owner, *ref)
	if err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "warden-new-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	start := time.Now()
	s := &scaffold{
		t:        &timing{Service: v.Name, StartedUTC: start.UTC().Format(time.RFC3339), Result: "failed", Resumed: *resume},
		v:        v,
		full:     render.Org + "/" + v.Repo,
		work:     work,
		deadline: start.Add(*timeout),
	}
	defer func() { s.t.TotalS = round(time.Since(start).Seconds()); writeTiming(s.t, *evidence) }()

	if *resume {
		err = s.resumeRepo()
	} else {
		err = s.createRepo(*dryRun)
	}
	if err != nil || *dryRun {
		return err
	}
	// A resumed repo may have received commits nobody reviewed; register it for review unless told otherwise.
	return s.deliver(*noMerge || (*resume && !*merge))
}

// createRepo renders the template and publishes it as a new public repository with rulesets.
func (s *scaffold) createRepo(dryRun bool) error {
	repoDir := filepath.Join(s.work, s.v.Repo)
	if err := s.step("render template", func() (string, error) {
		if !dryRun && gh.RepoExists(s.full) {
			return "", fmt.Errorf("%s already exists (use --resume to continue with it)", s.full)
		}
		files, err := render.Tree(templates.FS, "spring-service/repo", repoDir, s.v)
		return fmt.Sprintf("%d files", len(files)), err
	}); err != nil {
		return err
	}
	if dryRun {
		keep := filepath.Join(os.TempDir(), "warden-dry-run-"+s.v.Repo)
		_ = os.RemoveAll(keep)
		if err := os.Rename(repoDir, keep); err != nil {
			return err
		}
		fmt.Printf("dry run: rendered into %s\n", keep)
		s.t.Result = "dry-run"
		return nil
	}
	if err := s.step("create "+s.full, func() (string, error) {
		for _, c := range [][]string{
			{"init", "-q", "-b", "main"}, {"add", "-A"}, {"update-index", "--chmod=+x", "mvnw"},
			{"commit", "-q", "-m", "feat: scaffold " + s.v.Repo + " with warden new"},
		} {
			if _, err := gh.Run(repoDir, "git", c...); err != nil {
				return "", err
			}
		}
		var err error
		if s.sha, err = gh.Run(repoDir, "git", "rev-parse", "HEAD"); err != nil {
			return "", err
		}
		_, err = gh.Run(repoDir, "gh", "repo", "create", s.full, "--public", "--source", ".", "--push",
			"--description", "Warden service "+s.v.Name+" (generated by warden new)")
		return s.sha[:12], err
	}); err != nil {
		return err
	}
	return s.step("apply branch rulesets", func() (string, error) {
		return "main-protection + main-review", applyRulesets(s.full)
	})
}

// resumeRepo continues with an existing repository at the current head of main. It re-applies missing rulesets
// and reports every commit on main that did not arrive through a pull request, so the operator sees exactly
// what was not reviewed before anything is registered for deployment.
func (s *scaffold) resumeRepo() error {
	if err := s.step("resume "+s.full, func() (string, error) {
		if !gh.RepoExists(s.full) {
			return "", fmt.Errorf("%s does not exist; run without --resume", s.full)
		}
		var head struct {
			SHA string `json:"sha"`
		}
		if err := gh.API(&head, "repos/"+s.full+"/commits/main"); err != nil {
			return "", err
		}
		s.sha = head.SHA
		return "main at " + s.sha[:12], nil
	}); err != nil {
		return err
	}
	if err := s.step("check branch rulesets", func() (string, error) {
		var have []struct {
			Name string `json:"name"`
		}
		if err := gh.API(&have, "repos/"+s.full+"/rulesets"); err != nil {
			return "", err
		}
		names := map[string]bool{}
		for _, r := range have {
			names[r.Name] = true
		}
		if names["main-protection"] && names["main-review"] {
			return "present", nil
		}
		return "re-applied", applyRulesets(s.full)
	}); err != nil {
		return err
	}
	return s.step("list commits that bypassed review", func() (string, error) {
		var commits []struct {
			SHA    string `json:"sha"`
			Commit struct {
				Message string `json:"message"`
			} `json:"commit"`
		}
		if err := gh.API(&commits, "repos/"+s.full+"/commits?sha=main&per_page=50"); err != nil {
			return "", err
		}
		var direct []string
		for i, c := range commits {
			if i == len(commits)-1 {
				break // the scaffold commit itself
			}
			var prs []any
			if err := gh.API(&prs, "repos/"+s.full+"/commits/"+c.SHA+"/pulls"); err == nil && len(prs) == 0 {
				direct = append(direct, c.SHA[:12]+" "+strings.SplitN(c.Commit.Message, "\n", 2)[0])
			}
		}
		if len(direct) == 0 {
			return "none", nil
		}
		for _, d := range direct {
			fmt.Printf("  ! pushed without a PR: %s\n", d)
		}
		return fmt.Sprintf("%d direct push(es) (ADR-027 owner bypass?)", len(direct)), nil
	})
}

// deliver waits for the first signed image, makes it public, registers the service in warden-gitops and
// waits for the canary rollout.
func (s *scaffold) deliver(noMerge bool) error {
	if err := s.step("grant promoter secret to this repo only", func() (string, error) {
		return grantPromoterSecret(s.full)
	}); err != nil {
		return err
	}
	if err := s.step("first build: sign, attest, push", func() (string, error) {
		return waitForRun(s.full, s.sha, s.deadline)
	}); err != nil {
		return err
	}

	pkg := strings.ToLower(render.Org) + "/" + s.v.Repo
	settings := fmt.Sprintf("https://github.com/orgs/%s/packages/container/%s/settings", render.Org, s.v.Repo)
	visibility := time.Now()
	digest, err := registry.AnonymousDigest(pkg, s.sha)
	if err != nil {
		fmt.Printf("▶ make the image public (ADR-028: GitHub has no API for this)\n"+
			"  Open %s → Danger Zone → Change visibility → Public\n"+
			"  If Public is greyed out, allow it once for the org: https://github.com/organizations/%s/settings/packages\n"+
			"  → Package creation → Public\n", settings, render.Org)
		openBrowser(settings)
		for err != nil {
			if time.Now().After(s.deadline) {
				return fmt.Errorf("timed out waiting for %s to become public", pkg)
			}
			time.Sleep(10 * time.Second)
			digest, err = registry.AnonymousDigest(pkg, s.sha)
		}
		s.t.ManualSteps++
		s.t.Steps = append(s.t.Steps, timingStep{Name: "make image public", Seconds: round(time.Since(visibility).Seconds()),
			Manual: true, Note: digest})
	}
	fmt.Printf("  ✔ image public; digest %s\n", digest)
	s.v.Digest = digest

	var prURL string
	if err := s.step("register in warden-gitops", func() (string, error) {
		var err error
		prURL, err = registerInGitops(s.work, s.v, s.sha, !noMerge, s.deadline)
		return prURL, err
	}); err != nil {
		return err
	}
	if noMerge {
		s.t.Result = "awaiting-review"
		fmt.Printf("warden-gitops PR left for review: %s\n", prURL)
		return nil
	}
	if err := s.step("Argo CD sync and canary rollout", func() (string, error) {
		return waitForRollout(s.v.Name, s.deadline)
	}); err != nil {
		return err
	}
	s.t.Result = "ok"
	fmt.Printf("\n%s is live: http://%s.warden.localhost:18080/api/info (kind)\n", s.v.Repo, s.v.Name)
	return nil
}

func applyRulesets(full string) error {
	rulesets := []map[string]any{
		{"name": "main-protection", "target": "branch", "enforcement": "active",
			"conditions":    map[string]any{"ref_name": map[string]any{"include": []string{"~DEFAULT_BRANCH"}, "exclude": []string{}}},
			"bypass_actors": []any{},
			"rules":         []map[string]any{{"type": "deletion"}, {"type": "non_fast_forward"}}},
		{"name": "main-review", "target": "branch", "enforcement": "active",
			"conditions":    map[string]any{"ref_name": map[string]any{"include": []string{"~DEFAULT_BRANCH"}, "exclude": []string{}}},
			"bypass_actors": []map[string]any{{"actor_id": 1, "actor_type": "OrganizationAdmin", "bypass_mode": "always"}},
			"rules": []map[string]any{{"type": "required_linear_history"}, {"type": "pull_request", "parameters": map[string]any{
				"required_approving_review_count": 1, "dismiss_stale_reviews_on_push": true, "require_code_owner_review": false,
				"require_last_push_approval": false, "required_review_thread_resolution": true,
				"allowed_merge_methods": []string{"squash", "rebase"}}}}},
	}
	for _, rs := range rulesets {
		body, _ := json.Marshal(rs)
		f, err := os.CreateTemp("", "ruleset-*.json")
		if err != nil {
			return err
		}
		_, _ = f.Write(body)
		f.Close()
		err = gh.API(nil, "-X", "POST", "repos/"+full+"/rulesets", "--input", f.Name())
		os.Remove(f.Name())
		if err != nil {
			return err
		}
	}
	return nil
}

// grantPromoterSecret adds the repo to the org secret's selected repositories (the secret is created with no
// repositories by `warden setup-apps`). Without it the first build still signs and pushes; only promotion is skipped.
func grantPromoterSecret(full string) (string, error) {
	var repo struct {
		ID int64 `json:"id"`
	}
	if err := gh.API(&repo, "repos/"+full); err != nil {
		return "", err
	}
	err := gh.API(nil, "-X", "PUT", fmt.Sprintf("orgs/%s/actions/secrets/WARDEN_PROMOTER_PRIVATE_KEY/repositories/%d", render.Org, repo.ID))
	if err != nil {
		return "skipped: org secret not set up (run `warden setup-apps`)", nil
	}
	return "WARDEN_PROMOTER_PRIVATE_KEY", nil
}

// waitForRun waits for the delivery workflow run of commit sha on main and requires success.
func waitForRun(full, sha string, deadline time.Time) (string, error) {
	for time.Now().Before(deadline) {
		var runs struct {
			WorkflowRuns []struct {
				ID         int64  `json:"id"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
				HTMLURL    string `json:"html_url"`
			} `json:"workflow_runs"`
		}
		if err := gh.API(&runs, "repos/"+full+"/actions/runs?head_sha="+sha+"&branch=main"); err == nil {
			for _, r := range runs.WorkflowRuns {
				if r.Status == "completed" {
					if r.Conclusion != "success" {
						return r.HTMLURL, fmt.Errorf("delivery run %s", r.Conclusion)
					}
					return r.HTMLURL, nil
				}
			}
		}
		time.Sleep(15 * time.Second)
	}
	return "", fmt.Errorf("timed out waiting for the first delivery run")
}

// registerInGitops opens a PR adding apps/<name>/ to warden-gitops and, when merge is true, merges it once the
// digest-only check passes (the author is an org owner, so the review rule is bypassed — ADR-027).
func registerInGitops(work string, v render.Values, sha string, merge bool, deadline time.Time) (string, error) {
	dir := filepath.Join(work, "warden-gitops")
	if _, err := gh.Run(work, "gh", "repo", "clone", render.Org+"/warden-gitops", dir, "--", "--depth", "1"); err != nil {
		return "", err
	}
	branch := "warden-new/" + v.Name
	if _, err := render.Tree(templates.FS, "spring-service/gitops", filepath.Join(dir, "apps", v.Name), v); err != nil {
		return "", err
	}
	for _, c := range [][]string{
		{"switch", "-q", "-c", branch}, {"add", "-A"},
		{"commit", "-q", "-m", fmt.Sprintf("feat(apps): register %s (%s from %s)", v.Name, v.Digest[7:19], sha[:12])},
		{"push", "-q", "-u", "origin", branch},
	} {
		if _, err := gh.Run(dir, "git", c...); err != nil {
			return "", err
		}
	}
	url, err := gh.Run(dir, "gh", "pr", "create", "--base", "main", "--head", branch,
		"--title", "feat(apps): register "+v.Name,
		"--body", fmt.Sprintf("Generated by `warden new`. Image `%s@%s`, built from %s/%s@%s.", v.Image, v.Digest, render.Org, v.Repo, sha))
	if err != nil || !merge {
		return url, err
	}
	for time.Now().Before(deadline) {
		out, _ := gh.Run(dir, "gh", "pr", "checks", url, "--json", "name,state", "--jq", `.[] | select(.name=="digest-only") | .state`)
		if out == "SUCCESS" {
			head, err := gh.Run(dir, "gh", "pr", "view", url, "--json", "headRefOid", "--jq", ".headRefOid")
			if err != nil {
				return url, err
			}
			_, err = gh.Run(dir, "gh", "pr", "merge", url, "--squash", "--admin", "--delete-branch", "--match-head-commit", head)
			return url, err
		}
		if out == "FAILURE" {
			return url, fmt.Errorf("digest-only check failed on %s", url)
		}
		time.Sleep(10 * time.Second)
	}
	return url, fmt.Errorf("timed out waiting for the digest-only check")
}

// waitForRollout waits until Argo CD reports svc-<name> Synced/Healthy and the Rollout is Healthy.
func waitForRollout(name string, deadline time.Time) (string, error) {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return "skipped (no kubectl)", nil
	}
	for time.Now().Before(deadline) {
		app, _ := gh.Run("", "kubectl", "-n", "argocd", "get", "application", "svc-"+name,
			"-o", "jsonpath={.status.sync.status}/{.status.health.status}")
		phase, _ := gh.Run("", "kubectl", "-n", "svc-"+name, "get", "rollout", name, "-o", "jsonpath={.status.phase}")
		if app == "Synced/Healthy" && phase == "Healthy" {
			return "Argo CD Synced/Healthy, Rollout Healthy", nil
		}
		time.Sleep(10 * time.Second)
	}
	return "", fmt.Errorf("timed out waiting for svc-%s to become healthy", name)
}

func writeTiming(t *timing, dir string) {
	if dir == "" {
		dir = findHubEvidence()
	}
	if dir == "" {
		return
	}
	_ = os.MkdirAll(dir, 0o755)
	b, _ := json.MarshalIndent(t, "", "  ")
	name := strings.ReplaceAll(t.StartedUTC, ":", "") + "-" + t.Service + ".json"
	if err := os.WriteFile(filepath.Join(dir, name), append(b, '\n'), 0o644); err == nil {
		fmt.Printf("timing written to %s\n", filepath.Join(dir, name))
	}
}

// findHubEvidence looks for the hub's evidence/scaffold-timings from the working directory upwards.
func findHubEvidence() string {
	dir, _ := os.Getwd()
	for i := 0; i < 5 && dir != ""; i++ {
		if _, err := os.Stat(filepath.Join(dir, "workspace.yaml")); err == nil {
			return filepath.Join(dir, "evidence", "scaffold-timings")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func openBrowser(url string) {
	switch runtime.GOOS {
	case "windows":
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		_ = exec.Command("open", url).Start()
	default:
		_ = exec.Command("xdg-open", url).Start()
	}
}

func round(f float64) float64 { return float64(int(f*10)) / 10 }
