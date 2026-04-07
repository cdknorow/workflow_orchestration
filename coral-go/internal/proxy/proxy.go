// Package proxy implements an HTTP proxy for LLM API calls with SSE streaming
// support and per-request cost tracking.
package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// TokenUsageRecord is the data passed to the token usage recorder callback.
type TokenUsageRecord struct {
	SessionID        string
	AgentName        string
	AgentType        string
	Model            string
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	CostUSD          float64
	RecordedAt       string
	Source           string // "proxy"
}

// TokenUsageRecorderFn writes a token usage record to the unified token_usage table.
type TokenUsageRecorderFn func(ctx context.Context, record *TokenUsageRecord) error

// Proxy is the core LLM proxy handler.
type Proxy struct {
	store         *Store
	providers     map[Provider]ProviderConfig
	client        *http.Client
	events        *EventHub
	startedAt     time.Time
	recordTokenFn TokenUsageRecorderFn
}

// New creates a new Proxy with the given database and provider configs.
func New(db *sqlx.DB, providers map[Provider]ProviderConfig) *Proxy {
	return &Proxy{
		store:     NewStore(db),
		providers: providers,
		client: &http.Client{
			Timeout: 10 * time.Minute, // LLM requests can be long
		},
		events:    NewEventHub(),
		startedAt: time.Now(),
	}
}

// Store returns the proxy's store for use by dashboard API handlers.
func (p *Proxy) Store() *Store {
	return p.store
}

// Events returns the proxy's event hub for WebSocket subscribers.
func (p *Proxy) Events() *EventHub {
	return p.events
}

// SetSessionUpstream stores the upstream config for a proxy session.
func (p *Proxy) SetSessionUpstream(ctx context.Context, sessionID, provider, upstreamURL string) error {
	return p.store.SetSessionUpstream(ctx, sessionID, provider, upstreamURL)
}

// GetSessionUpstream retrieves the upstream URL for a session, falling back to
// the provider config if no per-session upstream is set.
func (p *Proxy) GetSessionUpstream(ctx context.Context, sessionID string, provider Provider) string {
	if u, err := p.store.GetSessionUpstream(ctx, sessionID); err == nil && u.UpstreamURL != "" {
		return u.UpstreamURL
	}
	if cfg, ok := p.providers[provider]; ok && cfg.BaseURL != "" {
		return cfg.BaseURL
	}
	switch provider {
	case ProviderOpenAI:
		return "https://api.openai.com"
	default:
		return "https://api.anthropic.com"
	}
}

// SetTokenUsageRecorder sets the callback for writing to the unified token_usage table.
func (p *Proxy) SetTokenUsageRecorder(fn TokenUsageRecorderFn) {
	p.recordTokenFn = fn
}

// recordTokenUsage writes a token usage record if the recorder is configured.
func (p *Proxy) recordTokenUsage(ctx context.Context, sessionID string, provider Provider, model string, usage TokenUsage, breakdown CostBreakdown) {
	if p.recordTokenFn == nil {
		return
	}

	record := &TokenUsageRecord{
		SessionID:        sessionID,
		Model:            model,
		InputTokens:      usage.InputTokens,
		OutputTokens:     usage.OutputTokens,
		CacheReadTokens:  usage.CacheReadTokens,
		CacheWriteTokens: usage.CacheWriteTokens,
		CostUSD:          breakdown.TotalCostUSD,
		RecordedAt:       time.Now().UTC().Format(time.RFC3339),
		Source:           "proxy",
	}

	// Look up agent info from live_sessions
	if p.store != nil {
		var meta struct {
			AgentName *string `db:"agent_name"`
			AgentType *string `db:"agent_type"`
		}
		if err := p.store.db.GetContext(ctx, &meta,
			"SELECT agent_name, agent_type FROM live_sessions WHERE session_id = ?", sessionID); err == nil {
			if meta.AgentName != nil {
				record.AgentName = *meta.AgentName
			}
			if meta.AgentType != nil {
				record.AgentType = *meta.AgentType
			}
		}
	}

	if err := p.recordTokenFn(ctx, record); err != nil {
		slog.Debug("[proxy] failed to record token usage", "session_id", sessionID, "error", err)
	}
}

