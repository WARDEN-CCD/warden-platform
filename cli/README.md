# `warden` CLI — specification

Go (`go1.27.x`), cobra, go-github, sigstore-go. Templates embedded with `go:embed`. Released with GoReleaser
**through Warden's own build-sign-attest pipeline**, so `warden verify` can verify `warden` itself.
Decision: hub ADR-017. Implementation: weeks 8–9.

## Commands

### `warden new <name> [--db] [--owner <team>] [--dry-run]`

1. Validate `<name>` (DNS label, ≤ 30 chars, not taken).
2. Render `templates/spring-service` with `warden.yaml` values; create `WARDEN-CCD/svc-<name>` (public),
   push the initial commit.
3. Apply repo settings: branch ruleset on `main` (PR + 1 review + checks), Actions allowed-list, secret
   `WARDEN_PROMOTER_PRIVATE_KEY` + variable `WARDEN_PROMOTER_APP_ID`.
4. **Make the image public (hub ADR-028).** GitHub has no API for package visibility, so after the first push
   the CLI prints and opens `https://github.com/orgs/WARDEN-CCD/packages/container/svc-<name>/settings`, then
   polls an *anonymous* manifest fetch of the new digest until it succeeds. The wait is recorded as
   `manual_steps: 1` in the timing JSON.
5. Open a PR in `warden-gitops` adding `apps/<name>/` (namespace, Rollout with placeholder digest, Services,
   HTTPRoute, PodMonitor, `Database` if `--db`, NetworkPolicy, HPA).
6. Wait for: first CI run green → bot digest PR merged → Argo CD Synced → Rollout Healthy.
7. Print a timing breakdown; append it as JSON to `evidence/scaffold-timings/` in the hub (E1).

### `warden verify <image@sha256:…> [--policy-ref <tag>]`

Runs the same checks as admission, locally, and explains each one:

```
✔ signature        keyless, subject …/build-sign-attest.yml@refs/tags/v1.2.0, issuer token.actions…
✔ transparency log  Rekor inclusion proof present
✔ provenance        SLSA v1, repo WARDEN-CCD/svc-books, ref refs/heads/main, commit 3f2a…
✔ sbom              CycloneDX, 143 components
! vulnerabilities  2 HIGH, 0 CRITICAL (audit only)
→ would be ADMITTED
```

Constants (signer regexp, repo prefix, required predicate types) are read from `warden-policy` at `--policy-ref`
so the CLI and the cluster cannot drift.

### `warden doctor`

Local tooling vs the hub's `versions.yaml`; Docker memory; kube context; GitHub auth scopes.

### `warden status <name>`

Rollout phase and step, current/stable digests, last AnalysisRun result, last admission decision for the service.

### `warden dora [--since 2026-11-01] [--service <name>] [--json]`

Reads GitHub Deployments + statuses for every `svc-*` repo and prints the five DORA metrics (hub DESIGN §12).
`--json` output is what goes into `evidence/dora/`.

## Layout (planned)

```
cli/
  cmd/warden/main.go
  internal/scaffold/   render + GitHub API
  internal/verify/     sigstore-go verification mirroring the IVP
  internal/dora/       GitHub Deployments → metrics
  internal/doctor/
  templates/           go:embed of ../templates/spring-service at build time
```
