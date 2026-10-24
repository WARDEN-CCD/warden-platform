// Package gh wraps the GitHub CLI and git, which every Warden developer already has authenticated
// (hub scripts/doctor.py). Using them keeps the warden binary free of third-party modules.
package gh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Run executes name with args in dir and returns trimmed stdout.
func Run(dir, name string, args ...string) (string, error) {
	var out, errb bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return strings.TrimSpace(out.String()), fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err,
			strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// API calls `gh api` and decodes the JSON response into v (when v is non-nil).
func API(v any, args ...string) error {
	out, err := Run("", "gh", append([]string{"api"}, args...)...)
	if err != nil {
		return err
	}
	if v == nil || out == "" {
		return nil
	}
	return json.Unmarshal([]byte(out), v)
}

// RepoExists reports whether owner/repo exists and is visible.
func RepoExists(fullName string) bool {
	_, err := Run("", "gh", "repo", "view", fullName, "--json", "name")
	return err == nil
}
