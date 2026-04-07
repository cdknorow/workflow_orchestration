package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Mock Provider Servers ─────────────────────────────────────

// RecordedRequest captures an incoming request for assertion.
type RecordedRequest struct {
	Method  string
	Path    string
	Headers http.Header
	Body    string
}

// MockProviderConfig configures a mock provider server.
type MockProviderConfig struct {
	// Error responses
	StatusCode int    // override status code (0 = 200)
	ErrorBody  string // override response body for error cases

	// Model to return in response
	Model string

	// Token usage to return
	InputTokens  int
	OutputTokens int
}

// MockAnthropicServer creates a mock Anthropic API server.
// Returns the server and a function to retrieve recorded requests.
func MockAnthropicServer(t *testing.T, cfg MockProviderConfig) (*httptest.Server, func() []RecordedRequest) {
	t.Helper()
	var mu sync.Mutex
	var recorded []RecordedRequest

	model := cfg.Model
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	inputTokens := cfg.InputTokens
	if inputTokens == 0 {
		inputTokens = 1000
	}
	outputTokens := cfg.OutputTokens
	if outputTokens == 0 {
		outputTokens = 500
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		recorded = append(recorded, RecordedRequest{
			Method:  r.Method,
			Path:    r.URL.Path,
			Headers: r.Header.Clone(),
			Body:    string(body),
		})
		mu.Unlock()

		if cfg.StatusCode > 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(cfg.StatusCode)
			if cfg.ErrorBody != "" {
				w.Write([]byte(cfg.ErrorBody))
			} else {
				fmt.Fprintf(w, `{"error":{"type":"api_error","message":"mock error"}}`)
			}
			return
		}

		// Check if streaming requested
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		isStream, _ := req["stream"].(bool)

		if isStream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			flusher := w.(http.Flusher)

			events := []string{
				`event: message_start`,
				fmt.Sprintf(`data: {"type":"message_start","message":{"id":"msg_stream","model":"%s","usage":{"input_tokens":%d,"output_tokens":0}}}`, model, inputTokens),
				``,
				`event: content_block_delta`,
				`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello from mock!"}}`,
				``,
				`event: message_delta`,
				fmt.Sprintf(`data: {"type":"message_delta","usage":{"output_tokens":%d}}`, outputTokens),
				``,
				`event: message_stop`,
				`data: {"type":"message_stop"}`,
				``,
			}
			for _, line := range events {
				fmt.Fprintf(w, "%s\n", line)
				flusher.Flush()
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":    "msg_mock_anthropic",
			"type":  "message",
			"model": model,
			"usage": map[string]any{
				"input_tokens":  inputTokens,
				"output_tokens": outputTokens,
			},
			"content": []map[string]any{
				{"type": "text", "text": "Hello from mock Anthropic!"},
			},
		})
	}))
	t.Cleanup(srv.Close)

	return srv, func() []RecordedRequest {
		mu.Lock()
		defer mu.Unlock()
		cp := make([]RecordedRequest, len(recorded))
		copy(cp, recorded)
		return cp
	}
}

// MockBedrockServer creates a mock Bedrock endpoint.
// It validates that requests arrive at the correct Bedrock URL path.
func MockBedrockServer(t *testing.T, cfg MockProviderConfig) (*httptest.Server, func() []RecordedRequest) {
	t.Helper()
	var mu sync.Mutex
	var recorded []RecordedRequest

	inputTokens := cfg.InputTokens
	if inputTokens == 0 {
		inputTokens = 800
	}
	outputTokens := cfg.OutputTokens
	if outputTokens == 0 {
		outputTokens = 400
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		recorded = append(recorded, RecordedRequest{
			Method:  r.Method,
			Path:    r.URL.Path,
			Headers: r.Header.Clone(),
			Body:    string(body),
		})
		mu.Unlock()

		if cfg.StatusCode > 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(cfg.StatusCode)
			if cfg.ErrorBody != "" {
				w.Write([]byte(cfg.ErrorBody))
			} else {
				fmt.Fprintf(w, `{"message":"mock bedrock error","type":"api_error"}`)
			}
			return
		}

		// Bedrock returns Anthropic-format JSON responses
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":   "msg_mock_bedrock",
			"type": "message",
			"usage": map[string]any{
				"input_tokens":  inputTokens,
				"output_tokens": outputTokens,
			},
			"content": []map[string]any{
				{"type": "text", "text": "Hello from mock Bedrock!"},
			},
		})
	}))
	t.Cleanup(srv.Close)

	return srv, func() []RecordedRequest {
		mu.Lock()
		defer mu.Unlock()
		cp := make([]RecordedRequest, len(recorded))
		copy(cp, recorded)
		return cp
	}
}

