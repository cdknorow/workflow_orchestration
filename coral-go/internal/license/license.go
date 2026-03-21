// Package license handles Lemon Squeezy license key activation and validation.
package license

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	activateEndpoint = "https://api.lemonsqueezy.com/v1/licenses/activate"
	validateEndpoint = "https://api.lemonsqueezy.com/v1/licenses/validate"
)

const (
	revalidateInterval = 7 * 24 * time.Hour // re-check every 7 days
	offlineGraceDays   = 30                 // work offline for 30 days
)

// setActivateURL overrides the activate endpoint (for testing).
func setActivateURL(url string) { activateEndpoint = url }

// setValidateURL overrides the validate endpoint (for testing).
func setValidateURL(url string) { validateEndpoint = url }

// CachedLicense is persisted to ~/.coral/license.json.
type CachedLicense struct {
	LicenseKey    string `json:"license_key"`
	InstanceID    string `json:"instance_id"`
	CustomerName  string `json:"customer_name,omitempty"`
	CustomerEmail string `json:"customer_email,omitempty"`
	ActivatedAt   string `json:"activated_at"`
	LastValidated string `json:"last_validated"`
	Valid         bool   `json:"valid"`
}

// lsResponse is the Lemon Squeezy API response for activate/validate.
type lsResponse struct {
	Valid     bool   `json:"valid"`
	Error     string `json:"error,omitempty"`
	LicenseKey struct {
		Status string `json:"status"`
	} `json:"license_key"`
	Instance struct {
		ID string `json:"id"`
	} `json:"instance"`
	Meta struct {
		CustomerName  string `json:"customer_name"`
		CustomerEmail string `json:"customer_email"`
	} `json:"meta"`
}

// Manager handles license checking, activation, and caching.
type Manager struct {
	mu       sync.RWMutex
	cache    *CachedLicense
	filePath string
}

// NewManager creates a license manager that stores state in coralDir.
func NewManager(coralDir string) *Manager {
	m := &Manager{
		filePath: filepath.Join(coralDir, "license.json"),
	}
	m.load()
	return m
}

// IsValid returns whether the app has a valid, non-expired license.
func (m *Manager) IsValid() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.cache == nil || !m.cache.Valid {
		return false
	}

	lastValidated, err := time.Parse(time.RFC3339, m.cache.LastValidated)
	if err != nil {
		return false
	}

	// Within revalidation window → valid
	if time.Since(lastValidated) < revalidateInterval {
		return true
	}

	// Past revalidation but within offline grace → still valid (will re-check on next startup)
	if time.Since(lastValidated) < time.Duration(offlineGraceDays)*24*time.Hour {
		return true
	}

	return false
}

// NeedsRevalidation returns true if the license should be re-checked against the API.
func (m *Manager) NeedsRevalidation() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.cache == nil || !m.cache.Valid {
		return false
	}

	lastValidated, err := time.Parse(time.RFC3339, m.cache.LastValidated)
	if err != nil {
		return true
	}

	return time.Since(lastValidated) >= revalidateInterval
}

// Revalidate checks the license against Lemon Squeezy and updates the cache.
// Returns true if still valid. Fails gracefully if offline.
func (m *Manager) Revalidate() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cache == nil {
		return false
	}

	resp, err := m.callAPI(validateEndpoint, m.cache.LicenseKey, m.cache.InstanceID)
	if err != nil {
		// Offline — keep current state, don't update last_validated
		return m.cache.Valid
	}

	m.cache.Valid = resp.Valid && resp.LicenseKey.Status == "active"
	if m.cache.Valid {
		m.cache.LastValidated = time.Now().UTC().Format(time.RFC3339)
	}
	m.save()
	return m.cache.Valid
}

// Activate activates a license key and caches the result.
func (m *Manager) Activate(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	instanceName := machineFingerprint()
	resp, err := m.callAPI(activateEndpoint, key, instanceName)
	if err != nil {
		return fmt.Errorf("failed to reach license server: %w", err)
	}

	if !resp.Valid {
		msg := resp.Error
		if msg == "" {
			msg = "invalid license key"
		}
		return fmt.Errorf("%s", msg)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	m.cache = &CachedLicense{
		LicenseKey:    key,
		InstanceID:    resp.Instance.ID,
		CustomerName:  resp.Meta.CustomerName,
		CustomerEmail: resp.Meta.CustomerEmail,
		ActivatedAt:   now,
		LastValidated: now,
		Valid:         true,
	}
	return m.save()
}

// GetInfo returns the cached license info (nil if not activated).
func (m *Manager) GetInfo() *CachedLicense {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cache == nil {
		return nil
	}
	c := *m.cache
	return &c
}

func (m *Manager) callAPI(endpoint, licenseKey, instanceID string) (*lsResponse, error) {
	form := url.Values{
		"license_key":   {licenseKey},
		"instance_name": {instanceID},
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.PostForm(endpoint, form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result lsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("invalid API response: %w", err)
	}
	return &result, nil
}

func (m *Manager) load() {
	data, err := os.ReadFile(m.filePath)
	if err != nil {
		return
	}
	var c CachedLicense
	if json.Unmarshal(data, &c) == nil {
		m.cache = &c
	}
}

func (m *Manager) save() error {
	dir := filepath.Dir(m.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m.cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.filePath, append(data, '\n'), 0600)
}

// machineFingerprint returns a stable identifier for this machine.
func machineFingerprint() string {
	hostname, _ := os.Hostname()

	// Collect MAC addresses for stability
	var macs []string
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.HardwareAddr != nil && iface.Flags&net.FlagLoopback == 0 {
			macs = append(macs, iface.HardwareAddr.String())
		}
	}
	sort.Strings(macs)

	raw := hostname + "|" + strings.Join(macs, ",")
	hash := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", hash[:8])
}
