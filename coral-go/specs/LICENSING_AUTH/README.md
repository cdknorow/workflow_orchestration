# Licensing & Authentication Strategy

## Overview

Coral serves three distinct deployment models, each with different auth and
licensing requirements. This spec defines the tiered approach and what to build
in each phase.

## Product Tiers

### Solo (Ship Now)

Single user, local machine. The product as it exists today.

- **Auth:** API key (auto-generated, localhost bypass)
- **License:** Machine activation via Lemon Squeezy (3 machines per key)
- **Identity:** None — the machine *is* the user
- **Use case:** Developer running Coral on their laptop, personal remote server

### Team (Phase 2)

Multiple users on a shared Coral server. Login required.

- **Auth:** Google OAuth (identifies who is using the server)
- **License:** License key per team/org with N seats
- **Identity:** Google account (email, name, avatar)
- **Use case:** Engineering team running Coral on a shared dev server, each
  person sees their own agents + shared team boards

### Managed Service (Phase 3)

Coral hosted for customers. Full multi-tenant.

- **Auth:** Google OAuth (mandatory)
- **License:** Subscription via Lemon Squeezy (usage-based billing)
- **Identity:** Google email → Lemon Squeezy customer → entitlement check
- **Use case:** Coral as a hosted product, customer onboarding, billing dashboard

---

## Current State (Solo)

### Machine-Based Licensing

```
User purchases license on Lemon Squeezy
    ↓
Enters license key in Coral activation UI
    ↓
Coral generates machine fingerprint (sha256 of hostname + MAC addresses)
    ↓
POST to Lemon Squeezy /v1/licenses/activate
    ↓
Receives instance_id, stores license.json locally
    ↓
Revalidates every 7 days, 30-day offline grace period
```

### What Works

- Per-machine activation with fingerprinting
- Offline grace (30 days)
- Low-overhead revalidation (7-day window)
- Edition-based feature limits (forDropbox: 2 teams, 8 agents)
- Dev mode bypass (`--dev`, `CORAL_DEV=1`, `SkipLicense` ldflags)

### What's Missing for Team/Managed

- No concept of *who* is using the server
- No per-user session tracking
- No concurrent session enforcement across machines
- No shared user state (settings, history)
- API key auth bypasses localhost entirely — fine for Solo, not for shared servers

---

## Phase 2: Team — Google OAuth

### Why Google Auth

- No password management, no account creation friction
- Google handles MFA, account recovery, email verification
- Email-based identity maps directly to Lemon Squeezy customers later
- Most engineering teams already use Google Workspace
- Free tier covers all our needs

### OAuth Flow

#### Desktop (localhost)

```
User clicks "Sign in with Google"
    ↓
Browser opens: https://accounts.google.com/o/oauth2/v2/auth
    ?client_id=<CORAL_GOOGLE_CLIENT_ID>
    &redirect_uri=http://localhost:8420/auth/google/callback
    &response_type=code
    &scope=openid email profile
    ↓
User consents → Google redirects to localhost:8420/auth/google/callback?code=xxx
    ↓
Server exchanges code for tokens (server-side, secure)
    POST https://oauth2.googleapis.com/token
    ↓
Server extracts user info from ID token (email, name, picture)
    ↓
Creates/updates user record in local SQLite
    ↓
Sets session cookie → user is logged in
```

#### Remote Server

Same flow, but redirect URI is the server's public URL:
```
redirect_uri=https://coral.mycompany.com/auth/google/callback
```

Google requires each redirect URI to be pre-registered. Options:
1. **Self-hosted:** Customer registers their own Google OAuth app (most secure)
2. **Wildcard subdomain:** We register `*.coral.app` and provide subdomains
3. **Auth relay:** Our server proxies the OAuth callback (adds dependency)

Recommendation: Option 1 for Team, Option 2 for Managed Service.

### New Endpoints

```
GET  /auth/google           → Redirect to Google OAuth consent screen
GET  /auth/google/callback  → Handle OAuth callback, create session
GET  /auth/me               → Return current user info
POST /auth/logout           → Clear session
GET  /auth/users            → List users (admin only)
```

### User Record (SQLite)

```sql
CREATE TABLE users (
    id          TEXT PRIMARY KEY,   -- UUID
    email       TEXT UNIQUE NOT NULL,
    name        TEXT,
    picture     TEXT,               -- Google avatar URL
    role        TEXT DEFAULT 'member',  -- 'admin', 'member'
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_login  DATETIME
);
```

Stored in `sessions.db` alongside existing tables.

### Session Management

- Session token stored as HTTP-only secure cookie
- Token is a signed JWT (HS256 with server-generated secret stored in CoralDir)
- JWT contains: user ID, email, issued-at, expiry
- Token expiry: 30 days (long-lived for desktop convenience)
- Refresh: re-auth with Google on expiry

### Auth Middleware Changes

Current auth flow:
```
Request → API Key middleware → if localhost, bypass → if valid key, allow
```

Team auth flow:
```
Request → if localhost + Solo mode, bypass (current behavior)
        → if Team mode, check session cookie → extract user → attach to context
        → if no session, redirect to /auth/google
```

The mode is determined by config:
```go
type Config struct {
    // ...
    AuthMode string // "solo" (default), "team", "managed"
}
```

`solo` preserves current behavior. `team` requires Google login for non-localhost.
`managed` requires Google login for everyone.

### Per-User Features (unlocked by login)

Once we know *who* is using Coral:

