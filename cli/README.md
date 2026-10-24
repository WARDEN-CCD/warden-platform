# `warden` CLI

Go 1.27, **standard library only**: no third-party modules, so the CLI adds nothing to Warden's own supply
chain. It drives `gh`, `git`, `cosign` and `kubectl`, which hub `scripts/doctor.py` already requires. Templates
are embedded from `../templates` with `go:embed`. Decision: hub ADR-017.

```bash
go build -o warden ./cli/cmd/warden      # from the warden-platform root
go test ./...
```

## Commands

| Command | What it does |
|---------|--------------|
| `warden new <name> [--owner TEAM] [--platform-ref vX.Y.Z] [--no-merge] [--dry-run]` | Renders the template, creates public `WARDEN-CCD/svc-<name>`, applies rulesets, waits for the first signed build, waits for the image to be made public (one manual click, ADR-028), opens the `apps/<name>/` PR in `warden-gitops` and merges it once `digest-only` passes, then waits for Argo CD and the Rollout. Writes per-step timings to the hub's `evidence/scaffold-timings/` (E1). |
| `warden render <name> --out DIR [--digest sha256:…]` | Renders the service repo (and, with a digest, its gitops directory) without touching GitHub. |
| `warden verify <image@sha256:…> [--policy-ref REF \| --policy-file F]` | Runs admission's checks locally with cosign: signature + Rekor, provenance signer, provenance repo prefix and ref, SBOM, and vulnerabilities (audit). Constants are parsed from `warden-policy`'s ImageValidatingPolicy, so the CLI and the cluster cannot drift. Exit 1 if admission would deny. |
| `warden status <name>` | Argo CD state, Rollout phase/step/canary weight, current image, latest AnalysisRun. |
| `warden dora [--since YYYY-MM-DD] [--json]` | DORA five metrics from the GitHub Deployments ledger on `warden-gitops` (ADR-019). |
| `warden setup-apps` | Creates the `warden-ccd-promoter` and `warden-ccd-deployments` GitHub Apps through GitHub's manifest flow (two clicks each), saves keys to `~/.warden/`, sets the promoter org secret/variable, enables auto-merge and adds the promoter to `warden-gitops`' review bypass (PRs only). |

Example `verify` output:

```
✔ signature         keyless signature by ^https://github\.com/WARDEN-CCD/warden-platform/…; Rekor inclusion proof verified
✔ provenance        SLSA v1, signed by the platform workflow
✔ provenance repo   https://github.com/WARDEN-CCD/svc-books (must start with https://github.com/WARDEN-CCD/svc-)
✔ provenance ref    refs/heads/main (must be refs/heads/main)
✔ sbom              CycloneDX, 143 components
! vulnerabilities   0 CRITICAL, 2 HIGH (audit only, ADR-022)
→ would be ADMITTED
```

## Layout

```
cli/cmd/warden/        commands (new, render, verify, status, dora, setup-apps)
cli/internal/render/   template rendering ([[ ]] delimiters; GitHub's ${{ }} is left alone)
cli/internal/verify/   policy parsing + cosign-based verification
cli/internal/dora/     metric computation (pure, unit-tested)
cli/internal/gh/       gh/git subprocess helpers
cli/internal/registry/ anonymous GHCR access (how clusters pull)
```
