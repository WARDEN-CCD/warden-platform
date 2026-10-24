// Package verify reproduces Warden's admission checks locally (`warden verify`), using cosign as the verifier
// and the warden-policy ImageValidatingPolicy as the single source of the expected identities.
package verify

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// PolicyURL is the supply-chain policy admission enforces; %s is the git ref.
const PolicyURL = "https://raw.githubusercontent.com/WARDEN-CCD/warden-policy/%s/policies/supply-chain/ivp-verify-warden-images.yaml"

// Policy holds the constants admission uses to accept an image.
type Policy struct {
	SubjectRegExp  string // certificate subject of the platform build workflow
	Issuer         string // OIDC issuer
	RepoPrefix     string // allowed provenance source repository prefix
	RequiredRef    string // provenance ref that must have been built
	PredicateTypes []string
}

var (
	subjectRE = regexp.MustCompile(`subjectRegExp:\s*'([^']+)'`)
	issuerRE  = regexp.MustCompile(`issuer:\s*(https://\S+)`)
	prefixRE  = regexp.MustCompile(`allowedRepoPrefix\s*\n\s*expression:\s*"'([^']+)'"`)
	refRE     = regexp.MustCompile(`workflow\.ref\s*\n\s*==\s*'([^']+)'`)
	typeRE    = regexp.MustCompile(`intoto:\s*\n\s*type:\s*(https://\S+)`)
)

// ParsePolicy extracts the constants from the policy YAML text. The patterns are deliberately narrow: if the
// policy file changes shape, parsing fails loudly instead of silently verifying something else.
func ParsePolicy(text string) (Policy, error) {
	var p Policy
	get := func(re *regexp.Regexp, what string) (string, error) {
		m := re.FindStringSubmatch(text)
		if m == nil {
			return "", fmt.Errorf("policy: cannot find %s", what)
		}
		return m[1], nil
	}
	var err error
	if p.SubjectRegExp, err = get(subjectRE, "attestor subjectRegExp"); err != nil {
		return p, err
	}
	if p.Issuer, err = get(issuerRE, "attestor issuer"); err != nil {
		return p, err
	}
	if p.RepoPrefix, err = get(prefixRE, "allowedRepoPrefix variable"); err != nil {
		return p, err
	}
	if p.RequiredRef, err = get(refRE, "required workflow ref"); err != nil {
		return p, err
	}
	for _, m := range typeRE.FindAllStringSubmatch(text, -1) {
		p.PredicateTypes = append(p.PredicateTypes, m[1])
	}
	if len(p.PredicateTypes) == 0 {
		return p, fmt.Errorf("policy: no attestation predicate types found")
	}
	return p, nil
}

// LoadPolicy reads the policy from a local file, or from warden-policy at ref when file is empty.
func LoadPolicy(file, ref string) (Policy, error) {
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return Policy{}, err
		}
		return ParsePolicy(string(b))
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(fmt.Sprintf(PolicyURL, ref))
	if err != nil {
		return Policy{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Policy{}, fmt.Errorf("fetch policy at %s: HTTP %d", ref, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Policy{}, err
	}
	return ParsePolicy(strings.ReplaceAll(string(b), "\r\n", "\n"))
}
