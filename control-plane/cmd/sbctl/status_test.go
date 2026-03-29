package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kevin/supabase-control-plane/internal/usecase"
)

func viewWithStatus(slug, displayName, status string) *usecase.ProjectView {
	return &usecase.ProjectView{
		Slug:        slug,
		DisplayName: displayName,
		Status:      status,
		CreatedAt:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestStatus_SummaryLineFixedOrder(t *testing.T) {
	root := newTestRootCmd(&mockSvc{
		ListFn: func(_ context.Context) ([]*usecase.ProjectView, error) {
			return []*usecase.ProjectView{
				viewWithStatus("a", "A", "stopped"),
				viewWithStatus("b", "B", "running"),
				viewWithStatus("c", "C", "running"),
				viewWithStatus("d", "D", "error"),
			}, nil
		},
	})
	out, _, err := runCmd(t, root, []string{"status"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should show "4 projects: 2 running, 1 stopped, 1 error"
	if !strings.Contains(out, "4 projects:") {
		t.Errorf("expected total count, got: %q", out)
	}
	// "running" must appear before "stopped" and "stopped" before "error"
	ri := strings.Index(out, "2 running")
	si := strings.Index(out, "1 stopped")
	ei := strings.Index(out, "1 error")
	if ri < 0 || si < 0 || ei < 0 {
		t.Fatalf("expected all counts in output, got: %q", out)
	}
	if ri > si || si > ei {
		t.Errorf("expected fixed order (running < stopped < error), got: %q", out)
	}
}

func TestStatus_ZeroCountStatusesOmitted(t *testing.T) {
	root := newTestRootCmd(&mockSvc{
		ListFn: func(_ context.Context) ([]*usecase.ProjectView, error) {
			return []*usecase.ProjectView{
				viewWithStatus("a", "A", "running"),
			}, nil
		},
	})
	out, _, err := runCmd(t, root, []string{"status"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "stopped") {
		t.Errorf("zero-count 'stopped' should be omitted, got: %q", out)
	}
	if !strings.Contains(out, "1 running") {
		t.Errorf("expected '1 running', got: %q", out)
	}
}

func TestStatus_AlertSectionForErrorProjects(t *testing.T) {
	root := newTestRootCmd(&mockSvc{
		ListFn: func(_ context.Context) ([]*usecase.ProjectView, error) {
			return []*usecase.ProjectView{
				viewWithStatus("broken", "Broken", "error"),
				viewWithStatus("ok", "OK", "running"),
			}, nil
		},
	})
	out, _, err := runCmd(t, root, []string{"status"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "⚠ Projects needing attention:") {
		t.Errorf("expected alert header, got: %q", out)
	}
	if !strings.Contains(out, "broken") {
		t.Errorf("expected error project slug in alerts, got: %q", out)
	}
}

func TestStatus_NoAlertSectionWhenNoErrors(t *testing.T) {
	root := newTestRootCmd(&mockSvc{
		ListFn: func(_ context.Context) ([]*usecase.ProjectView, error) {
			return []*usecase.ProjectView{
				viewWithStatus("a", "A", "running"),
				viewWithStatus("b", "B", "stopped"),
			}, nil
		},
	})
	out, _, err := runCmd(t, root, []string{"status"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "⚠") {
		t.Errorf("expected no alert section, got: %q", out)
	}
}

func TestStatus_EmptyProjectList(t *testing.T) {
	root := newTestRootCmd(&mockSvc{
		ListFn: func(_ context.Context) ([]*usecase.ProjectView, error) {
			return nil, nil
		},
	})
	out, _, err := runCmd(t, root, []string{"status"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "No projects found") {
		t.Errorf("expected guidance message, got: %q", out)
	}
}

func TestStatus_JSON(t *testing.T) {
	root := newTestRootCmd(&mockSvc{
		ListFn: func(_ context.Context) ([]*usecase.ProjectView, error) {
			return []*usecase.ProjectView{
				viewWithStatus("alpha", "Alpha", "running"),
				viewWithStatus("beta", "Beta", "error"),
			}, nil
		},
	})
	out, _, err := runCmd(t, root, []string{"-o", "json", "status"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var overview StatusOverview
	if err := json.Unmarshal([]byte(out), &overview); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, out)
	}
	if overview.Total != 2 {
		t.Errorf("expected total=2, got %d", overview.Total)
	}
	if overview.Summary["running"] != 1 {
		t.Errorf("expected 1 running, got %d", overview.Summary["running"])
	}
	if overview.Summary["error"] != 1 {
		t.Errorf("expected 1 error, got %d", overview.Summary["error"])
	}
	if len(overview.Alerts) != 1 {
		t.Errorf("expected 1 alert, got %d", len(overview.Alerts))
	}
	if len(overview.Projects) != 2 {
		t.Errorf("expected 2 projects, got %d", len(overview.Projects))
	}
}

func TestStatus_YAML(t *testing.T) {
	root := newTestRootCmd(&mockSvc{
		ListFn: func(_ context.Context) ([]*usecase.ProjectView, error) {
			return []*usecase.ProjectView{
				viewWithStatus("alpha", "Alpha", "running"),
			}, nil
		},
	})
	out, _, err := runCmd(t, root, []string{"-o", "yaml", "status"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "total: 1") {
		t.Errorf("expected 'total: 1' in YAML output, got: %q", out)
	}
	if !strings.Contains(out, "slug: alpha") {
		t.Errorf("expected 'slug: alpha' in YAML output, got: %q", out)
	}
}

func TestStatus_MixedStatuses(t *testing.T) {
	root := newTestRootCmd(&mockSvc{
		ListFn: func(_ context.Context) ([]*usecase.ProjectView, error) {
			return []*usecase.ProjectView{
				viewWithStatus("a", "A", "running"),
				viewWithStatus("b", "B", "creating"),
				viewWithStatus("c", "C", "running"),
				viewWithStatus("d", "D", "stopped"),
				viewWithStatus("e", "E", "stopping"),
				viewWithStatus("f", "F", "error"),
			}, nil
		},
	})
	out, _, err := runCmd(t, root, []string{"status"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "6 projects:") {
		t.Errorf("expected '6 projects:', got: %q", out)
	}
	if !strings.Contains(out, "2 running") {
		t.Errorf("expected '2 running', got: %q", out)
	}
	if !strings.Contains(out, "1 creating") {
		t.Errorf("expected '1 creating', got: %q", out)
	}
	if !strings.Contains(out, "1 stopping") {
		t.Errorf("expected '1 stopping', got: %q", out)
	}
}

func TestStatus_UnknownStatusIncluded(t *testing.T) {
	root := newTestRootCmd(&mockSvc{
		ListFn: func(_ context.Context) ([]*usecase.ProjectView, error) {
			return []*usecase.ProjectView{
				viewWithStatus("a", "A", "running"),
				viewWithStatus("b", "B", "migrating"),
			}, nil
		},
	})
	out, _, err := runCmd(t, root, []string{"status"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "1 migrating") {
		t.Errorf("expected unknown status 'migrating' in output, got: %q", out)
	}
}

func TestStatus_ListError(t *testing.T) {
	root := newTestRootCmd(&mockSvc{
		ListFn: func(_ context.Context) ([]*usecase.ProjectView, error) {
			return nil, &usecase.UsecaseError{Code: usecase.ErrCodeInternal, Message: "db down"}
		},
	})
	_, errOut, err := runCmd(t, root, []string{"status"}, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errOut, "db down") {
		t.Errorf("expected error message in stderr, got: %q", errOut)
	}
}

func TestStatus_ProjectTable(t *testing.T) {
	root := newTestRootCmd(&mockSvc{
		ListFn: func(_ context.Context) ([]*usecase.ProjectView, error) {
			return []*usecase.ProjectView{
				viewWithStatus("web", "Web App", "running"),
				viewWithStatus("api", "API Server", "stopped"),
			}, nil
		},
	})
	out, _, err := runCmd(t, root, []string{"status"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "SLUG") || !strings.Contains(out, "STATUS") || !strings.Contains(out, "UPDATED") {
		t.Errorf("expected table headers, got: %q", out)
	}
	if !strings.Contains(out, "web") || !strings.Contains(out, "api") {
		t.Errorf("expected project slugs in table, got: %q", out)
	}
}
