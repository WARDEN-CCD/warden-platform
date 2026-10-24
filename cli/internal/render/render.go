// Package render turns the embedded service template into a service repository and its warden-gitops directory.
//
// Templates use [[ ]] delimiters because GitHub workflow files already use ${{ }}. Path segments named
// __PACKAGE_PATH__ become the Java package path.
package render

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

// Org is the GitHub organisation that owns every Warden repository.
const Org = "WARDEN-CCD"

var nameRE = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,28}[a-z0-9])?$`)

// Values are the template inputs.
type Values struct {
	Name        string // books
	Repo        string // svc-books
	Package     string // dev.warden.books
	Owner       string // GitHub team slug that owns the service
	Image       string // ghcr.io/warden-ccd/svc-books
	Org         string // WARDEN-CCD
	PlatformRef string // tag of warden-platform's build-sign-attest workflow, e.g. v0.1.0
	Digest      string // sha256:<64 hex>; required only for the gitops tree
}

// NewValues validates name and derives the other values.
func NewValues(name, owner, platformRef string) (Values, error) {
	if !nameRE.MatchString(name) {
		return Values{}, fmt.Errorf("invalid service name %q: lowercase letters, digits and '-', 2-30 chars, start with a letter", name)
	}
	if owner == "" {
		owner = "warden-maintainers"
	}
	repo := "svc-" + name
	return Values{
		Name:        name,
		Repo:        repo,
		Package:     "dev.warden." + strings.ReplaceAll(name, "-", ""),
		Owner:       owner,
		Image:       "ghcr.io/" + strings.ToLower(Org) + "/" + repo,
		Org:         Org,
		PlatformRef: platformRef,
	}, nil
}

var digestRE = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// Tree renders the template subtree root ("repo" or "gitops") of fsys into dst and returns the written paths
// relative to dst.
func Tree(fsys fs.FS, root, dst string, v Values) ([]string, error) {
	if path.Base(root) == "gitops" && !digestRE.MatchString(v.Digest) {
		return nil, fmt.Errorf("gitops tree needs an image digest (sha256:<64 hex>), got %q", v.Digest)
	}
	pkgPath := strings.ReplaceAll(v.Package, ".", "/")
	var written []string
	err := fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(p, root+"/")
		rel = strings.ReplaceAll(rel, "__PACKAGE_PATH__", pkgPath)
		raw, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		tmpl, err := template.New(p).Delims("[[", "]]").Option("missingkey=error").Parse(string(raw))
		if err != nil {
			return fmt.Errorf("parse %s: %w", p, err)
		}
		var out bytes.Buffer
		if err := tmpl.Execute(&out, v); err != nil {
			return fmt.Errorf("render %s: %w", p, err)
		}
		target := filepath.Join(dst, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if path.Base(rel) == "mvnw" {
			mode = 0o755
		}
		if err := os.WriteFile(target, out.Bytes(), mode); err != nil {
			return err
		}
		written = append(written, rel)
		return nil
	})
	return written, err
}
