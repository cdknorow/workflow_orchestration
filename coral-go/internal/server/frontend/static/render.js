/* Rendering functions for session lists, chat history, and status updates */

import { state } from './state.js';
import { escapeHtml, showToast, escapeAttr, dbg, showView } from './utils.js';
import { renderSidebarTagDots } from './tags.js';
import { getFolderTags, renderFolderTagPills } from './folder_tags.js';
import { updateSectionVisibility } from './sidebar.js';
import { syncMobileAgentList } from './mobile.js';

function formatShortTime(isoStr) {
    const d = new Date(isoStr);
    if (isNaN(d)) return "";
    const dd = String(d.getDate()).padStart(2, "0");
    const mm = String(d.getMonth() + 1).padStart(2, "0");
    const yy = String(d.getFullYear()).slice(-2);
    return `${dd}/${mm}/${yy}`;
}

function formatDuration(sec) {
    if (sec == null || sec <= 0) return '';
    if (sec < 60) return `${sec}s`;
    if (sec < 3600) return `${Math.round(sec / 60)}m`;
    const h = Math.floor(sec / 3600);
    const m = Math.round((sec % 3600) / 60);
    return m > 0 ? `${h}h ${m}m` : `${h}h`;
}

function formatStaleness(seconds) {
    if (seconds === null || seconds === undefined) return "Unknown";
    if (seconds < 5) return "Just now";
    if (seconds < 60) return `${Math.floor(seconds)}s ago`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
    return `${Math.floor(seconds / 3600)}h ago`;
}

function getStateLabel(s) {
    if (s.sleeping) return "Sleeping";
    if (s.waiting_for_input) return "Needs input";
    if (s.stuck) return "Stuck";
    if (s.working) return "Working";
    if (s.done) return "Done";
    return "Idle";
}

function getMobileStatusChip(s) {
    if (s.waiting_for_input) return { label: "Needs Input", className: "needs-input" };
    if (s.stuck) return { label: "Error", className: "error" };
    if (s.working) return { label: "Running", className: "running" };
    return { label: "Idle", className: "idle" };
}

function buildSessionTooltip(s) {
    const stateLabel = getStateLabel(s);
    const lastAction = formatStaleness(s.staleness_seconds);
    const goal = s.summary || "No goal set";
    const status = s.status || "No status";
    const branch = s.branch || "—";
    const agent = s.agent_type || "claude";

    let rows = [
        `<tr><td class="tt-label">State</td><td class="tt-value tt-state-${stateLabel.toLowerCase().replace(/ /g, '-')}">${escapeHtml(stateLabel)}</td></tr>`,
        `<tr><td class="tt-label">Last action</td><td class="tt-value">${escapeHtml(lastAction)}</td></tr>`,
        `<tr><td class="tt-label">Goal</td><td class="tt-value">${escapeHtml(goal)}</td></tr>`,
        `<tr><td class="tt-label">Status</td><td class="tt-value">${escapeHtml(status)}</td></tr>`,
        `<tr><td class="tt-label">Branch</td><td class="tt-value">${escapeHtml(branch)}</td></tr>`,
        `<tr><td class="tt-label">Agent</td><td class="tt-value">${escapeHtml(agent)}</td></tr>`,
    ];
    if (s.board_project) {
        const unreadBadge = s.board_unread > 0 ? ` <span class="tt-unread">(${s.board_unread} unread)</span>` : '';
        rows.push(`<tr><td class="tt-label">Board</td><td class="tt-value">${escapeHtml(s.board_project)}${unreadBadge}</td></tr>`);
        rows.push(`<tr><td class="tt-label">Role</td><td class="tt-value">${escapeHtml(s.board_job_title)}</td></tr>`);
    }
    return `<table class="session-tooltip-table">${rows.join("")}</table>`;
}

function getDotClass(s) {
    if (s.sleeping) return "sleeping";
    if (s.waiting_for_input) return "waiting";
    if (s.stuck) return "stuck";
    if (s.working) return "working";
    if (s.done) return "done";
    return "stale";
}

// ── Board accent colors (localStorage) ────────────────────────────────

function _getBoardColor(boardName) {
    try {
        const colors = JSON.parse(localStorage.getItem('coral-board-colors') || '{}');
        return colors[boardName] || null;
    } catch { return null; }
}

function _setBoardColor(boardName, color) {
    try {
        const colors = JSON.parse(localStorage.getItem('coral-board-colors') || '{}');
        if (color) {
            colors[boardName] = color;
        } else {
            delete colors[boardName];
        }
        localStorage.setItem('coral-board-colors', JSON.stringify(colors));
    } catch { /* ignore */ }
}

const _colorSwatches = [
    '#81a1c1', '#a3be8c', '#b48ead', '#d08770', '#bf616a',
    '#88c0d0', '#ebcb8b', '#8fbcbb', '#58a6ff', '#f778ba',
    '#7ee787', '#ffa657', '#ff7b72', '#d2a8ff', '#79c0ff',
    '#e6edf3', '#8b949e', '#484f58',
];

