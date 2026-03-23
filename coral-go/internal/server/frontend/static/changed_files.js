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

/* ── Helper: build API query string for the current session ── */

function _apiQs(filepath) {
    const qs = new URLSearchParams({ filepath });
    const sid = state.currentSession && state.currentSession.session_id;
    if (sid) qs.set('session_id', sid);
    return qs;
}

function _agentName() {
    return state.currentSession && state.currentSession.name;
}

/* ── CodeMirror 6 (lazy-loaded) ─────────────────────────────── */

let _cmModules = null;    // cached CodeMirror imports
let _cmView = null;       // active EditorView instance (edit mode)
let _cmMergeView = null;  // active merge view instance (diff mode)

async function _loadCmModules() {
    if (_cmModules) return _cmModules;
    const cm = await import('codemirror-bundle');
    _cmModules = cm;
    return _cmModules;
}

function _getLangExtension(cm, langName) {
    const loaders = {
        javascript: () => cm.javascript(),
        typescript: () => cm.javascript({ typescript: true }),
        jsx:        () => cm.javascript({ jsx: true }),
        tsx:        () => cm.javascript({ jsx: true, typescript: true }),
        python: () => cm.python(), html: () => cm.html(), css: () => cm.css(),
        json: () => cm.json(), markdown: () => cm.markdown(), sql: () => cm.sql(),
        rust: () => cm.rust(), cpp: () => cm.cpp(), c: () => cm.cpp(),
        java: () => cm.java(), go: () => cm.go(), xml: () => cm.xml(), yaml: () => cm.yaml(),
    };
    const loader = loaders[langName];
    if (!loader) return null;
    try { return loader(); } catch { return null; }
}

async function _createCmEditor(container, content, langName) {
    const cm = await _loadCmModules();

    const extensions = [
        cm.basicSetup,
        cm.oneDark,
        cm.search(),
        cm.EditorView.lineWrapping,
        cm.EditorView.theme({ '&': { height: '100%' }, '.cm-scroller': { overflow: 'auto' } }),
    ];

    // Load language extension
    const langExt = _getLangExtension(cm, langName);
    if (langExt) extensions.push(langExt);

    _cmView = new cm.EditorView({
        state: cm.EditorState.create({ doc: content, extensions }),
        parent: container,
    });
}

function _destroyCmEditor() {
    if (_cmView) {
        _cmView.destroy();
        _cmView = null;
    }
    if (_cmMergeView) {
        _cmMergeView.destroy();
        _cmMergeView = null;
    }
}

async function _createCmMergeView(container, originalContent, currentContent, langName) {
    const cm = await _loadCmModules();

    const extensions = [
        cm.basicSetup,
        cm.oneDark,
        cm.EditorView.lineWrapping,
        cm.EditorView.editable.of(false),
        cm.EditorState.readOnly.of(true),
        cm.EditorView.theme({ '&': { height: '100%' }, '.cm-scroller': { overflow: 'auto' } }),
        cm.unifiedMergeView({
            original: cm.Text.of(originalContent.split('\n')),
        }),
    ];

    const langExt = _getLangExtension(cm, langName);
    if (langExt) extensions.push(langExt);

    _cmMergeView = new cm.EditorView({
        state: cm.EditorState.create({ doc: currentContent, extensions }),
        parent: container,
    });
}

function _getCmContent() {
    return _cmView ? _cmView.state.doc.toString() : null;
}

/* ── Inline Preview Pane ───────────────────────────────────── */

let _previewState = null; // { filepath, mode, content, hasDiff, diffText, gen }
let _previewGen = 0;      // generation counter to guard against stale async writes

/** Show inline diff for a file (clicking a file row). */
export function openFileDiff(filepath) {
    if (!state.currentSession || state.currentSession.type !== 'live') return;
    _openInlinePane(filepath, 'diff');
}

/** Show inline preview for a file (clicking the preview icon). */
export function openFilePreview(filepath) {
    if (!state.currentSession || state.currentSession.type !== 'live') return;
    _openInlinePane(filepath, 'preview');
}

/** Open file directly in edit mode (clicking the edit icon). */
export function openFileEdit(filepath) {
    if (!state.currentSession || state.currentSession.type !== 'live') return;
    _openInlinePane(filepath, 'edit');
}

