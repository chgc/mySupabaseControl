package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kevin/supabase-control-plane/internal/usecase"
)

func makeToolRequest(args map[string]interface{}) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: args,
		},
	}
}

func TestMCPListProjects_Empty(t *testing.T) {
	deps := &Deps{ProjectService: &mockSvc{
		ListFn: func(_ context.Context) ([]*usecase.ProjectView, error) { return nil, nil },
	}}
	handler := makeMCPListProjects(deps)
	res, err := handler(context.Background(), makeToolRequest(nil))
	if err != nil {
		t.Fatalf("unexpected framework error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %v", res.Content)
	}
	text := extractText(t, res)
	if !strings.Contains(text, "[]") && !strings.Contains(text, "null") {
		// JSON empty array is acceptable
		if text != "[]" && text != "null" && !strings.HasPrefix(strings.TrimSpace(text), "[") {
			t.Errorf("unexpected result: %q", text)
		}
	}
}

func TestMCPGetProject_Success(t *testing.T) {
	deps := &Deps{ProjectService: &mockSvc{
		GetFn: func(_ context.Context, slug string) (*usecase.ProjectView, error) {
			return &usecase.ProjectView{Slug: slug, DisplayName: "Test", Status: "stopped", UpdatedAt: time.Now()}, nil
		},
	}}
	handler := makeMCPGetProject(deps)
	res, err := handler(context.Background(), makeToolRequest(map[string]interface{}{"slug": "myproj"}))
	if err != nil {
		t.Fatalf("unexpected framework error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %v", res.Content)
	}
	text := extractText(t, res)
	if !strings.Contains(text, "myproj") {
		t.Errorf("expected slug in result, got: %q", text)
	}
}

func TestMCPGetProject_MissingSlug(t *testing.T) {
	deps := &Deps{ProjectService: &mockSvc{}}
	handler := makeMCPGetProject(deps)
	res, err := handler(context.Background(), makeToolRequest(map[string]interface{}{}))
	if err != nil {
		t.Fatalf("unexpected framework error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected tool error for missing slug")
	}
}

func TestMCPCreateProject_Success(t *testing.T) {
	deps := &Deps{ProjectService: &mockSvc{}}
	handler := makeMCPCreateProject(deps)
	res, err := handler(context.Background(), makeToolRequest(map[string]interface{}{
		"slug": "newproj", "display_name": "New Project",
	}))
	if err != nil {
		t.Fatalf("unexpected framework error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %v", res.Content)
	}
	text := extractText(t, res)
	if !strings.Contains(text, "newproj") {
		t.Errorf("expected slug in result, got: %q", text)
	}
}

func TestMCPDeleteProject_Success(t *testing.T) {
	deps := &Deps{ProjectService: &mockSvc{}}
	handler := makeMCPDeleteProject(deps)
	res, err := handler(context.Background(), makeToolRequest(map[string]interface{}{"slug": "myproj"}))
	if err != nil {
		t.Fatalf("unexpected framework error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %v", res.Content)
	}
	text := extractText(t, res)
	if !strings.Contains(text, `"deleted":true`) && !strings.Contains(text, `"deleted": true`) {
		t.Errorf("expected deleted:true in result, got: %q", text)
	}
}

func TestMCPDeleteProject_Error(t *testing.T) {
	deps := &Deps{ProjectService: &mockSvc{
		DeleteFn: func(_ context.Context, _ string) (*usecase.ProjectView, error) {
			return nil, &usecase.UsecaseError{Code: usecase.ErrCodeNotFound, Message: "project not found"}
		},
	}}
	handler := makeMCPDeleteProject(deps)
	res, err := handler(context.Background(), makeToolRequest(map[string]interface{}{"slug": "ghost"}))
	if err != nil {
		t.Fatalf("unexpected framework error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected tool error for not-found")
	}
}

// extractText returns the text content of the first TextContent item.
func extractText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

// --- tool description tests ---

func TestBuildMCPServer_NoPanic(t *testing.T) {
	deps := &Deps{ProjectService: &mockSvc{}}
	require.NotPanics(t, func() {
		s := buildMCPServer(deps)
		require.NotNil(t, s)
	})
}

