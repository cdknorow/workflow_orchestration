# Doc Feature Guide: Webhook Notifications

## Overview

Webhook Notifications send HTTP POST alerts when agents need attention. Instead of watching the dashboard, users can route notifications to Slack, Discord, or any HTTP endpoint. Coral supports three platforms with built-in retry logic and a circuit breaker for reliability.

---

## Key Source Files & Architecture

| File | Role |
|------|------|
| `src/coral/store/webhooks.py` | SQLite CRUD for `webhook_configs` and `webhook_deliveries` tables |
| `src/coral/api/webhooks.py` | FastAPI routes for webhook management and delivery history |
| `src/coral/background_tasks/idle_detector.py` | Polls live sessions to detect agents waiting for input; fires `needs_input` events |
| `src/coral/background_tasks/webhook_dispatcher.py` | Processes pending deliveries with retries and circuit breaker logic |

### Database Tables

| Table | Purpose |
|-------|---------|
| `webhook_configs` | Webhook definitions — name, platform, URL, enabled flag, event/agent filters, consecutive failure count |
| `webhook_deliveries` | Delivery log — webhook_id, agent, event type, status, HTTP status, error, attempt count, retry schedule |

### Architecture Flow

1. **Idle Detector** runs every ~60 seconds, scanning all live sessions for agents waiting for input
2. When an agent has been idle for 5+ minutes, it creates a `needs_input` event
3. Event is dispatched to all matching webhooks (enabled, event filter matches, agent filter matches)
4. **Webhook Dispatcher** runs every ~15 seconds, processing pending deliveries:
   - Formats payload per platform (Slack blocks, Discord embeds, or generic JSON)
   - Sends HTTP POST with 10-second timeout
   - On failure: retries up to 3 times with exponential backoff (30s, 2m, 10m)
   - After 10 consecutive failures: auto-disables the webhook (circuit breaker)
5. One-shot behavior: notification fires once per waiting period, resets when agent becomes active

---

## User-Facing Functionality & Workflows

### Setting Up a Webhook

1. Click **Webhooks** button in the top toolbar
2. Click **+Add Webhook**
3. Fill in: Name, Platform (Slack/Discord/Generic), URL, Agent Filter (optional), Enabled toggle
4. Click **Save**
5. Click **Test** to verify connectivity with a synthetic event

### Platform-Specific Setup

- **Slack**: Create incoming webhook via api.slack.com → paste URL
- **Discord**: Channel Settings → Integrations → Webhooks → copy URL
- **Generic**: Any endpoint accepting JSON POST, returns 2xx

### Managing Webhooks

- **Edit**: Click a webhook to modify settings
- **Disable/Enable**: Toggle the enabled switch
- **Delete**: Remove webhook and delivery history
- **History**: View up to 50 recent deliveries with status, agent, event, timestamp
- **Auto-disable recovery**: Fix the endpoint, re-enable, and Test to confirm

---

## Suggested MkDocs Page Structure

### Title: "Webhook Notifications"

1. **Introduction** — What webhooks do and why they matter
2. **How It Works** — Detection → dispatch → delivery pipeline
   - Idle detection threshold, one-shot behavior
3. **Setting Up Webhooks** — Step-by-step with screenshots
   - Creating a webhook via the UI
   - Platform-specific guides (Slack, Discord, Generic)
4. **Testing Webhooks** — The Test button and what it sends
5. **Delivery History** — Viewing past deliveries, debugging failures
6. **Managing Webhooks** — Edit, disable, delete, auto-disable recovery
7. **Events** — Table of supported event types (`needs_input`)
   - Event schema (agent_name, session_id, event_type, event_summary)
8. **Payload Formats** — Platform-specific JSON examples
   - Slack (blocks), Discord (embeds), Generic (flat JSON)
9. **Reliability Details** — Table of parameters
   - Retry schedule, circuit breaker threshold, timeout, history retention
10. **API Reference** — Endpoint table

### Screenshots to Include

- Webhook modal showing configured webhooks list
- Add/edit webhook form
- Delivery history view with status indicators
- Slack/Discord notification examples (if available)

### Code Examples

- Payload JSON for each platform (Slack, Discord, Generic)
- curl examples for API access

---

## Important Details for Technical Writer

1. **Event types**: Currently only `needs_input` is supported. The schema is extensible for future events.
2. **Idle threshold**: 5 minutes of continuous idle before the event fires. Configurable per-webhook via `idle_threshold_seconds` in the DB but defaults to the global 5-minute threshold.
3. **One-shot behavior**: A `needs_input` event fires once per waiting period. It won't repeat until the agent becomes active and then re-enters waiting state.
4. **Circuit breaker**: After 10 consecutive failures, the webhook is auto-disabled. The `consecutive_failures` counter resets on successful delivery.
5. **URL validation**: HTTPS is required except for `localhost` URLs (for local development).
6. **Agent filter**: Optional — limits notifications to specific agents. When empty, all agents trigger the webhook.
7. **Low confidence filter**: The `low_confidence_only` field in `webhook_configs` can restrict notifications to low-confidence events only.
8. **Delivery retention**: Latest 200 deliveries per webhook are kept.
