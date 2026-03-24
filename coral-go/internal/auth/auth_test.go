package auth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestKeyStore(t *testing.T) *KeyStore {
	t.Helper()
	dir := t.TempDir()
	return NewKeyStore(dir)
}

// ── KeyStore Tests ──────────────────────────────────────────

func TestKeyStore_GeneratesKeyOnFirstRun(t *testing.T) {
	ks := newTestKeyStore(t)
	key := ks.Key()
	assert.Len(t, key, keyLength)

	// Key should be persisted to file
	data, err := os.ReadFile(ks.keyPath)
	require.NoError(t, err)
	assert.Equal(t, key, strings.TrimSpace(string(data)))
}

func TestKeyStore_LoadsExistingKey(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "api_key")
	os.WriteFile(keyPath, []byte("abcdef1234567890abcdef1234567890\n"), 0600)

	ks := NewKeyStore(dir)
	assert.Equal(t, "abcdef1234567890abcdef1234567890", ks.Key())
}

func TestKeyStore_RegenerateKey(t *testing.T) {
	ks := newTestKeyStore(t)
	oldKey := ks.Key()
	newKey := ks.RegenerateKey()
	assert.NotEqual(t, oldKey, newKey)
	assert.Len(t, newKey, keyLength)
	assert.Equal(t, newKey, ks.Key())
}

func TestKeyStore_ValidateKey(t *testing.T) {
	ks := newTestKeyStore(t)
	assert.True(t, ks.ValidateKey(ks.Key()))
	assert.False(t, ks.ValidateKey("wrong-key"))
	assert.False(t, ks.ValidateKey(""))
}

func TestKeyStore_RegenerateInvalidatesOldKey(t *testing.T) {
	ks := newTestKeyStore(t)
	oldKey := ks.Key()
	ks.RegenerateKey()
	assert.False(t, ks.ValidateKey(oldKey))
}

// ── Session Tests ───────────────────────────────────────────

func TestKeyStore_CreateAndValidateSession(t *testing.T) {
	ks := newTestKeyStore(t)
	token := ks.CreateSession("192.168.1.5", "Mozilla/5.0")
	assert.Len(t, token, sessionLength)
	assert.True(t, ks.ValidateSession(token))
}

func TestKeyStore_InvalidSession(t *testing.T) {
	ks := newTestKeyStore(t)
	assert.False(t, ks.ValidateSession("nonexistent-token"))
	assert.False(t, ks.ValidateSession(""))
}

func TestKeyStore_SessionExpiry(t *testing.T) {
	ks := newTestKeyStore(t)
	token := ks.CreateSession("192.168.1.5", "test")

	// Manually expire the session
	ks.mu.Lock()
	ks.sessions[token].CreatedAt = time.Now().Add(-25 * time.Hour)
	ks.mu.Unlock()

	assert.False(t, ks.ValidateSession(token))
}

func TestKeyStore_SessionLRUEviction(t *testing.T) {
	ks := newTestKeyStore(t)
	var firstToken string
	for i := 0; i < maxSessions+5; i++ {
		token := ks.CreateSession("192.168.1.5", "test")
		if i == 0 {
			firstToken = token
		}
	}
	// First sessions should be evicted
	assert.False(t, ks.ValidateSession(firstToken))
}

func TestKeyStore_SessionsSurviveKeyRegeneration(t *testing.T) {
	ks := newTestKeyStore(t)
	token := ks.CreateSession("192.168.1.5", "test")
	ks.RegenerateKey()
	// Existing sessions should still be valid after key regeneration
	assert.True(t, ks.ValidateSession(token))
}

// ── Rate Limiting Tests ─────────────────────────────────────

func TestKeyStore_RateLimit(t *testing.T) {
	ks := newTestKeyStore(t)
	ip := "10.0.0.1"
	for i := 0; i < rateLimitMax; i++ {
		assert.True(t, ks.CheckRateLimit(ip), "attempt %d should be allowed", i+1)
	}
	// Next attempt should be blocked
	assert.False(t, ks.CheckRateLimit(ip))
}

func TestKeyStore_RateLimitPerIP(t *testing.T) {
	ks := newTestKeyStore(t)
	// Exhaust rate limit for IP1
	for i := 0; i < rateLimitMax+1; i++ {
		ks.CheckRateLimit("10.0.0.1")
	}
	// IP2 should still be allowed
	assert.True(t, ks.CheckRateLimit("10.0.0.2"))
}

func TestKeyStore_RateLimitWindowReset(t *testing.T) {
	ks := newTestKeyStore(t)
	ip := "10.0.0.1"
	for i := 0; i < rateLimitMax+1; i++ {
		ks.CheckRateLimit(ip)
	}
	assert.False(t, ks.CheckRateLimit(ip))

	// Reset the window manually
	ks.mu.Lock()
	ks.rateMap[ip].windowAt = time.Now().Add(-2 * rateLimitWindow)
	ks.mu.Unlock()

	assert.True(t, ks.CheckRateLimit(ip))
}

// ── Localhost Check Tests ───────────────────────────────────

func TestIsLocalhost_IPv4(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:54321"
	assert.True(t, IsLocalhost(r))
}

func TestIsLocalhost_IPv6(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "[::1]:54321"
	assert.True(t, IsLocalhost(r))
}

func TestIsLocalhost_Remote(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.168.1.5:54321"
	assert.False(t, IsLocalhost(r))
}