func TestMCPTools_AllHaveDescriptions(t *testing.T) {
	deps := &Deps{ProjectService: &mockSvc{}}
	s := buildMCPServer(deps)

	expectedTools := []string{
		"list_projects",
		"get_project",
		"create_project",
		"start_project",
		"stop_project",
		"reset_project",
		"delete_project",
	}

	tools := s.ListTools()
	require.Len(t, tools, 7, "expected exactly 7 MCP tools")

	for _, name := range expectedTools {
		st := s.GetTool(name)
		require.NotNilf(t, st, "tool %q not registered", name)
		assert.NotEmpty(t, st.Tool.Description, "tool %q has empty description", name)
	}
}

func TestMCPTools_ParametersHaveDescriptions(t *testing.T) {
	deps := &Deps{ProjectService: &mockSvc{}}
	s := buildMCPServer(deps)

	tools := s.ListTools()
	for name, st := range tools {
		for paramName, paramSchema := range st.Tool.InputSchema.Properties {
			paramMap, ok := paramSchema.(map[string]any)
			if !ok {
				continue
			}
			desc, _ := paramMap["description"].(string)
			assert.NotEmptyf(t, desc, "tool %q param %q has empty description", name, paramName)
		}
	}
}

func TestMCPTools_KeyPhrases(t *testing.T) {
	deps := &Deps{ProjectService: &mockSvc{}}
	s := buildMCPServer(deps)

	tests := []struct {
		tool   string
		phrase string
	}{
		{"list_projects", "get_project"},
		{"get_project", "***"},
		{"create_project", "stopped"},
		{"create_project", "start_project"},
		{"start_project", "stopped"},
		{"stop_project", "preserves data"},
		{"reset_project", "WARNING"},
		{"reset_project", "slug and display_name"},
		{"delete_project", "IRREVERSIBLE"},
	}

	for _, tc := range tests {
		t.Run(tc.tool+"_contains_"+tc.phrase, func(t *testing.T) {
			st := s.GetTool(tc.tool)
			require.NotNilf(t, st, "tool %q not found", tc.tool)
			assert.Containsf(t, st.Tool.Description, tc.phrase,
				"tool %q description should contain %q", tc.tool, tc.phrase)
		})
	}
}

func TestMCPTools_CreateProjectParamDescriptions(t *testing.T) {
	deps := &Deps{ProjectService: &mockSvc{}}
	s := buildMCPServer(deps)
	st := s.GetTool("create_project")
	require.NotNil(t, st)

	props := st.Tool.InputSchema.Properties

	tests := []struct {
		param  string
		phrase string
	}{
		{"slug", "3-40"},
		{"slug", "Immutable"},
		{"display_name", "100"},
		{"runtime", "docker-compose"},
		{"runtime", "kubernetes"},
	}

	for _, tc := range tests {
		t.Run(tc.param+"_contains_"+tc.phrase, func(t *testing.T) {
			paramMap, ok := props[tc.param].(map[string]any)
			require.Truef(t, ok, "param %q not found in create_project", tc.param)
			desc, _ := paramMap["description"].(string)
			assert.Containsf(t, desc, tc.phrase,
				"create_project param %q description should contain %q", tc.param, tc.phrase)
		})
	}
}

func TestMCPTools_SlugParamDescription(t *testing.T) {
	deps := &Deps{ProjectService: &mockSvc{}}
	s := buildMCPServer(deps)

	slugTools := []string{
		"get_project",
		"start_project",
		"stop_project",
		"reset_project",
		"delete_project",
	}

	for _, name := range slugTools {
		t.Run(name, func(t *testing.T) {
			st := s.GetTool(name)
			require.NotNilf(t, st, "tool %q not found", name)
			paramMap, ok := st.Tool.InputSchema.Properties["slug"].(map[string]any)
			require.True(t, ok, "slug param not found")
			desc, _ := paramMap["description"].(string)
			assert.Contains(t, desc, "list_projects",
				"slug description in %q should reference list_projects", name)
		})
	}
}
