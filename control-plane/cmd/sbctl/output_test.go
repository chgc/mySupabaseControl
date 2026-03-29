package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kevin/supabase-control-plane/internal/usecase"
)

func viewWithURLs() *usecase.ProjectView {
	v := stubView("demo", "Demo Project")
	v.URLs = &usecase.ProjectURLs{
		API:    "http://localhost:54321",
		Studio: "http://localhost:54321",
	}
	return v
}

func TestWriteProjectView_Table_WithURLs(t *testing.T) {
	var buf bytes.Buffer
	v := viewWithURLs()
	if err := writeProjectView(&buf, "table", v, noColor()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "URLs:") {
		t.Errorf("expected URLs header, got:\n%s", out)
	}
	if !strings.Contains(out, "API      http://localhost:54321") {
		t.Errorf("expected API URL line, got:\n%s", out)
	}
	if !strings.Contains(out, "Studio   http://localhost:54321") {
		t.Errorf("expected Studio URL line, got:\n%s", out)
	}
}

func TestWriteProjectView_Table_NoURLs(t *testing.T) {
	var buf bytes.Buffer
	v := stubView("demo", "Demo")
	if err := writeProjectView(&buf, "table", v, noColor()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(buf.String(), "URLs:") {
		t.Errorf("expected no URLs section when URLs is nil, got:\n%s", buf.String())
	}
}

func TestWriteProjectView_Table_InbucketOmittedWhenEmpty(t *testing.T) {
	var buf bytes.Buffer
	v := viewWithURLs()
	// Inbucket left as empty string (default)
	if err := writeProjectView(&buf, "table", v, noColor()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(buf.String(), "Inbucket") {
		t.Errorf("expected Inbucket line to be absent when empty, got:\n%s", buf.String())
	}
}

func TestWriteProjectView_Table_InbucketShownWhenSet(t *testing.T) {
	var buf bytes.Buffer
	v := viewWithURLs()
	v.URLs.Inbucket = "http://localhost:54324"
	if err := writeProjectView(&buf, "table", v, noColor()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "Inbucket http://localhost:54324") {
		t.Errorf("expected Inbucket line, got:\n%s", buf.String())
	}
}

func TestWriteProjectView_JSON_IncludesURLs(t *testing.T) {
	var buf bytes.Buffer
	v := viewWithURLs()
	if err := writeProjectView(&buf, "json", v, noColor()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var decoded struct {
		URLs *usecase.ProjectURLs `json:"urls"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if decoded.URLs == nil {
		t.Fatal("expected urls in JSON output")
	}
	if decoded.URLs.API != "http://localhost:54321" {
		t.Errorf("expected API URL in JSON, got %q", decoded.URLs.API)
	}
}

func TestWriteProjectView_JSON_NoURLs(t *testing.T) {
	var buf bytes.Buffer
	v := stubView("demo", "Demo")
	if err := writeProjectView(&buf, "json", v, noColor()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// urls should be omitted
	if strings.Contains(buf.String(), `"urls"`) {
		t.Errorf("expected urls to be omitted from JSON when nil, got:\n%s", buf.String())
	}
}

// noColor returns a nil *colorer, matching test convention (nil is safe).
func noColor() *colorer {
	return nil
}

// Ensure stubView's timestamps are deterministic enough for tests.
func init() {
	_ = time.Now() // reference to keep time import used
}
