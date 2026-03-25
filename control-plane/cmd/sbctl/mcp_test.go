package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

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
