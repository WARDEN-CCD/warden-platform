# warden-platform

The parts of Warden that developers touch: the trusted build workflow, the `warden` CLI, the service template,
and the observability that judges releases. Design: hub `DESIGN.md` §5–6, §9, §11–12.

```
.github/workflows/build-sign-attest.yml   the ONLY workflow identity admission trusts (ADR-009)
cli/                                      `warden` CLI (Go) — spec in cli/README.md
templates/spring-service/                 what `warden new` generates
observability/
  analysis/                               ClusterAnalysisTemplates for canaries (ADR-015)
  dashboards/                             Grafana dashboards (golden signals, rollouts, admission, DORA)
  rules/                                  Prometheus recording/alert rules
load/                                     k6 scripts + TestRun manifests (40 rps constant arrival)
dora/                                     metric definitions + Argo CD Notifications templates (ADR-019)
```

## Releasing the build workflow

Callers pin `@vX.Y.Z`; admission accepts only `refs/tags/v<semver>` identities. A release is:

1. PR with the workflow change + security review (CODEOWNERS).
2. Merge, then create release `vX.Y.Z` (immutable release; tag ruleset forbids moving/deleting `v*` tags).
3. Renovate bumps callers in `svc-*` repos.

Never move a tag. A bad release is fixed by a new release.
