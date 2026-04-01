# Connected Apps API

List OAuth providers, start connection flows, retrieve fresh access tokens, and manage saved app connections.

Connected Apps store OAuth credentials for external services such as Gmail, Google Calendar, GitHub, and Slack. Access tokens and client secrets are never exposed in normal connection responses.

---

## List available providers

```
GET /api/connected-apps/providers
```

Returns the providers Coral knows how to connect plus the local callback URL to register in external OAuth apps.

### Response

```json
{
  "providers": [
    {
      "id": "github",
      "name": "GitHub",
      "auth_url": "https://github.com/login/oauth/authorize",
      "token_url": "https://github.com/login/oauth/access_token",
      "scopes": ["repo", "read:org"],
      "icon": "code",
      "instructions": "Create an OAuth App in GitHub...",
      "client_id": "",
      "one_click": false
    },
    {
      "id": "gmail",
      "name": "Gmail",
      "auth_url": "https://accounts.google.com/o/oauth2/v2/auth",
      "token_url": "https://oauth2.googleapis.com/token",
      "scopes": ["https://www.googleapis.com/auth/gmail.readonly"],
      "icon": "mail",
      "instructions": "Connect your Gmail account to let Coral read your emails.",
      "client_id": "684211507805-...",
      "one_click": true
    }
  ],
  "callback_url": "http://localhost:8420/api/connected-apps/callback"
}
```

`one_click: true` means Coral ships with embedded OAuth credentials for that provider, so the user does not need to supply their own client ID and secret.

---

## List saved connections

```
GET /api/connected-apps
```

### Response

```json
{
  "connections": [
    {
      "id": 3,
      "provider_id": "github",
      "name": "Personal GitHub",
      "scopes": "repo read:org",
      "account_email": "user@example.com",
      "account_name": "octocat",
      "status": "active",
      "created_at": "2025-03-11T10:00:00+00:00",
      "updated_at": "2025-03-11T10:00:00+00:00"
    }
  ]
}
```

---

## Get one connection

```
GET /api/connected-apps/{id}
```

Returns one saved connection with tokens redacted.

Returns `{"error": "connection not found"}, 404` if the ID does not exist.

---

## Start OAuth authorization

```
POST /api/connected-apps/auth/start
```

Starts an OAuth flow and returns a browser URL plus a short-lived `state` token. The server keeps the pending auth state in memory for up to 10 minutes.

### Request body

| Field | Type | Default | Description |
|---|---|---|---|
| `provider_id` | string | **required** | Provider from `/providers`. |
| `name` | string | **required** | User-visible connection name. Must be unique per provider. |
| `client_id` | string | required for non-embedded providers | OAuth app client ID. |
| `client_secret` | string | required for non-embedded providers | OAuth app client secret. |
| `scopes` | array of strings | provider defaults | Optional custom scopes. |

### Example

```bash
curl -X POST http://localhost:8420/api/connected-apps/auth/start \
  -H "Content-Type: application/json" \
  -d '{
    "provider_id": "github",
    "name": "Personal GitHub",
    "client_id": "Ov23li...",
    "client_secret": "shhh",
    "scopes": ["repo", "read:org"]
  }'
```

### Response

```json
{
  "auth_url": "https://github.com/login/oauth/authorize?...",
  "state": "b74f6c8e8d0b..."
}
```

### Errors

| Status | Body | Cause |
|---|---|---|
| 400 | `{"error": "invalid JSON"}` | Malformed request body. |
| 400 | `{"error": "provider_id is required"}` | Missing provider ID. |
| 400 | `{"error": "name is required"}` | Missing connection name. |
| 400 | `{"error": "unknown provider"}` | Unsupported provider. |
| 400 | `{"error": "client_id is required for this provider"}` | Missing client ID for a non-embedded provider. |
| 400 | `{"error": "client_secret is required for this provider"}` | Missing client secret for a non-embedded provider. |
| 409 | `{"error": "a connection with this provider and name already exists"}` | Duplicate provider/name pair. |

---

## OAuth callback

```
GET /api/connected-apps/callback?state=...&code=...
```

This endpoint is meant for browser redirects from the OAuth provider. It returns HTML, not JSON.

Successful callbacks:

- exchange the code for tokens
- create the connection row with `status: "active"`
- kick off a background profile fetch to populate `account_email` and `account_name`
- post an `oauth-complete` message to the opener window and close the popup

Failure callbacks render an error page and close the popup.

---

## Get a fresh access token

```
GET /api/connected-apps/{id}/token
```

Returns a usable access token for the connection. If the stored token is expired or within five minutes of expiry, Coral refreshes it first.

### Response

```json
{
  "access_token": "gho_...",
  "provider_id": "github",
  "name": "Personal GitHub"
}
```

### Notes

- If no refresh token is available and the token is stale, Coral marks the connection as `expired`.
- This endpoint intentionally exposes the access token. Use it only from trusted local tooling.

---

## Delete a connection

```
DELETE /api/connected-apps/{id}
```

Deletes the saved connection. Coral also attempts best-effort token revocation with the upstream provider in the background before deleting the row.

### Response

```json
{"ok": true}
```

Returns `{"error": "connection not found"}, 404` if the ID does not exist.

---

## Test a connection

```
POST /api/connected-apps/{id}/test
```

Fetches a fresh token if needed, then performs a lightweight provider API call to verify the connection and refresh profile metadata.

### Success response

```json
{
  "ok": true,
  "account_email": "user@example.com",
  "account_name": "octocat"
}
```

### Failure response

```json
{
  "ok": false,
  "error": "failed to get token: ..."
}
```

### Provider-specific behavior

- `gmail` and `google-calendar`: fetches Google profile info from the userinfo endpoint.
- `github`: fetches `/user` and uses `name` or falls back to `login`.
- `slack`: calls `auth.test` and returns a synthesized name like `user (team)`.
