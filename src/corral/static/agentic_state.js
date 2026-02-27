/* Agentic State — event loading, timeline rendering, tab switching, filtering */

import { state } from './state.js';

const TOOL_ICONS = {
    Read:       { char: 'R', cls: 'tool-read' },
    Write:      { char: 'W', cls: 'tool-write' },
    Edit:       { char: 'E', cls: 'tool-edit' },
    Bash:       { char: '$', cls: 'tool-bash' },
    Grep:       { char: '?', cls: 'tool-grep' },
    Glob:       { char: '*', cls: 'tool-glob' },
    WebFetch:   { char: 'F', cls: 'tool-web' },
    WebSearch:  { char: 'S', cls: 'tool-web' },
    TaskCreate: { char: 'T', cls: 'tool-task' },
    TaskUpdate: { char: 'T', cls: 'tool-task' },
    TaskList:   { char: 'T', cls: 'tool-task' },
    TaskGet:    { char: 'T', cls: 'tool-task' },
    Task:       { char: 'A', cls: 'tool-agent' },
};

// Filter groups — maps display label to the set of tool_names/event_types it covers
const FILTER_GROUPS = [
    { key: 'read',   label: 'R', title: 'Read',          cls: 'tool-read',   match: ev => ev.tool_name === 'Read' },
    { key: 'write',  label: 'W', title: 'Write',         cls: 'tool-write',  match: ev => ev.tool_name === 'Write' },
    { key: 'edit',   label: 'E', title: 'Edit',          cls: 'tool-edit',   match: ev => ev.tool_name === 'Edit' },
    { key: 'bash',   label: '$', title: 'Bash',          cls: 'tool-bash',   match: ev => ev.tool_name === 'Bash' },
    { key: 'search', label: '?', title: 'Grep / Glob',   cls: 'tool-grep',   match: ev => ev.tool_name === 'Grep' || ev.tool_name === 'Glob' },
    { key: 'web',    label: 'W', title: 'Web',           cls: 'tool-web',    match: ev => ev.tool_name === 'WebFetch' || ev.tool_name === 'WebSearch' },
    { key: 'task',   label: 'T', title: 'Tasks',         cls: 'tool-task',   match: ev => ['TaskCreate','TaskUpdate','TaskList','TaskGet'].includes(ev.tool_name) },
    { key: 'agent',  label: 'A', title: 'Subagents',     cls: 'tool-agent',  match: ev => ev.tool_name === 'Task' },
    { key: 'status',     label: 'S', title: 'Status',          cls: 'tool-status',     match: ev => ev.event_type === 'status' },
    { key: 'goal',       label: 'G', title: 'Goal',            cls: 'tool-goal',       match: ev => ev.event_type === 'goal' },
    { key: 'confidence', label: 'C', title: 'Confidence',      cls: 'tool-confidence', match: ev => ev.event_type === 'confidence' },
    { key: 'system',     label: '!', title: 'Stop / Notify',   cls: 'tool-stop',       match: ev => ev.event_type === 'stop' || ev.event_type === 'notification' },
    { key: 'pulse',      label: 'P', title: 'Pulse (other)',   cls: 'tool-pulse',      match: ev => ev.event_type && !ev.tool_name && !['status','goal','confidence','stop','notification'].includes(ev.event_type) },
];

// Hidden filters are persisted per-session in state
// state.eventFiltersHidden = Set of filter keys that are hidden

function getHiddenFilters() {
    if (!state.eventFiltersHidden) {
        state.eventFiltersHidden = new Set();
    }
    return state.eventFiltersHidden;
}

// Known pulse event types — add new PULSE:<TYPE> entries here
const PULSE_ICONS = {
    status:       { char: 'S', cls: 'tool-status',     title: 'Status' },
    goal:         { char: 'G', cls: 'tool-goal',       title: 'Goal' },
    confidence:   { char: 'C', cls: 'tool-confidence', title: 'Confidence' },
    stop:         { char: '!', cls: 'tool-stop',       title: 'Stop' },
    notification: { char: 'N', cls: 'tool-notification', title: 'Notification' },
};

