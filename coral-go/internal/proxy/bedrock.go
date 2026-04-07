package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// HandleBedrockMessages proxies Anthropic Messages API requests to AWS Bedrock.
// It detects auth type:
//   - SigV4 (AWS4-HMAC-SHA256): strips signature, re-signs for real Bedrock endpoint
//   - Bearer token: simple passthrough (no re-signing needed)
//
// POST /proxy/{sessionID}/v1/messages (when session provider is "bedrock")
func (p *Proxy) HandleBedrockMessages(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")

	// Look up session upstream config
	sessionMeta, err := p.store.GetSessionUpstream(r.Context(), sessionID)
	if err != nil || sessionMeta == nil {
		http.Error(w, `{"error":"no bedrock session config found"}`, http.StatusBadGateway)
		return
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

	authHeader := r.Header.Get("Authorization")

	if IsSigV4Request(authHeader) {
		p.handleBedrockSigV4(w, r, body, reqID, sessionID, sessionMeta, meta.Model, meta.Stream)
	} else {
		// Bearer token passthrough — forward to Bedrock as-is
		p.handleBedrockPassthrough(w, r, body, reqID, sessionID, sessionMeta, meta.Model, meta.Stream)
	}
}

// handleBedrockSigV4 handles requests that need SigV4 re-signing.
func (p *Proxy) handleBedrockSigV4(w http.ResponseWriter, r *http.Request, body []byte,
	reqID, sessionID string, meta *SessionUpstream, model string, stream bool) {

	// Build the Bedrock URL: https://bedrock-runtime.<region>.amazonaws.com/model/<model>/invoke
	bedrockURL, err := buildBedrockURL(meta.UpstreamURL, model, stream)
	if err != nil {
		p.completeWithError(r.Context(), reqID, sessionID, model, 0, "invalid bedrock URL: "+err.Error())
		http.Error(w, `{"error":"invalid bedrock configuration"}`, http.StatusBadGateway)
		return
	}

	// Transform request body for Bedrock format
	bedrockBody := transformToBedrockBody(body)

	// Create upstream request
	upstreamReq, err := http.NewRequestWithContext(r.Context(), "POST", bedrockURL, bytes.NewReader(bedrockBody))
	if err != nil {
		p.completeWithError(r.Context(), reqID, sessionID, model, 0, "failed to create upstream request: "+err.Error())
		http.Error(w, `{"error":"proxy internal error"}`, http.StatusInternalServerError)
		return
	}

	upstreamReq.Header.Set("Content-Type", "application/json")
	if stream {
		upstreamReq.Header.Set("Accept", "application/vnd.amazon.eventstream")
	}

	// Resolve AWS credentials and sign the request
	resolver := &AWSCredResolver{}
	region := extractRegionFromURL(meta.UpstreamURL)
	creds, signer, err := resolver.ResolveCredentials(r.Context(), meta.AWSRegion, region)
	if err != nil {
		p.completeWithError(r.Context(), reqID, sessionID, model, 0, "AWS credential error: "+err.Error())
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadGateway)
		return
	}

	// Compute payload hash for signing
	payloadHash := sha256Hex(bedrockBody)
	upstreamReq.Header.Set("X-Amz-Content-Sha256", payloadHash)

	// Sign the request
	err = signer.SignHTTP(r.Context(), creds, upstreamReq, payloadHash, "bedrock", region, time.Now())
	if err != nil {
		p.completeWithError(r.Context(), reqID, sessionID, model, 0, "SigV4 signing failed")
		http.Error(w, `{"error":"AWS request signing failed"}`, http.StatusInternalServerError)
		return
	}

	if stream {
		p.handleBedrockSSE(w, upstreamReq, reqID, sessionID, model)
	} else {
		p.handleBedrockJSON(w, upstreamReq, reqID, sessionID, model)
	}
}

