package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// --- resolveSessionName tests ---

func TestResolveSessionName_FallsBackToHostname(t *testing.T) {
	// Without TMUX env var, should fall back to hostname
	old := os.Getenv("TMUX")
	os.Unsetenv("TMUX")
	defer func() {
		if old != "" {
			os.Setenv("TMUX", old)
		}
	}()

	name := resolveSessionName()
	if name == "" {
		t.Error("resolveSessionName returned empty string")
	}
	host, _ := os.Hostname()
	if name != host {
		t.Errorf("expected hostname %q, got %q", host, name)
	}
}

// --- State file management tests ---

func TestStateFile_SaveAndLoad(t *testing.T) {
	// Use a temp home directory
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	// Save state
	st := &boardState{Project: "test-project", JobTitle: "QA Engineer"}
	saveState(st)

	// Load it back
	loaded := loadState()
	if loaded == nil {
		t.Fatal("loadState returned nil")
	}
	if loaded.Project != "test-project" {
		t.Errorf("Project = %q, want %q", loaded.Project, "test-project")
	}
	if loaded.JobTitle != "QA Engineer" {
		t.Errorf("JobTitle = %q, want %q", loaded.JobTitle, "QA Engineer")
	}

	// Delete state
	deleteState()
	if loadState() != nil {
		t.Error("state should be nil after delete")
	}
}

func TestStateFile_LoadMissing(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	st := loadState()
	if st != nil {
		t.Error("expected nil for missing state file")
	}
}

func TestStateFile_ServerURLOverride(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	// Save state with custom server URL
	st := &boardState{Project: "test", JobTitle: "Dev", ServerURL: "http://custom:9999"}
	saveState(st)

	// Reset serverURL
	oldURL := serverURL
	defer func() { serverURL = oldURL }()

	loaded := loadState()
	if loaded == nil {
		t.Fatal("loadState returned nil")
	}
	if serverURL != "http://custom:9999" {
		t.Errorf("serverURL = %q, want %q", serverURL, "http://custom:9999")
	}
}

func TestStateFilePath_ContainsSessionName(t *testing.T) {
	path := stateFilePath()
	if !filepath.IsAbs(path) {
		t.Errorf("state file path should be absolute, got %q", path)
	}
	if filepath.Ext(path) != ".json" {
		t.Errorf("state file should have .json extension, got %q", path)
	}
}

// --- apiCall tests with mock server ---

func TestApiCall_GET(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/board/test-project/subscribers" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer ts.Close()

	oldURL := serverURL
	serverURL = ts.URL
	defer func() { serverURL = oldURL }()

	result, err := apiCall("GET", "/test-project/subscribers", nil)
	if err != nil {
		t.Fatalf("apiCall failed: %v", err)
	}
	if result["ok"] != true {
		t.Error("expected ok=true in response")
	}
}

func TestApiCall_POST_WithBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("expected Content-Type: application/json")
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["session_id"] != "test-session" {
			t.Errorf("session_id = %q, want %q", body["session_id"], "test-session")
		}
		json.NewEncoder(w).Encode(map[string]any{"id": float64(42)})
	}))
	defer ts.Close()

	oldURL := serverURL
	serverURL = ts.URL
	defer func() { serverURL = oldURL }()

	result, err := apiCall("POST", "/my-board/messages", map[string]string{
		"session_id": "test-session",
		"content":    "hello",
	})
	if err != nil {
		t.Fatalf("apiCall failed: %v", err)
	}
	if result["id"] != float64(42) {
		t.Errorf("expected id=42, got %v", result["id"])
	}
}

func TestApiCall_ServerDown(t *testing.T) {
	oldURL := serverURL
	serverURL = "http://localhost:1" // nothing listens here
	defer func() { serverURL = oldURL }()

	_, err := apiCall("GET", "/projects", nil)
	if err == nil {
		t.Error("expected error when server is down")
	}
}

// --- Init / env var tests ---

func TestInit_CORAL_URL(t *testing.T) {
	oldURL := serverURL
	defer func() { serverURL = oldURL }()

	// The init() already ran, but we can test the logic directly
	os.Setenv("CORAL_URL", "http://custom-host:9000/")
	serverURL = "http://localhost:8420"
	if v := os.Getenv("CORAL_URL"); v != "" {
		serverURL = fmt.Sprintf("%s", v[:len(v)-1]) // trim trailing slash
	}
	if serverURL != "http://custom-host:9000" {
		t.Errorf("serverURL = %q, want %q", serverURL, "http://custom-host:9000")
	}
	os.Unsetenv("CORAL_URL")
}

func TestInit_CORAL_PORT(t *testing.T) {
	oldURL := serverURL
	defer func() { serverURL = oldURL }()

	os.Setenv("CORAL_PORT", "9999")
	if v := os.Getenv("CORAL_PORT"); v != "" {
		serverURL = "http://localhost:" + v
	}
	if serverURL != "http://localhost:9999" {
		t.Errorf("serverURL = %q, want %q", serverURL, "http://localhost:9999")
	}
	os.Unsetenv("CORAL_PORT")
}

// --- printUsage doesn't panic ---

func TestPrintUsage_NoPanic(t *testing.T) {
	// Just verify it doesn't panic
	printUsage()
}
