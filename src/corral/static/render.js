/* Rendering functions for session lists, chat history, and status updates */

import { state } from './state.js';
import { escapeHtml } from './utils.js';
import { renderSidebarTagDots } from './tags.js';

function formatShortTime(isoStr) {
    const d = new Date(isoStr);
    if (isNaN(d)) return "";
    const dd = String(d.getDate()).padStart(2, "0");
    const mm = String(d.getMonth() + 1).padStart(2, "0");
    const yy = String(d.getFullYear()).slice(-2);
    return `${dd}/${mm}/${yy}`;
}

function getDotClass(staleness) {
    if (staleness === null || staleness === undefined) return "stale";
    if (staleness < 60) return "active";
    if (staleness < 300) return "recent";
    return "stale";
}

export function renderLiveSessions(sessions) {
    const list = document.getElementById("live-sessions-list");

    if (!sessions.length) {
        list.innerHTML = '<li class="empty-state">No live sessions</li>';
        return;
    }

    // Group sessions by agent_name (folder)
    const groups = {};
    for (const s of sessions) {
        const key = s.name || "unknown";
        if (!groups[key]) groups[key] = [];
        groups[key].push(s);
    }

    let html = "";
    for (const [groupName, groupSessions] of Object.entries(groups)) {
        const isMulti = groupSessions.length > 1;
        const countBadge = isMulti ? ` <span class="session-group-count">${groupSessions.length}</span>` : "";
        html += `<li class="session-group-header">${escapeHtml(groupName)}${countBadge}</li>`;

        for (const s of groupSessions) {
            const dotClass = getDotClass(s.staleness_seconds);
            const isActive = state.currentSession && state.currentSession.type === "live" && state.currentSession.session_id === s.session_id;
            const typeTag = s.agent_type && s.agent_type !== "claude" ? ` <span class="badge ${escapeHtml(s.agent_type)}">${escapeHtml(s.agent_type)}</span>` : "";
            const branchTag = s.branch ? ` <span class="sidebar-branch">${escapeHtml(s.branch)}</span>` : "";
            const goal = s.summary ? escapeHtml(s.summary) : "No goal set";
            const displayLabel = s.display_name || "Agent";
            const sid = s.session_id ? escapeHtml(s.session_id) : "";
            const editBtn = `<button class="sidebar-edit-btn" onclick="event.stopPropagation(); renameAgent('${escapeHtml(s.name)}', '${escapeHtml(s.agent_type)}', '${sid}')" title="Rename agent">&#x270E;</button>`;
            html += `<li class="session-group-item${isActive ? ' active' : ''}" onclick="selectLiveSession('${escapeHtml(s.name)}', '${escapeHtml(s.agent_type)}', '${sid}')">
                <span class="session-dot ${dotClass}"></span>
                <div class="session-info">
                    <span class="session-label">${escapeHtml(displayLabel)}${typeTag}${branchTag}</span>
                    <span class="session-goal">${goal}</span>
                </div>
                ${editBtn}
            </li>`;
        }
    }

    list.innerHTML = html;
}

export function renderHistorySessions(sessions, total, page, pageSize) {
    const list = document.getElementById("history-sessions-list");
    state.historySessionsList = sessions || [];

    if (!sessions || !sessions.length) {
        list.innerHTML = '<li class="empty-state">No history found</li>';
        renderPaginationControls(0, 1, 50);
        return;
    }

    list.innerHTML = sessions.map(s => {
        const label = s.summary_title || s.summary || s.session_id;
        const truncated = label.length > 40 ? label.substring(0, 40) + "..." : label;
        const isActive = state.currentSession && state.currentSession.type === "history" && state.currentSession.name === s.session_id;
        const typeTag = s.source_type === "gemini" ? ' <span class="badge gemini">gemini</span>' : "";
        const branchTag = s.branch ? `<span class="sidebar-branch">${escapeHtml(s.branch)}</span>` : "";
        const tagDots = s.tags ? renderSidebarTagDots(s.tags) : "";
        const timeStr = s.last_timestamp ? formatShortTime(s.last_timestamp) : "";
        const timeTag = timeStr ? `<span class="session-time">${escapeHtml(timeStr)}</span>` : "";
        return `<li class="${isActive ? 'active' : ''}" onclick="selectHistorySession('${escapeHtml(s.session_id)}')">
            <div class="session-row-top">${timeTag}<span class="session-label" title="${escapeHtml(label)}">${escapeHtml(truncated)}${typeTag}${tagDots}</span></div>
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
            messageHtml = marked.parse(cleaned);
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
        el.style.display = "";
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

export function updateSessionSummary(summary) {
    const el = document.getElementById("session-summary");
    if (summary) {
        el.querySelector(".summary-text").textContent = summary;
        el.style.display = "";
    } else {
        el.style.display = "none";
    }
}
