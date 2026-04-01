/* Agent task bar — CRUD, rendering, drag reorder */

import { state } from './state.js';
import { escapeHtml, escapeAttr, showToast } from './utils.js';

export async function loadAgentTasks(agentName, sessionId) {
    if (!agentName) return;
    try {
        const params = new URLSearchParams();
        const sid = sessionId || (state.currentSession && state.currentSession.session_id);
        if (sid) params.set("session_id", sid);
        const qs = params.toString() ? `?${params}` : "";
        const resp = await fetch(`/api/sessions/live/${encodeURIComponent(agentName)}/tasks${qs}`);
        if (!resp.ok) throw new Error(`tasks fetch failed: ${resp.status}`);
        state.currentAgentTasks = await resp.json();
    } catch (e) {
        state.currentAgentTasks = [];
    }
    renderTaskList();
}

export async function addAgentTask() {
    if (!state.currentSession || state.currentSession.type !== 'live') return;
    const input = document.getElementById('task-bar-input');
    const title = input.value.trim();
    if (!title) return;

    try {
        const sid = state.currentSession.session_id;
        await fetch(`/api/sessions/live/${encodeURIComponent(state.currentSession.name)}/tasks`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ title, session_id: sid }),
        });
        input.value = '';
        await loadAgentTasks(state.currentSession.name, sid);
    } catch (e) {
        showToast('Failed to add task', true);
    }
}

export async function toggleAgentTask(taskId, completed) {
    if (!state.currentSession || state.currentSession.type !== 'live') return;
    try {
        await fetch(`/api/sessions/live/${encodeURIComponent(state.currentSession.name)}/tasks/${taskId}`, {
            method: 'PATCH',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ completed: completed ? 1 : 0 }),
        });
        await loadAgentTasks(state.currentSession.name, state.currentSession.session_id);
    } catch (e) {
        showToast('Failed to update task', true);
    }
}

export async function deleteAgentTask(taskId) {
    if (!state.currentSession || state.currentSession.type !== 'live') return;
    try {
        await fetch(`/api/sessions/live/${encodeURIComponent(state.currentSession.name)}/tasks/${taskId}`, {
            method: 'DELETE',
        });
        await loadAgentTasks(state.currentSession.name, state.currentSession.session_id);
    } catch (e) {
        showToast('Failed to delete task', true);
    }
}

export function editAgentTaskTitle(taskId, spanEl) {
    if (!state.currentSession || state.currentSession.type !== 'live') return;
    const original = spanEl.textContent;
    spanEl.contentEditable = 'true';
    spanEl.focus();

    // Select all text
    const range = document.createRange();
    range.selectNodeContents(spanEl);
    const sel = window.getSelection();
    sel.removeAllRanges();
    sel.addRange(range);

    const finish = async () => {
        spanEl.contentEditable = 'false';
        const newTitle = spanEl.textContent.trim();
        if (!newTitle || newTitle === original) {
            spanEl.textContent = original;
            return;
        }
        try {
            await fetch(`/api/sessions/live/${encodeURIComponent(state.currentSession.name)}/tasks/${taskId}`, {
                method: 'PATCH',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ title: newTitle }),
            });
            await loadAgentTasks(state.currentSession.name, state.currentSession.session_id);
        } catch (e) {
            spanEl.textContent = original;
            showToast('Failed to update task title', true);
        }
    };

    spanEl.addEventListener('blur', finish, { once: true });
    spanEl.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            spanEl.blur();
        } else if (e.key === 'Escape') {
            spanEl.textContent = original;
            spanEl.blur();
        }
    });
}

export function renderTaskList() {
    const list = document.getElementById('task-bar-list');
    const countEl = document.getElementById('task-bar-count');
    if (!list) return;

    const tasks = state.currentAgentTasks || [];
    const doneCount = tasks.filter(t => t.completed === 1).length;

    if (countEl) {
        countEl.textContent = tasks.length > 0 ? `${doneCount}/${tasks.length}` : '';
    }

    if (tasks.length === 0) {
        list.innerHTML = '<div class="task-empty">No tasks yet</div>';
        return;
    }

    // completed: 0=pending, 1=done, 2=in_progress
    list.innerHTML = tasks.map(t => {
        const statusClass = t.completed === 1 ? 'completed' : t.completed === 2 ? 'in-progress' : '';
        const icon = t.completed === 2
            ? '<span class="task-spinner" title="In progress"></span>'
            : `<input type="checkbox" class="task-checkbox" ${t.completed === 1 ? 'checked' : ''}
                onchange="toggleAgentTask(${t.id}, this.checked)">`;
        return `
        <div class="task-item ${statusClass}" data-task-id="${t.id}" draggable="true">
            ${icon}
            <span class="task-title" ondblclick="editAgentTaskTitle(${t.id}, this)">${escapeHtml(t.title)}</span>
            <button class="task-delete-btn" onclick="deleteAgentTask(${t.id})" title="Delete task">&times;</button>
        </div>`;
    }).join('');

    initTaskDragReorder();
}

