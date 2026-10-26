package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/WARDEN-CCD/warden-platform/cli/internal/gh"
	"github.com/WARDEN-CCD/warden-platform/cli/internal/render"
)

// The two GitHub Apps Warden needs (hub ADR-013, ADR-019). Created with GitHub's App-manifest flow: the CLI
// pre-fills the form, the org owner clicks "Create" and "Install", and the private key goes to ~/.warden/.
type appSpec struct {
	Name        string
	EnvPrefix   string
	Description string
	Permissions map[string]string
}

var apps = []appSpec{
	{Name: "warden-ccd-promoter", EnvPrefix: "WARDEN_PROMOTER",
		Description: "Opens digest-only promotion PRs in warden-gitops (Warden ADR-013).",
		Permissions: map[string]string{"contents": "write", "pull_requests": "write", "metadata": "read"}},
	{Name: "warden-ccd-deployments", EnvPrefix: "WARDEN_DEPLOYMENTS",
		Description: "Argo CD Notifications records GitHub Deployments for the DORA ledger (Warden ADR-019).",
		Permissions: map[string]string{"deployments": "write", "metadata": "read"}},
}

func cmdSetupApps(args []string) error {
	fs := flag.NewFlagSet("setup-apps", flag.ContinueOnError)
	only := fs.String("only", "", "create just this app (warden-ccd-promoter or warden-ccd-deployments)")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".warden")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, spec := range apps {
		if *only != "" && *only != spec.Name {
			continue
		}
		fmt.Printf("\n== %s ==\n", spec.Name)
		id, slug, pem, err := createApp(spec)
		if err != nil {
			return err
		}
		pemPath := filepath.Join(dir, slug+".pem")
		if err := os.WriteFile(pemPath, []byte(pem), 0o600); err != nil {
			return err
		}
		if err := setEnv(filepath.Join(dir, "env"), spec.EnvPrefix+"_APP_ID", strconv.FormatInt(id, 10)); err != nil {
			return err
		}
		fmt.Printf("  created app %s (id %d); private key saved to %s\n", slug, id, pemPath)

		install := "https://github.com/apps/" + slug + "/installations/new"
		fmt.Printf("  Install it on %s/warden-gitops only: %s\n", render.Org, install)
		openBrowser(install)
		instID, err := waitForInstallation(slug)
		if err != nil {
			return err
		}
		if err := setEnv(filepath.Join(dir, "env"), spec.EnvPrefix+"_INSTALLATION_ID", strconv.FormatInt(instID, 10)); err != nil {
			return err
		}
		fmt.Printf("  installed (installation id %d)\n", instID)

		if spec.EnvPrefix == "WARDEN_PROMOTER" {
			if err := wirePromoter(id, pemPath); err != nil {
				return err
			}
		}
	}
	fmt.Println("\nDone. Re-run `task up:local` (or warden-infra scripts/bootstrap_secrets.py) to load the deployments app into Argo CD.")
	return nil
}

