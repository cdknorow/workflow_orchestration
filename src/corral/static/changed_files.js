/* Changed files panel — load and render per-agent file diffs */

import { state } from './state.js';
import { escapeHtml } from './utils.js';

let _currentFiles = [];

export async function loadChangedFiles(agentName, sessionId) {
    if (!agentName) return;
    try {
        const params = new URLSearchParams();
        const sid = sessionId || (state.currentSession && state.currentSession.session_id);
        if (sid) params.set("session_id", sid);
        const qs = params.toString() ? `?${params}` : "";
        const resp = await fetch(`/api/sessions/live/${encodeURIComponent(agentName)}/files${qs}`);
        const data = await resp.json();
        _currentFiles = data.files || [];
    } catch (e) {
        _currentFiles = [];
    }
    renderChangedFiles();
}

export function updateChangedFileCount(count) {
    const el = document.getElementById('files-bar-count');
    if (el) {
        el.textContent = count > 0 ? String(count) : '';
    }
}

function getStatusLabel(status) {
    const map = {
        'M': 'Modified',
        'A': 'Added',
        'D': 'Deleted',
        'R': 'Renamed',
        'C': 'Copied',
        '??': 'Untracked',
        'AM': 'Added',
        'MM': 'Modified',
    };
    return map[status] || status;
}

function getStatusClass(status) {
    if (status === 'A' || status === 'AM' || status === '??') return 'file-added';
    if (status === 'D') return 'file-deleted';
    if (status === 'R') return 'file-renamed';
    return 'file-modified';
}

function splitPath(filepath) {
    const lastSlash = filepath.lastIndexOf('/');
    if (lastSlash === -1) return { dir: '', name: filepath };
    return { dir: filepath.substring(0, lastSlash + 1), name: filepath.substring(lastSlash + 1) };
}

export function openFileDiff(filepath) {
    if (!state.currentSession || state.currentSession.type !== 'live') return;
    const agentName = state.currentSession.name;
    const sessionId = state.currentSession.session_id;

    // Build the file list for prev/next navigation
    const fileList = _currentFiles.map(f => f.filepath);

    const qs = new URLSearchParams({
        agent: agentName,
        file: filepath,
        files: fileList.join('\n'),
    });
    if (sessionId) qs.set('session_id', sessionId);

    const width = Math.min(1200, Math.round(window.screen.width * 0.7));
    const height = Math.min(900, Math.round(window.screen.height * 0.8));
    const left = Math.round((window.screen.width - width) / 2);
    const top = Math.round((window.screen.height - height) / 2);

    window.open(
        `/diff?${qs}`,
        'corral-diff',
        `width=${width},height=${height},left=${left},top=${top},menubar=no,toolbar=no,status=no`,
    );
}

export async function refreshChangedFiles() {
    if (!state.currentSession || state.currentSession.type !== 'live') return;
    const btn = document.querySelector('.refresh-files-btn');
    if (btn) btn.classList.add('refreshing');

    const agentName = state.currentSession.name;
    const sessionId = state.currentSession.session_id;

    try {
        const body = {};
        if (sessionId) body.session_id = sessionId;
        const resp = await fetch(
            `/api/sessions/live/${encodeURIComponent(agentName)}/files/refresh`,
            {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body),
            }
        );

        if (resp.ok) {
            const data = await resp.json();
            _currentFiles = data.files || [];
            renderChangedFiles();
        } else {
            // Fall back to cached data
            await loadChangedFiles(agentName, sessionId);
        }
    } catch (e) {
        await loadChangedFiles(agentName, sessionId);
    }

    if (btn) setTimeout(() => btn.classList.remove('refreshing'), 300);
}

export function renderChangedFiles() {
    const list = document.getElementById('changed-files-list');
    const titleEl = document.getElementById('changed-files-title');
    const countEl = document.getElementById('files-bar-count');
    if (!list) return;

    const files = _currentFiles;

    if (titleEl) {
        titleEl.textContent = `${files.length} file${files.length !== 1 ? 's' : ''} changed`;
    }
    if (countEl) {
        countEl.textContent = files.length > 0 ? String(files.length) : '';
    }

    if (files.length === 0) {
        list.innerHTML = '<div class="file-empty">No changed files</div>';
        return;
    }

    list.innerHTML = files.map((f, idx) => {
        const { dir, name } = splitPath(f.filepath);
        const statusCls = getStatusClass(f.status);
        const statusLabel = getStatusLabel(f.status);
        const adds = f.additions > 0 ? `<span class="file-adds">+${f.additions}</span>` : '';
        const dels = f.deletions > 0 ? `<span class="file-dels">-${f.deletions}</span>` : '';
        const stats = (adds || dels) ? `<span class="file-stats">${adds}${dels}</span>` : '';
        const statusIcon = f.status === '??' ? '?' : f.status === 'A' || f.status === 'AM' ? '+' : f.status === 'D' ? '-' : '~';
        const escapedPath = escapeHtml(f.filepath).replace(/'/g, "\\'");

        return `<div class="file-item ${statusCls}" title="${escapeHtml(f.filepath)} (${statusLabel})"
                     onclick="openFileDiff('${escapedPath}')">
            <span class="file-status-icon">${statusIcon}</span>
            <div class="file-path-wrap">
                <span class="file-name">${escapeHtml(name)}</span>
                ${dir ? `<span class="file-dir">${escapeHtml(dir)}</span>` : ''}
            </div>
            ${stats}
        </div>`;
    }).join('');
}