// Health returns proxy health status.
// GET /proxy/health
func (p *Proxy) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":         "ok",
		"uptime_seconds": int(time.Since(p.startedAt).Seconds()),
	})
}

// HandleAnthropicMessages proxies Anthropic /v1/messages requests.
// POST /proxy/{sessionID}/v1/messages
func (p *Proxy) HandleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")

	// Check if this session uses Bedrock — delegate to Bedrock handler
	if p.store != nil {
		if meta, err := p.store.GetSessionUpstream(r.Context(), sessionID); err == nil && meta != nil && meta.Provider == "bedrock" {
			p.HandleBedrockMessages(w, r)
			return
		}
	}

	cfg, ok := p.providers[ProviderAnthropic]

	// Determine auth mode: prefer client auth when present (CLI agents bring
	// their own key via x-api-key or Authorization: Bearer). Fall back to
	// server-side API key only when the client provides no auth headers.
	hasClientAuth := r.Header.Get("x-api-key") != "" || r.Header.Get("Authorization") != ""
	usePassthroughAuth := !ok || cfg.APIKey == "" || hasClientAuth
	if usePassthroughAuth {
		// Ensure we have at least one auth header from the agent
		hasAuth := r.Header.Get("x-api-key") != "" || r.Header.Get("Authorization") != ""
		if !hasAuth {
			http.Error(w, `{"error":"no Anthropic auth provided — set ANTHROPIC_API_KEY or use Claude Code CLI"}`, http.StatusBadGateway)
			return
		}
		if cfg.BaseURL == "" {
			cfg.BaseURL = "https://api.anthropic.com"
		}
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 50*1024*1024))
	if err != nil {
		http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadRequest)
		return
	}

	var meta struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	json.Unmarshal(body, &meta)

	reqID := uuid.New().String()
	if err := p.store.CreateRequest(r.Context(), reqID, sessionID, ProviderAnthropic, meta.Model, meta.Stream); err != nil {
		slog.Error("[proxy] failed to create request record", "error", err, "request_id", reqID)
	}
	p.events.PublishStarted(reqID, sessionID, ProviderAnthropic, meta.Model, meta.Stream)

	// Use per-session upstream URL if available, otherwise fall back to provider config
	baseURL := p.GetSessionUpstream(r.Context(), sessionID, ProviderAnthropic)
	if baseURL == "" {
		baseURL = cfg.BaseURL
	}
	upstreamURL := strings.TrimRight(baseURL, "/") + "/v1/messages"
	upstreamReq, err := http.NewRequestWithContext(r.Context(), "POST", upstreamURL, bytes.NewReader(body))
	if err != nil {
		p.completeWithError(r.Context(), reqID, sessionID, meta.Model, 0, "failed to create upstream request: "+err.Error())
		http.Error(w, `{"error":"proxy internal error"}`, http.StatusInternalServerError)
		return
	}

	upstreamReq.Header.Set("content-type", "application/json")

	if usePassthroughAuth {
		// Forward all auth headers from the agent (supports both API key and OAuth)
		if v := r.Header.Get("x-api-key"); v != "" {
			upstreamReq.Header.Set("x-api-key", v)
		}
		if v := r.Header.Get("Authorization"); v != "" {
			upstreamReq.Header.Set("Authorization", v)
		}
	} else {
		upstreamReq.Header.Set("x-api-key", cfg.APIKey)
	}

	if v := r.Header.Get("anthropic-version"); v != "" {
		upstreamReq.Header.Set("anthropic-version", v)
	} else {
		upstreamReq.Header.Set("anthropic-version", "2023-06-01")
	}
	// Forward all Anthropic-specific headers (beta, org, etc.)
	forwardProviderHeaders(r.Header, upstreamReq.Header, "anthropic-")

	if meta.Stream {
		p.handleAnthropicSSE(w, upstreamReq, reqID, sessionID, meta.Model)
	} else {
		p.handleAnthropicJSON(w, upstreamReq, reqID, sessionID, meta.Model)
	}
}

