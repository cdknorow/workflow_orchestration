# Webhooks API

Configure webhook notifications, send test deliveries, and review delivery history — all via REST.

Webhooks send HTTP POST callbacks to Slack, Discord, or any generic endpoint when agents need attention. For a user-focused guide on setting up webhooks through the dashboard, see [Webhook Notifications](../webhooks.md).

---

## List webhooks

```
GET /api/webhooks
```

Returns all configured webhooks.

### Response

```json
[
  {
    "id": 1,
    "name": "Slack #agents",
    "platform": "slack",
    "url": "https://hooks.slack.com/services/T.../B.../...",
    "enabled": 1,
    "event_filter": "*",
    "idle_threshold_seconds": 0,
    "agent_filter": null,
    "low_confidence_only": 0,
    "consecutive_failures": 0,
    "created_at": "2025-03-11T10:00:00+00:00",
    "updated_at": "2025-03-11T10:00:00+00:00"
  }
]
```

---

## Create a webhook

```
POST /api/webhooks
```

### Request body

| Field | Type | Default | Description |
|---|---|---|---|
| `name` | string | **required** | Display name for the webhook. |
| `platform` | string | `"generic"` | Target platform: `slack`, `discord`, or `generic`. |
| `url` | string | **required** | Destination URL. HTTPS required except for `localhost`/`127.0.0.1`. |
| `agent_filter` | string | `null` | Limit notifications to a specific agent name. `null` means all agents. |

### Example

```bash
curl -X POST http://localhost:8420/api/webhooks \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Slack #agents",
    "platform": "slack",
    "url": "https://hooks.slack.com/services/T.../B.../..."
  }'
```

### Response

Returns the created webhook object (same shape as the list response items).

### Errors

| Status | Body | Cause |
|---|---|---|
| 200 | `{"error": "name and url are required"}` | Missing required fields. |
| 200 | `{"error": "platform must be one of: discord, generic, slack"}` | Invalid platform value. |
| 200 | `{"error": "URL must use http or https"}` | Bad URL scheme. |
| 200 | `{"error": "HTTP (non-HTTPS) is only allowed for localhost"}` | Non-local HTTP URL. |

---

## Update a webhook

```
PATCH /api/webhooks/{webhook_id}
```

Updates one or more fields on an existing webhook. Only include the fields you want to change.

### Request body

| Field | Type | Description |
|---|---|---|
| `name` | string | New display name. |
| `platform` | string | `slack`, `discord`, or `generic`. |
| `url` | string | New destination URL (validated). |
| `enabled` | int | `1` to enable, `0` to disable. |
| `agent_filter` | string | Agent name filter, or `null` for all. |

### Example

```bash
# Disable a webhook
curl -X PATCH http://localhost:8420/api/webhooks/1 \
  -H "Content-Type: application/json" \
  -d '{"enabled": 0}'

# Re-enable and change URL
curl -X PATCH http://localhost:8420/api/webhooks/1 \
  -H "Content-Type: application/json" \
  -d '{"enabled": 1, "url": "https://hooks.slack.com/services/NEW/URL"}'
```

### Response

```json
{"ok": true}
```

---

## Delete a webhook

```
DELETE /api/webhooks/{webhook_id}
```

Deletes the webhook configuration and all its delivery history.

### Example

```bash
curl -X DELETE http://localhost:8420/api/webhooks/1
```

### Response

```json
{"ok": true}
```

!!! warning
    This permanently removes the webhook and all associated delivery records. This action cannot be undone.

---

## Send a test notification

```
POST /api/webhooks/{webhook_id}/test
```

Sends a test `needs_input` event immediately, bypassing the normal delivery queue. Use this to verify your webhook URL and platform configuration.

### Example

```bash
curl -X POST http://localhost:8420/api/webhooks/1/test
```

### Response

Returns the delivery record with the result of the test:

```json
{
  "id": 15,
  "webhook_id": 1,
  "agent_name": "corral-test",
  "session_id": null,
  "event_type": "needs_input",
  "event_summary": "Test notification from Corral dashboard",
  "status": "delivered",
  "http_status": 200,
  "error_msg": null,
  "attempt_count": 1,
  "next_retry_at": null,
  "delivered_at": "2025-03-11T10:05:00+00:00",
  "created_at": "2025-03-11T10:05:00+00:00"
}
```

| Field | Values |
|---|---|
| `status` | `delivered` (success) or `failed` (delivery error) |
| `http_status` | HTTP status code from the target, or `null` on network error |
| `error_msg` | Error details on failure, `null` on success |

Returns `{"error": "Webhook not found"}` if the webhook_id doesn't exist.

---

## List delivery history

```
GET /api/webhooks/{webhook_id}/deliveries?limit=50
```

Returns recent deliveries for a webhook, ordered newest first.

### Parameters

| Parameter | Type | Default | Description |
|---|---|---|---|
| `limit` | int | `50` | Max results (1–200). |

### Response

```json
[
  {
    "id": 15,
    "webhook_id": 1,
    "agent_name": "claude-a1b2c3d4",
    "session_id": "a1b2c3d4-...",
    "event_type": "needs_input",
    "event_summary": "Agent needs input — waiting for 12 minutes",
    "status": "delivered",
    "http_status": 200,
    "error_msg": null,
    "attempt_count": 1,
    "next_retry_at": null,
    "delivered_at": "2025-03-11T10:05:00+00:00",
    "created_at": "2025-03-11T10:05:00+00:00"
  }
]
```

| Field | Values |
|---|---|
| `status` | `pending`, `delivered`, or `failed` |
| `attempt_count` | Number of delivery attempts (max 3) |
| `next_retry_at` | Scheduled retry time for pending retries, `null` otherwise |

---

## Webhook config fields

Full reference for the webhook configuration object:

| Field | Type | Description |
|---|---|---|
| `id` | int | Auto-generated ID. |
| `name` | string | Display name. |
| `platform` | string | `slack`, `discord`, or `generic`. |
| `url` | string | Destination URL. |
| `enabled` | int | `1` = active, `0` = disabled. |
| `event_filter` | string | Event types to match. Default `*` (all). |
| `idle_threshold_seconds` | int | Custom idle threshold. Default `0` (uses system default of 300s). |
| `agent_filter` | string | Restrict to a specific agent name. `null` = all agents. |
| `low_confidence_only` | int | `1` = only fire on low-confidence events. Default `0`. |
| `consecutive_failures` | int | Failure counter for circuit breaker. Auto-disables at 10. |
| `created_at` | string | ISO 8601 creation timestamp. |
| `updated_at` | string | ISO 8601 last-modified timestamp. |

!!! info
    The `event_filter`, `idle_threshold_seconds`, and `low_confidence_only` fields exist in the schema but are not yet exposed in the dashboard UI. They can be set via the API for advanced use cases.

---

## Reliability

| Behavior | Detail |
|---|---|
| Retry schedule | 3 attempts with exponential backoff: 30s, 2m, 10m |
| Circuit breaker | Auto-disables webhook after 10 consecutive failures |
| Delivery pruning | Only the latest 200 non-pending deliveries kept per webhook |
| Dispatcher interval | Pending deliveries flushed every 15 seconds |
| Delivery timeout | 10 seconds per HTTP attempt |
| URL validation | HTTPS required, except `http://localhost` and `http://127.0.0.1` |
