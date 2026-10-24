package verify

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// Result is one check, printed as a line of `warden verify` output.
type Result struct {
	Name   string
	OK     bool
	Audit  bool // informational: does not change the admission verdict (ADR-022)
	Detail string
}

// Runner executes cosign; replaced in tests.
type Runner func(args ...string) ([]byte, error)

// Cosign runs the cosign binary on PATH.
func Cosign(args ...string) ([]byte, error) {
	var out, errb bytes.Buffer
	cmd := exec.Command("cosign", args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if err != nil {
		return out.Bytes(), fmt.Errorf("%v: %s", err, lastLine(errb.String()))
	}
	return out.Bytes(), nil
}

var imageRefRE = regexp.MustCompile(`^[a-z0-9./_-]+@sha256:[a-f0-9]{64}$`)

// Image runs every admission check against image (name@sha256:...) and reports whether admission would accept it.
func Image(image string, p Policy, run Runner) ([]Result, bool) {
	if !imageRefRE.MatchString(image) {
		return []Result{{Name: "reference", Detail: "must be name@sha256:<64 hex> (digests, not tags)"}}, false
	}
	identity := []string{"--certificate-identity-regexp", p.SubjectRegExp, "--certificate-oidc-issuer", p.Issuer}
	var results []Result

	_, err := run(append([]string{"verify", image}, identity...)...)
	results = append(results, check("signature", err, "keyless signature by "+p.SubjectRegExp+"; Rekor inclusion proof verified"))

	for _, t := range p.PredicateTypes {
		out, err := run(append([]string{"verify-attestation", image, "--type", t}, identity...)...)
		if err != nil {
			results = append(results, Result{Name: short(t), Detail: err.Error()})
			continue
		}
		st, perr := firstStatement(out)
		if perr != nil {
			results = append(results, Result{Name: short(t), Detail: perr.Error()})
			continue
		}
		results = append(results, inspect(t, st, p)...)
	}

	vulnType := "https://cosign.sigstore.dev/attestation/vuln/v1"
	if out, err := run(append([]string{"verify-attestation", image, "--type", "vuln"}, identity...)...); err == nil {
		if st, perr := firstStatement(out); perr == nil {
			results = append(results, vulnSummary(st))
		}
	} else {
		results = append(results, Result{Name: short(vulnType), Audit: true, Detail: "no verified vulnerability attestation"})
	}

	admitted := true
	for _, r := range results {
		if !r.OK && !r.Audit {
			admitted = false
		}
	}
	return results, admitted
}

type statement struct {
	PredicateType string          `json:"predicateType"`
	Predicate     json.RawMessage `json:"predicate"`
}

func firstStatement(out []byte) (statement, error) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		var env struct {
			Payload string `json:"payload"`
		}
		if json.Unmarshal(sc.Bytes(), &env) != nil || env.Payload == "" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(env.Payload)
		if err != nil {
			return statement{}, fmt.Errorf("decode DSSE payload: %w", err)
		}
		var st statement
		if err := json.Unmarshal(raw, &st); err != nil {
			return statement{}, fmt.Errorf("parse in-toto statement: %w", err)
		}
		return st, nil
	}
	return statement{}, fmt.Errorf("no attestation payload in cosign output")
}

func inspect(predicateType string, st statement, p Policy) []Result {
	switch {
	case strings.HasPrefix(predicateType, "https://slsa.dev/provenance/"):
		var pred struct {
			BuildDefinition struct {
				ExternalParameters struct {
					Workflow struct {
						Repository string `json:"repository"`
						Ref        string `json:"ref"`
						Path       string `json:"path"`
					} `json:"workflow"`
				} `json:"externalParameters"`
			} `json:"buildDefinition"`
		}
		if err := json.Unmarshal(st.Predicate, &pred); err != nil {
			return []Result{{Name: "provenance", Detail: err.Error()}}
		}
		w := pred.BuildDefinition.ExternalParameters.Workflow
		return []Result{
			{Name: "provenance", OK: true, Detail: "SLSA v1, signed by the platform workflow"},
			{Name: "provenance repo", OK: strings.HasPrefix(w.Repository, p.RepoPrefix),
				Detail: fmt.Sprintf("%s (must start with %s)", w.Repository, p.RepoPrefix)},
			{Name: "provenance ref", OK: w.Ref == p.RequiredRef,
				Detail: fmt.Sprintf("%s (must be %s)", w.Ref, p.RequiredRef)},
		}
	case predicateType == "https://cyclonedx.org/bom":
		var pred struct {
			Components []json.RawMessage `json:"components"`
		}
		_ = json.Unmarshal(st.Predicate, &pred)
		return []Result{{Name: "sbom", OK: true, Detail: fmt.Sprintf("CycloneDX, %d components", len(pred.Components))}}
	default:
		return []Result{{Name: short(predicateType), OK: true, Detail: "attestation verified"}}
	}
}

func vulnSummary(st statement) Result {
	var pred struct {
		Scanner struct {
			Result struct {
				Results []struct {
					Vulnerabilities []struct {
						Severity string `json:"Severity"`
					} `json:"Vulnerabilities"`
				} `json:"Results"`
			} `json:"result"`
		} `json:"scanner"`
	}
	_ = json.Unmarshal(st.Predicate, &pred)
	counts := map[string]int{}
	for _, r := range pred.Scanner.Result.Results {
		for _, v := range r.Vulnerabilities {
			counts[v.Severity]++
		}
	}
	return Result{Name: "vulnerabilities", OK: counts["CRITICAL"] == 0, Audit: true,
		Detail: fmt.Sprintf("%d CRITICAL, %d HIGH (audit only, ADR-022)", counts["CRITICAL"], counts["HIGH"])}
}

func check(name string, err error, okDetail string) Result {
	if err != nil {
		return Result{Name: name, Detail: err.Error()}
	}
	return Result{Name: name, OK: true, Detail: okDetail}
}

func short(t string) string {
	switch {
	case strings.Contains(t, "slsa.dev/provenance"):
		return "provenance"
	case strings.Contains(t, "cyclonedx"):
		return "sbom"
	case strings.Contains(t, "attestation/vuln"):
		return "vulnerabilities"
	}
	return t
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
