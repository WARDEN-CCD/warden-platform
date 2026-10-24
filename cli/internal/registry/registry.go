// Package registry talks to GHCR anonymously, the way clusters pull images (hub ADR-028).
package registry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var client = &http.Client{Timeout: 20 * time.Second}

// AnonymousDigest returns the digest of ghcr.io/<repo>:<tag> if it can be pulled without credentials, which is
// true only once the package is public. repo is lowercase, e.g. warden-ccd/svc-books.
func AnonymousDigest(repo, tag string) (string, error) {
	resp, err := client.Get("https://ghcr.io/token?scope=repository:" + repo + ":pull")
	if err != nil {
		return "", err
	}
	var tok struct {
		Token string `json:"token"`
	}
	err = json.NewDecoder(resp.Body).Decode(&tok)
	resp.Body.Close()
	if err != nil || tok.Token == "" {
		return "", fmt.Errorf("no anonymous token for %s", repo)
	}
	req, _ := http.NewRequest(http.MethodHead, "https://ghcr.io/v2/"+repo+"/manifests/"+tag, nil)
	req.Header.Set("Authorization", "Bearer "+tok.Token)
	req.Header.Set("Accept", strings.Join([]string{
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
	}, ", "))
	resp, err = client.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anonymous pull of %s:%s: HTTP %d", repo, tag, resp.StatusCode)
	}
	digest := resp.Header.Get("Docker-Content-Digest")
	if !strings.HasPrefix(digest, "sha256:") {
		return "", fmt.Errorf("registry returned no digest for %s:%s", repo, tag)
	}
	return digest, nil
}
