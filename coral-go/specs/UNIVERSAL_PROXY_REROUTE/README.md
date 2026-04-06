# Universal Proxy Reroute

## Problem

Coral's LLM proxy only captures tokens when the agent uses the direct Anthropic API (`api.anthropic.com`). Users authenticating via **AWS Bedrock**, **Azure**, or any custom gateway get zero token tracking because:

1. The proxy only handles `x-api-key` / `Authorization: Bearer` auth
2. Bedrock uses SigV4-signed requests to `bedrock-runtime.<region>.amazonaws.com`
3. `ANTHROPIC_BASE_URL` doesn't affect Bedrock-mode requests
4. Users configure their provider endpoint in `~/.claude/settings.json`, not just env vars

## Goal

Make the proxy a **universal passthrough** that works regardless of provider or auth method. At launch time, detect the user's configured upstream endpoint, save it, and reroute through our proxy. The proxy forwards requests as-is (preserving auth headers) and captures tokens from responses.

## Approach

### General Flow (All Providers)

1. **At launch**, `buildMergedSettings()` reads the user's Claude settings
2. **Detect provider base URLs** in the merged env block and OS env vars:
   - `ANTHROPIC_BASE_URL` (direct Anthropic API)
   - `ANTHROPIC_BEDROCK_BASE_URL` (AWS Bedrock)
   - `ANTHROPIC_VERTEX_BASE_URL` (Google Vertex AI)
   - `OPENAI_BASE_URL` (OpenAI-compatible)
   - Also detect `CLAUDE_CODE_USE_BEDROCK=1` / `CLAUDE_CODE_USE_VERTEX=1` mode flags
3. **Save the real upstream URL** as session metadata in the proxy (per-session config)
4. **Override the base URL** in the agent's env to point at our proxy: `http://127.0.0.1:<port>/proxy/<sessionID>`
5. **Proxy receives request** → captures tokens from response → forwards to real upstream

### Provider Auth Matrix (Confirmed)

| Provider | Auth Header | Host-Bound? | Passthrough? | Action |
|----------|------------|-------------|-------------|--------|
| Anthropic Direct | `x-api-key` | No | YES | Forward as-is |
| Anthropic OAuth | `Authorization: Bearer` | No | YES | Forward as-is |
| OpenAI | `Authorization: Bearer` | No | YES | Forward as-is |
| Vertex AI | OAuth2 bearer | No | YES | Forward as-is |
| Azure OpenAI | `api-key` header | No | YES | Forward as-is |
| **Bedrock SigV4** | `Authorization: AWS4-HMAC-SHA256` | **YES** | **NO** | Strip + re-sign |
| Bedrock Bearer | `Authorization: Bearer` | No | YES | Forward as-is |

**Bedrock SigV4 is the ONLY case requiring special handling.** All other providers work with simple passthrough.

### Bedrock SigV4 Handling (Confirmed from SDK Source)

The Anthropic SDK SigV4-signs for whatever URL is in `ANTHROPIC_BEDROCK_BASE_URL`. When we override it to our proxy URL:
- CLI signs the request with **our proxy's hostname** as the target
- Signature is valid when it arrives at our proxy
- But it's **invalid for the real Bedrock endpoint** (wrong host in signature)

The proxy must:
1. Receive the request (SigV4 signature is valid for our hostname)
2. Strip `Authorization`, `X-Amz-Date`, `X-Amz-Security-Token`, `X-Amz-Content-Sha256` headers
3. Re-sign for the real Bedrock endpoint using AWS creds from the environment
4. Forward to the real Bedrock endpoint

**Detection**: If a request has `Authorization: AWS4-HMAC-SHA256`, it needs re-signing. If it has `Authorization: Bearer`, it's passthrough. This makes the proxy provider-agnostic at the request level.

**Alternative for bearer token users**: `AWS_BEARER_TOKEN_BEDROCK` skips SigV4 entirely — simple passthrough works.

### Non-Bedrock Providers (Simpler)

For direct Anthropic API, OpenAI, Vertex AI, and Azure:
- Simple passthrough — the CLI adds auth headers, proxy forwards them unchanged
- No re-signing needed
- Existing `HandleAnthropicMessages` and `HandleOpenAIChatCompletions` handlers work as-is, just need to forward to the saved upstream URL instead of hardcoded `api.anthropic.com`

## Implementation

### Phase 1: Universal Upstream Reroute

**Files to modify:**

#### `internal/agent/claude.go` — `buildMergedSettings()`
- After merging settings, detect provider base URLs from both merged env block AND `os.Getenv()`
- Save the original upstream URL to `LaunchParams` (new field: `UpstreamBaseURL`)
- Override the base URL env var to point at the proxy
- **Fix pre-existing bug**: Deep-merge the `env` block instead of shallow-merging it (currently project-level env replaces global env entirely, losing AWS_REGION etc.)