async function _openInlinePane(filepath, initialView) {
    const panel = document.getElementById('agentic-panel-files');
    if (!panel) return;

    // Switch to files tab if not active
    if (window.switchAgenticTab) window.switchAgenticTab('files', 'top');

    const { name } = splitPath(filepath);
    const gen = ++_previewGen;

    _previewState = { filepath, mode: 'preview', content: '', originalContent: null, hasDiff: false, gen };

    // Render the pane shell
    panel.innerHTML = `
        <div class="inline-preview-header">
            <button class="inline-preview-back" onclick="window._closeInlinePreview()" title="Back to file list">
                <span class="material-icons">arrow_back</span>
            </button>
            <span class="inline-preview-filepath" title="${escapeHtml(filepath)}">${escapeHtml(name)}</span>
            <div class="inline-preview-actions">
                <button class="inline-preview-mode-btn" id="mode-btn-diff" onclick="window._switchMode('diff')" title="Diff"><span class="material-icons">difference</span></button>
                <button class="inline-preview-mode-btn" id="mode-btn-preview" onclick="window._switchMode('preview')" title="Preview"><span class="material-icons">visibility</span></button>
                <button class="inline-preview-mode-btn" id="mode-btn-edit" onclick="window._switchMode('edit')" title="Edit"><span class="material-icons">edit</span></button>
                <button class="inline-preview-save" id="preview-save-btn" onclick="window._savePreviewFile()" style="display:none">Save</button>
            </div>
        </div>
        <div class="inline-preview-body" id="inline-preview-body">
            <div class="inline-preview-loading">Loading...</div>
        </div>
        <div class="inline-preview-cm" id="inline-preview-cm" style="display:none"></div>
    `;

    // Set active mode button and load content
    _updateModeButtons(initialView);
    if (initialView === 'edit') {
        _previewState.mode = 'edit';
        await _prefetchContent(filepath, gen);
        if (!_isStale(gen)) {
            const body = document.getElementById('inline-preview-body');
            const cmContainer = document.getElementById('inline-preview-cm');
            if (body && cmContainer) {
                body.style.display = 'none';
                cmContainer.style.display = 'block';
                const saveBtn = document.getElementById('preview-save-btn');
                if (saveBtn) saveBtn.style.display = '';
                const langName = _getLangFromPath(filepath);
                await _createCmEditor(cmContainer, _previewState.content, langName);
            }
        }
    } else if (initialView === 'diff') {
        _previewState.mode = 'diff';
        await _loadDiffView(filepath, gen);
    } else {
        _previewState.mode = 'preview';
        await _loadContentView(filepath, gen);
    }
}

/** Check if this async operation is still for the current pane. */
function _isStale(gen) {
    return !_previewState || _previewState.gen !== gen;
}

async function _loadDiffView(filepath, gen) {
    const body = document.getElementById('inline-preview-body');
    if (!body) return;

    const agentName = _agentName();
    if (!agentName) return;

    try {
        // Fetch original and current content in parallel
        const [origResp, curResp] = await Promise.all([
            fetch(`/api/sessions/live/${encodeURIComponent(agentName)}/file-original?${_apiQs(filepath)}`),
            fetch(`/api/sessions/live/${encodeURIComponent(agentName)}/file-content?${_apiQs(filepath)}`),
        ]);

        const origData = await origResp.json();
        const curData = await curResp.json();

        if (_isStale(gen)) return;

        if (curData.error) {
            body.innerHTML = `<div class="inline-preview-error">${escapeHtml(curData.error)}</div>`;
            return;
        }

        const currentContent = curData.content || '';
        const originalContent = origData.error ? '' : (origData.content || '');
        _previewState.content = currentContent;
        _previewState.originalContent = originalContent;

        // If original and current are the same (no changes), show content view
        if (originalContent === currentContent) {
            _renderContentView(body, currentContent, filepath);
            return;
        }

        _previewState.hasDiff = true;

        // Hide the body div and show merge view in-place
        body.style.display = 'none';
        const cmContainer = document.getElementById('inline-preview-cm');
        if (cmContainer) {
            cmContainer.style.display = 'block';
            const langName = _getLangFromPath(filepath);
            await _createCmMergeView(cmContainer, originalContent, currentContent, langName);
        }
    } catch (e) {
        if (_isStale(gen)) return;
        body.innerHTML = '<div class="inline-preview-error">Failed to load diff</div>';
    }
}