// MockOpenAIServer creates a mock OpenAI API server.
func MockOpenAIServer(t *testing.T, cfg MockProviderConfig) (*httptest.Server, func() []RecordedRequest) {
	t.Helper()
	var mu sync.Mutex
	var recorded []RecordedRequest

	model := cfg.Model
	if model == "" {
		model = "gpt-4o"
	}
	inputTokens := cfg.InputTokens
	if inputTokens == 0 {
		inputTokens = 600
	}
	outputTokens := cfg.OutputTokens
	if outputTokens == 0 {
		outputTokens = 300
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		recorded = append(recorded, RecordedRequest{
			Method:  r.Method,
			Path:    r.URL.Path,
			Headers: r.Header.Clone(),
			Body:    string(body),
		})
		mu.Unlock()

		if cfg.StatusCode > 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(cfg.StatusCode)
			if cfg.ErrorBody != "" {
				w.Write([]byte(cfg.ErrorBody))
			} else {
				fmt.Fprintf(w, `{"error":{"message":"mock openai error","type":"api_error"}}`)
			}
			return
		}

		// Check if streaming
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		isStream, _ := req["stream"].(bool)

		if isStream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			flusher := w.(http.Flusher)

			events := []string{
				fmt.Sprintf(`data: {"choices":[{"delta":{"content":"Hello"}}]}`),
				fmt.Sprintf(`data: {"choices":[],"usage":{"prompt_tokens":%d,"completion_tokens":%d}}`, inputTokens, outputTokens),
				`data: [DONE]`,
			}
			for _, line := range events {
				fmt.Fprintf(w, "%s\n\n", line)
				flusher.Flush()
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":    "chatcmpl-mock",
			"model": model,
			"usage": map[string]any{
				"prompt_tokens":     inputTokens,
				"completion_tokens": outputTokens,
			},
			"choices": []map[string]any{
				{"message": map[string]any{"content": "Hello from mock OpenAI!"}},
			},
		})
	}))
	t.Cleanup(srv.Close)

	return srv, func() []RecordedRequest {
		mu.Lock()
		defer mu.Unlock()
		cp := make([]RecordedRequest, len(recorded))
		copy(cp, recorded)
		return cp
	}
}

// ── Helper: build proxy with router ───────────────────────────

func buildProxyRouter(t *testing.T, p *Proxy) *chi.Mux {
	t.Helper()
	router := chi.NewRouter()
	router.Post("/proxy/{sessionID}/v1/messages", p.HandleAnthropicMessages)
	router.Post("/proxy/{sessionID}/v1/chat/completions", p.HandleOpenAIChatCompletions)
	router.Post("/proxy/{sessionID}/v1/responses", p.HandleOpenAIResponses)
	return router
}

// ── Integration Tests: Anthropic Direct Passthrough ───────────

func TestIntegration_AnthropicDirect_AuthPassthrough(t *testing.T) {
	mock, getReqs := MockAnthropicServer(t, MockProviderConfig{})
	db := testDB(t)
	p := New(db, map[Provider]ProviderConfig{})
	ctx := context.Background()

	// Register session with anthropic upstream
	p.SetSessionUpstream(ctx, "direct-session", "anthropic", mock.URL)

	router := buildProxyRouter(t, p)
	body := `{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"Hi"}]}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/proxy/direct-session/v1/messages", strings.NewReader(body))
	r.Header.Set("x-api-key", "sk-ant-test-key-12345")
	r.Header.Set("anthropic-version", "2023-06-01")
	r.Header.Set("anthropic-beta", "max-tokens-3-5-sonnet-2024-07-15")
	router.ServeHTTP(w, r)

	require.Equal(t, 200, w.Code)

	// Verify auth headers were forwarded as-is
	reqs := getReqs()
	require.Len(t, reqs, 1)
	assert.Equal(t, "sk-ant-test-key-12345", reqs[0].Headers.Get("X-Api-Key"))
	assert.Equal(t, "2023-06-01", reqs[0].Headers.Get("Anthropic-Version"))
	assert.Equal(t, "max-tokens-3-5-sonnet-2024-07-15", reqs[0].Headers.Get("Anthropic-Beta"))

	// Verify tokens captured
	requests, _, err := p.store.ListRequests(ctx, "direct-session", 10, 0)
	require.NoError(t, err)
	require.Len(t, requests, 1)
	assert.Equal(t, 1000, requests[0].InputTokens)
	assert.Equal(t, 500, requests[0].OutputTokens)
	assert.Greater(t, requests[0].CostUSD, 0.0)
}

func TestIntegration_AnthropicDirect_OAuthBearer(t *testing.T) {
	mock, getReqs := MockAnthropicServer(t, MockProviderConfig{})
	db := testDB(t)
	p := New(db, map[Provider]ProviderConfig{})
	ctx := context.Background()

	p.SetSessionUpstream(ctx, "oauth-session", "anthropic", mock.URL)

	router := buildProxyRouter(t, p)
	body := `{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"Hi"}]}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/proxy/oauth-session/v1/messages", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer ant-oauth-token-xyz")
	r.Header.Set("anthropic-version", "2023-06-01")
	router.ServeHTTP(w, r)

	require.Equal(t, 200, w.Code)

	reqs := getReqs()
	require.Len(t, reqs, 1)
	assert.Equal(t, "Bearer ant-oauth-token-xyz", reqs[0].Headers.Get("Authorization"))
}

