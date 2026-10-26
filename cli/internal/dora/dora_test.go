package dora

import (
	"math"
	"testing"
	"time"
)

func at(day, hour int) time.Time { return time.Date(2026, 11, day, hour, 0, 0, 0, time.UTC) }

func TestCompute(t *testing.T) {
	deps := []Deployment{
		{Service: "svc-books", State: "success", DeployedAt: at(2, 10), CommitAt: at(2, 9), CommitTitle: "feat: list books"},
		{Service: "svc-books", State: "failure", DeployedAt: at(3, 10), CommitAt: at(3, 8), CommitTitle: "feat: faulty release"},
		{Service: "svc-books", State: "success", DeployedAt: at(3, 11), CommitAt: at(3, 10), CommitTitle: "fix: restore error handling"},
		{Service: "svc-loans", State: "success", DeployedAt: at(4, 12), CommitAt: at(4, 8), CommitTitle: "feat: loans"},
		{Service: "svc-loans", State: "success", DeployedAt: at(30, 12), CommitAt: at(30, 11), CommitTitle: "fix: late fee"},
	}
	m := Compute(deps, at(1, 0), at(11, 0)) // 10-day window; the 30th is outside
	if m.Deployments != 4 || m.Successful != 3 || m.Failed != 1 {
		t.Fatalf("counts: %+v", m)
	}
	check := func(name string, got, want float64) {
		t.Helper()
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	check("deployments/day", m.DeploymentsPerDay, 0.3)
	check("median lead time (h)", m.MedianLeadTimeHours, 1) // leads 1h, 1h, 4h
	check("change fail rate", m.ChangeFailRate, 0.25)
	check("recovery (min)", m.MedianRecoveryTimeMinutes, 60)
	check("rework rate", m.ReworkRate, 0.25) // only the books fix within 7 days counts
}

func TestCollapse(t *testing.T) {
	deps := []Deployment{
		// one promotion announced three times (other services moved the repository HEAD in between)
		{Service: "svc-books", State: "success", DeployedAt: at(2, 12), Key: "p1"},
		{Service: "svc-books", State: "success", DeployedAt: at(2, 10), Key: "p1"},
		{Service: "svc-books", State: "success", DeployedAt: at(2, 14), Key: "p1"},
		// aborted canary: announced as a success, then failed
		{Service: "svc-books", State: "success", DeployedAt: at(3, 10), Key: "p2"},
		{Service: "svc-books", State: "failure", DeployedAt: at(3, 11), Key: "p2"},
		// same promotion key in another service is another deployment
		{Service: "svc-loans", State: "success", DeployedAt: at(2, 10), Key: "p1"},
		// no key: kept as is
		{Service: "svc-loans", State: "success", DeployedAt: at(4, 10)},
		{Service: "svc-loans", State: "success", DeployedAt: at(4, 11)},
	}
	got := Collapse(deps)
	if len(got) != 5 {
		t.Fatalf("want 5 deployments, got %d: %+v", len(got), got)
	}
	if !got[0].DeployedAt.Equal(at(2, 10)) || got[0].State != "success" {
		t.Errorf("first promotion should keep the earliest record: %+v", got[0])
	}
	if got[1].State != "failure" || !got[1].DeployedAt.Equal(at(3, 11)) {
		t.Errorf("a failure wins over the success announced before it: %+v", got[1])
	}
}

func TestEmptyWindow(t *testing.T) {
	m := Compute(nil, at(1, 0), at(2, 0))
	if m.Deployments != 0 || m.ChangeFailRate != 0 {
		t.Errorf("empty window should be all zero: %+v", m)
	}
}
