/* Connected Apps: provider cards, connection list, OAuth flow */

import { showView, showToast } from './utils.js';
import { apiFetch } from './api.js';

let providers = [];
let connections = [];
let currentProviderId = null;

// ── API helpers ────────────────────────────────────────────────────────

async function fetchProviders() {
    try {
        const data = await apiFetch('/api/connected-apps/providers');
        providers = data.providers || data || [];
    } catch (e) {
        console.error('Failed to fetch providers:', e);
        providers = [];
    }
}

async function fetchConnections() {
    try {
        const data = await apiFetch('/api/connected-apps');
        connections = data.connections || data || [];
    } catch (e) {
        console.error('Failed to fetch connections:', e);
        connections = [];
    }
}

// ── Initialization ─────────────────────────────────────────────────────

export async function showConnectedApps() {
    // Block access in prod/staging builds (defense-in-depth — button is already hidden)
    try {
        const status = await apiFetch('/api/system/status');
        if (status.tier_name === 'prod' || status.tier_name === 'staging') return;
    } catch { /* allow if status fetch fails */ }

    showView('connected-apps-view');
    await Promise.all([fetchProviders(), fetchConnections()]);
    renderProviders();
    renderConnections();
}

// ── Render connections ─────────────────────────────────────────────────

function renderConnections() {
    const list = document.getElementById('ca-connections-list');
    const empty = document.getElementById('ca-connections-empty');
    if (!list) return;

    if (!connections.length) {
        empty.style.display = '';
        list.innerHTML = '';
        return;
    }
    empty.style.display = 'none';

    list.innerHTML = connections.map(conn => {
        const provider = providers.find(p => p.id === conn.provider_id);
        const icon = provider ? provider.icon : 'link';
        const providerName = provider ? provider.name : conn.provider_id;

        const accountInfo = conn.account_email
            ? `<span>${esc(conn.account_email)}</span>`
            : conn.account_name
                ? `<span>${esc(conn.account_name)}</span>`
                : '';

        const envVar = 'CORAL_TOKEN_' + conn.provider_id.toUpperCase().replace(/-/g, '_') + '_' + conn.name.toUpperCase().replace(/\s+/g, '_').replace(/[^A-Z0-9_]/g, '');

        return `<div class="ca-connection-card">
            <div class="ca-connection-icon">
                <span class="material-icons">${esc(icon)}</span>
            </div>
            <div class="ca-connection-info">
                <div class="ca-connection-name">${esc(conn.name)}</div>
                <div class="ca-connection-detail">
                    <span>${esc(providerName)}</span>
                    ${accountInfo}
                    <span class="ca-status ${esc(conn.status)}">${esc(conn.status)}</span>
                </div>
                <div class="ca-connection-envvar" title="Use this env var in workflow steps">
                    <span class="material-icons" style="font-size:12px">code</span>
                    <code>$${esc(envVar)}</code>
                </div>
            </div>
            <div class="ca-connection-actions">
                <button class="btn btn-sm" onclick="testConnectedApp(${conn.id})">Test</button>
                <button class="btn btn-sm" style="color:#f85149" onclick="disconnectApp(${conn.id}, '${escAttr(conn.name)}')">Disconnect</button>
            </div>
        </div>`;
    }).join('');
}

// ── Render providers ───────────────────────────────────────────────────

function renderProviders() {
    const container = document.getElementById('ca-providers');
    if (!container) return;

    container.innerHTML = providers.map(p => {
        const scopeLabels = (p.scopes || []).map(s => {
            const short = s.includes('/') ? s.split('/').pop().replace(/_/g, ' ') : s;
            return esc(short);
        }).join(', ');

        const existingCount = connections.filter(c => c.provider_id === p.id).length;
        const countBadge = existingCount > 0
            ? `<span style="font-size:11px;color:var(--text-muted);margin-left:auto">${existingCount} connected</span>`
            : '';

        const hasEmbedded = p.has_credentials || p.one_click;
        const btnClass = hasEmbedded ? 'ca-provider-connect-btn ca-one-click' : 'ca-provider-connect-btn';
        const btnLabel = hasEmbedded ? 'Connect' : '+ Connect with your own credentials';

        return `<div class="ca-provider-card${hasEmbedded ? ' ca-provider-embedded' : ''}" onclick="showConnectAppModal('${esc(p.id)}')">
            <div class="ca-provider-top">
                <div class="ca-provider-icon">
                    <span class="material-icons">${esc(p.icon)}</span>
                </div>
                <span class="ca-provider-name">${esc(p.name)}</span>
                ${countBadge}
            </div>
            <div class="ca-provider-scopes">${scopeLabels || 'No default scopes'}</div>
            <button class="${btnClass}" onclick="event.stopPropagation(); showConnectAppModal('${esc(p.id)}')">
                ${btnLabel}
            </button>
        </div>`;
    }).join('');
}

// ── Connect modal ──────────────────────────────────────────────────────