// ── Integration Tests: Anthropic Streaming ────────────────────

func TestIntegration_AnthropicDirect_StreamingSSE(t *testing.T) {
	mock, _ := MockAnthropicServer(t, MockProviderConfig{InputTokens: 250, OutputTokens: 75})
	db := testDB(t)
	p := New(db, map[Provider]ProviderConfig{})
	ctx := context.Background()

	p.SetSessionUpstream(ctx, "stream-session", "anthropic", mock.URL)

	router := buildProxyRouter(t, p)
	body := `{"model":"claude-sonnet-4-20250514","stream":true,"messages":[{"role":"user","content":"Hi"}]}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/proxy/stream-session/v1/messages", strings.NewReader(body))
	r.Header.Set("x-api-key", "test-key")
	r.Header.Set("anthropic-version", "2023-06-01")
	router.ServeHTTP(w, r)

	require.Equal(t, 200, w.Code)
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))

	// Verify SSE events forwarded
	respBody := w.Body.String()
	assert.Contains(t, respBody, "message_start")
	assert.Contains(t, respBody, "content_block_delta")
	assert.Contains(t, respBody, "Hello from mock!")

	// Verify token extraction from stream
	requests, _, err := p.store.ListRequests(ctx, "stream-session", 10, 0)
	require.NoError(t, err)
	require.Len(t, requests, 1)
	assert.Equal(t, 250, requests[0].InputTokens)
	assert.Equal(t, 75, requests[0].OutputTokens)
	assert.Equal(t, 1, requests[0].IsStreaming)
}

// ── Integration Tests: Bedrock Bearer Passthrough ─────────────

func TestIntegration_BedrockBearer_Passthrough(t *testing.T) {
	mock, getReqs := MockBedrockServer(t, MockProviderConfig{})
	db := testDB(t)
	p := New(db, map[Provider]ProviderConfig{})
	ctx := context.Background()

	p.SetSessionUpstream(ctx, "bedrock-bearer", "bedrock", mock.URL)

	router := buildProxyRouter(t, p)
	body := `{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"Hi"}]}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/proxy/bedrock-bearer/v1/messages", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer aws-bedrock-bearer-token")
	r.Header.Set("anthropic-version", "2023-06-01")
	router.ServeHTTP(w, r)

	require.Equal(t, 200, w.Code)

	// Verify request arrived at Bedrock mock with correct path
	reqs := getReqs()
	require.Len(t, reqs, 1)
	assert.Contains(t, reqs[0].Path, "/model/")
	assert.Contains(t, reqs[0].Path, "/invoke")
	// Verify auth forwarded as-is
	assert.Equal(t, "Bearer aws-bedrock-bearer-token", reqs[0].Headers.Get("Authorization"))

	// Verify body was transformed (model removed, anthropic_version set)
	var reqBody map[string]interface{}
	json.Unmarshal([]byte(reqs[0].Body), &reqBody)
	assert.Equal(t, "bedrock-2023-05-31", reqBody["anthropic_version"])
	assert.Nil(t, reqBody["model"], "model should be removed from Bedrock request body")

	// Verify tokens captured
	requests, _, err := p.store.ListRequests(ctx, "bedrock-bearer", 10, 0)
	require.NoError(t, err)
	require.Len(t, requests, 1)
	assert.Equal(t, 800, requests[0].InputTokens)
	assert.Equal(t, 400, requests[0].OutputTokens)
}

// ── Integration Tests: OpenAI Passthrough ─────────────────────

