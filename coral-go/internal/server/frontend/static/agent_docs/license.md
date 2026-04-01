# License API

Coral uses [Lemon Squeezy](https://lemonsqueezy.com) for license management. License state is cached locally in `~/.coral/license.json` (AES-256-GCM encrypted, tied to machine fingerprint).

License endpoints are **ungated** — they work without an active license so users can activate.

**Revalidation schedule:** Licenses are re-checked against the Lemon Squeezy API every 7 days. An offline grace period of 30 days allows the app to work without network access.

**Build tiers:** License checking can be disabled entirely with the `dev` or `beta` build tags. See CLAUDE.md for details.

## Activate License

```
POST /api/license/activate
```

Activates a Lemon Squeezy license key on this machine.

**Request Body:**
```json
{
  "license_key": "XXXXX-XXXXX-XXXXX-XXXXX"
}
```

**Response (success):**
```json
{
  "valid": true,
  "customer_name": "Jane Doe",
  "customer_email": "jane@example.com",
  "activated_at": "2026-03-31T10:00:00Z"
}
```

**Response (failure):**
```json
{
  "valid": false,
  "error": "license activation failed"
}
```

Note: Activation failures return HTTP 200 with `"valid": false`, not an error status code.

**Errors:**
- `400` — Missing or empty `license_key`

## License Status

```
GET /api/license/status
```

Returns the current license status and cached details.

**Response (activated):**
```json
{
  "valid": true,
  "activated": true,
  "customer_name": "Jane Doe",
  "customer_email": "jane@example.com",
  "product_name": "Coral",
  "activated_at": "2026-03-01T00:00:00Z",
  "last_validated": "2026-03-30T12:00:00Z",
  "machine_id": "a1b2c3d4e5f6",
  "days_until_revalidation": 5,
  "trial_ends_at": ""
}
```

**Response (not activated):**
```json
{
  "valid": false,
  "activated": false
}
```

| Field | Description |
|-------|-------------|
| `valid` | Whether the license is currently valid |
| `activated` | Whether any license has been activated on this machine |
| `machine_id` | SHA-256 fingerprint of hostname + MAC addresses |
| `days_until_revalidation` | Days until next API re-check (0 = overdue) |
| `trial_ends_at` | ISO 8601 timestamp if on a trial, empty otherwise |

## Deactivate License

```
POST /api/license/deactivate
```

Deactivates the license on Lemon Squeezy and clears local state. This frees up the machine slot so the license can be used on another machine.

**Response (success):**
```json
{
  "ok": true
}
```

**Response (failure):**
```json
{
  "ok": false,
  "error": "deactivation failed — check your network connection and try again"
}
```

## Webhook

```
POST /api/license/webhook
```

Receives webhook events from Lemon Squeezy. Requires a configured webhook secret and valid HMAC-SHA256 signature.

**Headers:**
| Name | Description |
|------|-------------|
| `X-Signature` | HMAC-SHA256 hex digest of the request body |

**Handled Events:**

| Event | Action |
|-------|--------|
| `license_key.revoked` | Marks license as invalid |
| `subscription_expired` | Marks license as invalid |
| `subscription_cancelled` | Marks license as invalid (immediate) |
| `subscription_created` | Re-validates license (refreshes cache) |
| `subscription_updated` | Re-validates license (refreshes cache) |

**Response:** HTTP 200 (empty body)

**Errors:**
- `400` — Invalid payload
- `401` — Invalid signature
- `503` — Webhook secret not configured

## License Middleware

When license checking is enabled (prod builds), all API requests except the following are gated behind a valid license:

- `/api/license/*` — License management
- `/api/health`, `/api/system/status` — Health checks
- `/static/*` — Static assets
- `/` — Root page (shows activation UI)

Unlicensed requests receive:
```json
{
  "error": "license_required",
  "message": "A valid license is required. Please activate your license.",
  "activate": "/api/license/activate"
}
```
HTTP status: `403 Forbidden`
