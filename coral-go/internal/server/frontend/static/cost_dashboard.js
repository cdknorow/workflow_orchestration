/* Token Usage Dashboard — proxy token tracking and cost UI */

import { showView, escapeHtml, escapeAttr } from './utils.js';
import { state } from './state.js';

function _formatTokens(n) {
    if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M';
    if (n >= 1000) return (n / 1000).toFixed(1) + 'K';
    return String(n);
}

function _formatCost(c) {
    if (c === 0) return '$0.00';
    if (c < 0.01) return '$' + c.toFixed(4);
    return '$' + c.toFixed(2);
}

function _formatLatency(ms) {
    if (ms == null) return '\u2014';
    if (ms >= 1000) return (ms / 1000).toFixed(1) + 's';
    return ms + 'ms';
}

function _formatDate(isoStr) {
    if (!isoStr) return '\u2014';
    try {
        const d = new Date(isoStr);
        return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
            + ' ' + d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
    } catch { return isoStr; }
}

function _findSession(sessionId) {
    if (!sessionId || !state.liveSessions) return null;
    return state.liveSessions.find(s => s.session_id === sessionId) || null;
}

let _costRefreshTimer = null;

export async function showCostDashboard() {
    showView('cost-dashboard-view');
    await _refreshCostDashboard();
    stopCostDashboard(); // clear any existing timer
    _costRefreshTimer = setInterval(_refreshCostDashboard, 15000);
}

export function stopCostDashboard() {
    if (_costRefreshTimer) {
        clearInterval(_costRefreshTimer);
        _costRefreshTimer = null;
    }
}

export async function _refreshCostDashboard() {
    const period = document.getElementById('cost-time-range')?.value || 'day';

    // Map period to a 'since' timestamp for the unified token-usage API
    const sinceMap = { 'hour': 1, 'day': 24, 'week': 168, 'month': 720, 'all': 0 };
    const hoursAgo = sinceMap[period] || 24;
    const since = hoursAgo > 0 ? new Date(Date.now() - hoursAgo * 3600000).toISOString() : '';
    const sinceParam = since ? `?since=${encodeURIComponent(since)}` : '';

    try {
        const [summaryResp, reqResp] = await Promise.all([
            fetch(`/api/token-usage/summary${sinceParam}`).catch(() => null),
            fetch('/api/proxy/requests?limit=100').catch(() => null),
        ]);

        if (summaryResp && summaryResp.ok) {
            const data = await summaryResp.json();
            const t = data.totals || {};
            _setText('cost-input-tokens', _formatTokens(t.input_tokens || 0));
            _setText('cost-output-tokens', _formatTokens(t.output_tokens || 0));
            _setText('cost-cache-read', _formatTokens(t.cache_read_tokens || 0));
            _setText('cost-cache-write', _formatTokens(t.cache_write_tokens || 0));
            _setText('cost-total-requests', String(t.num_sessions || 0));
            _setText('cost-total-cost', _formatCost(t.cost_usd || 0));

            // Render per-agent-type breakdown as model table
            _renderModelTable((data.by_agent_type || []).map(a => ({
                model: a.agent_type || 'unknown',
                requests: a.num_sessions || 0,
                input_tokens: a.input_tokens || 0,
                output_tokens: a.output_tokens || 0,
                cache_read_tokens: a.cache_read_tokens || 0,
                cache_write_tokens: a.cache_write_tokens || 0,
                cost_usd: a.cost_usd || 0,
            })));

            // Render per-agent (session) breakdown
            _renderAgentTable((data.by_agent || []).map(a => {
                let name = a.agent_name || 'unknown';
                if (a.board_name) name += ` (${a.board_name})`;
                return {
                    session_id: a.session_id,
                    agent_name: a.agent_name,
                    display_name: name,
                    requests: a.requests || 0,
                    input_tokens: a.input_tokens || 0,
                    output_tokens: a.output_tokens || 0,
                    cache_read_tokens: a.cache_read_tokens || 0,
                    cache_write_tokens: a.cache_write_tokens || 0,
                    cost_usd: a.cost_usd || 0,
                };
            }));
        }

        if (reqResp && reqResp.ok) {
            const data = await reqResp.json();
            _renderRequestLog(data.requests || []);
        }
    } catch (e) {
        console.error('[cost-dashboard] refresh error:', e);
    }
}

export function _costTimeRangeChanged() {
    _refreshCostDashboard();
}

function _setText(id, text) {
    const el = document.getElementById(id);
    if (el) el.textContent = text;
}