// HandleOpenAIChatCompletions proxies OpenAI /v1/chat/completions requests.
// POST /proxy/{sessionID}/v1/chat/completions
func (p *Proxy) HandleOpenAIChatCompletions(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	cfg, ok := p.providers[ProviderOpenAI]

	// Prefer client auth when present (CLI agents like Codex bring their own key).
	// Fall back to server key only when the client provides no auth header.
	usePassthroughAuth := !ok || cfg.APIKey == "" || r.Header.Get("Authorization") != ""
	if usePassthroughAuth {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, `{"error":"no OpenAI auth provided — set OPENAI_API_KEY or pass Authorization header"}`, http.StatusBadGateway)
			return
		}
		if cfg.BaseURL == "" {
			cfg.BaseURL = "https://api.openai.com"
		}
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 50*1024*1024))
	if err != nil {
		http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadRequest)
		return
	}

	var meta struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	json.Unmarshal(body, &meta)

	reqID := uuid.New().String()
	if err := p.store.CreateRequest(r.Context(), reqID, sessionID, ProviderOpenAI, meta.Model, meta.Stream); err != nil {
		slog.Error("[proxy] failed to create request record", "error", err, "request_id", reqID)
	}
	p.events.PublishStarted(reqID, sessionID, ProviderOpenAI, meta.Model, meta.Stream)

	// Use per-session upstream URL if available, otherwise fall back to provider config
	baseURL := p.GetSessionUpstream(r.Context(), sessionID, ProviderOpenAI)
	if baseURL == "" {
		baseURL = cfg.BaseURL
	}
	upstreamURL := strings.TrimRight(baseURL, "/") + "/v1/chat/completions"
	upstreamReq, err := http.NewRequestWithContext(r.Context(), "POST", upstreamURL, bytes.NewReader(body))
	if err != nil {
		p.completeWithError(r.Context(), reqID, sessionID, meta.Model, 0, "failed to create upstream request: "+err.Error())
		http.Error(w, `{"error":"proxy internal error"}`, http.StatusInternalServerError)
		return
	}

	upstreamReq.Header.Set("Content-Type", "application/json")
	if usePassthroughAuth {
		upstreamReq.Header.Set("Authorization", r.Header.Get("Authorization"))
	} else {
		upstreamReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	// Forward all OpenAI-specific headers (org, project, beta — needed for OAuth scoping)
	forwardProviderHeaders(r.Header, upstreamReq.Header, "openai-")

	if meta.Stream {
		p.handleOpenAISSE(w, upstreamReq, reqID, sessionID, meta.Model)
	} else {
		p.handleOpenAIJSON(w, upstreamReq, reqID, sessionID, meta.Model)
	}
}

