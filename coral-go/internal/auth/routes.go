package auth

import (
	"encoding/json"
	"net"
	"net/http"
)

// Routes provides HTTP handlers for authentication endpoints.
type Routes struct {
	ks *KeyStore
}

// NewRoutes creates auth route handlers.
func NewRoutes(ks *KeyStore) *Routes {
	return &Routes{ks: ks}
}

// AuthPage serves the authentication page.
// GET /auth
func (ar *Routes) AuthPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(authPageHTML))
}

// ValidateKey validates an API key and sets a session cookie.
// POST /auth/key
func (ar *Routes) ValidateKey(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit
	var body struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !ar.ks.CheckRateLimit(ip) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "Too many attempts. Try again later."})
		return
	}

	if !ar.ks.ValidateKey(body.Key) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid API key"})
		return
	}

	token := ar.ks.CreateSession(ip, r.UserAgent())
	SetSessionCookie(w, token)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// GetAPIKey returns the API key (localhost only).
// GET /api/system/api-key
func (ar *Routes) GetAPIKey(w http.ResponseWriter, r *http.Request) {
	if !IsLocalhost(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "localhost only"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": ar.ks.Key()})
}

// RegenerateKey generates a new API key (localhost only).
// POST /api/system/api-key/regenerate
func (ar *Routes) RegenerateKey(w http.ResponseWriter, r *http.Request) {
	if !IsLocalhost(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "localhost only"})
		return
	}
	newKey := ar.ks.RegenerateKey()
	writeJSON(w, http.StatusOK, map[string]any{"key": newKey})
}

// AuthStatus returns the current authentication status.
// GET /api/system/auth-status
func (ar *Routes) AuthStatus(w http.ResponseWriter, r *http.Request) {
	method := "none"
	authenticated := false

	if IsLocalhost(r) {
		method = "localhost"
		authenticated = true
	} else if key := ExtractAPIKey(r); key != "" && ar.ks.ValidateKey(key) {
		method = "key"
		authenticated = true
	} else if token := ExtractSessionCookie(r); token != "" && ar.ks.ValidateSession(token) {
		method = "session"
		authenticated = true
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": authenticated,
		"method":        method,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
