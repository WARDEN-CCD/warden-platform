package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WARDEN-CCD/warden-platform/templates"
)

const digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestNewValuesRejectsBadNames(t *testing.T) {
	for _, bad := range []string{"", "B", "1books", "books-", "has_underscore", strings.Repeat("a", 31)} {
		if _, err := NewValues(bad, "", "v0.1.0"); err == nil {
			t.Errorf("NewValues(%q) accepted an invalid name", bad)
		}
	}
	v, err := NewValues("order-api", "", "v0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if v.Repo != "svc-order-api" || v.Package != "dev.warden.orderapi" || v.Image != "ghcr.io/warden-ccd/svc-order-api" {
		t.Errorf("unexpected derived values: %+v", v)
	}
}

func TestRepoTreeRendersCompletely(t *testing.T) {
	v, _ := NewValues("books", "", "v0.1.0")
	dst := t.TempDir()
	files, err := Tree(templates.FS, "spring-service/repo", dst, v)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, files, "pom.xml", "mvnw", ".github/workflows/delivery.yml", ".mvn/wrapper/maven-wrapper.properties",
		"src/main/java/dev/warden/books/Application.java")
	assertNoPlaceholders(t, dst)
	wf, _ := os.ReadFile(filepath.Join(dst, ".github/workflows/delivery.yml"))
	if !strings.Contains(string(wf), "WARDEN-CCD/warden-platform/.github/workflows/build-sign-attest.yml@v0.1.0") {
		t.Errorf("delivery workflow does not call the platform workflow at the requested tag:\n%s", wf)
	}
	if !strings.Contains(string(wf), "${{ secrets.WARDEN_PROMOTER_PRIVATE_KEY }}") {
		t.Error("GitHub expression syntax was altered by rendering")
	}
}

func TestGitopsTreeNeedsDigestAndPinsIt(t *testing.T) {
	v, _ := NewValues("books", "", "v0.1.0")
	if _, err := Tree(templates.FS, "spring-service/gitops", t.TempDir(), v); err == nil {
		t.Fatal("gitops tree rendered without a digest")
	}
	v.Digest = digest
	dst := t.TempDir()
	if _, err := Tree(templates.FS, "spring-service/gitops", dst, v); err != nil {
		t.Fatal(err)
	}
	assertNoPlaceholders(t, dst)
	rollout, _ := os.ReadFile(filepath.Join(dst, "rollout.yaml"))
	if !strings.Contains(string(rollout), "image: ghcr.io/warden-ccd/svc-books@"+digest) {
		t.Errorf("rollout does not reference the image by digest:\n%s", rollout)
	}
}

func mustContain(t *testing.T, files []string, want ...string) {
	t.Helper()
	have := map[string]bool{}
	for _, f := range files {
		have[f] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("rendered tree is missing %s", w)
		}
	}
}

func assertNoPlaceholders(t *testing.T, dir string) {
	t.Helper()
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if strings.Contains(p, "__PACKAGE_PATH__") {
			t.Errorf("unreplaced path token in %s", p)
		}
		b, _ := os.ReadFile(p)
		if strings.Contains(string(b), "[[") {
			t.Errorf("unrendered placeholder in %s", p)
		}
		return nil
	})
}