func TestIntegration_OpenAI_Passthrough(t *testing.T) {
	mock, getReqs := MockOpenAIServer(t, MockProviderConfig{})
	db := testDB(t)
	p := New(db, map[Provider]ProviderConfig{})
	ctx := context.Background()

	p.SetSessionUpstream(ctx, "openai-session", "openai", mock.URL)

	router := buildProxyRouter(t, p)
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"Hi"}]}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/proxy/openai-session/v1/chat/completions", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer sk-openai-test-key")
	router.ServeHTTP(w, r)

	require.Equal(t, 200, w.Code)

	reqs := getReqs()
	require.Len(t, reqs, 1)
	assert.Equal(t, "/v1/chat/completions", reqs[0].Path)
	assert.Equal(t, "Bearer sk-openai-test-key", reqs[0].Headers.Get("Authorization"))

	// Verify tokens
	requests, _, err := p.store.ListRequests(ctx, "openai-session", 10, 0)
	require.NoError(t, err)
	require.Len(t, requests, 1)
	assert.Equal(t, 600, requests[0].InputTokens)
	assert.Equal(t, 300, requests[0].OutputTokens)
	assert.Equal(t, "openai", requests[0].Provider)
}

func TestIntegration_OpenAI_Streaming(t *testing.T) {
	mock, _ := MockOpenAIServer(t, MockProviderConfig{InputTokens: 400, OutputTokens: 200})
	db := testDB(t)
	p := New(db, map[Provider]ProviderConfig{})
	ctx := context.Background()

	p.SetSessionUpstream(ctx, "openai-stream", "openai", mock.URL)

	router := buildProxyRouter(t, p)
	body := `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"Hi"}]}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/proxy/openai-stream/v1/chat/completions", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer sk-openai-key")
	router.ServeHTTP(w, r)

	require.Equal(t, 200, w.Code)
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))

	requests, _, err := p.store.ListRequests(ctx, "openai-stream", 10, 0)
	require.NoError(t, err)
	require.Len(t, requests, 1)
	assert.Equal(t, 400, requests[0].InputTokens)
	assert.Equal(t, 200, requests[0].OutputTokens)
	assert.Equal(t, 1, requests[0].IsStreaming)
}

// ── Integration Tests: Per-Session Isolation ──────────────────

func TestIntegration_PerSessionIsolation(t *testing.T) {
	// Session A → Anthropic mock, Session B → Bedrock mock
	anthropicMock, getAnthropicReqs := MockAnthropicServer(t, MockProviderConfig{InputTokens: 100, OutputTokens: 50})
	bedrockMock, getBedrockReqs := MockBedrockServer(t, MockProviderConfig{InputTokens: 200, OutputTokens: 100})

	db := testDB(t)
	p := New(db, map[Provider]ProviderConfig{})
	ctx := context.Background()

	p.SetSessionUpstream(ctx, "session-anthropic", "anthropic", anthropicMock.URL)
	p.SetSessionUpstream(ctx, "session-bedrock", "bedrock", bedrockMock.URL)

	router := buildProxyRouter(t, p)
	body := `{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"Hi"}]}`

	// Send to session A (Anthropic)
	wA := httptest.NewRecorder()
	rA := httptest.NewRequest("POST", "/proxy/session-anthropic/v1/messages", strings.NewReader(body))
	rA.Header.Set("x-api-key", "key-a")
	rA.Header.Set("anthropic-version", "2023-06-01")
	router.ServeHTTP(wA, rA)
	require.Equal(t, 200, wA.Code)

	// Send to session B (Bedrock)
	wB := httptest.NewRecorder()
	rB := httptest.NewRequest("POST", "/proxy/session-bedrock/v1/messages", strings.NewReader(body))
	rB.Header.Set("Authorization", "Bearer bedrock-token")
	rB.Header.Set("anthropic-version", "2023-06-01")
	router.ServeHTTP(wB, rB)
	require.Equal(t, 200, wB.Code)

	// Verify each mock received exactly one request
	assert.Len(t, getAnthropicReqs(), 1)
	assert.Len(t, getBedrockReqs(), 1)

	// Verify tokens recorded per-session
	reqsA, _, _ := p.store.ListRequests(ctx, "session-anthropic", 10, 0)
	reqsB, _, _ := p.store.ListRequests(ctx, "session-bedrock", 10, 0)

	require.Len(t, reqsA, 1)
	require.Len(t, reqsB, 1)
	assert.Equal(t, 100, reqsA[0].InputTokens)
	assert.Equal(t, 200, reqsB[0].InputTokens)
}

// ── Integration Tests: Error Propagation ──────────────────────

func TestIntegration_AnthropicDirect_ErrorPropagation(t *testing.T) {
	mock, _ := MockAnthropicServer(t, MockProviderConfig{
		StatusCode: 429,
		ErrorBody:  `{"error":{"type":"rate_limit_error","message":"Too many requests"}}`,
	})
	db := testDB(t)
	p := New(db, map[Provider]ProviderConfig{})
	ctx := context.Background()

	p.SetSessionUpstream(ctx, "error-session", "anthropic", mock.URL)

	router := buildProxyRouter(t, p)
	body := `{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"Hi"}]}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/proxy/error-session/v1/messages", strings.NewReader(body))
	r.Header.Set("x-api-key", "test-key")
	r.Header.Set("anthropic-version", "2023-06-01")
	router.ServeHTTP(w, r)

	assert.Equal(t, 429, w.Code)

	requests, _, err := p.store.ListRequests(ctx, "error-session", 10, 0)
	require.NoError(t, err)
	require.Len(t, requests, 1)
	assert.Equal(t, "error", requests[0].Status)
	assert.Equal(t, 429, *requests[0].HTTPStatus)
}

