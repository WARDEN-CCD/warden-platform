package main

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/WARDEN-CCD/warden-platform/cli/internal/gh"
)

func cmdStatus(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: warden status <name>")
	}
	name, ns := args[0], "svc-"+args[0]
	app, err := gh.Run("", "kubectl", "-n", "argocd", "get", "application", ns,
		"-o", "jsonpath={.status.sync.status} {.status.health.status} {.status.sync.revision}")
	if err != nil {
		return fmt.Errorf("Argo CD application %s not found: %w", ns, err)
	}
	fmt.Printf("argo cd   %s\n", app)

	raw, err := gh.Run("", "kubectl", "-n", ns, "get", "rollout", name, "-o", "json")
	if err != nil {
		return err
	}
	var ro struct {
		Spec struct {
			Template struct {
				Spec struct {
					Containers []struct {
						Image string `json:"image"`
					} `json:"containers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
		Status struct {
			Phase            string `json:"phase"`
			Message          string `json:"message"`
			CurrentStepIndex *int   `json:"currentStepIndex"`
			StableRS         string `json:"stableRS"`
			CurrentPodHash   string `json:"currentPodHash"`
			Canary           struct {
				Weights struct {
					Canary struct {
						Weight int `json:"weight"`
					} `json:"canary"`
				} `json:"weights"`
			} `json:"canary"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(raw), &ro); err != nil {
		return err
	}
	step := "-"
	if ro.Status.CurrentStepIndex != nil {
		step = fmt.Sprint(*ro.Status.CurrentStepIndex)
	}
	fmt.Printf("rollout   %s (step %s, canary weight %d%%) %s\n", ro.Status.Phase, step, ro.Status.Canary.Weights.Canary.Weight, ro.Status.Message)
	fmt.Printf("revision  stable=%s current=%s\n", ro.Status.StableRS, ro.Status.CurrentPodHash)
	if len(ro.Spec.Template.Spec.Containers) > 0 {
		fmt.Printf("image     %s\n", ro.Spec.Template.Spec.Containers[0].Image)
	}

	runs, err := gh.Run("", "kubectl", "-n", ns, "get", "analysisruns", "-o", "json")
	if err == nil {
		var list struct {
			Items []struct {
				Metadata struct {
					Name              string `json:"name"`
					CreationTimestamp string `json:"creationTimestamp"`
				} `json:"metadata"`
				Status struct {
					Phase   string `json:"phase"`
					Message string `json:"message"`
				} `json:"status"`
			} `json:"items"`
		}
		if json.Unmarshal([]byte(runs), &list) == nil && len(list.Items) > 0 {
			sort.Slice(list.Items, func(i, j int) bool {
				return list.Items[i].Metadata.CreationTimestamp > list.Items[j].Metadata.CreationTimestamp
			})
			last := list.Items[0]
			fmt.Printf("analysis  %s %s %s\n", last.Metadata.Name, last.Status.Phase, last.Status.Message)
		}
	}
	return nil
}
