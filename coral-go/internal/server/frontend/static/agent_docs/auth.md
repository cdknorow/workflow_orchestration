# Authentication API

Coral uses a layered authentication system:

1. **Localhost bypass** — Requests from `127.0.0.1` / `::1` are always authenticated without credentials.
2. **API key** — An auto-generated key stored in `~/.coral/`. Remote clients pass it via `Authorization: Bearer <key>` header or `X-API-Key` header.
3. **Session cookie** — After validating an API key via `POST /auth/key`, a session cookie is set for browser access.

## Auth Page

```
GET /auth
```

Serves the HTML authentication page where users can enter their API key.

**Response:** HTML document.

## Validate API Key

```
POST /auth
POST /auth/key
```

Validates an API key and sets a session cookie on success. Both paths are equivalent.

**Request Body:**
```json
{
  "key": "your-api-key"
}
```

**Response (200):**
```json
{
  "ok": true
}
```

A `coral_session` cookie is set in the response.

**Errors:**
- `400` — Invalid JSON
- `401` — Invalid API key
- `429` — Rate limited (too many failed attempts from this IP)

## Get API Key

```
GET /api/system/api-key
```

Returns the current API key. **Localhost only.**

**Response:**
```json
{
  "key": "abc123..."
}
```

**Errors:**
- `403` — Not accessed from localhost

## Regenerate API Key

```
POST /api/system/api-key/regenerate
```

Generates a new API key, invalidating the previous one. All existing sessions authenticated with the old key continue to work. **Localhost only.**

**Response:**
```json
{
  "key": "new-key-456..."
}
```

**Errors:**
- `403` — Not accessed from localhost

## Auth Status

```
GET /api/system/auth-status
```

Returns the current authentication status and method used.

**Response:**
```json
{
  "authenticated": true,
  "method": "localhost"
}
```

| Method | Description |
|--------|-------------|
| `"localhost"` | Request came from localhost |
| `"key"` | Authenticated via API key header |
| `"session"` | Authenticated via session cookie |
| `"none"` | Not authenticated |