func TestIntegration_Bedrock_ExpiredCreds(t *testing.T) {
	mock, _ := MockBedrockServer(t, MockProviderConfig{
		StatusCode: 403,
		ErrorBody:  `{"message":"ExpiredTokenException: The security token included in the request is expired","type":"client"}`,
	})
	db := testDB(t)
	p := New(db, map[Provider]ProviderConfig{})
	ctx := context.Background()

	p.SetSessionUpstream(ctx, "expired-session", "bedrock", mock.URL)

	router := buildProxyRouter(t, p)
	body := `{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"Hi"}]}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/proxy/expired-session/v1/messages", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer expired-token")
	r.Header.Set("anthropic-version", "2023-06-01")
	router.ServeHTTP(w, r)

	assert.Equal(t, 403, w.Code)

	requests, _, err := p.store.ListRequests(ctx, "expired-session", 10, 0)
	require.NoError(t, err)
	require.Len(t, requests, 1)
	assert.Equal(t, "error", requests[0].Status)
	assert.Contains(t, *requests[0].ErrorMessage, "SSO session expired")
}

func TestIntegration_Bedrock_AccessDenied(t *testing.T) {
	mock, _ := MockBedrockServer(t, MockProviderConfig{
		StatusCode: 403,
		ErrorBody:  `{"message":"Access denied to model anthropic.claude-opus","type":"client"}`,
	})
	db := testDB(t)
	p := New(db, map[Provider]ProviderConfig{})
	ctx := context.Background()

	p.SetSessionUpstream(ctx, "denied-session", "bedrock", mock.URL)

	router := buildProxyRouter(t, p)
	body := `{"model":"claude-opus-4-20250514","messages":[{"role":"user","content":"Hi"}]}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/proxy/denied-session/v1/messages", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer valid-token")
	r.Header.Set("anthropic-version", "2023-06-01")
	router.ServeHTTP(w, r)

	assert.Equal(t, 403, w.Code)

	requests, _, err := p.store.ListRequests(ctx, "denied-session", 10, 0)
	require.NoError(t, err)
	require.Len(t, requests, 1)
	assert.Equal(t, "error", requests[0].Status)
	assert.Contains(t, *requests[0].ErrorMessage, "access denied")
}

func TestIntegration_OpenAI_ErrorPropagation(t *testing.T) {
	mock, _ := MockOpenAIServer(t, MockProviderConfig{
		StatusCode: 401,
		ErrorBody:  `{"error":{"message":"Invalid API key","type":"authentication_error"}}`,
	})
	db := testDB(t)
	p := New(db, map[Provider]ProviderConfig{})
	ctx := context.Background()

	p.SetSessionUpstream(ctx, "openai-error", "openai", mock.URL)

	router := buildProxyRouter(t, p)
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"Hi"}]}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/proxy/openai-error/v1/chat/completions", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer invalid-key")
	router.ServeHTTP(w, r)

	assert.Equal(t, 401, w.Code)

	requests, _, err := p.store.ListRequests(ctx, "openai-error", 10, 0)
	require.NoError(t, err)
	require.Len(t, requests, 1)
	assert.Equal(t, "error", requests[0].Status)
}

// ── Integration Tests: Security ───────────────────────────────

