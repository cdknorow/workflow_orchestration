package routes

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
)

// decodeJSON decodes JSON from the request body into v.
func decodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

// debugEnabled returns true when CORAL_DEBUG=1 is set.
var debugEnabled = sync.OnceValue(func() bool {
	return os.Getenv("CORAL_DEBUG") == "1"
})

// DebugRequestLogger returns middleware that logs session-related API calls
// when CORAL_DEBUG=1 is set. Logs method, path, and query params for
// /api/sessions/ and /ws/ endpoints.
func DebugRequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if debugEnabled() {
			path := r.URL.Path
			if strings.HasPrefix(path, "/api/sessions/") || strings.HasPrefix(path, "/ws/") {
				slog.Info("[debug] request", "method", r.Method, "path", path, "query", r.URL.RawQuery, "remote", r.RemoteAddr)
			}
		}
		next.ServeHTTP(w, r)
	})
}
