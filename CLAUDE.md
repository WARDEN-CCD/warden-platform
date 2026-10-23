# CLAUDE.md — warden-platform

Trusted build workflow, `warden` CLI (Go), service template, observability. Workspace rules: `../CLAUDE.md` (hub).

## Workflow rules (this repo's workflows are a security boundary)

- `build-sign-attest.yml` is the identity admission trusts. Changes need security review and a new semver release.
- `permissions: {}` at the top; per-job grants. Only `sign-attest` gets `id-token: write`, and it must never
  check out or execute repository code.
- Third-party actions pinned by full SHA + `# vX.Y.Z` comment, matching the hub's `versions.yaml` `ci.actions`
  (`scripts/verify.py` enforces both). Resolve SHAs with `gh api`, never guess.
- Untrusted values (`github.event.*`, branch names) reach shell only via `env:`, never `${{ }}` inside `run:`.
- Security tools are installed from release binaries with cosign-verified checksums, not wrapper actions.
- Push/sign/promote only on `push` to `refs/heads/main`.
- Run zizmor on any workflow change.

## Local build rules

- Taskfiles load `dotenv: ['{{.USERPROFILE}}/.warden/env', '{{.HOME}}/.warden/env']` and prefix every Maven
  command with `{{.WITH_JDK}}` (defined as in the hub Taskfile). Task cannot override an OS `JAVA_HOME` via
  `env:`, and team members keep other JDKs globally. See hub CONTRIBUTING "Local setup".

## CLI rules

- Go module under `cli/`; `go test ./...` must pass; no network calls in unit tests (use fakes).
- `warden verify` must mirror the checks in `warden-policy/policies/supply-chain/` — read constants from there,
  do not duplicate them.

## Template rules

- Generated manifests reference images by digest only; Rollouts embed `template` (no `workloadRef`).
- Gateway API `HTTPRoute`, never Ingress.

Files headed `DRAFT` have not been run yet.