function initTaskDragReorder() {
    const list = document.getElementById('task-bar-list');
    if (!list) return;

    let dragItem = null;

    list.querySelectorAll('.task-item').forEach(item => {
        item.addEventListener('dragstart', (e) => {
            dragItem = item;
            item.classList.add('dragging');
            e.dataTransfer.effectAllowed = 'move';
        });

        item.addEventListener('dragend', () => {
            item.classList.remove('dragging');
            dragItem = null;
            // Save new order
            saveTaskOrder();
        });

        item.addEventListener('dragover', (e) => {
            e.preventDefault();
            e.dataTransfer.dropEffect = 'move';
            if (!dragItem || dragItem === item) return;

            const rect = item.getBoundingClientRect();
            const midY = rect.top + rect.height / 2;
            if (e.clientY < midY) {
                list.insertBefore(dragItem, item);
            } else {
                list.insertBefore(dragItem, item.nextSibling);
            }
        });
    });
}

/* ── Board Tasks ────────────────────────────────────────── */

export async function loadBoardTasks(boardName) {
    if (!boardName) {
        state.currentBoardTasks = [];
        renderBoardTaskList();
        return;
    }
    try {
        const resp = await fetch(`/api/board/${encodeURIComponent(boardName)}/tasks`);
        if (!resp.ok) throw new Error(`board tasks fetch failed: ${resp.status}`);
        const data = await resp.json();
        state.currentBoardTasks = data.tasks || [];
    } catch (e) {
        state.currentBoardTasks = [];
    }
    renderBoardTaskList();
}

export function renderBoardTaskList() {
    const container = document.getElementById('board-task-list');
    if (!container) return;

    const tasks = state.currentBoardTasks || [];
    const section = document.getElementById('board-tasks-section');

    if (tasks.length === 0) {
        if (section) section.style.display = 'none';
        return;
    }
    if (section) section.style.display = '';

    container.innerHTML = tasks.map(t => {
        const statusClass = t.status === 'completed' ? 'completed'
            : t.status === 'in_progress' ? 'in-progress'
            : t.status === 'skipped' ? 'completed' : '';
        const priorityClass = 'board-task-priority-' + (t.priority || 'medium');
        const assignee = t.assigned_to || '\u2014';
        const title = escapeHtml(t.title || t.description || '');
        const tooltip = t.body ? ` title="${escapeAttr(t.body)}"` : '';
        const statusIcon = t.status === 'completed'
            ? '<span class="material-icons board-task-status-icon completed">check_circle</span>'
            : t.status === 'in_progress'
            ? '<span class="task-spinner" title="In progress"></span>'
            : t.status === 'skipped'
            ? '<span class="material-icons board-task-status-icon skipped">block</span>'
            : '<span class="material-icons board-task-status-icon pending">radio_button_unchecked</span>';
        return `
        <div class="board-task-item ${statusClass}" onclick="showTaskDetailModal(${t.id})" style="cursor:pointer">
            ${statusIcon}
            <span class="board-task-priority ${priorityClass}">${escapeHtml(t.priority || 'medium')}</span>
            <span class="board-task-assignee">${escapeHtml(assignee)}</span>
            <span class="board-task-desc"${tooltip}>${title}</span>
        </div>`;
    }).join('');
}

/* ── Task Detail Modal ─────────────────────────────────── */

