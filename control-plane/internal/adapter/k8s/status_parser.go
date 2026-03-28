package k8s

import (
	"encoding/json"
	"time"

	"github.com/kevin/supabase-control-plane/internal/domain"
)

type podList struct {
	Items []podItem `json:"items"`
}

type podItem struct {
	Metadata podMetadata `json:"metadata"`
	Status   podStatus   `json:"status"`
}

type podMetadata struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels"`
}

type podStatus struct {
	Phase      string         `json:"phase"`
	Conditions []podCondition `json:"conditions"`
}

type podCondition struct {
	Type   string `json:"type"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// componentToService maps Helm chart component labels to domain service names.
var componentToService = map[string]domain.ServiceName{
	"supabase-analytics": domain.ServiceAnalytics,
	"supabase-auth":      domain.ServiceAuth,
	"supabase-db":        domain.ServiceDB,
	"supabase-functions": domain.ServiceFunctions,
	"supabase-imgproxy":  domain.ServiceImgProxy,
	"supabase-kong":      domain.ServiceKong,
	"supabase-meta":      domain.ServiceMeta,
	"supabase-realtime":  domain.ServiceRealtime,
	"supabase-rest":      domain.ServiceRest,
	"supabase-storage":   domain.ServiceStorage,
	"supabase-studio":    domain.ServiceStudio,
	"supabase-vector":    domain.ServiceVector,
}

// parseK8sPods converts kubectl JSON output into ProjectHealth.
func parseK8sPods(output []byte) *domain.ProjectHealth {
	now := time.Now()
	health := &domain.ProjectHealth{
		Services:  make(map[domain.ServiceName]domain.ServiceHealth),
		CheckedAt: now,
	}

	var pods podList
	if err := json.Unmarshal(output, &pods); err != nil {
		return health
	}

	for _, pod := range pods.Items {
		label := pod.Metadata.Labels["app.kubernetes.io/name"]
		svcName, ok := componentToService[label]
		if !ok {
			continue
		}

		status := mapPodStatus(pod.Status)
		health.Services[svcName] = domain.ServiceHealth{
			Status:    status,
			Message:   pod.Metadata.Name,
			CheckedAt: now,
		}
	}

	return health
}

func mapPodStatus(s podStatus) domain.ServiceStatus {
	switch s.Phase {
	case "Running":
		return mapRunningPod(s.Conditions)
	case "Pending":
		return domain.ServiceStatusStarting
	case "Succeeded":
		return domain.ServiceStatusStopped
	case "Failed":
		return domain.ServiceStatusUnhealthy
	default:
		return domain.ServiceStatusUnknown
	}
}

func mapRunningPod(conditions []podCondition) domain.ServiceStatus {
	for _, c := range conditions {
		if c.Type == "Ready" {
			switch c.Status {
			case "True":
				return domain.ServiceStatusHealthy
			case "False":
				if c.Reason == "ContainersNotReady" {
					return domain.ServiceStatusStarting
				}
				return domain.ServiceStatusUnhealthy
			default:
				return domain.ServiceStatusUnknown
			}
		}
	}
	return domain.ServiceStatusUnknown
}
