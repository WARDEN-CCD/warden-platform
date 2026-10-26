package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/WARDEN-CCD/warden-platform/cli/internal/dora"
	"github.com/WARDEN-CCD/warden-platform/cli/internal/gh"
	"github.com/WARDEN-CCD/warden-platform/cli/internal/render"
)

// The ledger: Argo CD Notifications records one GitHub Deployment per service sync on warden-gitops, with the
// environment set to the Application name (svc-<name>) and a final status of success or failure (ADR-019).
// The deployed gitops commit is a promotion commit whose title names the source commit:
// "deploy(<name>): <digest12> from <sha12>".
var fromRE = regexp.MustCompile(`from ([0-9a-f]{7,40})`)

func cmdDora(args []string) error {
	fs := flag.NewFlagSet("dora", flag.ContinueOnError)
	since := fs.String("since", time.Now().AddDate(0, 0, -30).Format("2006-01-02"), "window start: YYYY-MM-DD or an RFC 3339 timestamp, UTC")
	asJSON := fs.Bool("json", false, "print JSON (for evidence/dora)")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	from, err := parseSince(*since)
	if err != nil {
		return err
	}
	deps, err := fetchDeployments(from)
	if err != nil {
		return err
	}
	m := dora.Compute(deps, from, time.Now().UTC())
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(m)
	}
	fmt.Printf("DORA metrics %s → %s (%d deployments, %d failed)\n", m.From.Format("2006-01-02"), m.To.Format("2006-01-02"), m.Deployments, m.Failed)
	fmt.Printf("  deployment frequency        %.2f per day\n", m.DeploymentsPerDay)
	fmt.Printf("  change lead time (median)   %.2f h\n", m.MedianLeadTimeHours)
	fmt.Printf("  change fail rate            %.1f%%\n", 100*m.ChangeFailRate)
	fmt.Printf("  failed deployment recovery  %.1f min (median)\n", m.MedianRecoveryTimeMinutes)
	fmt.Printf("  deployment rework rate      %.1f%%\n", 100*m.ReworkRate)
	return nil
}

// parseSince accepts a day or an exact instant. A timestamp lets a window start after deployments that were
// recorded only because the notifications controller (re)started and re-announced the current state.
func parseSince(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("--since %q: want YYYY-MM-DD or RFC 3339 (2026-10-26T10:00:00Z)", s)
	}
	return t, nil
}

func fetchDeployments(from time.Time) ([]dora.Deployment, error) {
	gitops := render.Org + "/warden-gitops"
	var raw []struct {
		ID          int64  `json:"id"`
		SHA         string `json:"sha"`
		Environment string `json:"environment"`
	}
	if err := gh.API(&raw, "--paginate", "repos/"+gitops+"/deployments?per_page=100"); err != nil {
		return nil, err
	}
	var out []dora.Deployment
	for _, d := range raw {
		if !strings.HasPrefix(d.Environment, "svc-") {
			continue
		}
		var statuses []struct {
			State     string    `json:"state"`
			CreatedAt time.Time `json:"created_at"`
		}
		if err := gh.API(&statuses, fmt.Sprintf("repos/%s/deployments/%d/statuses?per_page=10", gitops, d.ID)); err != nil || len(statuses) == 0 {
			continue
		}
		final := statuses[0] // newest first
		if final.State != "success" && final.State != "failure" || final.CreatedAt.Before(from) {
			continue
		}
		dep := dora.Deployment{Service: d.Environment, State: final.State, DeployedAt: final.CreatedAt}
		// An Application's sync revision is the gitops repo's HEAD, which can be another service's promotion
		// (services deploy concurrently). The promotion that deployed this service is the newest commit that
		// touched its directory, reachable from that revision.
		var promotions []struct {
			Commit struct {
				Message string `json:"message"`
			} `json:"commit"`
		}
		path := "apps/" + strings.TrimPrefix(d.Environment, "svc-")
		if gh.API(&promotions, fmt.Sprintf("repos/%s/commits?sha=%s&path=%s&per_page=1", gitops, d.SHA, path)) == nil && len(promotions) == 1 {
			if m := fromRE.FindStringSubmatch(promotions[0].Commit.Message); m != nil {
				var src struct {
					Commit struct {
						Message string `json:"message"`
						Author  struct {
							Date time.Time `json:"date"`
						} `json:"author"`
					} `json:"commit"`
				}
				if gh.API(&src, "repos/"+render.Org+"/"+d.Environment+"/commits/"+m[1]) == nil {
					dep.CommitAt = src.Commit.Author.Date
					dep.CommitTitle = strings.SplitN(src.Commit.Message, "\n", 2)[0]
				}
			}
		}
		out = append(out, dep)
	}
	return out, nil
}
