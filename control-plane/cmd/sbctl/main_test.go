package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/kevin/supabase-control-plane/internal/usecase"
)

// --- mock ---

type mockSvc struct {
	CreateFn      func(ctx context.Context, slug, displayName string) (*usecase.ProjectView, error)
	ListFn        func(ctx context.Context) ([]*usecase.ProjectView, error)
	GetFn         func(ctx context.Context, slug string) (*usecase.ProjectView, error)
	StartFn       func(ctx context.Context, slug string) (*usecase.ProjectView, error)
	StopFn        func(ctx context.Context, slug string) (*usecase.ProjectView, error)
	ResetFn       func(ctx context.Context, slug string) (*usecase.ProjectView, error)
	DeleteFn      func(ctx context.Context, slug string) (*usecase.ProjectView, error)
	CredentialsFn func(ctx context.Context, slug string) (*usecase.CredentialsView, error)
}

func (m *mockSvc) Create(ctx context.Context, slug, dn string) (*usecase.ProjectView, error) {
	if m.CreateFn != nil {
		return m.CreateFn(ctx, slug, dn)
	}
	return stubView(slug, dn), nil
}
func (m *mockSvc) List(ctx context.Context) ([]*usecase.ProjectView, error) {
	if m.ListFn != nil {
		return m.ListFn(ctx)
	}
	return nil, nil
}
func (m *mockSvc) Get(ctx context.Context, slug string) (*usecase.ProjectView, error) {
	if m.GetFn != nil {
		return m.GetFn(ctx, slug)
	}
	return stubView(slug, slug), nil
}
func (m *mockSvc) Start(ctx context.Context, slug string) (*usecase.ProjectView, error) {
	if m.StartFn != nil {
		return m.StartFn(ctx, slug)
	}
	return stubView(slug, slug), nil
}
func (m *mockSvc) Stop(ctx context.Context, slug string) (*usecase.ProjectView, error) {
	if m.StopFn != nil {
		return m.StopFn(ctx, slug)
	}
	return stubView(slug, slug), nil
}
func (m *mockSvc) Reset(ctx context.Context, slug string) (*usecase.ProjectView, error) {
	if m.ResetFn != nil {
		return m.ResetFn(ctx, slug)
	}
	return stubView(slug, slug), nil
}
func (m *mockSvc) Delete(ctx context.Context, slug string) (*usecase.ProjectView, error) {
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, slug)
	}
	return stubView(slug, slug), nil
}
func (m *mockSvc) GetCredentials(ctx context.Context, slug string) (*usecase.CredentialsView, error) {
	if m.CredentialsFn != nil {
		return m.CredentialsFn(ctx, slug)
	}
	return &usecase.CredentialsView{
		Slug:              slug,
		StudioURL:         "http://localhost:54323",
		DashboardUsername: "supabase",
		DashboardPassword: "secret",
		APIURL:            "http://localhost:54321",
		AnonKey:           "anon-key",
		ServiceRoleKey:    "service-role-key",
		PostgresHost:      "localhost",
		PostgresPort:      "54322",
		PostgresDB:        "postgres",
		PostgresPassword:  "pg-password",
	}, nil
}