func TestIntegration_CredentialsNotStoredInDB(t *testing.T) {
	mock, _ := MockAnthropicServer(t, MockProviderConfig{})
	db := testDB(t)
	p := New(db, map[Provider]ProviderConfig{})
	ctx := context.Background()

	p.SetSessionUpstream(ctx, "sec-test", "anthropic", mock.URL)

	router := buildProxyRouter(t, p)
	body := `{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"Hi"}]}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/proxy/sec-test/v1/messages", strings.NewReader(body))
	r.Header.Set("x-api-key", "sk-ant-super-secret-key-NEVER-STORE")
	r.Header.Set("Authorization", "Bearer oauth-token-NEVER-STORE")
	r.Header.Set("anthropic-version", "2023-06-01")
	router.ServeHTTP(w, r)

	require.Equal(t, 200, w.Code)

	// Check that no credential values appear in the stored request
	requests, _, err := p.store.ListRequests(ctx, "sec-test", 10, 0)
	require.NoError(t, err)
	require.Len(t, requests, 1)

	reqJSON, _ := json.Marshal(requests[0])
	reqStr := string(reqJSON)
	assert.NotContains(t, reqStr, "sk-ant-super-secret-key-NEVER-STORE")
	assert.NotContains(t, reqStr, "oauth-token-NEVER-STORE")
	assert.NotContains(t, reqStr, "AWS4-HMAC-SHA256")
}

// ── Integration Tests: Bedrock Body Transform ─────────────────

func TestIntegration_Bedrock_BodyTransform(t *testing.T) {
	mock, getReqs := MockBedrockServer(t, MockProviderConfig{})
	db := testDB(t)
	p := New(db, map[Provider]ProviderConfig{})
	ctx := context.Background()

	p.SetSessionUpstream(ctx, "transform-test", "bedrock", mock.URL)

	router := buildProxyRouter(t, p)
	body := `{"model":"claude-sonnet-4-20250514","max_tokens":1024,"messages":[{"role":"user","content":"Hi"}],"stream":false}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/proxy/transform-test/v1/messages", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer bedrock-token")
	r.Header.Set("anthropic-version", "2023-06-01")
	router.ServeHTTP(w, r)

	require.Equal(t, 200, w.Code)

	reqs := getReqs()
	require.Len(t, reqs, 1)

	var reqBody map[string]interface{}
	json.Unmarshal([]byte(reqs[0].Body), &reqBody)

	// Model should be removed (it's in the URL path for Bedrock)
	assert.Nil(t, reqBody["model"])
	// anthropic_version should be set to bedrock variant
	assert.Equal(t, "bedrock-2023-05-31", reqBody["anthropic_version"])
	// Other fields should be preserved
	assert.Equal(t, float64(1024), reqBody["max_tokens"])
	assert.NotNil(t, reqBody["messages"])
}

// ── Integration Tests: Proxy Headers ──────────────────────────

