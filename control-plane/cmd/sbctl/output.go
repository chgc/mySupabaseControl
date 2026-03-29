package main

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/kevin/supabase-control-plane/internal/usecase"
)

func writeProjectView(w io.Writer, output string, view *usecase.ProjectView, c *colorer) error {
	switch output {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(view)
	case "yaml":
		return yaml.NewEncoder(w).Encode(view)
	default: // table
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "SLUG\tDISPLAY NAME\tSTATUS\tUPDATED")
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			view.Slug, view.DisplayName, c.status(string(view.Status)),
			view.UpdatedAt.UTC().Format(time.RFC3339))
		if err := tw.Flush(); err != nil {
			return err
		}
		if view.URLs != nil {
			fmt.Fprintln(w)
			fmt.Fprintln(w, "URLs:")
			fmt.Fprintf(w, "  API      %s\n", view.URLs.API)
			fmt.Fprintf(w, "  Studio   %s\n", view.URLs.Studio)
			if view.URLs.Inbucket != "" {
				fmt.Fprintf(w, "  Inbucket %s\n", view.URLs.Inbucket)
			}
		}
		return nil
	}
}

func writeProjectViews(w io.Writer, output string, views []*usecase.ProjectView, c *colorer) error {
	switch output {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if views == nil {
			views = []*usecase.ProjectView{}
		}
		return enc.Encode(views)
	case "yaml":
		if views == nil {
			views = []*usecase.ProjectView{}
		}
		return yaml.NewEncoder(w).Encode(views)
	default: // table
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "SLUG\tDISPLAY NAME\tSTATUS\tUPDATED")
		for _, v := range views {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
				v.Slug, v.DisplayName, c.status(string(v.Status)),
				v.UpdatedAt.UTC().Format(time.RFC3339))
		}
		return tw.Flush()
	}
}

type deleteResult struct {
	Deleted bool   `json:"deleted" yaml:"deleted"`
	Slug    string `json:"slug"    yaml:"slug"`
}

func writeDeleteResult(w io.Writer, output string, slug string) error {
	switch output {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(deleteResult{Deleted: true, Slug: slug})
	case "yaml":
		return yaml.NewEncoder(w).Encode(deleteResult{Deleted: true, Slug: slug})
	default: // table
		fmt.Fprintf(w, "Deleted project %q.\n", slug)
		return nil
	}
}

func writeCreateSummary(w io.Writer, creds *usecase.CredentialsView) error {
	fmt.Fprintln(w, "\nConnection Info:")
	fmt.Fprintf(w, "  API URL           %s\n", creds.APIURL)
	fmt.Fprintf(w, "  Anon Key          %s\n", creds.AnonKey)
	fmt.Fprintf(w, "  DB Host           %s\n", creds.PostgresHost)
	fmt.Fprintf(w, "  DB Port           %s\n", creds.PostgresPort)
	fmt.Fprintf(w, "  DB Password       %s\n", creds.PostgresPassword)
	fmt.Fprintf(w, "\n  Run 'sbctl project credentials %s' for full credentials.\n", creds.Slug)
	return nil
}

func writeCredentialsView(w io.Writer, output string, cv *usecase.CredentialsView) error {
	switch output {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(cv)
	case "yaml":
		return yaml.NewEncoder(w).Encode(cv)
	default: // table
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		rows := []struct{ k, v string }{
			{"Studio URL", cv.StudioURL},
			{"Dashboard Username", cv.DashboardUsername},
			{"Dashboard Password", cv.DashboardPassword},
			{"API URL", cv.APIURL},
			{"Anon Key", cv.AnonKey},
			{"Service Role Key", cv.ServiceRoleKey},
			{"Postgres Host", cv.PostgresHost},
			{"Postgres Port", cv.PostgresPort},
			{"Postgres DB", cv.PostgresDB},
			{"Postgres Password", cv.PostgresPassword},
			{"Pooler Port", cv.PoolerPort},
		}
		for _, r := range rows {
			if r.v != "" {
				fmt.Fprintf(tw, "%s:\t%s\n", r.k, r.v)
			}
		}
		return tw.Flush()
	}
}
