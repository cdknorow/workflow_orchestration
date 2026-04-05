package proxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MITMProxy handles HTTP CONNECT requests and proxies HTTPS traffic
// through a TLS-terminating man-in-the-middle proxy for cost tracking.
type MITMProxy struct {
	ca    tls.Certificate
	store *Store
	events *EventHub

	// certCache caches dynamically generated host certificates.
	certMu    sync.RWMutex
	certCache map[string]*tls.Certificate
}

// NewMITMProxy creates a new MITM proxy with the given CA certificate.
func NewMITMProxy(ca tls.Certificate, store *Store, events *EventHub) *MITMProxy {
	return &MITMProxy{
		ca:        ca,
		store:     store,
		events:    events,
		certCache: make(map[string]*tls.Certificate),
	}
}

// HandleConnect handles HTTP CONNECT requests for HTTPS proxying.
// The client sends "CONNECT host:port HTTP/1.1", we hijack the connection,
// do TLS with our CA-signed cert, then read/forward the inner HTTP request.
func (m *MITMProxy) HandleConnect(w http.ResponseWriter, r *http.Request) {
	targetHost := r.Host
	if targetHost == "" {
		targetHost = r.URL.Host
	}

	// Extract hostname without port for cert generation
	hostname := targetHost
	if h, _, err := net.SplitHostPort(targetHost); err == nil {
		hostname = h
	}

	slog.Info("[mitm] CONNECT request", "target", targetHost, "hostname", hostname)

	// Hijack the connection
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "CONNECT not supported", http.StatusInternalServerError)
		return
	}

	// Send 200 Connection Established
	w.WriteHeader(http.StatusOK)

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		slog.Error("[mitm] hijack failed", "error", err)
		return
	}
	defer clientConn.Close()

	// Get or generate a TLS certificate for this hostname
	hostCert, err := m.getOrCreateCert(hostname)
	if err != nil {
		slog.Error("[mitm] cert generation failed", "hostname", hostname, "error", err)
		return
	}

	// Wrap the hijacked connection in TLS (server-side)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{*hostCert},
	}
	tlsConn := tls.Server(clientConn, tlsConfig)
	if err := tlsConn.HandshakeContext(context.Background()); err != nil {
		slog.Error("[mitm] TLS handshake failed", "hostname", hostname, "error", err)
		return
	}
	defer tlsConn.Close()

	// Read the inner HTTP request from the TLS connection
	tlsBuf := bufio.NewReader(tlsConn)
	for {
		innerReq, err := http.ReadRequest(tlsBuf)
		if err != nil {
			if err != io.EOF {
				slog.Debug("[mitm] read inner request failed", "error", err)
			}
			return
		}

		// Set the full URL for the upstream request
		innerReq.URL.Scheme = "https"
		innerReq.URL.Host = targetHost
		innerReq.RequestURI = "" // Must be cleared for http.Client

		m.proxyRequest(tlsConn, innerReq, hostname)
	}
}

// proxyRequest forwards a single HTTP request to the upstream and tracks costs.
func (m *MITMProxy) proxyRequest(clientConn net.Conn, innerReq *http.Request, hostname string) {
	reqID := uuid.New().String()
	sessionID := "codex-mitm" // Shared session for now; per-session mapping later

	// Extract model from request body if possible (non-destructive peek)
	model := "unknown"

	isResponses := strings.Contains(innerReq.URL.Path, "/responses")
	isChatCompletions := strings.Contains(innerReq.URL.Path, "/chat/completions")

	provider := ProviderOpenAI
	if isResponses || isChatCompletions {
		// Try to read and replay the body to extract the model
		if innerReq.Body != nil {
			bodyBytes, err := io.ReadAll(innerReq.Body)
			innerReq.Body.Close()
			if err == nil {
				// Extract model name
				if m := extractModelFromJSON(bodyBytes); m != "" {
					model = m
				}
				// Replay body
				innerReq.Body = io.NopCloser(strings.NewReader(string(bodyBytes)))
			}
		}
	}

	if err := m.store.CreateRequest(context.Background(), reqID, sessionID, provider, model, true); err != nil {
		slog.Error("[mitm] failed to create request record", "error", err, "request_id", reqID)
	}
	m.events.PublishStarted(reqID, sessionID, provider, model, true)

	slog.Debug("[mitm] forwarding request", "method", innerReq.Method, "url", innerReq.URL.String(), "model", model, "request_id", reqID)

	// Make the upstream request
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Minute, // Long timeout for streaming responses
	}
	resp, err := client.Do(innerReq)
	if err != nil {
		slog.Error("[mitm] upstream request failed", "error", err, "request_id", reqID)
		m.completeWithError(reqID, sessionID, model, 0, "upstream request failed: "+err.Error())
		// Write error response back to client
		errResp := fmt.Sprintf("HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		clientConn.Write([]byte(errResp))
		return
	}
	defer resp.Body.Close()

	isSSE := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")

	if isSSE && (isResponses || isChatCompletions) {
		m.proxySSEResponse(clientConn, resp, reqID, sessionID, model, isResponses)
	} else {
		m.proxyFullResponse(clientConn, resp, reqID, sessionID, model, isResponses)
	}
}