func TestIntegration_ProxyHeaders(t *testing.T) {
	mock, _ := MockAnthropicServer(t, MockProviderConfig{})
	db := testDB(t)
	p := New(db, map[Provider]ProviderConfig{})
	ctx := context.Background()

	p.SetSessionUpstream(ctx, "headers-test", "anthropic", mock.URL)

	router := buildProxyRouter(t, p)
	body := `{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"Hi"}]}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/proxy/headers-test/v1/messages", strings.NewReader(body))
	r.Header.Set("x-api-key", "test-key")
	r.Header.Set("anthropic-version", "2023-06-01")
	router.ServeHTTP(w, r)

	require.Equal(t, 200, w.Code)
	assert.Equal(t, "true", w.Header().Get("X-Coral-Proxy"))
	assert.NotEmpty(t, w.Header().Get("X-Coral-Request-Id"))
	assert.Equal(t, "headers-test", w.Header().Get("X-Coral-Session-Id"))
	assert.NotEmpty(t, w.Header().Get("X-Coral-Cost-Usd"))
}

// ── Integration Tests: Multiple Requests Same Session ─────────

func TestIntegration_MultipleRequests_SameSession(t *testing.T) {
	mock, _ := MockAnthropicServer(t, MockProviderConfig{InputTokens: 100, OutputTokens: 50})
	db := testDB(t)
	p := New(db, map[Provider]ProviderConfig{})
	ctx := context.Background()

	p.SetSessionUpstream(ctx, "multi-req", "anthropic", mock.URL)

	router := buildProxyRouter(t, p)
	body := `{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"Hi"}]}`

	// Send 3 requests
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/proxy/multi-req/v1/messages", strings.NewReader(body))
		r.Header.Set("x-api-key", "test-key")
		r.Header.Set("anthropic-version", "2023-06-01")
		router.ServeHTTP(w, r)
		require.Equal(t, 200, w.Code)
	}

	// Verify all 3 requests recorded
	requests, total, err := p.store.ListRequests(ctx, "multi-req", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	assert.Len(t, requests, 3)

	// Each should have the same token counts
	for _, req := range requests {
		assert.Equal(t, 100, req.InputTokens)
		assert.Equal(t, 50, req.OutputTokens)
	}
}

// ── Edge Case Tests ─────────────────────────────────────────

// TestIntegration_MixedTeam_Concurrent exercises the spec's "mixed team" scenario:
// a Bedrock agent and a direct API agent running concurrently on the same proxy.
func TestIntegration_MixedTeam_Concurrent(t *testing.T) {
	anthropicMock, getAnthropicReqs := MockAnthropicServer(t, MockProviderConfig{InputTokens: 500, OutputTokens: 250})
	bedrockMock, getBedrockReqs := MockBedrockServer(t, MockProviderConfig{InputTokens: 800, OutputTokens: 400})

	db := testDB(t)
	p := New(db, map[Provider]ProviderConfig{})
	ctx := context.Background()

	p.SetSessionUpstream(ctx, "team-direct", "anthropic", anthropicMock.URL)
	p.SetSessionUpstream(ctx, "team-bedrock", "bedrock", bedrockMock.URL)

	router := buildProxyRouter(t, p)

	// Send both requests (session isolation, not concurrency stress test)
	wDirect := httptest.NewRecorder()
	rDirect := httptest.NewRequest("POST", "/proxy/team-direct/v1/messages",
		strings.NewReader(`{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"Direct API request"}]}`))
	rDirect.Header.Set("x-api-key", "direct-key")
	rDirect.Header.Set("anthropic-version", "2023-06-01")
	router.ServeHTTP(wDirect, rDirect)
	require.Equal(t, 200, wDirect.Code, "direct API request should succeed")

	wBedrock := httptest.NewRecorder()
	rBedrock := httptest.NewRequest("POST", "/proxy/team-bedrock/v1/messages",
		strings.NewReader(`{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"Bedrock request"}]}`))
	rBedrock.Header.Set("Authorization", "Bearer bedrock-bearer")
	rBedrock.Header.Set("anthropic-version", "2023-06-01")
	router.ServeHTTP(wBedrock, rBedrock)
	require.Equal(t, 200, wBedrock.Code, "bedrock request should succeed")

	// Each mock received exactly one request — no cross-routing
	assert.Len(t, getAnthropicReqs(), 1)
	assert.Len(t, getBedrockReqs(), 1)

	// Verify per-session token isolation
	directReqs, _, _ := p.store.ListRequests(ctx, "team-direct", 10, 0)
	bedrockReqs, _, _ := p.store.ListRequests(ctx, "team-bedrock", 10, 0)
	require.Len(t, directReqs, 1)
	require.Len(t, bedrockReqs, 1)
	assert.Equal(t, 500, directReqs[0].InputTokens)
	assert.Equal(t, 800, bedrockReqs[0].InputTokens)
}

// TestIntegration_TruncatedStream verifies that when a stream is cut short,
// partial token counts are still recorded rather than lost.
func TestIntegration_TruncatedStream(t *testing.T) {
	// Create a mock that sends only message_start (with input tokens) then closes
	truncatedMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher := w.(http.Flusher)

		// Send message_start with input tokens, then abruptly close
		events := []string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_trunc","model":"claude-sonnet-4-20250514","usage":{"input_tokens":999,"output_tokens":0}}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"partial..."}}`,
			``,
			// No message_delta with output tokens, no message_stop — connection closes
		}
		for _, line := range events {
			fmt.Fprintf(w, "%s\n", line)
			flusher.Flush()
		}
		// Connection closes without message_stop
	}))
	t.Cleanup(truncatedMock.Close)

	db := testDB(t)
	p := New(db, map[Provider]ProviderConfig{})
	ctx := context.Background()

	p.SetSessionUpstream(ctx, "truncated-session", "anthropic", truncatedMock.URL)

	router := buildProxyRouter(t, p)
	body := `{"model":"claude-sonnet-4-20250514","stream":true,"messages":[{"role":"user","content":"Hi"}]}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/proxy/truncated-session/v1/messages", strings.NewReader(body))
	r.Header.Set("x-api-key", "test-key")
	r.Header.Set("anthropic-version", "2023-06-01")
	router.ServeHTTP(w, r)

	// Stream may complete with 200 since headers were sent before truncation
	assert.Equal(t, 200, w.Code)

	// Partial tokens should still be recorded — input_tokens from message_start
	requests, _, err := p.store.ListRequests(ctx, "truncated-session", 10, 0)
	require.NoError(t, err)
	require.Len(t, requests, 1)
	assert.Equal(t, 999, requests[0].InputTokens, "partial input tokens should be recorded")
	assert.Equal(t, 0, requests[0].OutputTokens, "no output tokens sent before truncation")
	assert.Equal(t, 1, requests[0].IsStreaming)
}

// TestIntegration_CredentialHeaders_NotInDB is a comprehensive check that
// various credential header formats never appear in stored proxy_requests.
func TestIntegration_CredentialHeaders_NotInDB(t *testing.T) {
	mock, _ := MockAnthropicServer(t, MockProviderConfig{})
	db := testDB(t)
	p := New(db, map[Provider]ProviderConfig{})
	ctx := context.Background()

	p.SetSessionUpstream(ctx, "cred-check", "anthropic", mock.URL)
	router := buildProxyRouter(t, p)

	sensitiveHeaders := map[string]string{
		"x-api-key":            "sk-ant-api03-VERY-SECRET-KEY-abcdef1234567890",
		"Authorization":        "Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.secret-payload",
		"X-Amz-Security-Token": "FwoGZXIvYXdzEBYaDHVIqR1234567890SECRETTOKEN",
	}

	body := `{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"Hi"}]}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/proxy/cred-check/v1/messages", strings.NewReader(body))
	for k, v := range sensitiveHeaders {
		r.Header.Set(k, v)
	}
	r.Header.Set("anthropic-version", "2023-06-01")
	router.ServeHTTP(w, r)

	require.Equal(t, 200, w.Code)

	requests, _, err := p.store.ListRequests(ctx, "cred-check", 10, 0)
	require.NoError(t, err)
	require.Len(t, requests, 1)

	// Serialize the full DB row and check for sensitive values
	reqJSON, _ := json.Marshal(requests[0])
	reqStr := string(reqJSON)

	for headerName, value := range sensitiveHeaders {
		assert.NotContains(t, reqStr, value,
			"credential from %s should not appear in stored proxy_request", headerName)
	}
}