export function setBoardAccentColor(boardName) {
    // Remove any existing picker
    const existing = document.getElementById('board-color-picker');
    if (existing) { existing.remove(); return; }

    const current = _getBoardColor(boardName) || '#58a6ff';
    const picker = document.createElement('div');
    picker.id = 'board-color-picker';
    picker.className = 'color-picker-popover';
    picker.innerHTML = `
        <div class="color-picker-swatches">
            ${_colorSwatches.map(c =>
                `<button class="color-swatch${c === current ? ' active' : ''}" style="background:${c}" data-color="${c}" title="${c}"></button>`
            ).join('')}
        </div>
        <div class="color-picker-custom">
            <input type="text" class="color-picker-hex" value="${current}" placeholder="#hex" maxlength="7">
            <button class="btn btn-small color-picker-apply">Apply</button>
            <button class="btn btn-small color-picker-reset">Reset</button>
        </div>
    `;
    document.body.appendChild(picker);

    // Position near the clicked element
    const rect = event?.target?.getBoundingClientRect();
    if (rect) {
        picker.style.top = Math.min(rect.bottom + 4, window.innerHeight - 200) + 'px';
        picker.style.left = Math.min(rect.left, window.innerWidth - 220) + 'px';
    }

    const apply = (color) => {
        _setBoardColor(boardName, color);
        renderLiveSessions(state.liveSessions);
        picker.remove();
    };

    // Swatch clicks
    picker.querySelectorAll('.color-swatch').forEach(btn => {
        btn.addEventListener('click', (e) => { e.stopPropagation(); apply(btn.dataset.color); });
    });

    // Custom hex apply
    picker.querySelector('.color-picker-apply').addEventListener('click', (e) => {
        e.stopPropagation();
        const hex = picker.querySelector('.color-picker-hex').value.trim();
        if (/^#[0-9a-fA-F]{6}$/.test(hex)) apply(hex);
    });

    // Reset
    picker.querySelector('.color-picker-reset').addEventListener('click', (e) => {
        e.stopPropagation();
        _setBoardColor(boardName, null);
        renderLiveSessions(state.liveSessions);
        picker.remove();
    });

    // Close on outside click
    setTimeout(() => {
        document.addEventListener('click', function handler(e) {
            if (!picker.contains(e.target)) { picker.remove(); document.removeEventListener('click', handler); }
        });
    }, 0);
}

// ── Board Chat (right-panel tab) ──────────────────────────────────────

let _boardChatTimer = null;
let _boardChatLastId = {};
let _activeBoardChat = null;
let _boardChatMessages = [];
let _boardChatTotal = 0;
let _boardChatOffset = 0;
const _BOARD_CHAT_PAGE = 50;

export function showBoardChatTab(boardName) {
    _activeBoardChat = boardName;
    const panel = document.getElementById('agentic-panel-board');
    if (!panel) return;

    // Populate the board tab content (visibility managed by switchAgenticTab)
    panel.innerHTML = `
        <div class="board-chat-header">
            <a class="board-chat-title" href="#" onclick="event.preventDefault(); selectBoardProject('${escapeAttr(boardName)}')" title="Open full board view">${escapeHtml(boardName)}</a>
        </div>
        <div class="board-chat-messages" id="board-panel-msgs"></div>
        <div class="board-chat-input-pane" id="board-chat-input-pane">
            <div class="board-chat-resize-handle" id="board-chat-resize-handle"></div>
            <div class="command-pane-toolbar board-chat-toolbar">
                <div class="toolbar-group">
                    <button class="btn-nav" onclick="const i=document.getElementById('board-panel-input');if(i){i.value='@all '+i.value;i.focus()}" title="Notify all agents">@all</button>
                </div>
                <span class="toolbar-spacer"></span>
                <div class="toolbar-group">
                    <button class="btn-nav" onclick="window._sendBoardChat('${escapeAttr(boardName)}')" title="Send message">
                        <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><line x1="2" y1="8" x2="14" y2="8"/><polyline points="9 3 14 8 9 13"/></svg>
                    </button>
                </div>
            </div>
            <div class="board-chat-editor-wrap">
                <textarea class="board-chat-textarea" id="board-panel-input" placeholder="Message the team... (@ to mention)"
                    onkeydown="if(event.key==='Enter'&&event.ctrlKey){event.preventDefault();window._sendBoardChat('${escapeAttr(boardName)}')}"
                    oninput="window._boardMentionInput(event, '${escapeAttr(boardName)}')"></textarea>
                <div class="board-mention-dropdown" id="board-mention-dropdown" style="display:none"></div>
            </div>
        </div>`;

    // Restore saved input pane height
    const savedH = localStorage.getItem('coral-boardchat-height');
    if (savedH) {
        const pane = document.getElementById('board-chat-input-pane');
        const h = parseInt(savedH, 10);
        if (pane && h >= 80) pane.style.height = h + 'px';
    }

    // Reset pagination state for new board
    _boardChatMessages = [];
    _boardChatTotal = 0;
    _boardChatOffset = 0;
    _boardChatLastId[boardName] = null;

    _loadBoardPanelChat(boardName);
    _checkBoardPauseState(boardName);
    // Start polling
    if (_boardChatTimer) clearInterval(_boardChatTimer);
    _boardChatTimer = setInterval(() => _loadBoardPanelChat(boardName), 3000);
}

export function hideBoardChatTab() {
    _activeBoardChat = null;
    if (_boardChatTimer) { clearInterval(_boardChatTimer); _boardChatTimer = null; }
    const panel = document.getElementById('agentic-panel-board');
    if (panel) panel.innerHTML = '';
}

// Agent colors for board chat (same palette as message_board.js)
const _boardChatColors = [
    '#81a1c1', '#a3be8c', '#b48ead', '#d08770',
    '#bf616a', '#88c0d0', '#ebcb8b', '#8fbcbb',
];
const _boardChatColorMap = {};
function _getBoardChatColor(name) {
    if (!name) return _boardChatColors[0];
    if (_boardChatColorMap[name]) return _boardChatColorMap[name];
    const idx = Object.keys(_boardChatColorMap).length % _boardChatColors.length;
    _boardChatColorMap[name] = _boardChatColors[idx];
    return _boardChatColorMap[name];
}
function _renderMd(content) {
    if (!content) return '';
    if (typeof marked !== 'undefined') {
        try {
            const html = marked.parse(content);
            return typeof DOMPurify !== 'undefined' ? DOMPurify.sanitize(html) : html;
        } catch { /* fall through */ }
    }
    return escapeHtml(content);
}
function _formatTime(iso) {
    if (!iso) return '';
    const d = new Date(iso);
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

async function _loadBoardPanelChat(boardName) {
    const msgsEl = document.getElementById('board-panel-msgs');
    if (!msgsEl) return;
    try {
        // First call: get total count and load latest page
        const countResp = await fetch(`/api/board/${encodeURIComponent(boardName)}/messages/all?limit=1&offset=0&format=dashboard`);
        const countData = await countResp.json();
        const total = countData.total || (Array.isArray(countData) ? countData.length : 0);

        if (total === 0) {
            msgsEl.innerHTML = '<div class="board-chat-empty">No messages yet</div>';
            _boardChatMessages = [];
            _boardChatTotal = 0;
            return;
        }

        // Check if new messages arrived
        if (_boardChatTotal === total && _boardChatMessages.length > 0) return;

        // Load the latest page
        const startOffset = Math.max(0, total - _BOARD_CHAT_PAGE);
        const resp = await fetch(`/api/board/${encodeURIComponent(boardName)}/messages/all?limit=${_BOARD_CHAT_PAGE}&offset=${startOffset}&format=dashboard`);
        const data = await resp.json();
        const messages = Array.isArray(data) ? data : (data.messages || []);

        _boardChatMessages = messages;
        _boardChatTotal = total;
        _boardChatOffset = startOffset;

        _renderBoardPanelMessages(msgsEl, true);
    } catch { /* ignore */ }
}

async function _loadEarlierBoardChat() {
    if (!_activeBoardChat || _boardChatOffset <= 0) return;
    const msgsEl = document.getElementById('board-panel-msgs');
    if (!msgsEl) return;
    try {
        const newOffset = Math.max(0, _boardChatOffset - _BOARD_CHAT_PAGE);
        const fetchCount = _boardChatOffset - newOffset;
        const resp = await fetch(`/api/board/${encodeURIComponent(_activeBoardChat)}/messages/all?limit=${fetchCount}&offset=${newOffset}&format=dashboard`);
        const data = await resp.json();
        const messages = Array.isArray(data) ? data : (data.messages || []);

        // Save scroll height to preserve position after prepending
        const prevScrollHeight = msgsEl.scrollHeight;

        _boardChatMessages = [...messages, ..._boardChatMessages];
        _boardChatOffset = newOffset;

        _renderBoardPanelMessages(msgsEl, false);

        // Restore scroll position so user stays where they were
        msgsEl.scrollTop = msgsEl.scrollHeight - prevScrollHeight;
    } catch { /* ignore */ }
}
window._loadEarlierBoardChat = _loadEarlierBoardChat;

function _renderBoardPanelMessages(msgsEl, scrollToBottom) {
    const messages = _boardChatMessages;
    const wasAtBottom = scrollToBottom || msgsEl.scrollTop >= msgsEl.scrollHeight - msgsEl.clientHeight - 20;

    // "Load Earlier" button
    let loadEarlierHtml = '';
    if (_boardChatOffset > 0) {
        const remaining = _boardChatOffset;
        loadEarlierHtml = `<div style="text-align:center;padding:6px 0 8px">
            <button class="btn btn-small" onclick="_loadEarlierBoardChat()" style="font-size:11px;color:var(--text-muted)">
                ▲ Load ${Math.min(remaining, _BOARD_CHAT_PAGE)} earlier (${remaining} remaining)
            </button>
        </div>`;
    }

    msgsEl.innerHTML = loadEarlierHtml + messages.map((m, i) => {
        const agent = m.job_title || m.sender_name || 'Unknown';
        const color = _getBoardChatColor(agent);
        const prevAgent = i > 0 ? (messages[i - 1].job_title || messages[i - 1].sender_name || 'Unknown') : null;
        const sameAsPrev = agent === prevAgent;
        const spacing = sameAsPrev ? 'mb-message-grouped' : 'mb-message-first';
        const isLeader = /orchestrator/i.test(agent) || m.session_id === 'dashboard';
        const alignClass = isLeader ? ' board-msg-left' : ' board-msg-right';
        const r = parseInt(color.slice(1, 3), 16), g = parseInt(color.slice(3, 5), 16), b = parseInt(color.slice(5, 7), 16);

        return `<div class="mb-message ${spacing}${alignClass}" style="border-left:3px solid rgba(${r},${g},${b},0.55); border-bottom:2px solid rgba(${r},${g},${b},0.3)">
            <div class="mb-message-header">
                <span class="mb-agent-name" style="color:${color}">${m.icon ? escapeHtml(m.icon) + ' ' : ''}${escapeHtml(agent)}</span>
                <span class="mb-message-time">${_formatTime(m.created_at)}</span>
            </div>
            <div class="mb-message-body">${_renderMd(m.content)}</div>
        </div>`;
    }).join('');
    if (wasAtBottom) msgsEl.scrollTop = msgsEl.scrollHeight;
}

async function _sendBoardChat(boardName) {
    const inputEl = document.getElementById('board-panel-input');
    if (!inputEl) return;
    const content = inputEl.value.trim();
    if (!content) return;
    inputEl.value = '';
    try {
        await fetch(`/api/board/${encodeURIComponent(boardName)}/messages`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ session_id: 'dashboard', content }),
        });
        _loadBoardPanelChat(boardName);
    } catch { /* ignore */ }
}
window._sendBoardChat = _sendBoardChat;

async function _toggleBoardPause(boardName) {
    const btn = document.getElementById('board-pause-btn');
    const isPaused = btn?.classList.contains('paused');
    const endpoint = isPaused ? 'resume' : 'pause';
    try {
        await fetch(`/api/board/${encodeURIComponent(boardName)}/${endpoint}`, { method: 'POST' });
        if (btn) btn.classList.toggle('paused');
        showToast(isPaused ? `Board "${boardName}" resumed` : `Board "${boardName}" paused`);
    } catch { /* ignore */ }
}
window._toggleBoardPause = _toggleBoardPause;

async function _checkBoardPauseState(boardName) {
    try {
        const resp = await fetch(`/api/board/${encodeURIComponent(boardName)}/paused`);
        const data = await resp.json();
        const btn = document.getElementById('board-pause-btn');
        if (btn && data.paused) btn.classList.add('paused');
    } catch { /* ignore */ }
}

async function _clearBoardMessages(boardName) {
    showConfirmModal('Clear Messages', `Clear all messages from "${boardName}"? This cannot be undone.`, async () => {
        try {
            await fetch(`/api/board/${encodeURIComponent(boardName)}`, { method: 'DELETE' });
            _boardChatLastId[boardName] = null;
            _loadBoardPanelChat(boardName);
            showToast(`Cleared messages from "${boardName}"`);
        } catch { /* ignore */ }
    });
}
window._clearBoardMessages = _clearBoardMessages;

// Board chat @mention autocomplete
let _boardSubscribers = {};
async function _boardMentionInput(event, boardName) {
    const input = event.target;
    const text = input.value;
    const cursor = input.selectionStart;
    const beforeCursor = text.slice(0, cursor);
    const dropdown = document.getElementById('board-mention-dropdown');
    if (!dropdown) return;

    // Find @ trigger
    const atMatch = beforeCursor.match(/@(\w*)$/);
    if (!atMatch) { dropdown.style.display = 'none'; return; }
    const query = atMatch[1].toLowerCase();

    // Fetch subscribers if not cached
    if (!_boardSubscribers[boardName]) {
        try {
            const resp = await fetch(`/api/board/${encodeURIComponent(boardName)}/subscribers`);
            _boardSubscribers[boardName] = await resp.json();
        } catch { _boardSubscribers[boardName] = []; }
    }

    const subs = (_boardSubscribers[boardName] || [])
        .map(s => s.job_title || s.session_id || 'unknown')
        .filter(name => name.toLowerCase().includes(query));

    if (subs.length === 0) { dropdown.style.display = 'none'; return; }

    dropdown.innerHTML = subs.map(name =>
        `<div class="board-mention-item" onmousedown="event.preventDefault(); window._insertBoardMention('${escapeAttr(name)}')">${escapeHtml(name)}</div>`
    ).join('');
    dropdown.style.display = 'block';
}
window._boardMentionInput = _boardMentionInput;