// proxySSEResponse handles streaming SSE responses, inspecting each chunk for usage data.
func (m *MITMProxy) proxySSEResponse(clientConn net.Conn, resp *http.Response, reqID, sessionID, model string, isResponses bool) {
	// Write response headers to client
	if err := resp.Write(clientConn); err != nil {
		// resp.Write with a streaming body will write headers then stream.
		// But we need more control — write headers manually then stream body.
	}

	// Actually, write headers manually for streaming control
	headerBuf := &strings.Builder{}
	fmt.Fprintf(headerBuf, "HTTP/%d.%d %d %s\r\n", resp.ProtoMajor, resp.ProtoMinor, resp.StatusCode, resp.Status)
	for name, values := range resp.Header {
		for _, v := range values {
			fmt.Fprintf(headerBuf, "%s: %s\r\n", name, v)
		}
	}
	headerBuf.WriteString("\r\n")

	if _, err := clientConn.Write([]byte(headerBuf.String())); err != nil {
		slog.Error("[mitm] failed to write response headers", "error", err)
		return
	}

	var usage TokenUsage
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		// Extract usage from SSE data lines
		if strings.HasPrefix(line, "data: ") {
			data := line[6:]
			if isResponses {
				usage = parseOpenAIResponsesSSEChunk(data, usage)
			} else {
				usage = parseOpenAISSEChunk(data, usage)
			}
			// Try to extract model
			if model == "unknown" {
				if m := extractModelFromJSON([]byte(data)); m != "" {
					model = m
				}
			}
		}

		// Forward line to client
		if _, err := fmt.Fprintf(clientConn, "%s\n", line); err != nil {
			slog.Debug("[mitm] client write error during SSE", "error", err)
			break
		}
	}

	completeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := scanner.Err(); err != nil {
		m.completeWithError(reqID, sessionID, model, resp.StatusCode, "SSE read error: "+err.Error())
		return
	}

	breakdown := CalculateCostBreakdown(model, usage)
	m.store.CompleteRequest(completeCtx, reqID, usage, breakdown, resp.StatusCode, "success", "")
	m.events.PublishCompleted(reqID, sessionID, model, usage, breakdown.TotalCostUSD, 0, resp.StatusCode)

	slog.Info("[mitm] SSE complete", "request_id", reqID, "model", model,
		"input_tokens", usage.InputTokens, "output_tokens", usage.OutputTokens,
		"cost_usd", fmt.Sprintf("%.6f", breakdown.TotalCostUSD))
}

// proxyFullResponse handles non-streaming responses.
func (m *MITMProxy) proxyFullResponse(clientConn net.Conn, resp *http.Response, reqID, sessionID, model string, isResponses bool) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("[mitm] failed to read upstream response", "error", err)
		m.completeWithError(reqID, sessionID, model, resp.StatusCode, "read error: "+err.Error())
		return
	}

	// Extract usage from response body
	var usage TokenUsage
	if isResponses {
		usage = extractOpenAIResponsesUsage(body)
	} else {
		usage = extractOpenAIUsage(body)
	}

	// Write full response to client
	headerBuf := &strings.Builder{}
	fmt.Fprintf(headerBuf, "HTTP/%d.%d %d %s\r\n", resp.ProtoMajor, resp.ProtoMinor, resp.StatusCode, resp.Status)
	resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))
	for name, values := range resp.Header {
		for _, v := range values {
			fmt.Fprintf(headerBuf, "%s: %s\r\n", name, v)
		}
	}
	headerBuf.WriteString("\r\n")
	clientConn.Write([]byte(headerBuf.String()))
	clientConn.Write(body)

	completeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status := "success"
	errMsg := ""
	if resp.StatusCode >= 400 {
		status = "error"
		errMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	breakdown := CalculateCostBreakdown(model, usage)
	m.store.CompleteRequest(completeCtx, reqID, usage, breakdown, resp.StatusCode, status, errMsg)
	m.events.PublishCompleted(reqID, sessionID, model, usage, breakdown.TotalCostUSD, 0, resp.StatusCode)

	if usage.InputTokens > 0 {
		slog.Info("[mitm] request complete", "request_id", reqID, "model", model,
			"input_tokens", usage.InputTokens, "output_tokens", usage.OutputTokens,
			"cost_usd", fmt.Sprintf("%.6f", breakdown.TotalCostUSD))
	}
}

func (m *MITMProxy) getOrCreateCert(hostname string) (*tls.Certificate, error) {
	m.certMu.RLock()
	if cert, ok := m.certCache[hostname]; ok {
		m.certMu.RUnlock()
		return cert, nil
	}
	m.certMu.RUnlock()

	m.certMu.Lock()
	defer m.certMu.Unlock()

	// Double-check after acquiring write lock
	if cert, ok := m.certCache[hostname]; ok {
		return cert, nil
	}

	cert, err := generateHostCert(hostname, m.ca)
	if err != nil {
		return nil, err
	}
	m.certCache[hostname] = cert
	return cert, nil
}

func (m *MITMProxy) completeWithError(reqID, sessionID, model string, httpStatus int, errMsg string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m.store.CompleteRequest(ctx, reqID, TokenUsage{}, CostBreakdown{}, httpStatus, "error", errMsg)
	m.events.PublishError(reqID, sessionID, model, httpStatus, errMsg)
}