// handleBedrockPassthrough forwards bearer-token Bedrock requests as-is.
func (p *Proxy) handleBedrockPassthrough(w http.ResponseWriter, r *http.Request, body []byte,
	reqID, sessionID string, meta *SessionUpstream, model string, stream bool) {

	bedrockURL, err := buildBedrockURL(meta.UpstreamURL, model, stream)
	if err != nil {
		p.completeWithError(r.Context(), reqID, sessionID, model, 0, "invalid bedrock URL: "+err.Error())
		http.Error(w, `{"error":"invalid bedrock configuration"}`, http.StatusBadGateway)
		return
	}

	bedrockBody := transformToBedrockBody(body)

	upstreamReq, err := http.NewRequestWithContext(r.Context(), "POST", bedrockURL, bytes.NewReader(bedrockBody))
	if err != nil {
		p.completeWithError(r.Context(), reqID, sessionID, model, 0, "failed to create upstream request: "+err.Error())
		http.Error(w, `{"error":"proxy internal error"}`, http.StatusInternalServerError)
		return
	}

	upstreamReq.Header.Set("Content-Type", "application/json")
	// Forward auth headers as-is
	if v := r.Header.Get("Authorization"); v != "" {
		upstreamReq.Header.Set("Authorization", v)
	}
	// Forward Anthropic-specific headers
	forwardProviderHeaders(r.Header, upstreamReq.Header, "anthropic-")

	if stream {
		upstreamReq.Header.Set("Accept", "application/vnd.amazon.eventstream")
		p.handleBedrockSSE(w, upstreamReq, reqID, sessionID, model)
	} else {
		p.handleBedrockJSON(w, upstreamReq, reqID, sessionID, model)
	}
}

// handleBedrockJSON handles non-streaming Bedrock responses.
func (p *Proxy) handleBedrockJSON(w http.ResponseWriter, req *http.Request, reqID, sessionID, model string) {
	resp, err := p.client.Do(req)
	if err != nil {
		errMsg := classifyBedrockError(err)
		p.completeWithError(req.Context(), reqID, sessionID, model, 0, errMsg)
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, errMsg), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.completeWithError(req.Context(), reqID, sessionID, model, resp.StatusCode, "failed to read upstream response")
		http.Error(w, `{"error":"failed to read upstream response"}`, http.StatusBadGateway)
		return
	}

	// Bedrock returns the same usage JSON format as direct Anthropic API
	usage := extractAnthropicUsage(body)
	breakdown := CalculateCostBreakdown(model, usage)

	status := "success"
	errMsg := ""
	if resp.StatusCode >= 400 {
		status = "error"
		errMsg = classifyBedrockHTTPError(resp.StatusCode, body)
	}
	p.store.CompleteRequest(req.Context(), reqID, usage, breakdown, resp.StatusCode, status, errMsg)
	if status == "error" {
		p.events.PublishError(reqID, sessionID, model, resp.StatusCode, errMsg)
	} else {
		p.events.PublishCompleted(reqID, sessionID, model, usage, breakdown.TotalCostUSD, 0, resp.StatusCode)
	}

	p.setProxyHeaders(w, reqID, sessionID, breakdown.TotalCostUSD)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

// handleBedrockSSE handles streaming Bedrock responses.
// Bedrock streaming uses Anthropic SSE format (same event types).
func (p *Proxy) handleBedrockSSE(w http.ResponseWriter, req *http.Request, reqID, sessionID, model string) {
	resp, err := p.client.Do(req)
	if err != nil {
		errMsg := classifyBedrockError(err)
		p.completeWithError(req.Context(), reqID, sessionID, model, 0, errMsg)
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, errMsg), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		usage := extractAnthropicUsage(body)
		breakdown := CalculateCostBreakdown(model, usage)
		errMsg := classifyBedrockHTTPError(resp.StatusCode, body)
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
			usage = parseAnthropicSSEChunk(data, usage)
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

	if debugProxy() {
		slog.Info("[proxy] bedrock SSE complete",
			"request_id", reqID, "model", model,
			"input_tokens", usage.InputTokens, "output_tokens", usage.OutputTokens,
			"cost_usd", fmt.Sprintf("%.6f", breakdown.TotalCostUSD))
	}
}