func stubView(slug, displayName string) *usecase.ProjectView {
	return &usecase.ProjectView{
		Slug:        slug,
		DisplayName: displayName,
		Status:      "stopped",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

// --- test helpers ---

// newTestRootCmd builds a root command with deps pre-wired and a no-op
// PersistentPreRunE (bypassing BuildDeps / db-url validation).
func newTestRootCmd(svc usecase.ProjectService) *cobra.Command {
	deps := &Deps{ProjectService: svc}
	output := "table"

	root := &cobra.Command{Use: "sbctl"}
	root.SilenceErrors = true
	root.SilenceUsage = true
	root.PersistentPreRunE = func(_ *cobra.Command, _ []string) error { return nil }
	root.PersistentFlags().StringVarP(&output, "output", "o", "table", "")
	root.AddCommand(buildProjectCmd(&deps, &output))
	return root
}

// runCmd executes cmd with the given args and optional stdin, returning
// captured stdout and stderr strings.
func runCmd(t *testing.T, root *cobra.Command, args []string, stdin string) (stdout, stderr string, err error) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	if stdin != "" {
		root.SetIn(strings.NewReader(stdin))
	}
	root.SetArgs(args)
	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

// --- validateOutput tests ---

func TestValidateOutput(t *testing.T) {
	cases := []struct {
		input   string
		wantErr bool
	}{
		{"table", false},
		{"json", false},
		{"yaml", false},
		{"csv", true},
		{"", true},
		{"JSON", true},
	}
	for _, tc := range cases {
		err := validateOutput(tc.input)
		if (err != nil) != tc.wantErr {
			t.Errorf("validateOutput(%q): got err=%v, wantErr=%v", tc.input, err, tc.wantErr)
		}
	}
}

// --- project list ---

func TestProjectList_Empty(t *testing.T) {
	root := newTestRootCmd(&mockSvc{
		ListFn: func(_ context.Context) ([]*usecase.ProjectView, error) { return nil, nil },
	})
	out, _, err := runCmd(t, root, []string{"project", "list"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "SLUG") {
		t.Errorf("expected header in output, got: %q", out)
	}
}

func TestProjectList_JSON(t *testing.T) {
	root := newTestRootCmd(&mockSvc{
		ListFn: func(_ context.Context) ([]*usecase.ProjectView, error) {
			return []*usecase.ProjectView{stubView("alpha", "Alpha")}, nil
		},
	})
	out, _, err := runCmd(t, root, []string{"-o", "json", "project", "list"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `"slug"`) || !strings.Contains(out, "alpha") {
		t.Errorf("expected JSON with slug, got: %q", out)
	}
}

func TestProjectList_Error(t *testing.T) {
	root := newTestRootCmd(&mockSvc{
		ListFn: func(_ context.Context) ([]*usecase.ProjectView, error) {
			return nil, &usecase.UsecaseError{Code: usecase.ErrCodeInternal, Message: "db down"}
		},
	})
	_, errOut, err := runCmd(t, root, []string{"project", "list"}, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errOut, "db down") {
		t.Errorf("expected error message in stderr, got: %q", errOut)
	}
}

// --- project get ---

func TestProjectGet_Success(t *testing.T) {
	root := newTestRootCmd(&mockSvc{})
	out, _, err := runCmd(t, root, []string{"project", "get", "myproj"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "myproj") {
		t.Errorf("expected slug in output, got: %q", out)
	}
}

func TestProjectGet_NotFound(t *testing.T) {
	root := newTestRootCmd(&mockSvc{
		GetFn: func(_ context.Context, slug string) (*usecase.ProjectView, error) {
			return nil, &usecase.UsecaseError{Code: usecase.ErrCodeNotFound, Message: "project not found"}
		},
	})
	_, errOut, err := runCmd(t, root, []string{"project", "get", "ghost"}, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errOut, "not found") {
		t.Errorf("expected not-found message in stderr, got: %q", errOut)
	}
}

// --- project create ---

func TestProjectCreate_Success(t *testing.T) {
	root := newTestRootCmd(&mockSvc{})
	out, _, err := runCmd(t, root, []string{"project", "create", "newproj", "--display-name", "New Project"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "newproj") {
		t.Errorf("expected slug in output, got: %q", out)
	}
}

func TestProjectCreate_MissingDisplayName(t *testing.T) {
	root := newTestRootCmd(&mockSvc{})
	_, _, err := runCmd(t, root, []string{"project", "create", "newproj"}, "")
	if err == nil {
		t.Fatal("expected error for missing --display-name")
	}
}

// --- project start / stop / reset ---

func TestProjectStart_Success(t *testing.T) {
	root := newTestRootCmd(&mockSvc{})
	out, _, err := runCmd(t, root, []string{"project", "start", "myproj"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "myproj") {
		t.Errorf("expected slug in output, got: %q", out)
	}
}

func TestProjectStop_Success(t *testing.T) {
	root := newTestRootCmd(&mockSvc{})
	out, _, err := runCmd(t, root, []string{"project", "stop", "myproj"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "myproj") {
		t.Errorf("expected slug in output, got: %q", out)
	}
}

func TestProjectReset_Success(t *testing.T) {
	root := newTestRootCmd(&mockSvc{})
	out, _, err := runCmd(t, root, []string{"project", "reset", "myproj"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "myproj") {
		t.Errorf("expected slug in output, got: %q", out)
	}
}

// --- project delete ---

func TestProjectDelete_WithYesFlag(t *testing.T) {
	root := newTestRootCmd(&mockSvc{})
	out, _, err := runCmd(t, root, []string{"project", "delete", "--yes", "myproj"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "myproj") {
		t.Errorf("expected slug in output, got: %q", out)
	}
}

func TestProjectDelete_WithConfirmation(t *testing.T) {
	root := newTestRootCmd(&mockSvc{})
	// Provide correct slug as stdin confirmation
	out, _, err := runCmd(t, root, []string{"project", "delete", "myproj"}, "myproj\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "myproj") {
		t.Errorf("expected slug in output, got: %q", out)
	}
}

func TestProjectDelete_ConfirmationMismatch(t *testing.T) {
	root := newTestRootCmd(&mockSvc{})
	_, errOut, err := runCmd(t, root, []string{"project", "delete", "myproj"}, "wrongslug\n")
	if err == nil {
		t.Fatal("expected error when confirmation mismatches")
	}
	if !strings.Contains(errOut, "aborting") {
		t.Errorf("expected aborting message in stderr, got: %q", errOut)
	}
}

func TestProjectCredentials_Table(t *testing.T) {
root := newTestRootCmd(&mockSvc{})
out, _, err := runCmd(t, root, []string{"project", "credentials", "myproj"}, "")
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
for _, want := range []string{"Studio URL", "Dashboard Username", "supabase", "anon-key"} {
if !strings.Contains(out, want) {
t.Errorf("expected %q in output, got: %q", want, out)
}
}
}

func TestProjectCredentials_JSON(t *testing.T) {
root := newTestRootCmd(&mockSvc{})
out, _, err := runCmd(t, root, []string{"--output", "json", "project", "credentials", "myproj"}, "")
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
if !strings.Contains(out, `"anon_key"`) {
t.Errorf("expected JSON key in output, got: %q", out)
}
}