function _renderModelTable(rows) {
    const container = document.getElementById('cost-by-model');
    if (!container) return;

    if (rows.length === 0) {
        container.innerHTML = '<div class="cost-empty">No usage data yet</div>';
        return;
    }

    let html = `<table class="cost-table">
        <thead><tr>
            <th>Model</th>
            <th class="cost-col-right">Requests</th>
            <th class="cost-col-right">Input</th>
            <th class="cost-col-right">Output</th>
            <th class="cost-col-right">Cache Read</th>
            <th class="cost-col-right">Cache Write</th>
            <th class="cost-col-right">Cost</th>
        </tr></thead><tbody>`;

    for (const r of rows) {
        html += `<tr>
            <td class="cost-model-name">${escapeHtml(r.model)}</td>
            <td class="cost-col-right">${r.requests}</td>
            <td class="cost-col-right">${_formatTokens(r.input_tokens || 0)}</td>
            <td class="cost-col-right">${_formatTokens(r.output_tokens || 0)}</td>
            <td class="cost-col-right">${_formatTokens(r.cache_read_tokens || 0)}</td>
            <td class="cost-col-right">${_formatTokens(r.cache_write_tokens || 0)}</td>
            <td class="cost-col-right cost-cost-cell">${_formatCost(r.cost_usd)}</td>
        </tr>`;
    }

    html += '</tbody></table>';
    container.innerHTML = html;
}

function _renderAgentTable(rows) {
    const container = document.getElementById('cost-by-agent');
    if (!container) return;

    if (rows.length === 0) {
        container.innerHTML = '<div class="cost-empty">No usage data yet</div>';
        return;
    }

    let html = `<table class="cost-table">
        <thead><tr>
            <th>Agent</th>
            <th class="cost-col-right">Requests</th>
            <th class="cost-col-right">Input</th>
            <th class="cost-col-right">Output</th>
            <th class="cost-col-right">Cache Read</th>
            <th class="cost-col-right">Cache Write</th>
            <th class="cost-col-right">Cost</th>
        </tr></thead><tbody>`;

    for (const r of rows) {
        const session = _findSession(r.session_id);
        const displayName = r.display_name || (session && session.display_name) || r.agent_name || r.session_id || '\u2014';
        let nameHtml;
        if (session) {
            nameHtml = `<a href="#" class="cost-agent-link" onclick="event.preventDefault(); switchNavTab('agents'); selectLiveSession('${escapeAttr(session.name)}', '${escapeAttr(session.agent_type)}', '${escapeAttr(session.session_id)}')">${escapeHtml(displayName)}</a>`;
        } else if (r.session_id) {
            const endedSuffix = r.is_live === false ? ' <span class="cost-agent-ended">(ended)</span>' : '';
            nameHtml = `<a href="#" class="cost-agent-link cost-agent-terminated" onclick="event.preventDefault(); switchNavTab('history'); selectHistorySession('${escapeAttr(r.session_id)}')">${escapeHtml(displayName)}</a>${endedSuffix}`;
        } else {
            nameHtml = escapeHtml(displayName);
        }
        html += `<tr>
            <td class="cost-agent-name">${nameHtml}</td>
            <td class="cost-col-right">${r.requests}</td>
            <td class="cost-col-right">${_formatTokens(r.input_tokens || 0)}</td>
            <td class="cost-col-right">${_formatTokens(r.output_tokens || 0)}</td>
            <td class="cost-col-right">${_formatTokens(r.cache_read_tokens || 0)}</td>
            <td class="cost-col-right">${_formatTokens(r.cache_write_tokens || 0)}</td>
            <td class="cost-col-right cost-cost-cell">${_formatCost(r.cost_usd)}</td>
        </tr>`;
    }

    html += '</tbody></table>';
    container.innerHTML = html;
}

function _renderRequestLog(requests) {
    const container = document.getElementById('cost-request-log');
    if (!container) return;

    if (requests.length === 0) {
        container.innerHTML = '<div class="cost-empty">No requests yet</div>';
        return;
    }

    let html = `<table class="cost-table">
        <thead><tr>
            <th>Model</th>
            <th>Agent</th>
            <th class="cost-col-right">Input</th>
            <th class="cost-col-right">Output</th>
            <th class="cost-col-right">Cache R</th>
            <th class="cost-col-right">Cache W</th>
            <th class="cost-col-right">Cost</th>
            <th class="cost-col-right">Latency</th>
            <th>Status</th>
            <th>Time</th>
        </tr></thead><tbody>`;

    for (const r of requests) {
        const session = _findSession(r.session_id);
        const agentName = r.display_name || (session && session.display_name) || r.agent_name || '\u2014';
        const statusClass = r.status === 'completed' ? 'cost-status-ok'
            : r.status === 'error' ? 'cost-status-error'
            : 'cost-status-pending';
        html += `<tr>
            <td class="cost-model-name">${escapeHtml(r.model_used)}</td>
            <td class="cost-agent-name">${escapeHtml(agentName)}</td>
            <td class="cost-col-right">${_formatTokens(r.input_tokens)}</td>
            <td class="cost-col-right">${_formatTokens(r.output_tokens)}</td>
            <td class="cost-col-right">${_formatTokens(r.cache_read_tokens || 0)}</td>
            <td class="cost-col-right">${_formatTokens(r.cache_write_tokens || 0)}</td>
            <td class="cost-col-right cost-cost-cell">${_formatCost(r.cost_usd)}</td>
            <td class="cost-col-right">${_formatLatency(r.latency_ms)}</td>
            <td><span class="cost-status ${statusClass}">${escapeHtml(r.status)}</span></td>
            <td class="cost-date">${_formatDate(r.started_at)}</td>
        </tr>`;
    }

    html += '</tbody></table>';
    container.innerHTML = html;
}
