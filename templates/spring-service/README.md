# Service template — `spring-service`

What `warden new` generates for every service. Everything here is the golden path; a service that needs to
deviate needs an ADR. Implementation: weeks 6–9.

## Files in a generated `svc-<name>` repo

| File | Purpose |
|------|---------|
| `warden.yaml` | Service metadata: `name`, `owner`, `tier`, `slo.successRate`, `slo.latencyP95Ms`, `database: true/false`. Manifests are rendered from it |
| `pom.xml`, `mvnw` | Spring Boot 4.1.x, Java 25, Actuator, Micrometer Prometheus, OpenTelemetry, Jib 3.5.x (base image pinned by digest) |
| `src/main/java/...` | One REST controller, health groups, and demo fault toggles |
| `src/main/resources/application.yaml` | Actuator endpoints, `percentiles-histogram` for `http.server.requests`, graceful shutdown |
| `.github/workflows/delivery.yml` | ~15-line caller of the platform workflow (below) |
| `catalog-info.yaml` | Backstage-compatible `Component` entity (owner, system `warden`, lifecycle) |
| `CODEOWNERS`, `renovate.json`, `README.md` | Hygiene |

Fault toggles (compiled in, so a faulty release is a real signed release — EVALUATION E3):
`WARDEN_DEMO_ERROR_RATE` (0.0–1.0) and `WARDEN_DEMO_LATENCY_MS`, read from `application.yaml` defaults that a
commit changes.

## Caller workflow (generated)

```yaml
name: delivery
on:
  push:
    branches: [main]
  pull_request:
permissions: {}
jobs:
  delivery:
    uses: WARDEN_ORG/warden-platform/.github/workflows/build-sign-attest.yml@v1.0.0
    permissions:
      contents: read
      packages: write
      id-token: write
      attestations: write
    secrets:
      WARDEN_PROMOTER_PRIVATE_KEY: ${{ secrets.WARDEN_PROMOTER_PRIVATE_KEY }}
```

The caller grants the maximum; the reusable workflow narrows permissions per job (ADR-009).

## Files added to `warden-gitops/apps/<name>/`

`namespace.yaml`, `rollout.yaml` (canary steps from DESIGN §9, Gateway API plugin, analysis
`warden-canary`), `service-stable.yaml`, `service-canary.yaml`, `httproute.yaml`, `podmonitor.yaml`,
`networkpolicy.yaml`, `hpa.yaml`, and `database.yaml` when `database: true`.
Container security context: non-root, read-only rootfs, drop ALL, seccomp `RuntimeDefault`.
