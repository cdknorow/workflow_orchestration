# Spec: Codex Proxy Cost Tracking

## Status: In Progress (MITM proxy blocked, JSONL fallback working)

## Problem

Codex agents using ChatGPT OAuth auth need cost/token tracking. Unlike Claude agents where we set `ANTHROPIC_BASE_URL` to route API calls through our reverse proxy, Codex's OAuth auth mode has no equivalent mechanism — the ChatGPT backend URL can't be reliably overridden per-session.

## Approaches Tried

### 1. OPENAI_BASE_URL env var
**Result: Failed**

Set `OPENAI_BASE_URL` to redirect Codex API calls to our reverse proxy.

- Codex CLI warns: `OPENAI_BASE_URL is deprecated. Set openai_base_url in config.toml instead.`
- With OAuth auth, Codex sends requests to `chatgpt.com/backend-api/codex/responses`, not `api.openai.com/v1/responses`
- `OPENAI_BASE_URL` only affects the OpenAI API path, not the ChatGPT backend path
- When we redirect to our proxy → forward to `api.openai.com`, the ChatGPT OAuth token gets rejected: `Missing scopes: api.responses.write`
- When we detect JWT tokens and route to `chatgpt.com/backend-api/codex/responses`, the request format is wrong because Codex formats the request for OpenAI API (not ChatGPT backend) when `OPENAI_BASE_URL` is set

### 2. chatgpt_base_url config override via -c flag
**Result: Ignored by Codex**

Used `-c chatgpt_base_url="http://127.0.0.1:8420/proxy/{session}"` to override per-session.

- The config key exists in the Codex binary
- The `-c` flag accepts it without error
- However, Codex does NOT route API traffic through this URL — the agent made successful API calls that bypassed our proxy entirely
- `chatgpt_base_url` may only control auth/login endpoints, not API calls

### 3. openai_base_url config override via -c flag
**Result: Same issues as OPENAI_BASE_URL env var**

Used `-c openai_base_url="http://127.0.0.1:8420/proxy/{session}"` — equivalent to the env var approach but without the deprecation warning.

### 4. MITM HTTPS Proxy (CONNECT tunneling)
**Result: Partially working, TLS trust blocked**

Set `HTTPS_PROXY` env var to route ALL HTTPS traffic through our server. Implemented HTTP CONNECT handling with TLS termination using a self-signed CA.

Progress:
- ✅ Auth middleware updated to allow CONNECT from localhost (Host header is target, not proxy)
- ✅ CONNECT handler accepts connection, sends 200 Connection Established
- ✅ Dynamic host cert generation (signed by our CA) with caching
- ✅ Bidirectional request proxying with SSE streaming support
- ✅ Cost tracking via existing proxy store/events
- ❌ Codex rejects our CA cert: `invalid peer certificate: UnknownIssuer`

Attempted CA trust solutions:
- `-c ca-certificate=...`: This config key is for Codex's sandbox network proxy TLS, NOT the API HTTP client
- `SSL_CERT_FILE` env var with combined bundle (system CAs + Coral CA): Created bundle at `~/.coral/proxy-ca-bundle.pem` — **not yet validated**, may work with Rust's rustls/native-tls
- Adding to macOS system keychain: Not attempted (requires `security add-trusted-cert` with admin privileges)

### 5. network.proxy_url config
**Result: Not applicable**

`proxy_url` under `[network]` in config.toml is for Codex's **sandbox network proxy** — a managed proxy Codex runs itself for sandboxing agent tool network access. It's not a forwarding proxy for API calls.

## What Works Now

### JSONL Log-Based Token Tracking (Implemented)

Background poller (`background/token_poller.go`) reads Codex rollout JSONL files every 30s:
- Extracts `usage` entries (input_tokens, output_tokens, cached_input, total_tokens)
- Accumulates cumulative totals per session
- Estimates cost via existing pricing table
- Records via `TokenUsageStore.RecordUsage`
- Works regardless of proxy settings
- No TLS, no cert issues, no auth complications

**Limitation**: Session-level totals only. No per-request granularity like the reverse proxy provides for Claude.

## Current Implementation

### Settings
- `proxy_enabled_claude`: Enables reverse proxy for Claude agents (default: inherits `proxy_enabled`)
- `proxy_enabled_codex`: Enables MITM proxy for Codex agents (default: false, **hidden in UI**)

### Files
| File | Purpose |
|------|---------|
| `proxy/ca.go` | CA cert generation, host cert generation, cert bundle creation |
| `proxy/mitm.go` | CONNECT handler, TLS termination, request proxying, cost tracking |
| `agent/codex.go` | Launch command with HTTPS_PROXY, SSL_CERT_FILE, base URL overrides |
| `auth/middleware.go` | CONNECT bypass for localhost DNS rebinding check |
| `background/token_poller.go` | JSONL-based token tracking (working fallback) |

## Next Steps

1. **Validate SSL_CERT_FILE**: Test if Codex's Rust HTTP client (likely rustls) honors `SSL_CERT_FILE` for CA trust
2. **System keychain approach**: If SSL_CERT_FILE doesn't work, try adding Coral CA to macOS keychain on first run (with user consent)
3. **Upstream feature request**: Ask OpenAI for a proper `CHATGPT_BASE_URL` env var or `-c` flag that controls the API endpoint for OAuth mode
4. **Per-request JSONL tracking**: Enhance the JSONL poller to extract per-turn usage (not just cumulative) for better granularity