function _insertBoardMention(name) {
    const input = document.getElementById('board-panel-input');
    const dropdown = document.getElementById('board-mention-dropdown');
    if (!input) return;
    const text = input.value;
    const cursor = input.selectionStart;
    const beforeCursor = text.slice(0, cursor);
    const atIdx = beforeCursor.lastIndexOf('@');
    if (atIdx === -1) return;
    const after = text.slice(cursor);
    input.value = beforeCursor.slice(0, atIdx) + '@' + name + ' ' + after;
    input.selectionStart = input.selectionEnd = atIdx + name.length + 2;
    input.focus();
    if (dropdown) dropdown.style.display = 'none';
}
window._insertBoardMention = _insertBoardMention;

// ── Session ordering ──────────────────────────────────────────────────

function _getSessionOrder() {
    try {
        return JSON.parse(state.settings.session_order || "[]");
    } catch { return []; }
}

function _sortByOrder(sessions) {
    const order = _getSessionOrder();
    if (!order.length) return sessions;
    const posMap = {};
    order.forEach((sid, i) => { posMap[sid] = i; });
    return [...sessions].sort((a, b) => {
        const pa = posMap[a.session_id] ?? 9999;
        const pb = posMap[b.session_id] ?? 9999;
        return pa - pb;
    });
}

async function _saveSessionOrder(orderedIds) {
    state.settings.session_order = JSON.stringify(orderedIds);
    try {
        await fetch("/api/settings", {
            method: "PUT",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ session_order: state.settings.session_order }),
        });
    } catch (e) {
        console.error("Failed to save session order:", e);
    }
}

// ── Group ordering ───────────────────────────────────────────────────

function _getGroupOrder() {
    try {
        return JSON.parse(state.settings.group_order || "[]");
    } catch { return []; }
}

function _sortGroups(groupEntries) {
    const order = _getGroupOrder();
    if (!order.length) return groupEntries;
    const posMap = {};
    order.forEach((name, i) => { posMap[name] = i; });
    return [...groupEntries].sort((a, b) => {
        const pa = posMap[a[0]] ?? 9999;
        const pb = posMap[b[0]] ?? 9999;
        return pa - pb;
    });
}

async function _saveGroupOrder(orderedNames) {
    state.settings.group_order = JSON.stringify(orderedNames);
    try {
        await fetch("/api/settings", {
            method: "PUT",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ group_order: state.settings.group_order }),
        });
    } catch (e) {
        console.error("Failed to save group order:", e);
    }
}

function _getCurrentGroupNames() {
    // Get current group names from the rendered sidebar
    const headers = document.querySelectorAll('.session-group-header[data-group-name]');
    return Array.from(headers).map(el => el.dataset.groupName);
}

export async function moveGroupUp(groupName) {
    console.log('[coral] moveGroupUp:', groupName);
    const names = _getCurrentGroupNames();
    console.log('[coral] group names:', names);
    const idx = names.indexOf(groupName);
    console.log('[coral] idx:', idx);
    if (idx <= 0) return;
    [names[idx - 1], names[idx]] = [names[idx], names[idx - 1]];
    await _saveGroupOrder(names);
    renderLiveSessions(state.liveSessions || []);
}

export async function moveGroupDown(groupName) {
    console.log('[coral] moveGroupDown:', groupName);
    const names = _getCurrentGroupNames();
    console.log('[coral] group names:', names);
    const idx = names.indexOf(groupName);
    console.log('[coral] idx:', idx);
    if (idx < 0 || idx >= names.length - 1) return;
    [names[idx], names[idx + 1]] = [names[idx + 1], names[idx]];
    await _saveGroupOrder(names);
    renderLiveSessions(state.liveSessions || []);
}

// ── Session reorder (Move Up/Down) ───────────────────────────────────

export function moveSessionUp(sessionId) {
    _moveSession(sessionId, -1);
}

export function moveSessionDown(sessionId) {
    _moveSession(sessionId, 1);
}

function _moveSession(sessionId, direction) {
    console.log('[coral] _moveSession called:', sessionId, direction);
    const list = document.getElementById("live-sessions-list");
    if (!list) { console.log('[coral] _moveSession: list not found'); return; }
    const items = [...list.querySelectorAll(".session-group-item")];
    const ids = items.map(el => el.dataset.sessionId);
    console.log('[coral] _moveSession: found', ids.length, 'items, looking for', sessionId);
    const idx = ids.indexOf(sessionId);
    if (idx < 0) { console.log('[coral] _moveSession: session not found in list'); return; }
    const targetIdx = idx + direction;
    if (targetIdx < 0 || targetIdx >= ids.length) { console.log('[coral] _moveSession: at boundary'); return; }
    [ids[idx], ids[targetIdx]] = [ids[targetIdx], ids[idx]];
    console.log('[coral] _moveSession: swapped, saving order');
    _saveSessionOrder(ids);
    renderLiveSessions(state.liveSessions);
}

// ── Group collapse state ─────────────────────────────────────────────
function _getCollapsedGroups() {
    try { return JSON.parse(localStorage.getItem('coral_collapsed_groups') || '[]'); }
    catch { return []; }
}

function _isGroupCollapsed(groupName) {
    return _getCollapsedGroups().includes(groupName);
}

export function toggleGroupCollapse(groupName) {
    const groups = _getCollapsedGroups();
    const idx = groups.indexOf(groupName);
    if (idx >= 0) groups.splice(idx, 1);
    else groups.push(groupName);
    localStorage.setItem('coral_collapsed_groups', JSON.stringify(groups));
    renderLiveSessions(state.liveSessions);
}

export function killSessionDirect(name, agentType, sessionId) {
    showConfirmModal('Kill Session', `Kill session "${name}"? This will terminate the agent.`, async () => { try {
        const resp = await fetch(`/api/sessions/live/${encodeURIComponent(name)}/kill`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ agent_type: agentType, session_id: sessionId }),
        });
        const result = await resp.json();
        if (result.error) { showToast(result.error, true); return; }
        showToast(`Killed: ${name}`);
        state.liveSessions = state.liveSessions.filter(s => s.session_id !== sessionId);
        if (state.currentSession && state.currentSession.session_id === sessionId) {
            state.currentSession = null;
            showView("welcome-screen");
        }
        renderLiveSessions(state.liveSessions);
    } catch (e) {
        showToast("Failed to kill session", true);
    } });
}

export async function showInfoDirect(name, agentType, sessionId) {
    const { selectLiveSession } = await import('./sessions.js');
    await selectLiveSession(name, agentType, sessionId);
    const { showInfoModal } = await import('./modals.js');
    showInfoModal();
}

export async function attachDirect(name, agentType, sessionId) {
    try {
        const resp = await fetch(`/api/sessions/live/${encodeURIComponent(name)}/attach`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ agent_type: agentType, session_id: sessionId }),
        });
        const result = await resp.json();
        if (result.error) showToast(result.error, true);
        else showToast("Terminal opened");
    } catch (e) {
        showToast("Failed to open terminal", true);
    }
}

export async function restartDirect(name, agentType, sessionId) {
    const { selectLiveSession } = await import('./sessions.js');
    await selectLiveSession(name, agentType, sessionId);
    const { restartSession } = await import('./controls.js');
    restartSession();
}

// ── Confirm Modal ────────────────────────────────────────────────────

export function showConfirmModal(title, message, onConfirm) {
    document.getElementById("confirm-modal-title").textContent = title;
    document.getElementById("confirm-modal-message").textContent = message;
    const yesBtn = document.getElementById("confirm-modal-yes");
    const newBtn = yesBtn.cloneNode(true);
    yesBtn.parentNode.replaceChild(newBtn, yesBtn);
    newBtn.addEventListener("click", () => {
        hideConfirmModal();
        onConfirm();
    });
    document.getElementById("confirm-modal").style.display = "flex";
}

export function hideConfirmModal() {
    const modal = document.getElementById("confirm-modal");
    modal.style.display = "none";
    // Remove any extra width classes added by export modal
    const content = modal.querySelector('.modal-content');
    if (content) content.classList.remove('modal-content-wide', 'modal-content-extra-wide');
}

export function copyFolderPath(path) {
    if (!path) return;
    navigator.clipboard.writeText(path).then(() => {
        showToast("Copied path to clipboard");
    });
}

export async function killGroup(groupName) {
    const groupSessions = state.liveSessions.filter(s => (s.name || 'unknown') === groupName);
    if (!groupSessions.length) return;

    showConfirmModal(
        "Kill All Agents",
        `Kill all ${groupSessions.length} agent(s) in "${groupName}"? This will terminate them.`,
        async () => {
            for (const s of groupSessions) {
                try {
                    await fetch(`/api/sessions/live/${encodeURIComponent(s.name)}/kill`, {
                        method: "POST",
                        headers: { "Content-Type": "application/json" },
                        body: JSON.stringify({ agent_type: s.agent_type, session_id: s.session_id }),
                    });
                } catch (e) {
                    console.error(`Failed to kill ${s.name}:`, e);
                }
            }
            const killedIds = new Set(groupSessions.map(s => s.session_id));
            state.liveSessions = state.liveSessions.filter(s => !killedIds.has(s.session_id));
            renderLiveSessions(state.liveSessions);
        }
    );
}

export async function killBoard(boardName) {
    const boardSessions = state.liveSessions.filter(s => s.board_project === boardName);
    if (!boardSessions.length) return;

    showConfirmModal(
        "Kill Team",
        `Kill all ${boardSessions.length} agent(s) on board "${boardName}"? This will terminate them.`,
        async () => {
            for (const s of boardSessions) {
                try {
                    await fetch(`/api/sessions/live/${encodeURIComponent(s.name)}/kill`, {
                        method: "POST",
                        headers: { "Content-Type": "application/json" },
                        body: JSON.stringify({ agent_type: s.agent_type, session_id: s.session_id }),
                    });
                } catch (e) {
                    console.error(`Failed to kill ${s.name}:`, e);
                }
            }
            const killedIds = new Set(boardSessions.map(s => s.session_id));
            state.liveSessions = state.liveSessions.filter(s => !killedIds.has(s.session_id));
            renderLiveSessions(state.liveSessions);
        }
    );
}

