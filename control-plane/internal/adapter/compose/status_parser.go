package compose

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/kevin/supabase-control-plane/internal/domain"
)

// composePSEntry is a single line of `docker compose ps --format json` NDJSON output.
type composePSEntry struct {
	Service string `json:"Service"`
	State   string `json:"State"`
	Health  string `json:"Health"`
}

// parseComposePS parses NDJSON output from `docker compose ps --format json`
// into a ProjectHealth snapshot.
//
// Mapping rules:
//   - State="running" + Health="healthy"   → ServiceStatusHealthy
//   - State="running" + Health="starting"  → ServiceStatusStarting
//   - State="running" + Health="unhealthy" → ServiceStatusUnhealthy
//   - State="running" + Health=""          → ServiceStatusHealthy (no healthcheck)
//   - State="exited"                       → ServiceStatusStopped
//   - anything else                        → ServiceStatusUnknown
//
// Malformed JSON lines are silently skipped.
// Empty output returns a ProjectHealth with an empty Services map.
func parseComposePS(output []byte) *domain.ProjectHealth {
	health := &domain.ProjectHealth{
		Services:  make(map[domain.ServiceName]domain.ServiceHealth),
		CheckedAt: time.Now().UTC(),
	}

	for _, line := range bytes.Split(output, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		var entry composePSEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			// Malformed line — skip silently.
			continue
		}

		svcName := domain.ServiceName(entry.Service)
		svc := domain.ServiceHealth{CheckedAt: health.CheckedAt}

		switch {
		case entry.State == "running" && entry.Health == "healthy":
			svc.Status = domain.ServiceStatusHealthy
		case entry.State == "running" && entry.Health == "starting":
			svc.Status = domain.ServiceStatusStarting
		case entry.State == "running" && entry.Health == "unhealthy":
			svc.Status = domain.ServiceStatusUnhealthy
		case entry.State == "running" && entry.Health == "restarting":
			svc.Status = domain.ServiceStatusUnhealthy
		case entry.State == "running":
			// No healthcheck configured — treat as healthy.
			svc.Status = domain.ServiceStatusHealthy
		case entry.State == "exited":
			svc.Status = domain.ServiceStatusStopped
		default:
			svc.Status = domain.ServiceStatusUnknown
		}

		health.Services[svcName] = svc
	}

	return health
}
