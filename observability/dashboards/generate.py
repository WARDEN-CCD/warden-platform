"""Generate Warden's Grafana dashboards as ConfigMaps (picked up by the kube-prometheus-stack Grafana sidecar).

  python observability/dashboards/generate.py     # rewrites observability/dashboards/dashboards.yaml

Panels are described compactly below; the generator expands them into Grafana JSON. Stdlib only.
"""

import json
from pathlib import Path

DS = {"type": "prometheus", "uid": "prometheus"}
OUT = Path(__file__).with_name("dashboards.yaml")


def ts(title, exprs, unit="short", x=0, y=0, w=12, h=8, legend="{{rollouts_pod_template_hash}}"):
    """Time-series panel; exprs is a list of (expr, legend) or plain exprs."""
    targets = []
    for i, e in enumerate(exprs):
        expr, leg = (e if isinstance(e, tuple) else (e, legend))
        targets.append({"datasource": DS, "expr": expr, "legendFormat": leg, "refId": chr(65 + i)})
    return {"type": "timeseries", "title": title, "datasource": DS, "targets": targets,
            "gridPos": {"x": x, "y": y, "w": w, "h": h},
            "fieldConfig": {"defaults": {"unit": unit}, "overrides": []}}


def stat(title, expr, unit="short", x=0, y=0, w=6, h=4):
    return {"type": "stat", "title": title, "datasource": DS, "gridPos": {"x": x, "y": y, "w": w, "h": h},
            "targets": [{"datasource": DS, "expr": expr, "refId": "A", "instant": True}],
            "fieldConfig": {"defaults": {"unit": unit}, "overrides": []}}


def table(title, expr, x=0, y=0, w=24, h=8):
    return {"type": "table", "title": title, "datasource": DS, "gridPos": {"x": x, "y": y, "w": w, "h": h},
            "targets": [{"datasource": DS, "expr": expr, "refId": "A", "instant": True, "format": "table"}]}


def dashboard(uid, title, panels, variables=()):
    for i, p in enumerate(panels, 1):
        p["id"] = i
    return {"uid": uid, "title": title, "tags": ["warden"], "timezone": "browser", "schemaVersion": 39,
            "refresh": "30s", "time": {"from": "now-1h", "to": "now"}, "panels": panels,
            "templating": {"list": list(variables)}}


svc_var = {"name": "service", "type": "query", "datasource": DS, "refresh": 2,
           "query": {"query": 'label_values(http_server_requests_seconds_count{namespace=~"svc-.*"}, namespace)',
                     "refId": "v"},
           "current": {}, "includeAll": False}
SEL = 'namespace="$service", uri=~"/api/.*"'

services = dashboard("warden-services", "Warden / Services (golden signals by revision)", [
    ts("Requests per second by revision", [f"sum by (rollouts_pod_template_hash) (rate(http_server_requests_seconds_count{{{SEL}}}[1m]))"],
       "reqps", 0, 0),
    ts("Error ratio by revision (5xx / all)", [
        f'sum by (rollouts_pod_template_hash) (rate(http_server_requests_seconds_count{{{SEL}, status=~"5.."}}[1m]))'
        f" / sum by (rollouts_pod_template_hash) (rate(http_server_requests_seconds_count{{{SEL}}}[1m]))"],
       "percentunit", 12, 0),
    ts("p95 latency by revision", [
        f"histogram_quantile(0.95, sum by (le, rollouts_pod_template_hash) (rate(http_server_requests_seconds_bucket{{{SEL}}}[1m])))"],
       "s", 0, 8),
    ts("Ready pods", [('sum(kube_pod_status_ready{namespace="$service", condition="true"})', "ready")], "short", 12, 8),
], [svc_var])

rollouts = dashboard("warden-rollouts", "Warden / Rollouts and canary analysis", [
    table("Rollouts (phase per service)", 'rollout_info{namespace=~"svc-.*"}', 0, 0, 24, 7),
    ts("Canary traffic share (requests to the newest revision)", [(
        'sum by (namespace) (rate(http_server_requests_seconds_count{namespace=~"svc-.*", uri=~"/api/.*"}[1m]))',
        "{{namespace}} total")], "reqps", 0, 7),
    ts("AnalysisRuns by phase", [("sum by (phase) (analysis_run_info)", "{{phase}}")], "short", 12, 7),
    ts("Analysis metric results", [("sum by (namespace, metric, phase) (analysis_run_metric_phase)",
                                    "{{namespace}} {{metric}} {{phase}}")], "short", 0, 15, 24),
])

admission = dashboard("warden-admission", "Warden / Admission (verification cost and decisions)", [
    ts("Kyverno admission latency p50 / p95 (E8)", [
        ("histogram_quantile(0.5, sum by (le) (rate(kyverno_admission_review_duration_seconds_bucket[5m])))", "p50"),
        ("histogram_quantile(0.95, sum by (le) (rate(kyverno_admission_review_duration_seconds_bucket[5m])))", "p95")],
       "s", 0, 0),
    ts("Webhook latency seen by the API server (p95)", [(
        "histogram_quantile(0.95, sum by (le, name) (rate(apiserver_admission_webhook_admission_duration_seconds_bucket{name=~\".*kyverno.*\"}[5m])))",
        "{{name}}")], "s", 12, 0),
    ts("Kyverno policy results (fail) by policy", [(
        'sum by (policy_name) (increase(kyverno_policy_results_total{rule_result="fail"}[10m]))', "{{policy_name}}")],
       "short", 0, 8),
    ts("ValidatingAdmissionPolicy checks by policy and action", [(
        "sum by (policy, enforcement_action) (increase(apiserver_validating_admission_policy_check_total[10m]))",
        "{{policy}} {{enforcement_action}}")], "short", 12, 8),
    stat("Webhook rejections (last 1h)", 'sum(increase(apiserver_admission_webhook_rejection_count[1h]))', "short", 0, 16),
])


def configmap(name, dash):
    body = json.dumps(dash, indent=2)
    indented = "\n".join("    " + line for line in body.splitlines())
    return (f"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: {name}\n  namespace: monitoring\n"
            f"  labels:\n    grafana_dashboard: \"1\"\ndata:\n  {name}.json: |\n{indented}\n")


def main():
    header = "# Generated by observability/dashboards/generate.py; edit the generator, not this file.\n"
    docs = [configmap("warden-services", services), configmap("warden-rollouts", rollouts),
            configmap("warden-admission", admission)]
    OUT.write_text(header + "---\n".join(docs), encoding="utf-8", newline="\n")
    print(f"wrote {OUT} ({len(docs)} dashboards)")


if __name__ == "__main__":
    main()