export async function shareAgentTeam(boardName) {
    // Find all sessions on this board
    const boardSessions = (state.liveSessions || []).filter(s => s.board_project === boardName);
    if (!boardSessions.length) {
        showToast("No agents found on this board", "error");
        return;
    }

    // Fetch each agent's prompt from session info
    const agents = [];
    for (const s of boardSessions) {
        let agentPrompt = "";
        try {
            const resp = await fetch(`/api/sessions/live/${encodeURIComponent(s.name)}/info?session_id=${encodeURIComponent(s.session_id || "")}`);
            const info = await resp.json();
            agentPrompt = info.prompt || "";
        } catch { /* use empty prompt */ }
        agents.push({
            name: s.display_name || s.board_job_title || s.name,
            prompt: agentPrompt,
        });
    }

    const template = {
        version: 1,
        type: "coral-team-templates",
        templates: [{
            name: boardName,
            agents,
            flags: "",
        }],
    };

    const blob = new Blob([JSON.stringify(template, null, 2)], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `coral-team-${boardName.replace(/[^a-zA-Z0-9-_]/g, "_")}.json`;
    a.click();
    URL.revokeObjectURL(url);
    showToast(`Exported team "${boardName}" (${agents.length} agents)`);
}

export async function saveTeamFromSidebar(boardName) {
    const boardSessions = (state.liveSessions || []).filter(s => s.board_project === boardName);
    if (!boardSessions.length) {
        showToast("No agents found on this board", "error");
        return;
    }

    const templateName = window.prompt("Template name:", boardName);
    if (!templateName) return;

    // Fetch each agent's prompt from session info
    const agents = [];
    for (const s of boardSessions) {
        let agentPrompt = "";
        try {
            const resp = await fetch(`/api/sessions/live/${encodeURIComponent(s.name)}/info?session_id=${encodeURIComponent(s.session_id || "")}`);
            const info = await resp.json();
            agentPrompt = info.prompt || "";
        } catch { /* use empty prompt */ }
        agents.push({
            name: s.display_name || s.board_job_title || s.name,
            prompt: agentPrompt,
        });
    }

    // Save to user_settings via the settings API
    let existing = [];
    try {
        existing = JSON.parse(state.settings.saved_team_templates || "[]");
    } catch { /* ignore */ }
    const idx = existing.findIndex(t => t.name === templateName);
    const entry = { name: templateName, agents, flags: "" };
    if (idx >= 0) existing[idx] = entry; else existing.push(entry);
    state.settings.saved_team_templates = JSON.stringify(existing);
    await fetch("/api/settings", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ saved_team_templates: state.settings.saved_team_templates }),
    });
    showToast(`Saved template "${templateName}" (${agents.length} agents)`);
}

export function toggleTeamSleep(boardName, action) {
    const doIt = async () => {
        try {
            const resp = await fetch(`/api/sessions/live/team/${encodeURIComponent(boardName)}/${action}`, { method: 'POST' });
            const data = await resp.json();
            const sleeping = !!data.sleeping;
            for (const s of state.liveSessions) {
                if (s.board_project === boardName) s.sleeping = sleeping;
            }
            renderLiveSessions(state.liveSessions);
            showToast(sleeping ? `Team "${boardName}" is now sleeping` : `Team "${boardName}" is awake`);
        } catch (e) {
            showToast('Failed to toggle team sleep', true);
        }
    };
    if (action === 'sleep') {
        showConfirmModal('Sleep Team', `Put all agents on "${boardName}" to sleep?`, doIt);
    } else {
        doIt();
    }
}

export function sleepAllAgents() {
    showConfirmModal('Sleep All', 'Put all agents to sleep?', async () => {
        try {
            const resp = await fetch('/api/sessions/live/sleep-all', { method: 'POST' });
            const data = await resp.json();
            if (data.ok) {
                for (const s of state.liveSessions) s.sleeping = true;
                renderLiveSessions(state.liveSessions);
                showToast(`All agents are now sleeping (${data.sessions_affected} affected)`);
            }
        } catch (e) {
            showToast('Failed to sleep all agents', true);
        }
    });
}

export function wakeAllAgents() {
    showConfirmModal('Wake All', 'Wake all sleeping agents?', async () => {
        try {
            const resp = await fetch('/api/sessions/live/wake-all', { method: 'POST' });
            const data = await resp.json();
            if (data.ok) {
                for (const s of state.liveSessions) s.sleeping = false;
                renderLiveSessions(state.liveSessions);
                showToast(`All agents are awake (${data.sessions_relaunched} relaunched)`);
            }
        } catch (e) {
            showToast('Failed to wake agents', true);
        }
    });
}

export function toggleAgentSleep(name, agentType, sessionId, action) {
    const doIt = async () => {
        try {
            const resp = await fetch(`/api/sessions/live/${encodeURIComponent(sessionId)}/${action}`, { method: 'POST' });
            const data = await resp.json();
            if (!data.ok) { showToast(data.error || 'Failed to toggle sleep', true); return; }
            const sleeping = !!data.sleeping;
            const s = state.liveSessions.find(s => s.session_id === sessionId);
            if (s) s.sleeping = sleeping;
            renderLiveSessions(state.liveSessions);
            showToast(sleeping ? `"${name}" is now sleeping` : `"${name}" is awake`);
        } catch (e) {
            showToast('Failed to toggle agent sleep', true);
        }
    };
    if (action === 'sleep') {
        showConfirmModal('Sleep Agent', `Put "${name}" to sleep?`, doIt);
    } else {
        doIt();
    }
}

// Drag-and-drop state
let _draggedSid = null;

function _shortPath(fullPath, segments = 2) {
    if (!fullPath) return '';
    const parts = fullPath.replace(/\/+$/, '').split('/');
    return parts.length <= segments ? fullPath : '…/' + parts.slice(-segments).join('/');
}