// HandleOpenAIResponses proxies OpenAI Responses API requests.
// POST /proxy/{sessionID}/v1/responses
func (p *Proxy) HandleOpenAIResponses(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	cfg, ok := p.providers[ProviderOpenAI]

	// Prefer client auth when present (CLI agents like Codex bring their own key).
	// Fall back to server key only when the client provides no auth header.
	usePassthroughAuth := !ok || cfg.APIKey == "" || r.Header.Get("Authorization") != ""
	if usePassthroughAuth {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, `{"error":"no OpenAI auth provided — set OPENAI_API_KEY or pass Authorization header"}`, http.StatusBadGateway)
			return
		}
		if cfg.BaseURL == "" {
			cfg.BaseURL = "https://api.openai.com"
		}
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 50*1024*1024))
	if err != nil {
		http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadRequest)
		return
	}

	var meta struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	json.Unmarshal(body, &meta)

	reqID := uuid.New().String()
	if err := p.store.CreateRequest(r.Context(), reqID, sessionID, ProviderOpenAI, meta.Model, meta.Stream); err != nil {
		slog.Error("[proxy] failed to create request record", "error", err, "request_id", reqID)
	}
	p.events.PublishStarted(reqID, sessionID, ProviderOpenAI, meta.Model, meta.Stream)

	// Detect ChatGPT OAuth tokens (JWT format) and route to the ChatGPT
	// backend instead of api.openai.com. ChatGPT OAuth tokens only work
	// with chatgpt.com/backend-api/codex, not the public API.
	// Note: ChatGPT backend uses /responses (no /v1/ prefix).
	isChatGPT := usePassthroughAuth && isChatGPTOAuthToken(r.Header.Get("Authorization"))
	var upstreamURL string
	if isChatGPT {
		upstreamURL = "https://chatgpt.com/backend-api/codex/responses"
	} else {
		// Use per-session upstream URL if available
		respBaseURL := p.GetSessionUpstream(r.Context(), sessionID, ProviderOpenAI)
		if respBaseURL == "" {
			respBaseURL = cfg.BaseURL
		}
		upstreamURL = strings.TrimRight(respBaseURL, "/") + "/v1/responses"
	}

	upstreamReq, err := http.NewRequestWithContext(r.Context(), "POST", upstreamURL, bytes.NewReader(body))
	if err != nil {
		p.completeWithError(r.Context(), reqID, sessionID, meta.Model, 0, "failed to create upstream request: "+err.Error())
		http.Error(w, `{"error":"proxy internal error"}`, http.StatusInternalServerError)
		return
	}

	upstreamReq.Header.Set("Content-Type", "application/json")
	if usePassthroughAuth {
		upstreamReq.Header.Set("Authorization", r.Header.Get("Authorization"))
	} else {
		upstreamReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	// Forward all OpenAI-specific headers (org, project, beta — needed for OAuth scoping)
	forwardProviderHeaders(r.Header, upstreamReq.Header, "openai-")
	// Forward ChatGPT account ID header if present (needed for OAuth routing)
	if v := r.Header.Get("ChatGPT-Account-Id"); v != "" {
		upstreamReq.Header.Set("ChatGPT-Account-Id", v)
	}

	if meta.Stream {
		p.handleOpenAIResponsesSSE(w, upstreamReq, reqID, sessionID, meta.Model)
	} else {
		p.handleOpenAIResponsesJSON(w, upstreamReq, reqID, sessionID, meta.Model)
	}
}

// ── OpenAI Responses API handlers ─────────────────────────────────────