```go
// Detection priority: merged settings env > OS env > defaults
func detectUpstreamURL(mergedEnv map[string]interface{}) (provider string, upstream string) {
    // Check for Bedrock first (more specific)
    if url := envOrOS(mergedEnv, "ANTHROPIC_BEDROCK_BASE_URL"); url != "" {
        return "bedrock", url
    }
    if envOrOS(mergedEnv, "CLAUDE_CODE_USE_BEDROCK") == "1" {
        region := envOrOS(mergedEnv, "AWS_REGION")
        if region == "" { region = "us-east-1" }
        return "bedrock", fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", region)
    }
    // Direct Anthropic
    if url := envOrOS(mergedEnv, "ANTHROPIC_BASE_URL"); url != "" {
        return "anthropic", url
    }
    // OpenAI
    if url := envOrOS(mergedEnv, "OPENAI_BASE_URL"); url != "" {
        return "openai", url
    }
    return "anthropic", "https://api.anthropic.com"
}
```

#### `internal/background/launcher.go`
- Pass detected upstream URL and provider type to the proxy when creating the session
- Store per-session upstream config

#### `internal/proxy/proxy.go`
- Add per-session upstream URL storage (map of sessionID -> upstream config)
- New method: `SetSessionUpstream(sessionID, provider, upstreamURL string)`
- Modify `HandleAnthropicMessages` to look up the session's upstream URL instead of using the global provider config
- Forward auth headers as-is (passthrough)

#### `internal/proxy/store.go`
- Add `upstream_url` and `provider` columns to `proxy_requests` table for tracking

### Phase 2: Bedrock SigV4 Re-Signing

**Additional files:**

#### `internal/proxy/bedrock.go` (new)
- `HandleBedrockMessages(w, r)` — receives Anthropic Messages format, translates to Bedrock format
- Body transform: set `anthropic_version` to `bedrock-2023-05-31`, route model to URL path
- Strip incoming SigV4 signature
- Re-sign with AWS creds using `aws-sdk-go-v2/aws/signer/v4`
- Parse Bedrock response (binary event stream for streaming, JSON for non-streaming)
- Extract tokens — same `usage` fields as direct API

#### `internal/proxy/aws_creds.go` (new)
- AWS credential resolution using the default credential chain
- Support for: env vars, shared credentials file, AWS_PROFILE, IMDS, ECS task role
- Proactive credential refresh before expiry (STS creds default to 1 hour)
- **Must NOT log credential values** — X-Amz-Security-Token, secret keys, session tokens

#### Bedrock Pricing
- Add Bedrock model IDs to `internal/proxy/cost.go` pricing table
- Bedrock pricing may differ from direct API — verify and update

### Phase 3: Multi-Region Support

- Claude CLI supports `ANTHROPIC_SMALL_FAST_MODEL_AWS_REGION` for routing smaller models to a different region
- Proxy must inspect the model in each request and route to the correct regional endpoint
- Per-session config needs a region map, not just a single upstream URL

## Pre-Existing Bug Fix: Env Block Shallow Merge

**Critical**: `buildMergedSettings()` uses shallow merge at the top level (line 337-346). If a user has:
- Global: `{"env": {"AWS_REGION": "us-east-1", "CLAUDE_CODE_USE_BEDROCK": "1"}}`
- Project: `{"env": {"ANTHROPIC_API_KEY": "sk-..."}}`

The project env **replaces** the global env entirely — `AWS_REGION` and `CLAUDE_CODE_USE_BEDROCK` are lost. This silently breaks Bedrock mode.

**Fix**: Deep-merge the `env` key (same as we do for `hooks`):

```go
// Deep-merge env maps
mergedEnv := make(map[string]interface{})
for _, source := range []map[string]interface{}{global, project, local} {
    if env, ok := source["env"].(map[string]interface{}); ok {
        for k, v := range env {
            mergedEnv[k] = v
        }
    }
}
merged["env"] = mergedEnv
```

## AWS Credential Handling

Users may authenticate via:
- **AWS SSO** (`AWS_PROFILE` pointing to an SSO profile) — most common in enterprise
- **Static keys** (`AWS_ACCESS_KEY_ID` + `AWS_SECRET_ACCESS_KEY`)
- **`awsCredentialExport`** (Claude CLI setting that runs a command to output creds)

### Approach: Resolve Credentials Fresh on Each Request (No Caching)

The proxy stores only the profile name and region — **no credentials in memory**. On each Bedrock request, the AWS SDK resolves creds fresh from the shared `~/.aws/sso/cache/` or credential chain.

```go
type SessionProxyMeta struct {
    Provider        string // "anthropic", "bedrock", "bedrock-bearer", "vertex", "openai"
    RealUpstreamURL string // original base URL before proxy override
    AWSRegion       string // primary region for SigV4 signing
    AWSAltRegion    string // alt region for small/fast models
    AWSProfile      string // profile name — SDK resolves creds on each request
}
```

**Per-request signing flow:**
1. Look up session metadata (profile, region)
2. `config.LoadDefaultConfig(ctx, config.WithSharedConfigProfile(meta.AWSProfile), config.WithRegion(meta.AWSRegion))`
3. SDK reads `~/.aws/sso/cache/` (local file read, not network call) → returns fresh creds
4. Sign request with `v4.Signer`
5. Forward to Bedrock

