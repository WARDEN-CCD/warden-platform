// Package dora computes the five DORA metrics (hub DESIGN §12, ADR-019) from deployment records.
package dora

import (
	"sort"
	"strings"
	"time"
)

// Deployment is one production deployment of one service, as recorded in the GitHub Deployments ledger.
type Deployment struct {
	Service     string    // svc-books
	State       string    // success | failure
	DeployedAt  time.Time // when the final status was recorded
	CommitAt    time.Time // author time of the source commit that was deployed
	CommitTitle string    // first line of the source commit message (conventional commit)
}

// Metrics are computed over the deployments in [From, To).
type Metrics struct {
	From, To                  time.Time
	Deployments               int
	Successful                int
	Failed                    int
	DeploymentsPerDay         float64
	MedianLeadTimeHours       float64 // source commit -> successful deployment
	ChangeFailRate            float64 // failed / all
	MedianRecoveryTimeMinutes float64 // failed deployment -> next successful deployment of that service
	ReworkRate                float64 // fix:/revert: deployments within 7 days of the previous one / all
}

// Compute derives the metrics. Deployments may be in any order.
func Compute(deps []Deployment, from, to time.Time) Metrics {
	m := Metrics{From: from, To: to}
	var in []Deployment
	for _, d := range deps {
		if !d.DeployedAt.Before(from) && d.DeployedAt.Before(to) {
			in = append(in, d)
		}
	}
	sort.Slice(in, func(i, j int) bool { return in[i].DeployedAt.Before(in[j].DeployedAt) })
	m.Deployments = len(in)
	if m.Deployments == 0 {
		return m
	}

	var leads, recoveries []float64
	lastBy := map[string]time.Time{}
	failedSince := map[string]time.Time{}
	rework := 0
	for _, d := range in {
		prev, seen := lastBy[d.Service]
		if seen && d.DeployedAt.Sub(prev) <= 7*24*time.Hour && isRework(d.CommitTitle) {
			rework++
		}
		lastBy[d.Service] = d.DeployedAt
		switch d.State {
		case "success":
			m.Successful++
			if !d.CommitAt.IsZero() {
				leads = append(leads, d.DeployedAt.Sub(d.CommitAt).Hours())
			}
			if f, ok := failedSince[d.Service]; ok {
				recoveries = append(recoveries, d.DeployedAt.Sub(f).Minutes())
				delete(failedSince, d.Service)
			}
		case "failure":
			m.Failed++
			if _, ok := failedSince[d.Service]; !ok {
				failedSince[d.Service] = d.DeployedAt
			}
		}
	}
	days := to.Sub(from).Hours() / 24
	if days > 0 {
		m.DeploymentsPerDay = float64(m.Successful) / days
	}
	m.MedianLeadTimeHours = median(leads)
	m.ChangeFailRate = float64(m.Failed) / float64(m.Deployments)
	m.MedianRecoveryTimeMinutes = median(recoveries)
	m.ReworkRate = float64(rework) / float64(m.Deployments)
	return m
}

func isRework(title string) bool {
	t := strings.ToLower(strings.TrimSpace(title))
	return strings.HasPrefix(t, "fix") || strings.HasPrefix(t, "revert")
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}