/** Render file content with syntax highlighting into the given container. */
function _renderContentView(container, content, filepath) {
    const escaped = escapeHtml(content);
    const lang = _getLangFromPath(filepath);
    container.innerHTML = `<pre class="inline-preview-code"><code class="language-${lang}">${escaped}</code></pre>`;
    // Apply highlight.js if available
    if (window.hljs) {
        const block = container.querySelector('pre code');
        if (block) window.hljs.highlightElement(block);
    }
}

function _getLangFromPath(fp) {
    const ext = (fp.match(/\.(\w+)$/) || [])[1] || '';
    const map = {
        js: 'javascript', ts: 'typescript', tsx: 'typescript', jsx: 'javascript',
        py: 'python', rb: 'ruby', rs: 'rust', go: 'go', java: 'java',
        sh: 'bash', zsh: 'bash', bash: 'bash', yml: 'yaml', yaml: 'yaml',
        json: 'json', toml: 'toml', css: 'css', scss: 'scss',
        html: 'html', xml: 'xml', sql: 'sql', c: 'c', cpp: 'cpp',
        h: 'c', hpp: 'cpp', cs: 'csharp', swift: 'swift', kt: 'kotlin',
        md: 'markdown',
    };
    return map[ext.toLowerCase()] || ext.toLowerCase() || 'plaintext';
}

async function _loadContentView(filepath, gen) {
    const body = document.getElementById('inline-preview-body');
    if (!body) return;

    const agentName = _agentName();
    if (!agentName) return;

    try {
        const resp = await fetch(`/api/sessions/live/${encodeURIComponent(agentName)}/file-content?${_apiQs(filepath)}`);
        const data = await resp.json();

        if (_isStale(gen)) return;

        if (data.error) {
            body.innerHTML = `<div class="inline-preview-error">${escapeHtml(data.error)}</div>`;
            return;
        }

        const content = data.content || '';
        _previewState.content = content;

        _renderContentView(body, content, filepath);
    } catch (e) {
        if (_isStale(gen)) return;
        body.innerHTML = '<div class="inline-preview-error">Failed to load file</div>';
    }
}

async function _prefetchContent(filepath, gen) {
    const agentName = _agentName();
    if (!agentName) return;

    try {
        const resp = await fetch(`/api/sessions/live/${encodeURIComponent(agentName)}/file-content?${_apiQs(filepath)}`);
        const data = await resp.json();
        if (!_isStale(gen) && !data.error) _previewState.content = data.content || '';
    } catch (e) { /* best effort */ }
}

function _updateModeButtons(activeMode) {
    ['diff', 'preview', 'edit'].forEach(m => {
        const btn = document.getElementById(`mode-btn-${m}`);
        if (btn) btn.classList.toggle('active', m === activeMode);
    });
    const saveBtn = document.getElementById('preview-save-btn');
    if (saveBtn) saveBtn.style.display = activeMode === 'edit' ? '' : 'none';
}

/** Switch between diff, preview, and edit modes. */
window._switchMode = async function(targetMode) {
    if (!_previewState || _previewState.mode === targetMode) return;

    const body = document.getElementById('inline-preview-body');
    const cmContainer = document.getElementById('inline-preview-cm');
    if (!body || !cmContainer) return;

    // Save content from current editor before destroying
    const cmContent = _getCmContent();
    if (cmContent != null) _previewState.content = cmContent;
    _destroyCmEditor();

    _previewState.mode = targetMode;
    _updateModeButtons(targetMode);
    const langName = _getLangFromPath(_previewState.filepath);

    if (targetMode === 'edit') {
        body.style.display = 'none';
        cmContainer.style.display = 'block';
        await _createCmEditor(cmContainer, _previewState.content, langName);
    } else if (targetMode === 'diff') {
        if (_previewState.hasDiff && _previewState.originalContent != null) {
            body.style.display = 'none';
            cmContainer.style.display = 'block';
            await _createCmMergeView(cmContainer, _previewState.originalContent, _previewState.content, langName);
        } else {
            cmContainer.style.display = 'none';
            body.style.display = '';
            _renderContentView(body, _previewState.content, _previewState.filepath);
        }
    } else {
        // preview — show syntax-highlighted content
        cmContainer.style.display = 'none';
        body.style.display = '';
        _renderContentView(body, _previewState.content, _previewState.filepath);
    }
};