export function showTaskDetailModal(taskId) {
    const tasks = state.currentBoardTasks || [];
    const task = tasks.find(t => t.id === taskId);
    if (!task) return;

    const modal = document.getElementById('task-detail-modal');
    const titleEl = document.getElementById('task-detail-modal-title');
    const content = document.getElementById('task-detail-content');
    if (!modal || !content) return;

    titleEl.textContent = `Task #${task.id}`;

    const statusLabel = task.status === 'completed' ? 'Completed'
        : task.status === 'in_progress' ? 'In Progress'
        : task.status === 'skipped' ? 'Cancelled'
        : 'Pending';
    const statusClass = task.status === 'completed' ? 'task-detail-status-completed'
        : task.status === 'in_progress' ? 'task-detail-status-inprogress'
        : task.status === 'skipped' ? 'task-detail-status-cancelled'
        : 'task-detail-status-pending';
    const priorityClass = 'board-task-priority-' + (task.priority || 'medium');

    const assignee = task.assigned_to || '\u2014';
    const createdBy = task.created_by || '\u2014';
    const createdAt = task.created_at ? formatTaskDate(task.created_at) : '\u2014';
    const claimedAt = task.claimed_at ? formatTaskDate(task.claimed_at) : null;
    const completedAt = task.completed_at ? formatTaskDate(task.completed_at) : null;
    const completedBy = task.completed_by || null;

    let html = `
        <div class="task-detail-title">${escapeHtml(task.title)}</div>
        <div class="task-detail-meta">
            <span class="task-detail-status ${statusClass}">${statusLabel}</span>
            <span class="board-task-priority ${priorityClass}">${escapeHtml(task.priority || 'medium')}</span>
        </div>`;

    if (task.body) {
        html += `<div class="task-detail-section">
            <div class="task-detail-label">Description</div>
            <div class="task-detail-body">${escapeHtml(task.body)}</div>
        </div>`;
    }

    html += `<div class="task-detail-fields">
        <div class="task-detail-field">
            <span class="task-detail-label">Assigned To</span>
            <span class="task-detail-value">${escapeHtml(assignee)}</span>
        </div>
        <div class="task-detail-field">
            <span class="task-detail-label">Created By</span>
            <span class="task-detail-value">${escapeHtml(createdBy)}</span>
        </div>
        <div class="task-detail-field">
            <span class="task-detail-label">Created</span>
            <span class="task-detail-value">${createdAt}</span>
        </div>`;

    if (claimedAt) {
        html += `<div class="task-detail-field">
            <span class="task-detail-label">Claimed</span>
            <span class="task-detail-value">${claimedAt}</span>
        </div>`;
    }
    if (completedAt) {
        html += `<div class="task-detail-field">
            <span class="task-detail-label">${task.status === 'skipped' ? 'Cancelled' : 'Completed'}</span>
            <span class="task-detail-value">${completedAt}</span>
        </div>`;
    }
    if (completedBy) {
        html += `<div class="task-detail-field">
            <span class="task-detail-label">${task.status === 'skipped' ? 'Cancelled By' : 'Completed By'}</span>
            <span class="task-detail-value">${escapeHtml(completedBy)}</span>
        </div>`;
    }
    if (task.completion_message) {
        html += `<div class="task-detail-field task-detail-field-wide">
            <span class="task-detail-label">Message</span>
            <span class="task-detail-value">${escapeHtml(task.completion_message)}</span>
        </div>`;
    }

    html += `</div>`;
    content.innerHTML = html;
    modal.style.display = '';

    // Close on backdrop click
    modal.onclick = (e) => { if (e.target === modal) hideTaskDetailModal(); };
    // Close on Escape
    modal._escHandler = (e) => { if (e.key === 'Escape') hideTaskDetailModal(); };
    document.addEventListener('keydown', modal._escHandler);
}

export function hideTaskDetailModal() {
    const modal = document.getElementById('task-detail-modal');
    if (!modal) return;
    modal.style.display = 'none';
    if (modal._escHandler) {
        document.removeEventListener('keydown', modal._escHandler);
        modal._escHandler = null;
    }
}

function formatTaskDate(isoStr) {
    try {
        const d = new Date(isoStr);
        return d.toLocaleString(undefined, {
            month: 'short', day: 'numeric',
            hour: '2-digit', minute: '2-digit',
        });
    } catch {
        return isoStr;
    }
}

async function saveTaskOrder() {
    if (!state.currentSession || state.currentSession.type !== 'live') return;
    const list = document.getElementById('task-bar-list');
    if (!list) return;

    const taskIds = Array.from(list.querySelectorAll('.task-item'))
        .map(el => parseInt(el.dataset.taskId))
        .filter(id => !isNaN(id));

    try {
        await fetch(`/api/sessions/live/${encodeURIComponent(state.currentSession.name)}/tasks/reorder`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ task_ids: taskIds }),
        });
    } catch (e) {
        // silent fail for reorder
    }
}
