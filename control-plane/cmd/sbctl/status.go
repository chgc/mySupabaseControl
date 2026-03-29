package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/kevin/supabase-control-plane/internal/usecase"
	"github.com/spf13/cobra"
)

// statusOrder defines the fixed display order for status counts.
var statusOrder = []string{
	"running", "starting", "creating", "stopping",
	"stopped", "destroying", "destroyed", "error",
}

// StatusOverview is the structured representation of system-wide project status.
type StatusOverview struct {
	Total    int              `json:"total"    yaml:"total"`
	Summary  map[string]int   `json:"summary"  yaml:"summary"`
	Projects []ProjectSummary `json:"projects" yaml:"projects"`
	Alerts   []string         `json:"alerts,omitempty" yaml:"alerts,omitempty"`
}

// ProjectSummary is a condensed view of a project for the status overview.
type ProjectSummary struct {
	Slug        string `json:"slug"         yaml:"slug"`
	DisplayName string `json:"display_name" yaml:"display_name"`
	Status      string `json:"status"       yaml:"status"`
	UpdatedAt   string `json:"updated_at"   yaml:"updated_at"`
}

func buildStatusCmd(deps **Deps, output *string, colorOut **colorer) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show system-wide project status overview",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			views, err := (*deps).ProjectService.List(cmd.Context())
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Error: %v\n", err)
				return &ExitError{Code: 2, Err: err}
			}
			return writeStatusOverview(cmd.OutOrStdout(), *output, views, *colorOut)
		},
	}
}

func writeStatusOverview(w io.Writer, output string, views []*usecase.ProjectView, c *colorer) error {
	overview := buildStatusOverview(views)

	switch output {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(overview)
	case "yaml":
		return yaml.NewEncoder(w).Encode(overview)
	default: // table
		return writeStatusTable(w, views, overview, c)
	}
}

func buildStatusOverview(views []*usecase.ProjectView) StatusOverview {
	overview := StatusOverview{
		Total:   len(views),
		Summary: make(map[string]int),
	}
	for _, v := range views {
		overview.Summary[v.Status]++
		ps := ProjectSummary{
			Slug:        v.Slug,
			DisplayName: v.DisplayName,
			Status:      v.Status,
			UpdatedAt:   v.UpdatedAt.UTC().Format(time.RFC3339),
		}
		overview.Projects = append(overview.Projects, ps)
		if v.Status == "error" {
			alert := fmt.Sprintf("%s is in error state (updated: %s)", v.Slug, ps.UpdatedAt)
			overview.Alerts = append(overview.Alerts, alert)
		}
	}
	return overview
}

func writeStatusTable(w io.Writer, views []*usecase.ProjectView, overview StatusOverview, c *colorer) error {
	if overview.Total == 0 {
		fmt.Fprintln(w, "No projects found. Run 'sbctl project create <slug>' to get started.")
		return nil
	}

	// Summary line with counts in fixed order.
	var parts []string
	for _, s := range statusOrder {
		if n, ok := overview.Summary[s]; ok && n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, s))
		}
	}
	// Include any statuses not in statusOrder.
	for status, n := range overview.Summary {
		found := false
		for _, s := range statusOrder {
			if s == status {
				found = true
				break
			}
		}
		if !found && n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, status))
		}
	}
	fmt.Fprintf(w, "%d projects: %s\n", overview.Total, strings.Join(parts, ", "))

	// Alert section for error-state projects.
	if len(overview.Alerts) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "⚠ Projects needing attention:")
		for _, v := range views {
			if v.Status == "error" {
				fmt.Fprintf(w, "  %s\t%s\t%s\n", v.Slug, c.status("error"), v.UpdatedAt.UTC().Format(time.RFC3339))
			}
		}
	}

	// Project table.
	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SLUG\tSTATUS\tUPDATED")
	for _, v := range views {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", v.Slug, c.status(v.Status), v.UpdatedAt.UTC().Format(time.RFC3339))
	}
	return tw.Flush()
}