// ── ExtractAPIKey Tests ─────────────────────────────────────

func TestExtractAPIKey_BearerHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer my-api-key-123")
	assert.Equal(t, "my-api-key-123", ExtractAPIKey(r))
}

func TestExtractAPIKey_QueryParam(t *testing.T) {
	r := httptest.NewRequest("GET", "/?api_key=my-key-456", nil)
	assert.Equal(t, "my-key-456", ExtractAPIKey(r))
}

func TestExtractAPIKey_None(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	assert.Equal(t, "", ExtractAPIKey(r))
}

func TestExtractAPIKey_HeaderPrecedence(t *testing.T) {
	r := httptest.NewRequest("GET", "/?api_key=query-key", nil)
	r.Header.Set("Authorization", "Bearer header-key")
	assert.Equal(t, "header-key", ExtractAPIKey(r))
}

// ── Middleware Tests ─────────────────────────────────────────

func newMiddlewareTest(t *testing.T) (*KeyStore, http.Handler) {
	t.Helper()
	ks := newTestKeyStore(t)
	handler := Middleware(ks)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	return ks, handler
}

func TestMiddleware_LocalhostBypass(t *testing.T) {
	_, handler := newMiddlewareTest(t)
	r := httptest.NewRequest("GET", "http://localhost/api/sessions", nil)
	r.RemoteAddr = "127.0.0.1:54321"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestMiddleware_ValidAPIKeyHeader(t *testing.T) {
	ks, handler := newMiddlewareTest(t)
	r := httptest.NewRequest("GET", "/api/sessions", nil)
	r.RemoteAddr = "192.168.1.5:54321"
	r.Header.Set("Authorization", "Bearer "+ks.Key())
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestMiddleware_ValidAPIKeyQueryParam(t *testing.T) {
	ks, handler := newMiddlewareTest(t)
	r := httptest.NewRequest("GET", "/?api_key="+ks.Key(), nil)
	r.RemoteAddr = "192.168.1.5:54321"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	// Should also set session cookie
	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == cookieName {
			found = true
			break
		}
	}
	assert.True(t, found, "session cookie should be set on query param auth")
}

func TestMiddleware_InvalidAPIKey(t *testing.T) {
	_, handler := newMiddlewareTest(t)
	r := httptest.NewRequest("GET", "/api/sessions", nil)
	r.RemoteAddr = "192.168.1.5:54321"
	r.Header.Set("Authorization", "Bearer wrong-key")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMiddleware_ValidSessionCookie(t *testing.T) {
	ks, handler := newMiddlewareTest(t)
	token := ks.CreateSession("192.168.1.5", "test")
	r := httptest.NewRequest("GET", "/api/sessions", nil)
	r.RemoteAddr = "192.168.1.5:54321"
	r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestMiddleware_ExpiredSessionCookie(t *testing.T) {
	ks, handler := newMiddlewareTest(t)
	token := ks.CreateSession("192.168.1.5", "test")
	// Expire the session
	ks.mu.Lock()
	ks.sessions[token].CreatedAt = time.Now().Add(-25 * time.Hour)
	ks.mu.Unlock()

	r := httptest.NewRequest("GET", "/api/sessions", nil)
	r.RemoteAddr = "192.168.1.5:54321"
	r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMiddleware_NoAuth_APIRedirectsTo401(t *testing.T) {
	_, handler := newMiddlewareTest(t)
	r := httptest.NewRequest("GET", "/api/sessions", nil)
	r.RemoteAddr = "192.168.1.5:54321"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMiddleware_NoAuth_PageRedirectsToAuth(t *testing.T) {
	_, handler := newMiddlewareTest(t)
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.168.1.5:54321"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusTemporaryRedirect, w.Code)
	assert.Equal(t, "/auth", w.Header().Get("Location"))
}

func TestMiddleware_AuthPageAccessible(t *testing.T) {
	_, handler := newMiddlewareTest(t)
	r := httptest.NewRequest("GET", "/auth", nil)
	r.RemoteAddr = "192.168.1.5:54321"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestMiddleware_StaticAssetsAccessible(t *testing.T) {
	_, handler := newMiddlewareTest(t)
	r := httptest.NewRequest("GET", "/static/style.css", nil)
	r.RemoteAddr = "192.168.1.5:54321"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestMiddleware_RateLimitBlocks(t *testing.T) {
	ks, handler := newMiddlewareTest(t)
	_ = ks
	ip := "10.0.0.99"
	for i := 0; i < rateLimitMax; i++ {
		r := httptest.NewRequest("GET", "/api/sessions", nil)
		r.RemoteAddr = ip + ":54321"
		r.Header.Set("Authorization", "Bearer wrong-key")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
	}
	// Next attempt should hit rate limit
	r := httptest.NewRequest("GET", "/api/sessions", nil)
	r.RemoteAddr = ip + ":54321"
	r.Header.Set("Authorization", "Bearer wrong-key")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
}

// ── Cookie Attributes Tests ─────────────────────────────────

func TestSetSessionCookie_Attributes(t *testing.T) {
	w := httptest.NewRecorder()
	SetSessionCookie(w, "test-token-123")
	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	c := cookies[0]
	assert.Equal(t, cookieName, c.Name)
	assert.Equal(t, "test-token-123", c.Value)
	assert.Equal(t, "/", c.Path)
	assert.True(t, c.HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
	assert.Equal(t, int(sessionMaxAge.Seconds()), c.MaxAge)
}
