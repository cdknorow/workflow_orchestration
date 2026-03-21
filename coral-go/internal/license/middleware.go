package license

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

// Middleware returns an HTTP middleware that gates access behind a valid license.
// Requests to allowed paths (static assets, license endpoints) pass through.
// All other requests get a 403 with a JSON error when unlicensed.
func Middleware(m *Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path

			// Always allow: license endpoints, static assets, root page
			if isUngatedPath(path) {
				next.ServeHTTP(w, r)
				return
			}

			if !m.IsValid() {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]any{
					"error":    "license_required",
					"message":  "A valid license is required. Please activate your license.",
					"activate": "/api/license/activate",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// isUngatedPath returns true for paths that should work without a license.
func isUngatedPath(path string) bool {
	// License management endpoints
	if strings.HasPrefix(path, "/api/license") {
		return true
	}
	// Static assets (CSS, JS, images)
	if strings.HasPrefix(path, "/static/") {
		return true
	}
	// Root page (serves activation UI when unlicensed)
	if path == "/" {
		return true
	}
	return false
}

// Routes registers the license API endpoints on a chi-compatible router.
// Call this with r.Post("/api/license/activate", ...) etc.
type Routes struct {
	mgr *Manager
}

func NewRoutes(mgr *Manager) *Routes {
	return &Routes{mgr: mgr}
}

// Activate handles POST /api/license/activate
func (lr *Routes) Activate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		LicenseKey string `json:"license_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.LicenseKey == "" {
		writeJSONResponse(w, http.StatusBadRequest, map[string]string{"error": "license_key is required"})
		return
	}

	body.LicenseKey = strings.TrimSpace(body.LicenseKey)

	if err := lr.mgr.Activate(body.LicenseKey); err != nil {
		writeJSONResponse(w, http.StatusOK, map[string]any{
			"valid": false,
			"error": err.Error(),
		})
		return
	}

	info := lr.mgr.GetInfo()
	writeJSONResponse(w, http.StatusOK, map[string]any{
		"valid":          true,
		"customer_name":  info.CustomerName,
		"customer_email": info.CustomerEmail,
		"activated_at":   info.ActivatedAt,
	})
}

// Status handles GET /api/license/status
func (lr *Routes) Status(w http.ResponseWriter, r *http.Request) {
	info := lr.mgr.GetInfo()
	if info == nil {
		writeJSONResponse(w, http.StatusOK, map[string]any{"valid": false, "activated": false})
		return
	}
	writeJSONResponse(w, http.StatusOK, map[string]any{
		"valid":          info.Valid,
		"activated":      true,
		"customer_name":  info.CustomerName,
		"customer_email": info.CustomerEmail,
		"activated_at":   info.ActivatedAt,
		"last_validated": info.LastValidated,
	})
}

func writeJSONResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("JSON encode error: %v", err)
	}
}