function _renderSessionItem(s, groupName, isCompact, collapsed) {
    const dotClass = getDotClass(s);
    const isActive = state.currentSession && state.currentSession.type === "live" && state.currentSession.session_id === s.session_id;
    const typeTag = s.agent_type && s.agent_type !== "claude" ? ` <span class="badge ${escapeHtml(s.agent_type)}">${escapeHtml(s.agent_type)}</span>` : "";
    // Branch is shown at folder level, not per agent
    const branchTag = "";
    const waitingBadge = s.waiting_for_input
        ? ' <span class="badge waiting-badge">Needs input</span>'
        : '';
    const isTerminal = s.agent_type === "terminal";
    const sid = s.session_id ? escapeAttr(s.session_id) : "";
    const goalText = s.summary ? escapeHtml(s.summary) : null;
    const goal = goalText || "";
    const goalBtn = (!goalText && !isTerminal) ? `<button class="sidebar-goal-btn" onclick="event.stopPropagation(); requestGoal('${escapeAttr(s.name)}', '${escapeAttr(s.agent_type)}', '${sid}')" title="Generate Goal"><span class="material-icons" style="font-size:16px">auto_awesome</span></button>` : "";
    const displayLabel = s.display_name || (isCompact && s.board_job_title) || (isTerminal ? "Terminal" : "Agent");
    const mobileStatus = getMobileStatusChip(s);
    const lastActivity = formatStaleness(s.staleness_seconds);
    const needsAttention = !!(s.waiting_for_input || s.stuck);
    const unreadBoardBadge = s.board_unread > 0
        ? `<span class="session-mobile-meta-pill">${s.board_unread} unread</span>`
        : '';
    const activityLabel = s.waiting_for_input ? "Waiting since" : "Last activity";
    const mobileAttentionBanner = s.waiting_for_input
        ? '<div class="session-mobile-banner">Waiting for your input</div>'
        : (s.stuck ? '<div class="session-mobile-banner error">Session needs attention</div>' : '');
    const isOrchestrator = (s.display_name || s.board_job_title || '').toLowerCase().includes('orchestrator');
    const sleepIcon = s.sleeping ? '<span class="agent-icon">🌙</span> ' : '';
    const agentIcon = !s.sleeping && s.icon ? `<span class="agent-icon">${escapeHtml(s.icon)}</span> ` : '';
    const orchIcon = (!s.sleeping && !s.icon && isOrchestrator) ? '<svg class="orch-icon" width="12" height="12" viewBox="0 0 16 16" fill="var(--warning, #d29922)" stroke="none"><path d="M8 1l2 4 3-1-1 4H4L3 4l3 1 2-4zM4 10h8v2H4z"/></svg> ' : '';
    const _sleepingMenu = `
            <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); toggleAgentSleep('${escapeAttr(s.name)}', '${escapeAttr(s.agent_type)}', '${sid}', 'wake')">
                <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3a6 6 0 1 0 0 10 5 5 0 0 1 0-10z"/></svg>
                Wake
            </button>
            <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); renameAgent('${escapeAttr(s.name)}', '${escapeAttr(s.agent_type)}', '${sid}')">
                <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><path d="M11.5 1.5l3 3L5 14H2v-3z"/></svg>
                Rename
            </button>
            <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); setAgentIcon('${escapeAttr(s.name)}', '${escapeAttr(s.agent_type)}', '${sid}')">
                <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="8" cy="8" r="6.5"/><path d="M5.5 9.5c0 0 1 1.5 2.5 1.5s2.5-1.5 2.5-1.5"/><circle cx="6" cy="6.5" r="0.5" fill="currentColor" stroke="none"/><circle cx="10" cy="6.5" r="0.5" fill="currentColor" stroke="none"/></svg>
                Set Icon
            </button>
            <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); showInfoDirect('${escapeAttr(s.name)}', '${escapeAttr(s.agent_type)}', '${sid}')">
                <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><circle cx="8" cy="8" r="6.5"/><line x1="8" y1="7" x2="8" y2="11"/><circle cx="8" cy="5" r="0.5" fill="currentColor" stroke="none"/></svg>
                Session Info
            </button>
            <hr class="overflow-menu-divider">
            <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); moveSessionUp('${sid}')">
                <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 3v10M4 7l4-4 4 4"/></svg>
                Move Up
            </button>
            <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); moveSessionDown('${sid}')">
                <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 13V3M4 9l4 4 4-4"/></svg>
                Move Down
            </button>
            <hr class="overflow-menu-divider">
            <button class="overflow-menu-item overflow-menu-danger" onclick="event.stopPropagation(); closeSidebarKebabs(); killSessionDirect('${escapeAttr(s.name)}', '${escapeAttr(s.agent_type)}', '${sid}')">
                <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><line x1="4" y1="4" x2="12" y2="12"/><line x1="12" y1="4" x2="4" y2="12"/></svg>
                Remove Agent
            </button>`;
    const _awakeMenu = `
            <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); attachDirect('${escapeAttr(s.name)}', '${escapeAttr(s.agent_type)}', '${sid}')">
                <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="4,4 8,8 4,12"/><line x1="9" y1="12" x2="13" y2="12"/></svg>
                Attach
            </button>
            <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); restartDirect('${escapeAttr(s.name)}', '${escapeAttr(s.agent_type)}', '${sid}')">
                <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M3.5 2v4h4"/><path d="M3.5 6A5.5 5.5 0 1 1 2.5 8"/></svg>
                Restart
            </button>
            <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); renameAgent('${escapeAttr(s.name)}', '${escapeAttr(s.agent_type)}', '${sid}')">
                <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><path d="M11.5 1.5l3 3L5 14H2v-3z"/></svg>
                Rename
            </button>
            <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); setAgentIcon('${escapeAttr(s.name)}', '${escapeAttr(s.agent_type)}', '${sid}')">
                <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="8" cy="8" r="6.5"/><path d="M5.5 9.5c0 0 1 1.5 2.5 1.5s2.5-1.5 2.5-1.5"/><circle cx="6" cy="6.5" r="0.5" fill="currentColor" stroke="none"/><circle cx="10" cy="6.5" r="0.5" fill="currentColor" stroke="none"/></svg>
                Set Icon
            </button>
            <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); showInfoDirect('${escapeAttr(s.name)}', '${escapeAttr(s.agent_type)}', '${sid}')">
                <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><circle cx="8" cy="8" r="6.5"/><line x1="8" y1="7" x2="8" y2="11"/><circle cx="8" cy="5" r="0.5" fill="currentColor" stroke="none"/></svg>
                Session Info
            </button>
            <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); toggleAgentSleep('${escapeAttr(s.name)}', '${escapeAttr(s.agent_type)}', '${sid}', 'sleep')">
                <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3a6 6 0 1 0 0 10 5 5 0 0 1 0-10z"/></svg>
                Sleep
            </button>
            <hr class="overflow-menu-divider">
            <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); moveSessionUp('${sid}')">
                <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 3v10M4 7l4-4 4 4"/></svg>
                Move Up
            </button>
            <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); moveSessionDown('${sid}')">
                <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 13V3M4 9l4 4 4-4"/></svg>
                Move Down
            </button>
            <hr class="overflow-menu-divider">
            <button class="overflow-menu-item overflow-menu-danger" onclick="event.stopPropagation(); closeSidebarKebabs(); killSessionDirect('${escapeAttr(s.name)}', '${escapeAttr(s.agent_type)}', '${sid}')">
                <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><line x1="4" y1="4" x2="12" y2="12"/><line x1="12" y1="4" x2="4" y2="12"/></svg>
                Kill Session
            </button>`;
    const kebabMenu = `<div class="sidebar-kebab-wrapper">
        <button class="sidebar-kebab-btn" onclick="event.stopPropagation(); toggleSidebarKebab(this)" title="More actions">&#x22EE;</button>
        <div class="sidebar-kebab-menu" style="display:none">${s.sleeping ? _sleepingMenu : _awakeMenu}
        </div>
    </div>`;
    const tooltip = buildSessionTooltip(s);
    const compactClass = isCompact ? ' session-compact' : '';
    const collapsedClass = collapsed ? ' group-collapsed' : '';
    const sleepingClass = s.sleeping ? ' sleeping' : '';
    const attentionClass = needsAttention ? ' needs-attention' : '';
    return `<li class="session-group-item${isActive ? ' active' : ''}${compactClass}${collapsedClass}${sleepingClass}${attentionClass}"
        draggable="true"
        data-session-id="${sid}"
        data-group="${escapeAttr(groupName)}"
        onclick="selectLiveSession('${escapeAttr(s.name)}', '${escapeAttr(s.agent_type)}', '${sid}')">
        <span class="drag-grip" title="Drag to reorder">&#x2630;</span>
        <span class="session-dot ${dotClass}"></span>
        <div class="session-info">
            <div class="session-name-row">
                <span class="session-label">${isTerminal ? '<svg class="terminal-icon" width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="4,4 8,8 4,12"/><line x1="9" y1="12" x2="13" y2="12"/></svg> ' : ''}${sleepIcon}${agentIcon}${orchIcon}${escapeHtml(displayLabel)}${typeTag}</span>
                <span class="session-name-spacer"></span>
                ${waitingBadge}
                ${goalBtn}
                ${kebabMenu}
            </div>
            <div class="session-mobile-banner-row">
                ${mobileAttentionBanner}
            </div>
            <div class="session-mobile-meta">
                <span class="session-status-chip ${escapeAttr(mobileStatus.className)}">${escapeHtml(mobileStatus.label)}</span>
                <span class="session-activity-text" title="${escapeAttr(activityLabel)} ${escapeAttr(lastActivity)}">${escapeHtml(mobileStatus.label)}</span>
                ${unreadBoardBadge}
            </div>
            <span class="session-goal${isCompact ? ' session-goal-compact' : ''}">${goal}</span>
            ${branchTag}
            ${isActive && s.status ? `<span class="session-inline-status">${escapeHtml(s.status)}</span>` : ''}
        </div>
        <div class="session-tooltip">${tooltip}</div>
    </li>`;
}

export function toggleGroupByTeam() {
    const current = localStorage.getItem('coral-group-by-team') !== 'false';
    localStorage.setItem('coral-group-by-team', !current ? 'true' : 'false');
    const check = document.getElementById('group-by-team-check-top');
    if (check) check.style.opacity = !current ? '1' : '0.2';
    renderLiveSessions(state.liveSessions);
}

