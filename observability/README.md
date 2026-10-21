# Observability

- `analysis/warden-canary.yaml` — ClusterAnalysisTemplate: sample-size guard, success rate, p95 latency (ADR-015).
- `dashboards/` — Grafana JSON, provisioned via kube-prometheus-stack sidecar (label `grafana_dashboard: "1"`):
  `service-golden-signals`, `rollouts`, `admission-decisions`, `dora`.
- `rules/` — PrometheusRules: recording rules for per-service success rate/latency; alerts for Kyverno webhook
  errors, Argo CD out-of-sync > 10 min, Rollout aborted.

Applied to clusters by an Argo CD Application in `warden-gitops/platform/observability/`.