function getToolIcon(toolName, eventType) {
    // Check pulse event types first
    const pulse = PULSE_ICONS[eventType];
    if (pulse) return { ...pulse };
    // Tool-based events
    const icon = TOOL_ICONS[toolName];
    if (icon) return { ...icon, title: toolName };
    // Generic fallback for unknown pulse event types
    if (eventType && !toolName) {
        const label = eventType.charAt(0).toUpperCase();
        const title = eventType.charAt(0).toUpperCase() + eventType.slice(1);
        return { char: label, cls: 'tool-pulse', title };
    }
    return { char: '.', cls: 'tool-default', title: eventType || 'Event' };
}

function formatTime(isoStr) {
    if (!isoStr) return '';
    try {
        const d = new Date(isoStr);
        return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
    } catch {
        return '';
    }
}

function escapeAttr(str) {
    return (str || '').replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

function escapeHtml(str) {
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
}

function isEventVisible(ev) {
    const hidden = getHiddenFilters();
    if (hidden.size === 0) return true;
    for (const group of FILTER_GROUPS) {
        if (hidden.has(group.key) && group.match(ev)) return false;
    }
    return true;
}

export async function loadAgentEvents(agentName, sessionId) {
    if (!agentName) return;
    try {
        const params = new URLSearchParams({ limit: 50 });
        const sid = sessionId || (state.currentSession && state.currentSession.session_id);
        if (sid) params.set("session_id", sid);
        const resp = await fetch(`/api/sessions/live/${encodeURIComponent(agentName)}/events?${params}`);
        state.currentAgentEvents = await resp.json();
    } catch (e) {
        state.currentAgentEvents = [];
    }
    renderEventTimeline();
}

export function renderEventFilters() {
    const container = document.getElementById('event-filters');
    if (!container) return;

    const hidden = getHiddenFilters();
    const events = state.currentAgentEvents || [];

    const allHidden = hidden.size === FILTER_GROUPS.length;
    const noneHidden = hidden.size === 0;
    const toggleCls = noneHidden ? '' : allHidden ? 'filter-hidden' : 'filter-partial';

    const toggleBtn = `<button class="event-filter-chip filter-toggle ${toggleCls}"
                onclick="toggleAllEventFilters()"
                title="${noneHidden ? 'Hide all' : 'Show all'}">
                <span class="event-filter-char">*</span>
            </button>`;

    const chips = FILTER_GROUPS.map(group => {
        const count = events.filter(group.match).length;
        const isHidden = hidden.has(group.key);
        const dimClass = isHidden ? 'filter-hidden' : '';
        return `<button class="event-filter-chip ${group.cls} ${dimClass}"
                    onclick="toggleEventFilter('${group.key}')"
                    onmouseenter="showFilterPopup(event, '${group.key}')"
                    onmouseleave="hideFilterPopup()">
                    <span class="event-filter-char">${group.label}</span>
                </button>`;
    }).join('');

    container.innerHTML = toggleBtn + chips;
}

export function toggleEventFilter(key) {
    const hidden = getHiddenFilters();
    if (hidden.has(key)) {
        hidden.delete(key);
    } else {
        hidden.add(key);
    }
    renderEventFilters();
    renderEventTimeline();
}

export function toggleAllEventFilters() {
    const hidden = getHiddenFilters();
    if (hidden.size === 0) {
        // Hide all
        for (const group of FILTER_GROUPS) hidden.add(group.key);
    } else {
        // Show all
        hidden.clear();
    }
    renderEventFilters();
    renderEventTimeline();
}

export function renderEventTimeline() {
    const container = document.getElementById('agentic-state-events');
    if (!container) return;

    const events = state.currentAgentEvents || [];

    // Also update filter chips (counts may have changed)
    renderEventFilters();

    if (events.length === 0) {
        container.innerHTML = '<div class="event-empty">No activity yet</div>';
        return;
    }

    const visible = events.filter(isEventVisible);
    if (visible.length === 0) {
        container.innerHTML = '<div class="event-empty">All events filtered out</div>';
        return;
    }

    // Events come newest-first from API, display newest at top
    container.innerHTML = visible.map(ev => {
        const icon = getToolIcon(ev.tool_name, ev.event_type);
        const typeCls = ev.event_type ? `event-type-${ev.event_type}` : '';
        return `<div class="event-item ${typeCls}" data-tooltip="${icon.title}: ${escapeAttr(ev.summary)}">
            <span class="event-icon ${icon.cls}">${icon.char}</span>
            <span class="event-body">
                <span class="event-summary">${escapeHtml(ev.summary)}</span>
            </span>
            <span class="event-time">${formatTime(ev.created_at)}</span>
        </div>`;
    }).join('');

    renderLiveActivityChart();
}

function renderLiveActivityChart() {
    const container = document.getElementById('live-activity-chart');
    if (!container) return;

    const events = state.currentAgentEvents || [];
    if (events.length === 0) {
        container.innerHTML = '';
        return;
    }

    const counts = [];
    const matched = new Set();

    for (const group of FILTER_GROUPS) {
        let groupCount = 0;
        events.forEach((ev, idx) => {
            if (group.match(ev)) {
                groupCount++;
                matched.add(idx);
            }
        });
        if (groupCount > 0) {
            counts.push({ key: group.key, label: group.title, cls: group.cls, count: groupCount });
        }
    }

    const unmatchedCount = events.length - matched.size;
    if (unmatchedCount > 0) {
        counts.push({ key: 'other', label: 'Other', cls: 'tool-default', count: unmatchedCount });
    }

    if (counts.length === 0) {
        container.innerHTML = '';
        return;
    }

    counts.sort((a, b) => b.count - a.count);
    const maxCount = counts[0].count;

    container.innerHTML = counts.map(item => {
        const pct = Math.max(2, (item.count / maxCount) * 100);
        return `<div class="activity-chart-row">
            <span class="activity-chart-label">${escapeHtml(item.label)}</span>
            <div class="activity-chart-bar-track">
                <div class="activity-chart-bar ${item.cls}" style="width: ${pct}%"></div>
            </div>
            <span class="activity-chart-count">${item.count}</span>
        </div>`;
    }).join('');
}

let _popupTimer = null;

export function showFilterPopup(mouseEvent, key) {
    hideFilterPopup();
    const group = FILTER_GROUPS.find(g => g.key === key);
    if (!group) return;

    const events = (state.currentAgentEvents || []).filter(group.match);
    const hidden = getHiddenFilters();
    const isHidden = hidden.has(key);

    // Build popup content
    const statusText = isHidden ? 'Hidden — click to show' : 'Showing — click to hide';
    const recentItems = events.slice(0, 5);
    const recentHtml = recentItems.length > 0
        ? recentItems.map(ev =>
            `<div class="filter-popup-event">
                <span class="filter-popup-summary">${escapeHtml(ev.summary)}</span>
                <span class="filter-popup-time">${formatTime(ev.created_at)}</span>
            </div>`
        ).join('')
        : '<div class="filter-popup-empty">No events</div>';

    const popup = document.createElement('div');
    popup.className = 'filter-popup';
    popup.id = 'filter-popup';
    popup.innerHTML = `
        <div class="filter-popup-header">
            <span class="filter-popup-title">${escapeHtml(group.title)}</span>
            <span class="filter-popup-count">${events.length} event${events.length !== 1 ? 's' : ''}</span>
        </div>
        <div class="filter-popup-status">${statusText}</div>
        <div class="filter-popup-list">${recentHtml}</div>
        ${events.length > 5 ? `<div class="filter-popup-more">+${events.length - 5} more</div>` : ''}
    `;

    document.body.appendChild(popup);

    // Position relative to the chip
    const chip = mouseEvent.currentTarget;
    const rect = chip.getBoundingClientRect();
    const popupRect = popup.getBoundingClientRect();

    let left = rect.left + rect.width / 2 - popupRect.width / 2;
    // Clamp to viewport
    left = Math.max(4, Math.min(left, window.innerWidth - popupRect.width - 4));
    popup.style.left = left + 'px';
    popup.style.top = (rect.bottom + 6) + 'px';
}

export function hideFilterPopup() {
    const existing = document.getElementById('filter-popup');
    if (existing) existing.remove();
    if (_popupTimer) {
        clearTimeout(_popupTimer);
        _popupTimer = null;
    }
}

export function switchAgenticTab(tabName) {
    // Update tab buttons
    document.querySelectorAll('.agentic-tab').forEach(btn => btn.classList.remove('active'));
    const activeBtn = document.getElementById(`agentic-tab-${tabName}`);
    if (activeBtn) activeBtn.classList.add('active');

    // Update panels
    document.querySelectorAll('.agentic-panel').forEach(panel => panel.classList.remove('active'));
    const activePanel = document.getElementById(`agentic-panel-${tabName}`);
    if (activePanel) activePanel.classList.add('active');
}