export function showConnectAppModal(providerId) {
    currentProviderId = providerId;
    const provider = providers.find(p => p.id === providerId);
    if (!provider) return;

    const hasEmbedded = provider.has_credentials || provider.one_click;

    document.getElementById('ca-modal-title').textContent = `Connect ${provider.name}`;
    document.getElementById('ca-input-name').value = '';
    document.getElementById('ca-modal-error').style.display = 'none';

    // Show/hide credential fields based on whether provider has embedded creds
    const credFields = document.getElementById('ca-credential-fields');
    if (credFields) credFields.style.display = hasEmbedded ? 'none' : '';

    // Instructions: simplified for embedded, full for BYOC
    const instructionsEl = document.getElementById('ca-modal-instructions');
    if (hasEmbedded) {
        instructionsEl.textContent = `Give this connection a name (e.g. "Work ${provider.name}") and click Authorize. You'll be redirected to ${provider.name} to grant access.`;
    } else {
        instructionsEl.textContent = provider.instructions || '';
    }

    // Reset credential inputs
    const clientIdEl = document.getElementById('ca-input-client-id');
    const clientSecretEl = document.getElementById('ca-input-client-secret');
    if (clientIdEl) clientIdEl.value = '';
    if (clientSecretEl) clientSecretEl.value = '';

    // Callback URL
    const callbackEl = document.getElementById('ca-callback-url');
    if (callbackEl) callbackEl.textContent = `${location.origin}/api/connected-apps/callback`;

    // Scope checkboxes
    const scopesEl = document.getElementById('ca-scopes-checkboxes');
    scopesEl.innerHTML = (provider.scopes || []).map((s, i) => {
        const short = s.includes('/') ? s.split('/').pop().replace(/_/g, ' ') : s;
        return `<label class="ca-scope-item">
            <input type="checkbox" value="${escAttr(s)}" checked>
            ${esc(short)}
        </label>`;
    }).join('');

    // Update authorize button text
    const saveBtn = document.querySelector('#ca-connect-modal .btn-primary');
    if (saveBtn) {
        saveBtn.innerHTML = hasEmbedded
            ? '<span class="material-icons" style="font-size:16px">launch</span> Connect'
            : '<span class="material-icons" style="font-size:16px">launch</span> Authorize';
    }

    document.getElementById('ca-connect-modal').style.display = 'flex';
}

export function hideConnectAppModal() {
    document.getElementById('ca-connect-modal').style.display = 'none';
    currentProviderId = null;
}

// ── OAuth flow ─────────────────────────────────────────────────────────

export async function startOAuthFlow() {
    const provider = providers.find(p => p.id === currentProviderId);
    const hasEmbedded = provider && provider.has_credentials || provider.one_click;

    const name = document.getElementById('ca-input-name').value.trim();
    const errEl = document.getElementById('ca-modal-error');

    if (!name) { showError(errEl, 'Connection name is required'); return; }

    // Collect checked scopes
    const scopeCheckboxes = document.querySelectorAll('#ca-scopes-checkboxes input[type="checkbox"]:checked');
    const scopes = Array.from(scopeCheckboxes).map(cb => cb.value);

    const body = {
        provider_id: currentProviderId,
        name: name,
        scopes: scopes,
    };

    // Only require client credentials if provider doesn't have embedded ones
    if (!hasEmbedded) {
        const clientId = document.getElementById('ca-input-client-id').value.trim();
        const clientSecret = document.getElementById('ca-input-client-secret').value.trim();
        if (!clientId) { showError(errEl, 'Client ID is required'); return; }
        if (!clientSecret) { showError(errEl, 'Client Secret is required'); return; }
        body.client_id = clientId;
        body.client_secret = clientSecret;
    }

    try {
        const result = await apiFetch('/api/connected-apps/auth/start', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        });

        if (result.auth_url) {
            window.open(result.auth_url, '_blank');
            hideConnectAppModal();
            showToast('Authorization opened in browser. Complete the flow, then return here.');
            pollForNewConnection(name);
        }
    } catch (e) {
        let msg = e.message;
        try { const j = JSON.parse(msg.replace(/^\d+:\s*/, '')); msg = j.error || msg; } catch (_) {}
        showError(errEl, msg);
    }
}

// Poll until the connection appears (after OAuth callback completes)
async function pollForNewConnection(name) {
    let attempts = 0;
    const maxAttempts = 30; // 30 * 2s = 60s
    const interval = setInterval(async () => {
        attempts++;
        if (attempts > maxAttempts) {
            clearInterval(interval);
            return;
        }
        await fetchConnections();
        const found = connections.find(c => c.name === name);
        if (found) {
            clearInterval(interval);
            renderConnections();
            renderProviders();
            showToast(`Connected: ${name}`);
        }
    }, 2000);
}

// ── Test connection ────────────────────────────────────────────────────

export async function testConnectedApp(id) {
    try {
        const result = await apiFetch(`/api/connected-apps/${id}/test`, { method: 'POST' });
        showToast('Connection test successful');
    } catch (e) {
        showToast('Connection test failed: ' + e.message, true);
    }
}

// ── Disconnect ─────────────────────────────────────────────────────────

export async function disconnectApp(id, name) {
    if (!confirm(`Disconnect "${name}"? This will revoke the OAuth tokens.`)) return;
    try {
        await apiFetch(`/api/connected-apps/${id}`, { method: 'DELETE' });
        showToast(`Disconnected "${name}"`);
        await fetchConnections();
        renderConnections();
        renderProviders();
    } catch (e) {
        showToast('Failed to disconnect: ' + e.message, true);
    }
}

// ── Helpers ────────────────────────────────────────────────────────────

function esc(s) {
    if (s == null) return '';
    const d = document.createElement('div');
    d.textContent = String(s);
    return d.innerHTML;
}

function escAttr(s) {
    return esc(s).replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

function showError(el, msg) {
    el.textContent = msg;
    el.style.display = '';
}