// createApp runs the manifest flow on a loopback-only HTTP server.
func createApp(spec appSpec) (int64, string, string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, "", "", err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	state := hex.EncodeToString(buf)
	manifest, _ := json.Marshal(map[string]any{
		"name":                spec.Name,
		"url":                 "https://github.com/" + render.Org + "/warden",
		"description":         spec.Description,
		"hook_attributes":     map[string]any{"url": "https://github.com/" + render.Org + "/warden", "active": false},
		"redirect_url":        fmt.Sprintf("http://127.0.0.1:%d/callback", port),
		"public":              false,
		"default_permissions": spec.Permissions,
		"default_events":      []string{},
	})
	codes := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<!doctype html><title>Create %s</title><body onload="document.forms[0].submit()">
<form method="post" action="https://github.com/organizations/%s/settings/apps/new?state=%s">
<input type="hidden" name="manifest" value="%s"><noscript><button>Continue to GitHub</button></noscript></form>`,
			html.EscapeString(spec.Name), render.Org, state, html.EscapeString(string(manifest)))
	})
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, "App created. You can close this tab and return to the terminal.")
		select {
		case codes <- r.URL.Query().Get("code"):
		default:
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Shutdown(context.Background())

	start := fmt.Sprintf("http://127.0.0.1:%d/", port)
	fmt.Printf("  Opening %s — review the pre-filled app on GitHub and click \"Create GitHub App\".\n", start)
	openBrowser(start)

	var code string
	select {
	case code = <-codes:
	case <-time.After(15 * time.Minute):
		return 0, "", "", fmt.Errorf("timed out waiting for GitHub to redirect back")
	}
	var conv struct {
		ID   int64  `json:"id"`
		Slug string `json:"slug"`
		PEM  string `json:"pem"`
	}
	if err := gh.API(&conv, "-X", "POST", "app-manifests/"+code+"/conversions"); err != nil {
		return 0, "", "", fmt.Errorf("exchange manifest code: %w", err)
	}
	return conv.ID, conv.Slug, conv.PEM, nil
}

func waitForInstallation(slug string) (int64, error) {
	deadline := time.Now().Add(15 * time.Minute)
	for time.Now().Before(deadline) {
		var list struct {
			Installations []struct {
				ID      int64  `json:"id"`
				AppSlug string `json:"app_slug"`
			} `json:"installations"`
		}
		if gh.API(&list, "orgs/"+render.Org+"/installations") == nil {
			for _, i := range list.Installations {
				if i.AppSlug == slug {
					return i.ID, nil
				}
			}
		}
		time.Sleep(5 * time.Second)
	}
	return 0, fmt.Errorf("timed out waiting for %s to be installed", slug)
}

// wirePromoter makes the promoter usable by every service pipeline and lets its PRs merge on the
// digest-only check alone (ADR-013): org variable + secret, gitops review-rule bypass, auto-merge.
func wirePromoter(appID int64, pemPath string) error {
	if _, err := gh.Run("", "gh", "variable", "set", "WARDEN_PROMOTER_APP_ID", "--org", render.Org,
		"--visibility", "all", "--body", strconv.FormatInt(appID, 10)); err != nil {
		return err
	}
	key, err := os.ReadFile(pemPath)
	if err != nil {
		return err
	}
	// Selected repositories only: `warden new` grants each svc-* repo access individually (grantPromoterSecret),
	// so no other org repository can mint promotion tokens.
	if _, err := runWithStdin(string(key), "gh", "secret", "set", "WARDEN_PROMOTER_PRIVATE_KEY", "--org", render.Org,
		"--no-repos-selected"); err != nil {
		return err
	}
	gitops := "repos/" + render.Org + "/warden-gitops"
	if err := gh.API(nil, "-X", "PATCH", gitops, "-F", "allow_auto_merge=true"); err != nil {
		return err
	}
	// The promote job opens its PR with --label digest-only, and `gh pr create` refuses a label that does not exist.
	// The first live run of that job would otherwise fail after pushing its branch.
	if _, err := gh.Run("", "gh", "label", "create", "digest-only", "--repo", render.Org+"/warden-gitops",
		"--color", "0e8a16", "--description", "Promotion PR that changes exactly one image digest (opened by the promoter app)",
		"--force"); err != nil {
		return err
	}
	var rulesets []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := gh.API(&rulesets, gitops+"/rulesets"); err != nil {
		return err
	}
	for _, rs := range rulesets {
		if rs.Name != "main-review" {
			continue
		}
		var full map[string]any
		if err := gh.API(&full, fmt.Sprintf("%s/rulesets/%d", gitops, rs.ID)); err != nil {
			return err
		}
		actors, _ := full["bypass_actors"].([]any)
		actors = append(actors, map[string]any{"actor_id": appID, "actor_type": "Integration", "bypass_mode": "pull_request"})
		body, _ := json.Marshal(map[string]any{"bypass_actors": actors})
		f, err := os.CreateTemp("", "ruleset-*.json")
		if err != nil {
			return err
		}
		_, _ = f.Write(body)
		f.Close()
		err = gh.API(nil, "-X", "PUT", fmt.Sprintf("%s/rulesets/%d", gitops, rs.ID), "--input", f.Name())
		os.Remove(f.Name())
		if err != nil {
			return err
		}
	}
	fmt.Println("  wired: org variable + secret, auto-merge and the digest-only label on warden-gitops, main-review bypass (PRs only)")
	return nil
}

// setEnv sets KEY=value in a dotenv file, keeping other lines.
func setEnv(path, key, value string) error {
	b, _ := os.ReadFile(path)
	var lines []string
	found := false
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if strings.HasPrefix(l, key+"=") {
			l, found = key+"="+value, true
		}
		if l != "" || len(lines) > 0 {
			lines = append(lines, l)
		}
	}
	if !found {
		lines = append(lines, key+"="+value)
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}