func (p *Proxy) handleOpenAIResponsesJSON(w http.ResponseWriter, req *http.Request, reqID, sessionID, model string) {
	resp, err := p.client.Do(req)
	if err != nil {
		p.completeWithError(req.Context(), reqID, sessionID, model, 0, "upstream request failed: "+err.Error())
		http.Error(w, `{"error":"upstream request failed"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.completeWithError(req.Context(), reqID, sessionID, model, resp.StatusCode, "failed to read upstream response")
		http.Error(w, `{"error":"failed to read upstream response"}`, http.StatusBadGateway)
		return
	}

	usage := extractOpenAIResponsesUsage(body)
	breakdown := CalculateCostBreakdown(model, usage)

	status := "success"
	errMsg := ""
	if resp.StatusCode >= 400 {
		status = "error"
		errMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	p.store.CompleteRequest(req.Context(), reqID, usage, breakdown, resp.StatusCode, status, errMsg)
	if status == "error" {
		p.events.PublishError(reqID, sessionID, model, resp.StatusCode, errMsg)
	} else {
		p.events.PublishCompleted(reqID, sessionID, model, usage, breakdown.TotalCostUSD, 0, resp.StatusCode)
		p.recordTokenUsage(req.Context(), sessionID, ProviderOpenAI, model, usage, breakdown)
	}

	p.setProxyHeaders(w, reqID, sessionID, breakdown.TotalCostUSD)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

func (p *Proxy) handleOpenAIResponsesSSE(w http.ResponseWriter, req *http.Request, reqID, sessionID, model string) {
	resp, err := p.client.Do(req)
	if err != nil {
		p.completeWithError(req.Context(), reqID, sessionID, model, 0, "upstream request failed: "+err.Error())
		http.Error(w, `{"error":"upstream request failed"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		usage := extractOpenAIResponsesUsage(body)
		breakdown := CalculateCostBreakdown(model, usage)
		errMsg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		p.store.CompleteRequest(req.Context(), reqID, usage, breakdown, resp.StatusCode, "error", errMsg)
		p.events.PublishError(reqID, sessionID, model, resp.StatusCode, errMsg)
		p.setProxyHeaders(w, reqID, sessionID, breakdown.TotalCostUSD)
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		p.completeWithError(req.Context(), reqID, sessionID, model, 0, "streaming not supported")
		http.Error(w, `{"error":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	p.setProxyHeaders(w, reqID, sessionID, 0)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	var usage TokenUsage
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "data: ") {
			data := line[6:]
			usage = parseOpenAIResponsesSSEChunk(data, usage)
		}

		fmt.Fprintf(w, "%s\n", line)
		flusher.Flush()
	}

	completeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := scanner.Err(); err != nil {
		p.completeWithError(completeCtx, reqID, sessionID, model, resp.StatusCode, "stream read error: "+err.Error())
		return
	}

	breakdown := CalculateCostBreakdown(model, usage)
	p.store.CompleteRequest(completeCtx, reqID, usage, breakdown, resp.StatusCode, "success", "")
	p.events.PublishCompleted(reqID, sessionID, model, usage, breakdown.TotalCostUSD, 0, resp.StatusCode)
	p.recordTokenUsage(completeCtx, sessionID, ProviderOpenAI, model, usage, breakdown)

	if debugProxy() {
		slog.Info("[proxy] openai responses SSE complete",
			"request_id", reqID, "model", model,
			"input_tokens", usage.InputTokens, "output_tokens", usage.OutputTokens,
			"cost_usd", fmt.Sprintf("%.6f", breakdown.TotalCostUSD))
	}
}

// ── Anthropic response handlers ────────────────────────────────────────

func (p *Proxy) handleAnthropicJSON(w http.ResponseWriter, req *http.Request, reqID, sessionID, model string) {
	resp, err := p.client.Do(req)
	if err != nil {
		p.completeWithError(req.Context(), reqID, sessionID, model, 0, "upstream request failed: "+err.Error())
		http.Error(w, `{"error":"upstream request failed"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.completeWithError(req.Context(), reqID, sessionID, model, resp.StatusCode, "failed to read upstream response")
		http.Error(w, `{"error":"failed to read upstream response"}`, http.StatusBadGateway)
		return
	}

	// Extract usage from response
	usage := extractAnthropicUsage(body)
	breakdown := CalculateCostBreakdown(model, usage)

	status := "success"
	errMsg := ""
	if resp.StatusCode >= 400 {
		status = "error"
		errMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	p.store.CompleteRequest(req.Context(), reqID, usage, breakdown, resp.StatusCode, status, errMsg)
	if status == "error" {
		p.events.PublishError(reqID, sessionID, model, resp.StatusCode, errMsg)
	} else {
		p.events.PublishCompleted(reqID, sessionID, model, usage, breakdown.TotalCostUSD, 0, resp.StatusCode)
		p.recordTokenUsage(req.Context(), sessionID, ProviderAnthropic, model, usage, breakdown)
	}

	// Forward response to agent with proxy headers
	p.setProxyHeaders(w, reqID, sessionID, breakdown.TotalCostUSD)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

func (p *Proxy) handleAnthropicSSE(w http.ResponseWriter, req *http.Request, reqID, sessionID, model string) {
	resp, err := p.client.Do(req)
	if err != nil {
		p.completeWithError(req.Context(), reqID, sessionID, model, 0, "upstream request failed: "+err.Error())
		http.Error(w, `{"error":"upstream request failed"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// If upstream returned an error (non-streaming response), forward as-is
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		usage := extractAnthropicUsage(body)
		breakdown := CalculateCostBreakdown(model, usage)
		errMsg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		p.store.CompleteRequest(req.Context(), reqID, usage, breakdown, resp.StatusCode, "error", errMsg)
		p.events.PublishError(reqID, sessionID, model, resp.StatusCode, errMsg)
		p.setProxyHeaders(w, reqID, sessionID, breakdown.TotalCostUSD)
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		p.completeWithError(req.Context(), reqID, sessionID, model, 0, "streaming not supported")
		http.Error(w, `{"error":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	p.setProxyHeaders(w, reqID, sessionID, 0)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	var usage TokenUsage
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024) // 1MB buffer for large chunks

	for scanner.Scan() {
		line := scanner.Text()

		// Parse SSE data lines to extract token usage
		if strings.HasPrefix(line, "data: ") {
			data := line[6:]
			usage = parseAnthropicSSEChunk(data, usage)
		}

		// Forward line to agent exactly as received
		fmt.Fprintf(w, "%s\n", line)
		flusher.Flush()
	}

	// Use a detached context for the DB write — the request context may
	// already be cancelled once the SSE stream ends and the client disconnects.
	completeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := scanner.Err(); err != nil {
		p.completeWithError(completeCtx, reqID, sessionID, model, resp.StatusCode, "stream read error: "+err.Error())
		return
	}

	breakdown := CalculateCostBreakdown(model, usage)
	p.store.CompleteRequest(completeCtx, reqID, usage, breakdown, resp.StatusCode, "success", "")
	p.events.PublishCompleted(reqID, sessionID, model, usage, breakdown.TotalCostUSD, 0, resp.StatusCode)
	p.recordTokenUsage(completeCtx, sessionID, ProviderAnthropic, model, usage, breakdown)

	if debugProxy() {
		slog.Info("[proxy] anthropic SSE complete",
			"request_id", reqID, "model", model,
			"input_tokens", usage.InputTokens, "output_tokens", usage.OutputTokens,
			"cost_usd", fmt.Sprintf("%.6f", breakdown.TotalCostUSD))
	}
}

// ── OpenAI response handlers ───────────────────────────────────────────

func (p *Proxy) handleOpenAIJSON(w http.ResponseWriter, req *http.Request, reqID, sessionID, model string) {
	resp, err := p.client.Do(req)
	if err != nil {
		p.completeWithError(req.Context(), reqID, sessionID, model, 0, "upstream request failed: "+err.Error())
		http.Error(w, `{"error":"upstream request failed"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.completeWithError(req.Context(), reqID, sessionID, model, resp.StatusCode, "failed to read upstream response")
		http.Error(w, `{"error":"failed to read upstream response"}`, http.StatusBadGateway)
		return
	}

	usage := extractOpenAIUsage(body)
	breakdown := CalculateCostBreakdown(model, usage)

	status := "success"
	errMsg := ""
	if resp.StatusCode >= 400 {
		status = "error"
		errMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	p.store.CompleteRequest(req.Context(), reqID, usage, breakdown, resp.StatusCode, status, errMsg)
	if status == "error" {
		p.events.PublishError(reqID, sessionID, model, resp.StatusCode, errMsg)
	} else {
		p.events.PublishCompleted(reqID, sessionID, model, usage, breakdown.TotalCostUSD, 0, resp.StatusCode)
		p.recordTokenUsage(req.Context(), sessionID, ProviderOpenAI, model, usage, breakdown)
	}

	p.setProxyHeaders(w, reqID, sessionID, breakdown.TotalCostUSD)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

func (p *Proxy) handleOpenAISSE(w http.ResponseWriter, req *http.Request, reqID, sessionID, model string) {
	resp, err := p.client.Do(req)
	if err != nil {
		p.completeWithError(req.Context(), reqID, sessionID, model, 0, "upstream request failed: "+err.Error())
		http.Error(w, `{"error":"upstream request failed"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		usage := extractOpenAIUsage(body)
		breakdown := CalculateCostBreakdown(model, usage)
		errMsg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		p.store.CompleteRequest(req.Context(), reqID, usage, breakdown, resp.StatusCode, "error", errMsg)
		p.events.PublishError(reqID, sessionID, model, resp.StatusCode, errMsg)
		p.setProxyHeaders(w, reqID, sessionID, breakdown.TotalCostUSD)
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		p.completeWithError(req.Context(), reqID, sessionID, model, 0, "streaming not supported")
		http.Error(w, `{"error":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	p.setProxyHeaders(w, reqID, sessionID, 0)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	var usage TokenUsage
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "data: ") {
			data := line[6:]
			usage = parseOpenAISSEChunk(data, usage)
		}

		fmt.Fprintf(w, "%s\n", line)
		flusher.Flush()
	}

	// Use a detached context — request context may be cancelled after stream ends.
	completeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := scanner.Err(); err != nil {
		p.completeWithError(completeCtx, reqID, sessionID, model, resp.StatusCode, "stream read error: "+err.Error())
		return
	}

	breakdown := CalculateCostBreakdown(model, usage)
	p.store.CompleteRequest(completeCtx, reqID, usage, breakdown, resp.StatusCode, "success", "")
	p.events.PublishCompleted(reqID, sessionID, model, usage, breakdown.TotalCostUSD, 0, resp.StatusCode)
	p.recordTokenUsage(completeCtx, sessionID, ProviderOpenAI, model, usage, breakdown)

	if debugProxy() {
		slog.Info("[proxy] openai SSE complete",
			"request_id", reqID, "model", model,
			"input_tokens", usage.InputTokens, "output_tokens", usage.OutputTokens,
			"cost_usd", fmt.Sprintf("%.6f", breakdown.TotalCostUSD))
	}
}

// ── SSE parsing ────────────────────────────────────────────────────────

// parseAnthropicSSEChunk extracts token usage from Anthropic SSE data.
// Input tokens come from message_start, output tokens from message_delta.
func parseAnthropicSSEChunk(data string, current TokenUsage) TokenUsage {
	if data == "[DONE]" {
		return current
	}
	var chunk struct {
		Type    string `json:"type"`
		Message struct {
			Usage struct {
				InputTokens              int `json:"input_tokens"`
				OutputTokens             int `json:"output_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
		Usage struct {
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return current
	}

	switch chunk.Type {
	case "message_start":
		current.InputTokens = chunk.Message.Usage.InputTokens
		current.CacheReadTokens = chunk.Message.Usage.CacheReadInputTokens
		current.CacheWriteTokens = chunk.Message.Usage.CacheCreationInputTokens
	case "message_delta":
		current.OutputTokens = chunk.Usage.OutputTokens
	}
	return current
}

// parseOpenAISSEChunk extracts token usage from OpenAI SSE data.
// Usage appears in the final chunk when stream_options.include_usage is true.
func parseOpenAISSEChunk(data string, current TokenUsage) TokenUsage {
	if data == "[DONE]" {
		return current
	}
	var chunk struct {
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return current
	}
	if chunk.Usage != nil {
		current.InputTokens = chunk.Usage.PromptTokens
		current.OutputTokens = chunk.Usage.CompletionTokens
	}
	return current
}

// extractAnthropicUsage extracts token usage from a non-streaming Anthropic response.
func extractAnthropicUsage(body []byte) TokenUsage {
	var resp struct {
		Usage struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return TokenUsage{}
	}
	return TokenUsage{
		InputTokens:      resp.Usage.InputTokens,
		OutputTokens:     resp.Usage.OutputTokens,
		CacheReadTokens:  resp.Usage.CacheReadInputTokens,
		CacheWriteTokens: resp.Usage.CacheCreationInputTokens,
	}
}

// extractOpenAIUsage extracts token usage from a non-streaming OpenAI response.
func extractOpenAIUsage(body []byte) TokenUsage {
	var resp struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return TokenUsage{}
	}
	return TokenUsage{
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
	}
}

// extractOpenAIResponsesUsage extracts token usage from a non-streaming OpenAI Responses API response.
// The Responses API uses input_tokens/output_tokens (not prompt_tokens/completion_tokens).
func extractOpenAIResponsesUsage(body []byte) TokenUsage {
	var resp struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			InputTokensDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"input_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return TokenUsage{}
	}
	return TokenUsage{
		InputTokens:     resp.Usage.InputTokens,
		OutputTokens:    resp.Usage.OutputTokens,
		CacheReadTokens: resp.Usage.InputTokensDetails.CachedTokens,
	}
}

// parseOpenAIResponsesSSEChunk extracts token usage from OpenAI Responses API SSE data.
// Usage appears in the response.completed event.
func parseOpenAIResponsesSSEChunk(data string, current TokenUsage) TokenUsage {
	if data == "[DONE]" {
		return current
	}
	var chunk struct {
		Type     string `json:"type"`
		Response struct {
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
				InputTokensDetails struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"input_tokens_details"`
			} `json:"usage"`
		} `json:"response"`
	}
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return current
	}
	if chunk.Type == "response.completed" && chunk.Response.Usage.InputTokens > 0 {
		current.InputTokens = chunk.Response.Usage.InputTokens
		current.OutputTokens = chunk.Response.Usage.OutputTokens
		current.CacheReadTokens = chunk.Response.Usage.InputTokensDetails.CachedTokens
	}
	return current
}

// extractModelFromJSON extracts the model field from a JSON message if present.
// Checks both top-level "model" and nested "response.model" fields.
func extractModelFromJSON(data []byte) string {
	var m struct {
		Model    string `json:"model"`
		Response struct {
			Model string `json:"model"`
		} `json:"response"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	if m.Model != "" {
		return m.Model
	}
	return m.Response.Model
}

// ── Helpers ────────────────────────────────────────────────────────────

func (p *Proxy) setProxyHeaders(w http.ResponseWriter, reqID, sessionID string, costUSD float64) {
	w.Header().Set("X-Coral-Proxy", "true")
	w.Header().Set("X-Coral-Request-Id", reqID)
	w.Header().Set("X-Coral-Session-Id", sessionID)
	if costUSD > 0 {
		w.Header().Set("X-Coral-Cost-Usd", fmt.Sprintf("%.6f", costUSD))
	}
}

func (p *Proxy) completeWithError(ctx context.Context, reqID, sessionID, model string, httpStatus int, errMsg string) {
	p.store.CompleteRequest(ctx, reqID, TokenUsage{}, CostBreakdown{}, httpStatus, "error", errMsg)
	p.events.PublishError(reqID, sessionID, model, httpStatus, errMsg)
}

// forwardProviderHeaders copies all request headers matching the given
// lowercase prefix (e.g. "openai-", "anthropic-") to the upstream request.
// This ensures OAuth scoping headers (OpenAI-Organization, OpenAI-Project, etc.)
// and feature headers (OpenAI-Beta, Anthropic-Beta, etc.) are forwarded.
func forwardProviderHeaders(src, dst http.Header, lowercasePrefix string) {
	for name, values := range src {
		if strings.HasPrefix(strings.ToLower(name), lowercasePrefix) {
			for _, v := range values {
				dst.Set(name, v)
			}
		}
	}
}

// isChatGPTOAuthToken detects if an Authorization header contains a ChatGPT
// OAuth token (JWT). ChatGPT tokens are JWTs (eyJ...) and must be routed to
// chatgpt.com/backend-api/codex instead of api.openai.com.
// Standard OpenAI API keys start with "sk-".
func isChatGPTOAuthToken(authHeader string) bool {
	token := strings.TrimPrefix(authHeader, "Bearer ")
	return strings.HasPrefix(token, "eyJ")
}

func debugProxy() bool {
	// Reuse CORAL_DEBUG env var for proxy debug logging
	return strings.EqualFold(envOrDefault("CORAL_DEBUG", ""), "1")
}
