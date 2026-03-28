package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"

	"github.com/kevin/supabase-control-plane/internal/domain"
	"github.com/kevin/supabase-control-plane/internal/usecase"
)

// buildMCPCmd builds the "mcp" command group.
func buildMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP (Model Context Protocol) server commands",
	}
	// Prevent cobra startup messages from polluting the JSON-RPC stdout stream.
	cmd.SetOut(os.Stderr)
	cmd.AddCommand(buildMCPServeCmd())
	return cmd
}

// buildMCPServeCmd builds the "mcp serve" subcommand.
// Unlike the project subcommands, MCP serve initialises its own deps because
// it runs as a long-lived process (stdio server) and must NOT go through the
// root PersistentPreRunE / ExitError flow.
func buildMCPServeCmd() *cobra.Command {
	var (
		dbURL       string
		projectsDir string
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the MCP server (stdio transport)",
		// SilenceErrors and SilenceUsage are set on root; we rely on that.
		// RunE writes errors to stderr (not stdout) to keep the JSON-RPC stream clean.
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dbURL == "" {
				err := fmt.Errorf("--db-url or SBCTL_DB_URL is required")
				fmt.Fprintln(os.Stderr, "Error:", err)
				return &ExitError{Code: 1, Err: err}
			}

			deps, err := BuildDeps(cmd.Context(), dbURL, projectsDir)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
				return &ExitError{Code: 2, Err: err}
			}

			s := buildMCPServer(deps)
			// ServeStdio blocks until the client disconnects or SIGTERM/SIGINT.
			// The SDK registers signal handlers internally — do NOT wrap with
			// signal.NotifyContext, as that causes a goroutine race on double registration.
			errLogger := log.New(os.Stderr, "[mcp] ", log.LstdFlags)
			if err := server.ServeStdio(s,
				server.WithErrorLogger(errLogger),
			); err != nil {
				fmt.Fprintln(os.Stderr, "mcp server error:", err)
				return &ExitError{Code: 2, Err: err}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dbURL, "db-url",
		envOr("SBCTL_DB_URL", ""),
		"PostgreSQL DSN (env: SBCTL_DB_URL)")
	cmd.Flags().StringVar(&projectsDir, "projects-dir",
		envOr("SBCTL_PROJECTS_DIR", "./projects"),
		"Root directory for project files (env: SBCTL_PROJECTS_DIR)")
	// Prevent cobra from polluting the JSON-RPC stdout stream.
	cmd.SetOut(os.Stderr)
	return cmd
}

// buildMCPServer registers all 7 MCP tools on a new MCPServer instance.
func buildMCPServer(deps *Deps) *server.MCPServer {
	s := server.NewMCPServer("sbctl", version)

	s.AddTool(
		mcp.NewTool("list_projects",
			mcp.WithDescription("List all active Supabase projects."),
		),
		makeMCPListProjects(deps),
	)

	s.AddTool(
		mcp.NewTool("get_project",
			mcp.WithDescription("Get details of a Supabase project including config and health."),
			mcp.WithString("slug",
				mcp.Required(),
				mcp.Description("Project slug identifier"),
			),
		),
		makeMCPGetProject(deps),
	)

	s.AddTool(
		mcp.NewTool("create_project",
			mcp.WithDescription("Create and start a new Supabase project."),
			mcp.WithString("slug",
				mcp.Required(),
				mcp.Description("Unique project identifier (lowercase, hyphens allowed)"),
			),
			mcp.WithString("display_name",
				mcp.Required(),
				mcp.Description("Human-readable project name"),
			),
			mcp.WithString("runtime",
				mcp.Description("Runtime type: docker-compose (default) or kubernetes"),
			),
		),
		makeMCPCreateProject(deps),
	)

	s.AddTool(
		mcp.NewTool("start_project",
			mcp.WithDescription("Start a stopped Supabase project."),
			mcp.WithString("slug",
				mcp.Required(),
				mcp.Description("Project slug identifier"),
			),
		),
		makeMCPStartProject(deps),
	)

	s.AddTool(
		mcp.NewTool("stop_project",
			mcp.WithDescription("Stop a running Supabase project."),
			mcp.WithString("slug",
				mcp.Required(),
				mcp.Description("Project slug identifier"),
			),
		),
		makeMCPStopProject(deps),
	)

	s.AddTool(
		mcp.NewTool("reset_project",
			mcp.WithDescription("Reset a Supabase project: wipes all data and re-provisions."),
			mcp.WithString("slug",
				mcp.Required(),
				mcp.Description("Project slug identifier"),
			),
		),
		makeMCPResetProject(deps),
	)

	s.AddTool(
		mcp.NewTool("delete_project",
			mcp.WithDescription("Permanently delete a Supabase project and destroy all its data. This action is irreversible."),
			mcp.WithString("slug",
				mcp.Required(),
				mcp.Description("Project slug identifier"),
			),
		),
		makeMCPDeleteProject(deps),
	)

	return s
}

// --- tool handlers ---

func makeMCPListProjects(deps *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		views, err := deps.ProjectService.List(ctx)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if views == nil {
			views = []*usecase.ProjectView{}
		}
		return jsonResult(views)
	}
}

func makeMCPGetProject(deps *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		slug, err := req.RequireString("slug")
		if err != nil {
			return mcp.NewToolResultError("slug is required"), nil
		}
		view, err := deps.ProjectService.Get(ctx, slug)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return jsonResult(view)
	}
}

func makeMCPCreateProject(deps *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		slug, err := req.RequireString("slug")
		if err != nil {
			return mcp.NewToolResultError("slug is required"), nil
		}
		displayName, err := req.RequireString("display_name")
		if err != nil {
			return mcp.NewToolResultError("display_name is required"), nil
		}
		rtStr, _ := req.RequireString("runtime")
		if rtStr == "" {
			rtStr = string(domain.RuntimeDockerCompose)
		}
		rt := domain.RuntimeType(rtStr)
		if err := domain.ValidateRuntimeType(rt); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		view, err := deps.ProjectService.Create(ctx, slug, displayName, rt)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return jsonResult(view)
	}
}

func makeMCPStartProject(deps *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		slug, err := req.RequireString("slug")
		if err != nil {
			return mcp.NewToolResultError("slug is required"), nil
		}
		view, err := deps.ProjectService.Start(ctx, slug)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return jsonResult(view)
	}
}

func makeMCPStopProject(deps *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		slug, err := req.RequireString("slug")
		if err != nil {
			return mcp.NewToolResultError("slug is required"), nil
		}
		view, err := deps.ProjectService.Stop(ctx, slug)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return jsonResult(view)
	}
}

func makeMCPResetProject(deps *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		slug, err := req.RequireString("slug")
		if err != nil {
			return mcp.NewToolResultError("slug is required"), nil
		}
		view, err := deps.ProjectService.Reset(ctx, slug)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return jsonResult(view)
	}
}

func makeMCPDeleteProject(deps *Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		slug, err := req.RequireString("slug")
		if err != nil {
			return mcp.NewToolResultError("slug is required"), nil
		}
		_, err = deps.ProjectService.Delete(ctx, slug)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		result := map[string]interface{}{"deleted": true, "slug": slug}
		return jsonResult(result)
	}
}

// jsonResult marshals v to JSON and wraps it in a text tool result.
func jsonResult(v interface{}) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	return mcp.NewToolResultText(string(b)), nil
}