**Why no caching**: The CLI and proxy share the same `~/.aws/sso/cache/` filesystem. When Claude CLI's `awsAuthRefresh` runs `aws sso login`, the cache file updates. Our proxy picks up the fresh token on the next request automatically. One failed request during refresh is an acceptable tradeoff.

**This means**: No stale creds in memory, no custom refresh logic, no sensitive data to manage/leak/clean up. The `SessionProxyMeta` contains only a profile name and region string.

### SSO Token Expiry UX

SSO sessions typically last 1-12 hours (org-configurable). When expired:
1. Proxy gets `ExpiredTokenException` or `UnrecognizedClientException` from Bedrock
2. Proxy returns structured error: `"AWS SSO session expired. Run 'aws sso login --profile <profile>' to re-authenticate."`
3. Claude CLI's `awsAuthRefresh` setting (if configured) automatically runs `aws sso login`
4. CLI retries → proxy resolves fresh creds from updated cache → succeeds
5. Coral UI should surface SSO expiry errors in the session status panel

## Security Considerations

1. **Credential isolation**: Per-session `sync.Map` with synchronized access. One session's creds must never be used for another session's requests.
2. **No credential logging**: `SessionProxyMeta` must implement `fmt.Stringer` / `json.Marshaler` to redact credential fields. AWS secret keys, session tokens, and Authorization headers must NEVER appear in logs, error messages, or DB.
3. **No DB persistence**: AWS creds exist only in the in-memory map. The `proxy_sessions` DB table stores provider/region/upstream URL but NOT credentials.
4. **No API exposure**: No endpoint returns session credential data. Proxy dashboard shows provider type only.
5. **Session cleanup**: Creds removed from in-memory map on session end. Orphaned sessions should be purged after a TTL.
6. **Temp settings file permissions**: Merged settings files created with `0600` permissions and cleaned up on session end (already verified).
7. **CloudTrail attribution**: All re-signed requests appear from the SSO profile's IAM identity in CloudTrail. Per-user attribution within Coral relies on `proxy_requests` table.

## Per-Session Proxy Metadata

Each proxy session needs its own upstream config (not global):

```sql
CREATE TABLE proxy_sessions (
    session_id TEXT PRIMARY KEY,
    provider TEXT,         -- "anthropic", "bedrock", "vertex", "openai"
    upstream_url TEXT,     -- real endpoint before reroute
    aws_region TEXT,       -- for Bedrock SigV4 re-signing
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

Also add `upstream_url` column to `proxy_requests` for per-request tracking.

## Settings Detection Checklist

| Env Var | Provider | Action |
|---------|----------|--------|
| `ANTHROPIC_BEDROCK_BASE_URL` | Bedrock | Save as upstream, override to proxy URL |
| `CLAUDE_CODE_USE_BEDROCK=1` | Bedrock | Construct default Bedrock URL from `AWS_REGION` |
| `ANTHROPIC_VERTEX_BASE_URL` | Vertex AI | Save as upstream, override to proxy URL |
| `CLAUDE_CODE_USE_VERTEX=1` | Vertex AI | Construct default Vertex URL from region |
| `ANTHROPIC_BASE_URL` | Anthropic | Save as upstream, override to proxy URL |
| `OPENAI_BASE_URL` | OpenAI | Save as upstream, override to proxy URL |
| `AWS_BEARER_TOKEN_BEDROCK` | Bedrock (bearer) | Simple passthrough, no re-signing |

Detection checks both merged settings env block AND `os.Getenv()` — users may configure Bedrock via OS env vars only.

## Test Plan

### Core Functionality
1. Direct Anthropic API user — proxy reroute works, tokens tracked
2. Bedrock user (SigV4) — proxy re-signs, tokens tracked
3. Bedrock user (bearer token) — passthrough works, tokens tracked
4. OpenAI-compatible endpoint — proxy reroute works, tokens tracked
5. Custom gateway (`ANTHROPIC_BASE_URL` set) — passthrough works

### Env Merge
6. Global env + project env — deep merge preserves both
7. OS env var detection — Bedrock detected from OS env even if not in settings.json
8. Settings precedence — local > project > global for env vars

### Credentials
9. STS temporary creds — refresh before 1-hour expiry
10. Expired creds — clear error message, not raw AWS XML
11. SSO profile — AWS_PROFILE-based auth works
12. No creds — helpful error, request not logged as successful

### Streaming
13. Streaming SSE — tokens extracted from message_start + message_delta
14. Bedrock binary event stream — correctly parsed for streaming responses
15. Truncated stream — partial token counts still recorded

### Security
16. Credential headers not logged in proxy_requests
17. Temp settings files created with 0600 permissions
18. Temp settings files cleaned up on session end

### Edge Cases
19. Mixed team — Bedrock agent + direct API agent on same board
20. Multi-region — Haiku routed to different region than Opus
21. Credential rotation mid-stream — stream completes with original creds
