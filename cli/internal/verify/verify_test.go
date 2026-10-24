package verify

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The policy file Warden's cluster enforces, from the sibling warden-policy checkout when present.
func realPolicy(t *testing.T) Policy {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	file := filepath.Join(filepath.Dir(here), "..", "..", "..", "..", "warden-policy", "policies", "supply-chain",
		"ivp-verify-warden-images.yaml")
	b, err := os.ReadFile(file)
	if err != nil {
		t.Skipf("warden-policy checkout not found next to warden-platform: %v", err)
	}
	p, err := ParsePolicy(strings.ReplaceAll(string(b), "\r\n", "\n"))
	if err != nil {
		t.Fatalf("the real admission policy no longer parses: %v", err)
	}
	return p
}

func TestParsesTheRealAdmissionPolicy(t *testing.T) {
	p := realPolicy(t)
	if !strings.Contains(p.SubjectRegExp, "warden-platform") || p.Issuer != "https://token.actions.githubusercontent.com" {
		t.Errorf("unexpected identity: %+v", p)
	}
	if p.RepoPrefix != "https://github.com/WARDEN-CCD/svc-" || p.RequiredRef != "refs/heads/main" {
		t.Errorf("unexpected provenance constraints: %+v", p)
	}
	if len(p.PredicateTypes) != 2 {
		t.Errorf("want provenance + SBOM predicate types, got %v", p.PredicateTypes)
	}
}

func envelope(t *testing.T, predicateType string, predicate any) []byte {
	t.Helper()
	st, _ := json.Marshal(map[string]any{"predicateType": predicateType, "predicate": predicate})
	line, _ := json.Marshal(map[string]string{"payload": base64.StdEncoding.EncodeToString(st)})
	return append(line, '\n')
}

func fakeCosign(t *testing.T, repo, ref string, signed bool) Runner {
	return func(args ...string) ([]byte, error) {
		if !signed {
			return nil, errors.New("no signatures found")
		}
		if args[0] == "verify" {
			return []byte("[]"), nil
		}
		switch args[3] {
		case "https://slsa.dev/provenance/v1":
			return envelope(t, args[3], map[string]any{"buildDefinition": map[string]any{
				"externalParameters": map[string]any{"workflow": map[string]any{"repository": repo, "ref": ref}}}}), nil
		case "https://cyclonedx.org/bom":
			return envelope(t, args[3], map[string]any{"components": []any{1, 2, 3}}), nil
		case "vuln":
			return envelope(t, "https://cosign.sigstore.dev/attestation/vuln/v1", map[string]any{"scanner": map[string]any{
				"result": map[string]any{"Results": []any{map[string]any{"Vulnerabilities": []any{
					map[string]any{"Severity": "HIGH"}}}}}}}), nil
		}
		return nil, errors.New("unexpected type")
	}
}

const img = "ghcr.io/warden-ccd/svc-books@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestVerdicts(t *testing.T) {
	p := realPolicy(t)
	cases := []struct {
		name, image, repo, ref string
		signed, admit          bool
	}{
		{"good image", img, "https://github.com/WARDEN-CCD/svc-books", "refs/heads/main", true, true},
		{"tag instead of digest (A01)", "ghcr.io/warden-ccd/svc-books:v1", "", "", true, false},
		{"unsigned (A02)", img, "", "", false, false},
		{"foreign caller repo (A04)", img, "https://github.com/attacker/svc-books", "refs/heads/main", true, false},
		{"feature branch (A05)", img, "https://github.com/WARDEN-CCD/svc-books", "refs/heads/feature", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, admitted := Image(c.image, p, fakeCosign(t, c.repo, c.ref, c.signed))
			if admitted != c.admit {
				t.Errorf("admitted=%v, want %v", admitted, c.admit)
			}
		})
	}
}