export function renderLiveSessions(sessions) {
    const list = document.getElementById("live-sessions-list");

    updateSectionVisibility('live-sessions', sessions.length);

    if (!sessions.length) {
        list.innerHTML = '<li class="empty-state">No live sessions</li>';
        return;
    }

    // Sync the Group by Team checkmark
    const groupByTeam = localStorage.getItem('coral-group-by-team') !== 'false';
    const check = document.getElementById('group-by-team-check-top');
    if (check) check.style.opacity = groupByTeam ? '1' : '0.2';

    // Helper to generate a deterministic accent color from a string
    function _boardAccentColor(name) {
        // Check for user-set accent color first
        const custom = _getBoardColor(name);
        if (custom) return custom;
        // Default: hash-based color
        let hash = 0;
        for (let i = 0; i < name.length; i++) hash = ((hash << 5) - hash + name.charCodeAt(i)) | 0;
        const hue = ((hash % 360) + 360) % 360;
        return `hsl(${hue}, 60%, 55%)`;
    }

    let html = "";

    if (groupByTeam) {
    // ── Team-first mode: teams at top level, standalone by folder below ──

    // Step 1: Separate into team (has board_project) vs standalone
    const teamGroups = {};
    const standaloneByFolder = {};
    for (const s of sessions) {
        if (s.board_project) {
            if (!teamGroups[s.board_project]) teamGroups[s.board_project] = [];
            teamGroups[s.board_project].push(s);
        } else {
            const key = s.name || "unknown";
            if (!standaloneByFolder[key]) standaloneByFolder[key] = [];
            standaloneByFolder[key].push(s);
        }
    }

    // Step 2: Render team groups at top level
    // Apply saved group order first, then sort active before sleeping
    const teamEntries = _sortGroups(Object.entries(teamGroups));
    // Secondary sort: if no saved order, put sleeping teams after active
    const hasOrder = _getGroupOrder().length > 0;
    if (!hasOrder) {
        teamEntries.sort((a, b) => {
            const aAllSleeping = a[1].every(s => s.sleeping);
            const bAllSleeping = b[1].every(s => s.sleeping);
            if (aAllSleeping !== bAllSleeping) return aAllSleeping ? 1 : -1;
            return 0;
        });
    }

    for (const [boardName, boardSessions] of teamEntries) {
        const accentColor = _boardAccentColor(boardName);
        const boardCollapsed = _isGroupCollapsed(boardName);
        const bChevron = boardCollapsed ? '&#x25B8;' : '&#x25BE;';
        const boardLink = `<button class="group-board-link" onclick="event.stopPropagation(); selectBoardProject('${escapeAttr(boardName)}')" title="View board: ${escapeAttr(boardName)}"><svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor"><path d="M21 6h-2V3a1 1 0 0 0-1-1H3a1 1 0 0 0-1 1v12a1 1 0 0 0 1 1h2v3a1 1 0 0 0 1.6.8L10.33 17H18a1 1 0 0 0 1-1v-3h2a1 1 0 0 0 1-1V7a1 1 0 0 0-1-1zM17 15H10a1 1 0 0 0-.6.2L6 17.75V16a1 1 0 0 0-1-1H4V4h14v2H7a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h10zm3-3H8V8h12z"/></svg></button>`;
        const boardWorkDir = boardSessions[0]?.working_directory || '';
        const boardIsSleeping = boardSessions.every(s => s.sleeping);
        const boardSleepIcon = boardIsSleeping ? ' <span class="agent-icon" title="Team is sleeping">🌙</span>' : '';
        const sleepLabel = boardIsSleeping ? 'Wake Team' : 'Sleep Team';
        const sleepAction = boardIsSleeping ? 'wake' : 'sleep';
        const bKebab = `<div class="sidebar-kebab-wrapper group-kebab">
            <button class="sidebar-kebab-btn group-kebab-btn" onclick="event.stopPropagation(); toggleSidebarKebab(this)" title="Group actions">&#x22EE;</button>
            <div class="sidebar-kebab-menu" style="display:none">
                <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); showAddAgentToBoard('${escapeAttr(boardName)}', '${escapeAttr(boardWorkDir)}')">
                    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><line x1="8" y1="3" x2="8" y2="13"/><line x1="3" y1="8" x2="13" y2="8"/></svg>
                    Add Agent
                </button>
                <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); saveTeamFromSidebar('${escapeAttr(boardName)}')">
                    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M3 3h8l2 2v8a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1z"/><path d="M6 3v3h4V3"/><rect x="5" y="9" width="6" height="3"/></svg>
                    Save as Template
                </button>
                <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); shareAgentTeam('${escapeAttr(boardName)}')">
                    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M4 12v1a2 2 0 0 0 2 2h4a2 2 0 0 0 2-2v-1"/><polyline points="8 3 8 10"/><polyline points="5 6 8 3 11 6"/></svg>
                    Share Team
                </button>
                <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); toggleTeamSleep('${escapeAttr(boardName)}', '${sleepAction}')">
                    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3a6 6 0 1 0 0 10 5 5 0 0 1 0-10z"/></svg>
                    ${sleepLabel}
                </button>
                <button class="overflow-menu-item" onclick="event.stopPropagation(); setBoardAccentColor('${escapeAttr(boardName)}'); closeSidebarKebabs()">
                    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="8" cy="8" r="6.5"/><path d="M8 1.5v13M1.5 8h13"/></svg>
                    Set Color
                </button>
                <hr class="overflow-menu-divider">
                <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); moveGroupUp('${escapeAttr(boardName)}')">
                    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 3v10M4 7l4-4 4 4"/></svg>
                    Move Up
                </button>
                <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); moveGroupDown('${escapeAttr(boardName)}')">
                    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 13V3M4 9l4 4 4-4"/></svg>
                    Move Down
                </button>
                <hr class="overflow-menu-divider">
                <button class="overflow-menu-item overflow-menu-danger" onclick="event.stopPropagation(); closeSidebarKebabs(); killBoard('${escapeAttr(boardName)}')">
                    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><line x1="4" y1="4" x2="12" y2="12"/><line x1="12" y1="4" x2="4" y2="12"/></svg>
                    Kill All
                </button>
            </div>
        </div>`;
        const teamSubline = `<div class="board-card-subline"><svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="9" cy="7" r="3"/><circle cx="17" cy="7" r="3"/><path d="M3 21v-2a4 4 0 0 1 4-4h4a4 4 0 0 1 4 4v2"/><path d="M17 11a4 4 0 0 1 4 4v2"/></svg> Agent Team · ${boardSessions.length} agents</div>`;
        const sleepingClass = boardIsSleeping ? ' team-sleeping' : '';
        html += `<li class="session-board-card session-board-card-toplevel${sleepingClass}" style="border-left-color: ${accentColor}">
            <div class="session-group-header board-card-header" data-group-name="${escapeAttr(boardName)}" onclick="toggleGroupCollapse('${escapeAttr(boardName)}')">
                <span class="group-chevron">${bChevron}</span><div class="group-header-text"><div class="group-name-line">${escapeHtml(boardName)}${boardSleepIcon}</div>${teamSubline}</div><span class="session-name-spacer"></span>${boardLink}${bKebab}
            </div>
            <ul class="board-card-agents${boardCollapsed ? ' board-card-collapsed' : ''}">`;

        // Apply saved order, then always pin orchestrator to top
        const orderedBoard = _sortByOrder(boardSessions);
        orderedBoard.sort((a, b) => {
            const aOrch = (a.display_name || a.board_job_title || '').toLowerCase().includes('orchestrator');
            const bOrch = (b.display_name || b.board_job_title || '').toLowerCase().includes('orchestrator');
            if (aOrch && !bOrch) return -1;
            if (!aOrch && bOrch) return 1;
            return 0;
        });
        for (const s of orderedBoard) {
            html += _renderSessionItem(s, boardName, true);
        }
        html += `</ul>`;
        // Folder footer card showing working directory
        const folderPath = boardWorkDir;
        const copyBtn = `<button class="folder-copy-btn" onclick="event.stopPropagation(); copyFolderPath('${escapeAttr(folderPath)}')" title="Copy path"><svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="5.5" y="5.5" width="8" height="8" rx="1.5"/><path d="M5.5 10.5h-1a1.5 1.5 0 0 1-1.5-1.5v-5a1.5 1.5 0 0 1 1.5-1.5h5a1.5 1.5 0 0 1 1.5 1.5v1"/></svg></button>`;
        html += `<div class="board-card-folder-footer">
            <svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M2 4v8a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V6a1 1 0 0 0-1-1H8L6.5 3H3a1 1 0 0 0-1 1z"/></svg>
            <span class="board-card-folder-path">${escapeHtml(_shortPath(folderPath, 3))}</span>
            ${copyBtn}
        </div>`;
        html += `</li>`;
    }

    // Step 3: Render standalone agents grouped by folder
    const sortedFolders = _sortGroups(Object.entries(standaloneByFolder));
    for (const [groupName, groupSessions] of sortedFolders) {
        const sorted = _sortByOrder(groupSessions);
        const isMulti = sorted.length > 1;
        const countBadge = isMulti ? ` <span class="session-group-count">${sorted.length}</span>` : "";
        const collapsed = _isGroupCollapsed(groupName);
        const chevron = collapsed ? '&#x25B8;' : '&#x25BE;';
        const groupKebab = `<div class="sidebar-kebab-wrapper group-kebab">
            <button class="sidebar-kebab-btn group-kebab-btn" onclick="event.stopPropagation(); toggleSidebarKebab(this)" title="Group actions">&#x22EE;</button>
            <div class="sidebar-kebab-menu" style="display:none">
                <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); showFolderTagDropdown('${escapeAttr(groupName)}', this.closest('.session-group-header'))">
                    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M2 8.5V3a1 1 0 0 1 1-1h5.5l5.5 5.5-5.5 5.5L2 8.5z"/><circle cx="5.5" cy="5.5" r="1" fill="currentColor" stroke="none"/></svg>
                    Manage Tags
                </button>
                <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); moveGroupUp('${escapeAttr(groupName)}')">
                    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M8 3v10"/><path d="M4 7l4-4 4 4"/></svg>
                    Move Up
                </button>
                <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); moveGroupDown('${escapeAttr(groupName)}')">
                    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M8 13V3"/><path d="M4 9l4 4 4-4"/></svg>
                    Move Down
                </button>
                <button class="overflow-menu-item overflow-menu-danger" onclick="event.stopPropagation(); closeSidebarKebabs(); killGroup('${escapeAttr(groupName)}')">
                    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><line x1="4" y1="4" x2="12" y2="12"/><line x1="12" y1="4" x2="4" y2="12"/></svg>
                    Kill All
                </button>
            </div>
        </div>`;
        const groupBranch = sorted.find(s => s.branch)?.branch || "";
        const groupBranchLine = groupBranch ? `<div class="group-branch-line"><svg width="10" height="10" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M6 3v5a3 3 0 0 0 3 3h1"/><circle cx="6" cy="3" r="1.5"/><circle cx="11" cy="11" r="1.5"/></svg> ${escapeHtml(groupBranch)}</div>` : "";
        const fTags = getFolderTags(groupName);
        const tagDots = renderFolderTagPills(fTags);
        const groupWorkDir = sorted[0]?.working_directory || '';
        const copyBtn = `<button class="folder-copy-btn" onclick="event.stopPropagation(); copyFolderPath('${escapeAttr(groupWorkDir)}')" title="Copy path"><svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="5.5" y="5.5" width="8" height="8" rx="1.5"/><path d="M5.5 10.5h-1a1.5 1.5 0 0 1-1.5-1.5v-5a1.5 1.5 0 0 1 1.5-1.5h5a1.5 1.5 0 0 1 1.5 1.5v1"/></svg></button>`;
        html += `<li class="session-group-header" data-group-name="${escapeAttr(groupName)}" onclick="toggleGroupCollapse('${escapeAttr(groupName)}')">
            <span class="group-chevron">${chevron}</span><div class="group-header-text"><div class="group-name-line">${escapeHtml(groupName)}${countBadge}</div>${groupBranchLine}</div>${tagDots}<span class="session-name-spacer"></span>${copyBtn}${groupKebab}</li>`;

        if (!collapsed) {
            for (const s of sorted) {
                html += _renderSessionItem(s, groupName, false);
            }
        }
    }

    } else {
    // ── Folder-first mode: all sessions grouped by folder, boards nested inside ──

    // Group sessions by folder name (primary grouping)
    const groups = {};
    for (const s of sessions) {
        const key = s.name || "unknown";
        if (!groups[key]) groups[key] = [];
        groups[key].push(s);
    }

    const sortedGroups = _sortGroups(Object.entries(groups));
    for (const [groupName, groupSessions] of sortedGroups) {
        const sorted = _sortByOrder(groupSessions);
        const isMulti = sorted.length > 1;
        const countBadge = isMulti ? ` <span class="session-group-count">${sorted.length}</span>` : "";
        const collapsed = _isGroupCollapsed(groupName);
        const chevron = collapsed ? '&#x25B8;' : '&#x25BE;';
        const groupKebab = `<div class="sidebar-kebab-wrapper group-kebab">
            <button class="sidebar-kebab-btn group-kebab-btn" onclick="event.stopPropagation(); toggleSidebarKebab(this)" title="Group actions">&#x22EE;</button>
            <div class="sidebar-kebab-menu" style="display:none">
                <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); showFolderTagDropdown('${escapeAttr(groupName)}', this.closest('.session-group-header'))">
                    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M2 8.5V3a1 1 0 0 1 1-1h5.5l5.5 5.5-5.5 5.5L2 8.5z"/><circle cx="5.5" cy="5.5" r="1" fill="currentColor" stroke="none"/></svg>
                    Manage Tags
                </button>
                <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); moveGroupUp('${escapeAttr(groupName)}')">
                    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M8 3v10"/><path d="M4 7l4-4 4 4"/></svg>
                    Move Up
                </button>
                <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); moveGroupDown('${escapeAttr(groupName)}')">
                    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M8 13V3"/><path d="M4 9l4 4 4-4"/></svg>
                    Move Down
                </button>
                <button class="overflow-menu-item overflow-menu-danger" onclick="event.stopPropagation(); closeSidebarKebabs(); killGroup('${escapeAttr(groupName)}')">
                    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><line x1="4" y1="4" x2="12" y2="12"/><line x1="12" y1="4" x2="4" y2="12"/></svg>
                    Kill All
                </button>
            </div>
        </div>`;
        const groupBranch = sorted.find(s => s.branch)?.branch || "";
        const groupBranchLine = groupBranch ? `<div class="group-branch-line"><svg width="10" height="10" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M6 3v5a3 3 0 0 0 3 3h1"/><circle cx="6" cy="3" r="1.5"/><circle cx="11" cy="11" r="1.5"/></svg> ${escapeHtml(groupBranch)}</div>` : "";
        const fTags = getFolderTags(groupName);
        const tagDots = renderFolderTagPills(fTags);
        const groupWorkDir = sorted[0]?.working_directory || '';
        const copyBtn = `<button class="folder-copy-btn" onclick="event.stopPropagation(); copyFolderPath('${escapeAttr(groupWorkDir)}')" title="Copy path"><svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="5.5" y="5.5" width="8" height="8" rx="1.5"/><path d="M5.5 10.5h-1a1.5 1.5 0 0 1-1.5-1.5v-5a1.5 1.5 0 0 1 1.5-1.5h5a1.5 1.5 0 0 1 1.5 1.5v1"/></svg></button>`;
        html += `<li class="session-group-header" data-group-name="${escapeAttr(groupName)}" onclick="toggleGroupCollapse('${escapeAttr(groupName)}')">
            <span class="group-chevron">${chevron}</span><div class="group-header-text"><div class="group-name-line">${escapeHtml(groupName)}${countBadge}</div>${groupBranchLine}</div>${tagDots}<span class="session-name-spacer"></span>${copyBtn}${groupKebab}</li>`;

        if (collapsed) {
            // Skip rendering items when collapsed
        } else {
            // Sub-group by board within this folder
            const boardSubs = {};
            const unboardedItems = [];
            for (const s of sorted) {
                if (s.board_project) {
                    if (!boardSubs[s.board_project]) boardSubs[s.board_project] = [];
                    boardSubs[s.board_project].push(s);
                } else {
                    unboardedItems.push(s);
                }
            }

            // Render board sub-groups as cards within the folder
            for (const [boardName, boardSessions] of Object.entries(boardSubs)) {
                const accentColor = _boardAccentColor(boardName);
                const boardCollapsed = _isGroupCollapsed(boardName);
                const bChevron = boardCollapsed ? '&#x25B8;' : '&#x25BE;';
                const boardLink = `<button class="group-board-link" onclick="event.stopPropagation(); selectBoardProject('${escapeAttr(boardName)}')" title="View board: ${escapeAttr(boardName)}"><svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor"><path d="M21 6h-2V3a1 1 0 0 0-1-1H3a1 1 0 0 0-1 1v12a1 1 0 0 0 1 1h2v3a1 1 0 0 0 1.6.8L10.33 17H18a1 1 0 0 0 1-1v-3h2a1 1 0 0 0 1-1V7a1 1 0 0 0-1-1zM17 15H10a1 1 0 0 0-.6.2L6 17.75V16a1 1 0 0 0-1-1H4V4h14v2H7a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h10zm3-3H8V8h12z"/></svg></button>`;
                const boardWorkDir = boardSessions[0]?.working_directory || '';
                const boardIsSleeping = boardSessions.some(s => s.sleeping);
                const boardSleepIcon = boardIsSleeping ? ' <span class="agent-icon" title="Team is sleeping">🌙</span>' : '';
                const sleepLabel = boardIsSleeping ? 'Wake Team' : 'Sleep Team';
                const sleepAction = boardIsSleeping ? 'wake' : 'sleep';
                const bKebab = `<div class="sidebar-kebab-wrapper group-kebab">
                    <button class="sidebar-kebab-btn group-kebab-btn" onclick="event.stopPropagation(); toggleSidebarKebab(this)" title="Group actions">&#x22EE;</button>
                    <div class="sidebar-kebab-menu" style="display:none">
                        <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); showAddAgentToBoard('${escapeAttr(boardName)}', '${escapeAttr(boardWorkDir)}')">
                            <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><line x1="8" y1="3" x2="8" y2="13"/><line x1="3" y1="8" x2="13" y2="8"/></svg>
                            Add Agent
                        </button>
                        <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); saveTeamFromSidebar('${escapeAttr(boardName)}')">
                            <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M3 3h8l2 2v8a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1z"/><path d="M6 3v3h4V3"/><rect x="5" y="9" width="6" height="3"/></svg>
                            Save as Template
                        </button>
                        <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); shareAgentTeam('${escapeAttr(boardName)}')">
                            <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M4 12v1a2 2 0 0 0 2 2h4a2 2 0 0 0 2-2v-1"/><polyline points="8 3 8 10"/><polyline points="5 6 8 3 11 6"/></svg>
                            Share Team
                        </button>
                        <button class="overflow-menu-item" onclick="event.stopPropagation(); closeSidebarKebabs(); toggleTeamSleep('${escapeAttr(boardName)}', '${sleepAction}')">
                            <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3a6 6 0 1 0 0 10 5 5 0 0 1 0-10z"/></svg>
                            ${sleepLabel}
                        </button>
                        <button class="overflow-menu-item" onclick="event.stopPropagation(); setBoardAccentColor('${escapeAttr(boardName)}'); closeSidebarKebabs()">
                            <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="8" cy="8" r="6.5"/><path d="M8 1.5v13M1.5 8h13"/></svg>
                            Set Color
                        </button>
                        <hr class="overflow-menu-divider">
                        <button class="overflow-menu-item overflow-menu-danger" onclick="event.stopPropagation(); closeSidebarKebabs(); killBoard('${escapeAttr(boardName)}')">
                            <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><line x1="4" y1="4" x2="12" y2="12"/><line x1="12" y1="4" x2="4" y2="12"/></svg>
                            Kill All
                        </button>
                    </div>
                </div>`;
                const teamSubline = `<div class="board-card-subline"><svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="9" cy="7" r="3"/><circle cx="17" cy="7" r="3"/><path d="M3 21v-2a4 4 0 0 1 4-4h4a4 4 0 0 1 4 4v2"/><path d="M17 11a4 4 0 0 1 4 4v2"/></svg> Agent Team</div>`;
                html += `<li class="session-board-card" style="border-left-color: ${accentColor}">
                    <div class="session-group-header board-card-header" onclick="toggleGroupCollapse('${escapeAttr(boardName)}')">
                        <span class="group-chevron">${bChevron}</span><div class="group-header-text"><div class="group-name-line">${escapeHtml(boardName)}${boardSleepIcon} <span class="session-group-count">${boardSessions.length}</span></div>${teamSubline}</div><span class="session-name-spacer"></span>${boardLink}${bKebab}
                    </div>
                    <ul class="board-card-agents${boardCollapsed ? ' board-card-collapsed' : ''}">`;
                const orderedBoardNested = _sortByOrder(boardSessions);
                orderedBoardNested.sort((a, b) => {
                    const aOrch = (a.display_name || a.board_job_title || '').toLowerCase().includes('orchestrator');
                    const bOrch = (b.display_name || b.board_job_title || '').toLowerCase().includes('orchestrator');
                    if (aOrch && !bOrch) return -1;
                    if (!aOrch && bOrch) return 1;
                    return 0;
                });
                for (const s of orderedBoardNested) {
                    html += _renderSessionItem(s, groupName, true);
                }
                html += `</ul></li>`;
            }

            // Render unboarded items as flat list
            for (const s of unboardedItems) {
                html += _renderSessionItem(s, groupName, false);
            }
        }
    }

    } // end if/else groupByTeam

    list.innerHTML = html;

    // Attach hover listeners for fixed-position tooltips
    for (const item of list.querySelectorAll(".session-group-item")) {
        const tip = item.querySelector(".session-tooltip");
        if (!tip) continue;
        item.addEventListener("mouseenter", () => {
            const rect = item.getBoundingClientRect();
            tip.style.left = rect.right + 8 + "px";
            tip.style.top = rect.top + "px";
            tip.style.display = "block";
        });
        item.addEventListener("mouseleave", () => {
            tip.style.display = "none";
        });
    }

    // Attach drag-and-drop listeners for reordering
    _attachDragListeners(list);

    // Sync mobile agent list
    syncMobileAgentList();
}