- **Agent ownership:** "launched by <user>" visible in the dashboard
- **Personal settings:** Theme, layout preferences stored per-user
- **Activity feed:** Who did what, when
- **Access control:** Admin can restrict who launches agents
- **Audit log:** Compliance-friendly history of actions
- **Shared boards with attribution:** Messages show author name + avatar

---

## Phase 3: Managed Service — Lemon Squeezy + Google Auth

### Identity → Entitlement Flow

```
User signs in with Google (email: alice@company.com)
    ↓
Server looks up Lemon Squeezy customer by email
    GET https://api.lemonsqueezy.com/v1/customers?filter[email]=alice@company.com
    ↓
Found → check subscription status + plan
    ↓
Active subscription → grant access based on plan limits
No subscription → show upgrade/purchase page
```

### Concurrent Session Enforcement

For "3 concurrent Coral sessions per user":

```
User authenticates on machine A
    ↓
Server records: { user: alice, machine: fingerprint-A, last_seen: now }
    ↓
Heartbeat every 15 minutes updates last_seen
    ↓
User authenticates on machine B, C → same tracking
    ↓
User tries machine D → server checks active sessions
    → 3 sessions with last_seen < 30 min ago → reject
    → Session on machine A hasn't heartbeated in 30 min → slot freed → allow
```

**Where does this tracking live?**

- **Solo:** Not needed (machine activation handles it)
- **Team (self-hosted):** Local SQLite table, enforced per-server
- **Managed:** Central database (Postgres/Supabase), enforced across all servers

### Session Tracking Table (Managed)

```sql
CREATE TABLE active_sessions (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL,
    machine_id  TEXT NOT NULL,       -- fingerprint
    server_url  TEXT,                -- which Coral instance
    last_seen   DATETIME NOT NULL,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, machine_id)
);
```

### Plan Limits

| Plan | Concurrent Sessions | Teams | Agents | Price |
|------|-------------------|-------|--------|-------|
| Solo | 1 machine | 2 | 10 | $X/mo |
| Pro | 3 machines | Unlimited | Unlimited | $Y/mo |
| Team | N seats | Unlimited | Unlimited | $Z/seat/mo |

---

## Implementation Order

### Now (Solo — no code changes needed)
- Ship with current machine-based Lemon Squeezy licensing
- Set activation limit to 3 per license key in Lemon Squeezy dashboard
- Current API key auth for remote access

### Phase 2 (Team — Google OAuth)
1. Register Google OAuth app (Google Cloud Console)
2. Add `/auth/google` and `/auth/google/callback` endpoints
3. Add `users` table to SQLite
4. Add session cookie / JWT infrastructure
5. Add `AuthMode` config: `solo` (default) | `team`
6. Update auth middleware to check session cookie in team mode
7. Add user info to agent launch records
8. Add user display in dashboard (avatar, "launched by")

### Phase 3 (Managed — LS + Google converge)
1. Add Lemon Squeezy customer lookup by email
2. Add concurrent session tracking + heartbeat
3. Add plan-based limits (teams, agents, seats)
4. Build subscription management UI
5. Add usage tracking / analytics

---

## Configuration

### Solo (default, current)
```bash
coral --dev --host 127.0.0.1 --port 8420
# No auth config needed — API key + localhost bypass
```

### Team
```bash
export CORAL_AUTH_MODE=team
export CORAL_GOOGLE_CLIENT_ID=xxx.apps.googleusercontent.com
export CORAL_GOOGLE_CLIENT_SECRET=xxx
coral --host 0.0.0.0 --port 8420
# Users must sign in with Google
```

### Managed
```bash
export CORAL_AUTH_MODE=managed
export CORAL_GOOGLE_CLIENT_ID=xxx
export CORAL_GOOGLE_CLIENT_SECRET=xxx
export CORAL_LEMONSQUEEZY_API_KEY=xxx
coral --host 0.0.0.0 --port 443 --tls-cert cert.pem --tls-key key.pem
# Full auth + entitlement checking
```

---

## Security Considerations

### OAuth Token Storage
- Client ID/secret stored as env vars, never in code or config files
- JWT signing secret auto-generated on first run, stored in CoralDir
- Session cookies: HTTP-only, Secure flag (when HTTPS), SameSite=Lax

### Remote Server Hardening
- Team/Managed mode should enforce HTTPS (warn if not)
- CORS restricted to server's own origin
- Rate limiting on auth endpoints to prevent abuse
- Session revocation on password change (via Google's token revocation)

### Offline Handling
- Solo: 30-day offline grace (current behavior)
- Team: JWT valid for 30 days without re-auth. Google profile cached locally.
- Managed: Shorter grace (7 days) — managed service implies connectivity

### Privacy
- Only email, name, and avatar stored from Google
- No access to user's Google Drive, Gmail, or other data
- Scope is minimal: `openid email profile`

---

## Decision Log

| Decision | Rationale |
|----------|-----------|
| Google OAuth over email/password | Zero friction, no password management, maps to LS customer email |
| Three tiers (Solo/Team/Managed) | Each has distinct auth/licensing needs; one size doesn't fit |
| Solo ships first without login | Lowest friction for initial launch; login adds complexity |
| Machine fingerprint for Solo licensing | Works offline, no server dependency, Lemon Squeezy handles limits |
| JWT for session tokens | Stateless, works across restarts, can be verified without DB lookup |
| Auth mode as config, not build flag | Same binary serves all tiers; deployment config determines behavior |
| Phase Google Auth before LS approval | Identity is independent of billing; unblocks Team features now |
| Self-hosted Google OAuth for Team | Customer controls their own auth; no dependency on our infrastructure |