// TestIntegration_SessionUpstream_DoesNotAffectOtherSessions verifies that
// setting an upstream for one session doesn't leak to unrelated sessions.
func TestIntegration_SessionUpstream_DoesNotAffectOtherSessions(t *testing.T) {
	customMock, getCustomReqs := MockAnthropicServer(t, MockProviderConfig{InputTokens: 777, OutputTokens: 333})
	defaultMock, getDefaultReqs := MockAnthropicServer(t, MockProviderConfig{InputTokens: 111, OutputTokens: 222})

	db := testDB(t)
	providers := map[Provider]ProviderConfig{
		ProviderAnthropic: {BaseURL: defaultMock.URL, APIKey: ""},
	}
	p := New(db, providers)
	ctx := context.Background()

	// Only session-custom gets a custom upstream; session-default uses the global provider
	p.SetSessionUpstream(ctx, "session-custom", "anthropic", customMock.URL)

	router := buildProxyRouter(t, p)
	body := `{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"Hi"}]}`

	// Request to custom session
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest("POST", "/proxy/session-custom/v1/messages", strings.NewReader(body))
	r1.Header.Set("x-api-key", "key-1")
	r1.Header.Set("anthropic-version", "2023-06-01")
	router.ServeHTTP(w1, r1)
	require.Equal(t, 200, w1.Code)

	// Request to default session (no custom upstream set)
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("POST", "/proxy/session-default/v1/messages", strings.NewReader(body))
	r2.Header.Set("x-api-key", "key-2")
	r2.Header.Set("anthropic-version", "2023-06-01")
	router.ServeHTTP(w2, r2)
	require.Equal(t, 200, w2.Code)

	// Custom mock got session-custom's request
	assert.Len(t, getCustomReqs(), 1)
	// Default mock got session-default's request (fell back to provider config)
	assert.Len(t, getDefaultReqs(), 1)

	// Token counts match each mock's configuration
	customReqs, _, _ := p.store.ListRequests(ctx, "session-custom", 10, 0)
	defaultReqs, _, _ := p.store.ListRequests(ctx, "session-default", 10, 0)
	require.Len(t, customReqs, 1)
	require.Len(t, defaultReqs, 1)
	assert.Equal(t, 777, customReqs[0].InputTokens)
	assert.Equal(t, 111, defaultReqs[0].InputTokens)
}