function _attachDragListeners(list) {
    const items = list.querySelectorAll(".session-group-item[draggable]");

    // Allow drops inside nested board-card-agents containers
    for (const container of list.querySelectorAll(".board-card-agents")) {
        container.addEventListener("dragover", (e) => {
            e.preventDefault();
            e.dataTransfer.dropEffect = "move";
        });
    }

    for (const item of items) {
        item.addEventListener("dragstart", (e) => {
            e.stopPropagation(); // Prevent board card from intercepting
            _draggedSid = item.dataset.sessionId;
            item.classList.add("dragging");
            e.dataTransfer.effectAllowed = "move";
            // Use a minimal drag image to avoid tooltip flash
            e.dataTransfer.setData("text/plain", _draggedSid);
        });

        item.addEventListener("dragend", () => {
            item.classList.remove("dragging");
            _draggedSid = null;
            // Clear all drop indicators
            for (const el of list.querySelectorAll(".drag-over")) {
                el.classList.remove("drag-over");
            }
        });

        item.addEventListener("dragover", (e) => {
            e.preventDefault();
            if (!_draggedSid || _draggedSid === item.dataset.sessionId) return;
            // Only allow reorder within the same group
            const draggedEl = list.querySelector(`[data-session-id="${_draggedSid}"]`);
            if (!draggedEl || draggedEl.dataset.group !== item.dataset.group) return;
            e.dataTransfer.dropEffect = "move";
            item.classList.add("drag-over");
        });

        item.addEventListener("dragleave", () => {
            item.classList.remove("drag-over");
        });

        item.addEventListener("drop", (e) => {
            e.preventDefault();
            e.stopPropagation();
            item.classList.remove("drag-over");
            if (!_draggedSid || _draggedSid === item.dataset.sessionId) return;

            const group = item.dataset.group;
            const draggedEl = list.querySelector(`[data-session-id="${_draggedSid}"]`);
            if (!draggedEl || draggedEl.dataset.group !== group) return;

            // Gather current order of session_ids in this group
            const groupItems = [...list.querySelectorAll(".session-group-item")].filter(el => el.dataset.group === group);
            const ids = groupItems.map(el => el.dataset.sessionId);

            // Remove dragged from its position and insert before drop target
            const fromIdx = ids.indexOf(_draggedSid);
            const toIdx = ids.indexOf(item.dataset.sessionId);
            if (fromIdx === -1 || toIdx === -1) return;

            ids.splice(fromIdx, 1);
            ids.splice(toIdx, 0, _draggedSid);

            // Save all session IDs in current DOM order (preserves other groups)
            const allItems = [...list.querySelectorAll(".session-group-item")];
            const allIds = allItems.map(el => el.dataset.sessionId);
            // Replace the current group's IDs with the reordered ones
            const groupSet = new Set(ids);
            const merged = [];
            let groupInserted = false;
            for (const sid of allIds) {
                if (groupSet.has(sid)) {
                    if (!groupInserted) {
                        merged.push(...ids);
                        groupInserted = true;
                    }
                } else {
                    merged.push(sid);
                }
            }
            if (!groupInserted) merged.push(...ids);

            // Save and re-render
            _saveSessionOrder(merged);
            renderLiveSessions(state.liveSessions);
        });
    }
}