// Keep backward compat for the edit initial view
window._togglePreviewEdit = async function() {
    if (!_previewState) return;
    await window._switchMode(_previewState.mode === 'edit' ? 'preview' : 'edit');
};

/** Save the file from the editor. */
window._savePreviewFile = async function() {
    if (!_previewState) return;

    const agentName = _agentName();
    if (!agentName) return;

    const saveBtn = document.getElementById('preview-save-btn');
    const content = _getCmContent() ?? _previewState.content;

    if (saveBtn) { saveBtn.textContent = 'Saving...'; saveBtn.disabled = true; }

    try {
        const resp = await fetch(
            `/api/sessions/live/${encodeURIComponent(agentName)}/file-content?${_apiQs(_previewState.filepath)}`,
            {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ content }),
            }
        );
        const data = await resp.json();
        if (data.error) {
            alert('Error saving: ' + data.error);
        } else {
            _previewState.content = content;
            if (saveBtn) { saveBtn.textContent = 'Saved!'; setTimeout(() => { saveBtn.textContent = 'Save'; }, 1500); }
        }
    } catch (e) {
        alert('Failed to save: ' + e.message);
    } finally {
        if (saveBtn) saveBtn.disabled = false;
    }
};

/** Close inline preview and restore the files list. */
window._closeInlinePreview = function() {
    _destroyCmEditor();
    _previewState = null;
    const panel = document.getElementById('agentic-panel-files');
    if (panel) {
        panel.innerHTML = `
            <div class="changed-files-header" id="changed-files-header">
                <span class="changed-files-title" id="changed-files-title">Loading...</span>
                <button class="refresh-files-btn" onclick="refreshChangedFiles()" title="Refresh git diff">&#x21bb;</button>
            </div>
            <div class="changed-files-list" id="changed-files-list">
                <div class="file-empty">Loading...</div>
            </div>
        `;
    }
    if (state.currentSession) {
        loadChangedFiles(state.currentSession.name, state.currentSession.session_id);
    }
};

/* ── Refresh & Render ──────────────────────────────────────── */

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

    list.innerHTML = files.map((f) => {
        const { dir, name } = splitPath(f.filepath);
        const statusCls = getStatusClass(f.status);
        const statusLabel = getStatusLabel(f.status);
        const adds = f.additions > 0 ? `<span class="file-adds">+${f.additions}</span>` : '';
        const dels = f.deletions > 0 ? `<span class="file-dels">-${f.deletions}</span>` : '';
        const stats = (adds || dels) ? `<span class="file-stats">${adds}${dels}</span>` : '';
        const statusIcon = f.status === '??' ? '?' : f.status === 'A' || f.status === 'AM' ? '+' : f.status === 'D' ? '-' : '~';
        const escapedPath = escapeHtml(f.filepath).replace(/'/g, "\\'");
        const diffBtn = `<button class="file-action-btn" onclick="event.stopPropagation(); openFileDiff('${escapedPath}')" title="Diff"><span class="material-icons">difference</span></button>`;
        const previewBtn = `<button class="file-action-btn" onclick="event.stopPropagation(); openFilePreview('${escapedPath}')" title="Preview"><span class="material-icons">visibility</span></button>`;
        const editBtn = `<button class="file-action-btn" onclick="event.stopPropagation(); openFileEdit('${escapedPath}')" title="Edit"><span class="material-icons">edit</span></button>`;

        return `<div class="file-item ${statusCls}" title="${escapeHtml(f.filepath)} (${statusLabel})"
                     onclick="openFileDiff('${escapedPath}')">
            <span class="file-status-icon">${statusIcon}</span>
            <div class="file-path-wrap">
                <span class="file-name">${escapeHtml(name)}</span>
                ${dir ? `<span class="file-dir">${escapeHtml(dir)}</span>` : ''}
            </div>
            ${stats}
            <div class="file-action-btns">${diffBtn}${previewBtn}${editBtn}</div>
        </div>`;
    }).join('');
}
