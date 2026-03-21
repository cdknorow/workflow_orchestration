package license

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewManager_NoFile(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	if m.IsValid() {
		t.Error("expected invalid with no license file")
	}
	if m.GetInfo() != nil {
		t.Error("expected nil info with no license file")
	}
}

func TestNewManager_LoadsCachedLicense(t *testing.T) {
	dir := t.TempDir()
	cached := CachedLicense{
		LicenseKey:    "test-key",
		InstanceID:    "test-instance",
		ActivatedAt:   time.Now().UTC().Format(time.RFC3339),
		LastValidated: time.Now().UTC().Format(time.RFC3339),
		Valid:         true,
	}
	data, _ := json.MarshalIndent(cached, "", "  ")
	os.WriteFile(filepath.Join(dir, "license.json"), data, 0600)

	m := NewManager(dir)
	if !m.IsValid() {
		t.Error("expected valid license from cache")
	}
	info := m.GetInfo()
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	if info.LicenseKey != "test-key" {
		t.Errorf("expected key 'test-key', got %s", info.LicenseKey)
	}
}

func TestIsValid_ExpiredRevalidation(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-8 * 24 * time.Hour) // 8 days ago
	cached := CachedLicense{
		LicenseKey:    "test-key",
		InstanceID:    "test-instance",
		ActivatedAt:   old.Format(time.RFC3339),
		LastValidated: old.Format(time.RFC3339),
		Valid:         true,
	}
	data, _ := json.MarshalIndent(cached, "", "  ")
	os.WriteFile(filepath.Join(dir, "license.json"), data, 0600)

	m := NewManager(dir)
	// Still valid because within offline grace period (30 days)
	if !m.IsValid() {
		t.Error("expected valid within offline grace period")
	}
	if !m.NeedsRevalidation() {
		t.Error("expected needs revalidation after 8 days")
	}
}

func TestIsValid_BeyondGracePeriod(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-31 * 24 * time.Hour) // 31 days ago
	cached := CachedLicense{
		LicenseKey:    "test-key",
		InstanceID:    "test-instance",
		ActivatedAt:   old.Format(time.RFC3339),
		LastValidated: old.Format(time.RFC3339),
		Valid:         true,
	}
	data, _ := json.MarshalIndent(cached, "", "  ")
	os.WriteFile(filepath.Join(dir, "license.json"), data, 0600)

	m := NewManager(dir)
	if m.IsValid() {
		t.Error("expected invalid beyond offline grace period")
	}
}

func TestActivate_Success(t *testing.T) {
	// Mock Lemon Squeezy API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(lsResponse{
			Valid: true,
			LicenseKey: struct {
				Status string `json:"status"`
			}{Status: "active"},
			Instance: struct {
				ID string `json:"id"`
			}{ID: "inst-123"},
			Meta: struct {
				CustomerName  string `json:"customer_name"`
				CustomerEmail string `json:"customer_email"`
			}{CustomerName: "Test User", CustomerEmail: "test@example.com"},
		})
	}))
	defer server.Close()

	dir := t.TempDir()
	m := NewManager(dir)

	// Override the activate URL for testing
	origURL := activateEndpoint
	setActivateURL(server.URL)
	defer setActivateURL(origURL)

	err := m.Activate("valid-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.IsValid() {
		t.Error("expected valid after activation")
	}
	info := m.GetInfo()
	if info.CustomerName != "Test User" {
		t.Errorf("expected 'Test User', got %s", info.CustomerName)
	}

	// Verify persisted to disk
	m2 := NewManager(dir)
	if !m2.IsValid() {
		t.Error("expected valid from persisted cache")
	}
}

func TestActivate_InvalidKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(lsResponse{
			Valid: false,
			Error: "This license key was not found.",
		})
	}))
	defer server.Close()

	dir := t.TempDir()
	m := NewManager(dir)

	origURL := activateEndpoint
	setActivateURL(server.URL)
	defer setActivateURL(origURL)

	err := m.Activate("bad-key")
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
	if m.IsValid() {
		t.Error("should not be valid after failed activation")
	}
}

func TestMachineFingerprint_Stable(t *testing.T) {
	fp1 := machineFingerprint()
	fp2 := machineFingerprint()
	if fp1 != fp2 {
		t.Errorf("fingerprint not stable: %s != %s", fp1, fp2)
	}
	if len(fp1) != 16 { // 8 bytes hex-encoded
		t.Errorf("unexpected fingerprint length: %d", len(fp1))
	}
}

func TestMiddleware_BlocksWithoutLicense(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	handler := Middleware(m)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// API request should be blocked
	req := httptest.NewRequest("GET", "/api/sessions/live", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}

	// License endpoint should pass through
	req = httptest.NewRequest("GET", "/api/license/status", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for license endpoint, got %d", rec.Code)
	}

	// Static assets should pass through
	req = httptest.NewRequest("GET", "/static/app.js", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for static asset, got %d", rec.Code)
	}

	// Root page should pass through
	req = httptest.NewRequest("GET", "/", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for root page, got %d", rec.Code)
	}
}