export function renderHistorySessions(sessions, total, page, pageSize) {
    const list = document.getElementById("history-sessions-list");
    state.historySessionsList = sessions || [];

    updateSectionVisibility('history', total || (sessions ? sessions.length : 0));

    if (!sessions || !sessions.length) {
        list.innerHTML = '<li class="empty-state">No history found</li>';
        renderPaginationControls(0, 1, 50);
        return;
    }

    list.innerHTML = sessions.map(s => {
        const isGroup = s.type === 'group' || (s.session_id && s.session_id.startsWith('board:'));
        if (isGroup) {
            const projectName = s.title || s.session_id.replace(/^board:/, '');
            const label = projectName;
            const truncated = label.length > 40 ? label.substring(0, 40) + "..." : label;
            const isActive = state.currentSession && state.currentSession.type === "board" && state.currentSession.name === projectName;
            const timeStr = s.updated_at ? formatShortTime(s.updated_at) : "";
            const timeTag = timeStr ? `<span class="session-time">${escapeHtml(timeStr)}</span>` : "";
            const countInfo = s.message_count != null ? `${s.message_count} msgs` : "";
            const subsInfo = s.subscriber_count != null ? `${s.subscriber_count} members` : "";
            const meta = [countInfo, subsInfo].filter(Boolean).join(' · ');
            return `<li class="${isActive ? 'active' : ''}" onclick="selectBoardProject('${escapeAttr(projectName)}')">
                <div class="session-row-top">${timeTag}<span class="badge board-badge">Group</span></div>
                <div class="session-row-mid"><span class="session-label" title="${escapeHtml(label)}">${escapeHtml(truncated)}</span></div>
                ${meta ? `<div class="session-row-bottom"><span class="session-meta">${escapeHtml(meta)}</span></div>` : ""}
            </li>`;
        }
        const label = s.summary_title || s.summary || s.session_id;
        const truncated = label.length > 40 ? label.substring(0, 40) + "..." : label;
        const isActive = state.currentSession && state.currentSession.type === "history" && state.currentSession.name === s.session_id;
        const typeTag = s.source_type === "gemini" ? '<span class="badge gemini">gemini</span>' : "";
        const branchTag = s.branch ? `<span class="sidebar-branch">${escapeHtml(s.branch)}</span>` : "";
        const tagDots = s.tags ? renderSidebarTagDots(s.tags) : "";
        const timeStr = s.last_timestamp ? formatShortTime(s.last_timestamp) : "";
        const timeTag = timeStr ? `<span class="session-time">${escapeHtml(timeStr)}</span>` : "";
        const durStr = s.duration_sec != null ? formatDuration(s.duration_sec) : '';
        const durTag = durStr ? `<span class="session-dur">${escapeHtml(durStr)}</span>` : '';
        return `<li class="${isActive ? 'active' : ''}" onclick="selectHistorySession('${escapeAttr(s.session_id)}')">
            <div class="session-row-top">${timeTag}${durTag}${typeTag}${tagDots}</div>
            <div class="session-row-mid"><span class="session-label" title="${escapeHtml(label)}">${escapeHtml(truncated)}</span></div>
            ${branchTag ? `<div class="session-row-bottom">${branchTag}</div>` : ""}
        </li>`;
    }).join("");

    if (total !== undefined) {
        renderPaginationControls(total, page || 1, pageSize || 50);
    }
}

export function renderPaginationControls(total, page, pageSize) {
    let container = document.getElementById("history-pagination");
    if (!container) {
        // Create pagination container after the history list
        const list = document.getElementById("history-sessions-list");
        container = document.createElement("div");
        container.id = "history-pagination";
        container.className = "pagination-controls";
        list.parentNode.appendChild(container);
    }

    const totalPages = Math.max(1, Math.ceil(total / pageSize));
    if (totalPages <= 1) {
        container.innerHTML = `<span class="pagination-info">${total} session${total !== 1 ? 's' : ''}</span>`;
        return;
    }

    container.innerHTML = `
        <button class="btn btn-small" ${page <= 1 ? 'disabled' : ''} onclick="loadHistoryPage(${page - 1})">Prev</button>
        <span class="pagination-info">${page} / ${totalPages} (${total})</span>
        <button class="btn btn-small" ${page >= totalPages ? 'disabled' : ''} onclick="loadHistoryPage(${page + 1})">Next</button>
    `;
}

function stripPulseLines(text) {
    return text.replace(/^\|\|PULSE:(STATUS|SUMMARY|CONFIDENCE)\s[^\|]*\|\|$/gm, '').replace(/\n{3,}/g, '\n\n');
}

export function renderHistoryChat(messages) {
    const container = document.getElementById("history-messages");
    container.innerHTML = "";

    for (const entry of messages) {
        const type = entry.type || "unknown";
        const msg = entry.message || {};
        let content = "";

        if (typeof msg.content === "string") {
            content = msg.content;
        } else if (Array.isArray(msg.content)) {
            content = msg.content
                .filter(b => b.type === "text")
                .map(b => b.text)
                .join("\n");
        }

        if (!content.trim()) continue;

        const isHuman = type === "human" || type === "user";
        const bubbleClass = isHuman ? "human" : "assistant";
        const roleLabel = isHuman ? "You" : "Assistant";

        const bubble = document.createElement("div");
        bubble.className = `chat-bubble ${bubbleClass}`;

        let messageHtml;
        if (isHuman) {
            messageHtml = escapeHtml(content);
        } else {
            const cleaned = stripPulseLines(content);
            const rawHtml = marked.parse(cleaned);
            messageHtml = typeof DOMPurify !== 'undefined' ? DOMPurify.sanitize(rawHtml) : rawHtml;
        }

        bubble.innerHTML = `
            <div class="role-label">${roleLabel}</div>
            <div class="message-text${!isHuman ? " markdown-body" : ""}">${messageHtml}</div>
            ${isHuman ? `<button class="edit-btn" onclick="editAndResubmit(this)">Edit & Resubmit</button>` : ""}
        `;
        container.appendChild(bubble);
    }

    container.scrollTop = container.scrollHeight;
}

export function updateSessionStatus(status) {
    const el = document.getElementById("session-status");
    if (status) {
        el.querySelector(".status-text").textContent = status;
    }
}

export function updateSessionBranch(branch) {
    const el = document.getElementById("session-branch");
    if (branch) {
        el.querySelector(".branch-text").textContent = branch;
        el.style.display = "";
    } else {
        el.style.display = "none";
    }
}

export function updateWaitingIndicator(s) {
    const dot = document.getElementById("session-status-dot");
    const banner = document.getElementById("waiting-banner");
    if (dot) {
        dot.classList.toggle("waiting", !!s.waiting_for_input);
        dot.classList.toggle("stuck", !!s.stuck);
        dot.classList.toggle("working", !!s.working);
        dot.classList.toggle("done", !!s.done);
    }
    if (banner) {
        // Only show banner for needs-input state
        banner.style.display = s.waiting_for_input ? "" : "none";
        if (s.waiting_for_input) {
            banner.className = "waiting-banner";
            banner.textContent = "⏳ Agent is waiting for input";
        }
    }
}

export function updateSessionSummary(summary) {
    const el = document.getElementById("session-summary");
    if (!el) return;
    if (summary) {
        el.querySelector(".summary-text").textContent = summary;
        el.style.display = "";
    }
    // Don't hide — a null summary from a WebSocket tick shouldn't
    // clear a previously-known goal. The log parser may simply not
    // have found the PULSE:SUMMARY line in the current chunk.
}