// ── Bedrock helpers ───────────────────────────────────────────

// buildBedrockURL constructs the Bedrock invoke URL from the base URL and model.
// Format: https://bedrock-runtime.<region>.amazonaws.com/model/<model>/invoke
// For streaming: .../invoke-with-response-stream
func buildBedrockURL(baseURL, model string, stream bool) (string, error) {
	if baseURL == "" {
		return "", fmt.Errorf("bedrock base URL is empty")
	}
	base := strings.TrimRight(baseURL, "/")

	// Convert standard Claude model IDs to Bedrock model IDs if needed
	bedrockModel := toBedrockModelID(model)

	endpoint := "invoke"
	if stream {
		endpoint = "invoke-with-response-stream"
	}
	return fmt.Sprintf("%s/model/%s/%s", base, url.PathEscape(bedrockModel), endpoint), nil
}

// toBedrockModelID converts a standard Claude model ID to a Bedrock model ID.
// Bedrock model IDs use the format: anthropic.claude-<variant>-<version>-v1:0
// If the model already looks like a Bedrock ID (contains "anthropic."), return as-is.
func toBedrockModelID(model string) string {
	if strings.Contains(model, "anthropic.") {
		return model // Already a Bedrock model ID
	}
	// Map standard model names to Bedrock IDs
	// Bedrock uses: us.anthropic.<model>-v1:0 for cross-region
	// and: anthropic.<model>-v1:0 for same-region
	return "anthropic." + model + "-v1:0"
}

// transformToBedrockBody modifies the request body for the Bedrock API format.
// Sets anthropic_version to bedrock-2023-05-31 and removes the model field
// (model is specified in the URL path for Bedrock).
func transformToBedrockBody(body []byte) []byte {
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		return body // Return original if parse fails
	}

	req["anthropic_version"] = "bedrock-2023-05-31"
	delete(req, "model") // Model is in the URL path for Bedrock

	out, err := json.Marshal(req)
	if err != nil {
		return body
	}
	return out
}

// extractRegionFromURL extracts the AWS region from a Bedrock URL.
// e.g., "https://bedrock-runtime.us-east-1.amazonaws.com" → "us-east-1"
func extractRegionFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "us-east-1"
	}
	// Host format: bedrock-runtime.<region>.amazonaws.com
	parts := strings.Split(u.Hostname(), ".")
	if len(parts) >= 3 {
		return parts[1]
	}
	return "us-east-1"
}

// sha256Hex returns the hex-encoded SHA256 hash of the data.
func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// classifyBedrockError converts connection-level errors to user-friendly messages.
func classifyBedrockError(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "no such host") || strings.Contains(msg, "dial tcp") {
		return "cannot reach Bedrock endpoint — check AWS_REGION and network connectivity"
	}
	return "upstream request failed: " + msg
}

// classifyBedrockHTTPError parses Bedrock error responses into user-friendly messages.
func classifyBedrockHTTPError(statusCode int, body []byte) string {
	var errResp struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	}
	json.Unmarshal(body, &errResp)

	switch {
	case statusCode == 403 && strings.Contains(errResp.Message, "ExpiredToken"):
		return "AWS SSO session expired. Run 'aws sso login' to re-authenticate."
	case statusCode == 403:
		return fmt.Sprintf("Bedrock access denied: %s", errResp.Message)
	case statusCode == 404:
		return "Bedrock model not found — check model ID and region access"
	default:
		if errResp.Message != "" {
			return fmt.Sprintf("Bedrock HTTP %d: %s", statusCode, errResp.Message)
		}
		return fmt.Sprintf("Bedrock HTTP %d", statusCode)
	}
}
