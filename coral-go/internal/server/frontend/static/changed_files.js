/* Changed files panel — load and render per-agent file diffs */

import { state } from './state.js';
import { escapeHtml, showToast } from './utils.js';
import { fetchFileList, fuzzyFilter } from './file_mention.js';

let _currentFiles = [];
let _searchTimeout = null;
let _renderTimer = null;

/* ── Starred files (persisted in localStorage per session) ── */

function _starKey() {
    const sid = state.currentSession && state.currentSession.session_id;
    return sid ? `coral-starred-files-${sid}` : null;
}

function _getStarredFiles() {
    const key = _starKey();
    if (!key) return [];
    try { return JSON.parse(localStorage.getItem(key) || '[]'); } catch { return []; }
}

function _setStarredFiles(files) {
    const key = _starKey();
    if (key) localStorage.setItem(key, JSON.stringify(files));
}

export function toggleStarFile(filepath) {
    const starred = _getStarredFiles();
    const idx = starred.indexOf(filepath);
    if (idx >= 0) {
        starred.splice(idx, 1);
    } else {
        starred.push(filepath);
    }
    _setStarredFiles(starred);
    renderStarredFiles();
    renderChangedFiles();
    // Re-render search results if visible
    const searchResults = document.getElementById('files-search-results');
    if (searchResults && searchResults.style.display !== 'none') {
        const items = searchResults.querySelectorAll('.file-star-btn');
        const starredSet = new Set(starred);
        items.forEach(btn => {
            const fp = btn.dataset.filepath;
            btn.classList.toggle('starred', starredSet.has(fp));
            btn.textContent = starredSet.has(fp) ? '★' : '☆';
        });
    }
}

export function renderStarredFiles() {
    const container = document.getElementById('starred-files-list');
    if (!container) return;
    const starred = _getStarredFiles();
    if (starred.length === 0) {
        container.style.display = 'none';
        container.innerHTML = '';
        return;
    }
    container.style.display = '';
    container.innerHTML = `<div class="starred-section-label">★ Starred</div>` +
        starred.map(filepath => {
            const { dir, name } = splitPath(filepath);
            const escapedPath = escapeHtml(filepath).replace(/'/g, "\\'");
            const starBtn = `<button class="file-star-btn starred" data-filepath="${escapeHtml(filepath)}" onclick="event.stopPropagation(); toggleStarFile('${escapedPath}')" title="Unstar">★</button>`;
            const previewBtn = `<button class="file-action-btn" onclick="event.stopPropagation(); openFilePreview('${escapedPath}')" title="Preview"><span class="material-icons">visibility</span></button>`;
            const editBtn = `<button class="file-action-btn" onclick="event.stopPropagation(); openFileEdit('${escapedPath}')" title="Edit"><span class="material-icons">edit</span></button>`;
            return `<div class="file-item file-starred" onclick="openFilePreview('${escapedPath}')">
                ${starBtn}
                <div class="file-path-wrap">
                    <span class="file-name">${escapeHtml(name)}</span>
                    ${dir ? `<span class="file-dir">${escapeHtml(dir)}</span>` : ''}
                </div>
                <div class="file-action-btns">${previewBtn}${editBtn}</div>
            </div>`;
        }).join('');
}

/* ── File search ── */

export async function searchRepoFiles(query) {
    if (!query || !state.currentSession || state.currentSession.type !== 'live') {
        const el = document.getElementById('files-search-results');
        if (el) el.style.display = 'none';
        return;
    }
    // Client-side fuzzy search using cached file list (shared with @file mention).
    // No server round-trip per keystroke.
    const files = await fetchFileList();
    const matches = fuzzyFilter(files, query);
    // Throttle rendering to avoid layout thrashing on fast typing
    clearTimeout(_renderTimer);
    _renderTimer = setTimeout(() => _renderSearchResults(matches), 30);
}

function _renderSearchResults(files) {
    const container = document.getElementById('files-search-results');
    if (!container) return;

    const query = (document.getElementById('files-search-input')?.value || '').trim();
    const hasExactMatch = files.some(f => f === query);
    const looksLikePath = query.includes('/') || query.includes('.');

    let html = '';

    // Show "Create <path>" option when query looks like a file path and no exact match
    if (query && looksLikePath && !hasExactMatch) {
        const escapedQuery = escapeHtml(query).replace(/'/g, "\\'");
        html += `<div class="file-item file-create-option" onclick="_createFile('${escapedQuery}')" style="color:var(--accent);font-weight:500">
            <span style="margin-right:6px;font-size:16px">+</span>
            <div class="file-path-wrap">Create ${escapeHtml(query)}</div>
        </div>`;
    }

    if (files.length === 0 && !html) {
        container.style.display = '';
        container.innerHTML = '<div class="file-empty">No matches</div>';
        return;
    }

    const starred = new Set(_getStarredFiles());
    html += files.slice(0, 50).map(filepath => {
        const { dir, name } = splitPath(filepath);
        const escapedPath = escapeHtml(filepath).replace(/'/g, "\\'");
        const isStarred = starred.has(filepath);
        const starBtn = `<button class="file-star-btn ${isStarred ? 'starred' : ''}" data-filepath="${escapeHtml(filepath)}" onclick="event.stopPropagation(); toggleStarFile('${escapedPath}')" title="${isStarred ? 'Unstar' : 'Star'}">` +
            `${isStarred ? '★' : '☆'}</button>`;
        const previewBtn = `<button class="file-action-btn" onclick="event.stopPropagation(); openFilePreview('${escapedPath}')" title="Preview"><span class="material-icons">visibility</span></button>`;
        return `<div class="file-item" onclick="openFilePreview('${escapedPath}')">
            ${starBtn}
            <div class="file-path-wrap">
                <span class="file-name">${escapeHtml(name)}</span>
                ${dir ? `<span class="file-dir">${escapeHtml(dir)}</span>` : ''}
            </div>
            <div class="file-action-btns">${previewBtn}</div>
        </div>`;
    }).join('');

    container.style.display = '';
    container.innerHTML = html;
}

export function initFileSearch() {
    const input = document.getElementById('files-search-input');
    if (!input || input.dataset.searchBound) return;
    input.dataset.searchBound = '1';

    function onSearchInput() {
        clearTimeout(_searchTimeout);
        const q = input.value.trim();
        if (!q) {
            const el = document.getElementById('files-search-results');
            if (el) { el.style.display = 'none'; el.innerHTML = ''; }
            return;
        }
        _searchTimeout = setTimeout(() => searchRepoFiles(q), 200);
    }

    // Use both input and keyup — some WebKit webviews don't fire input reliably
    input.addEventListener('input', onSearchInput);
    input.addEventListener('keyup', onSearchInput);
    input.addEventListener('keydown', (e) => {
        if (e.key === 'Escape') {
            input.value = '';
            const el = document.getElementById('files-search-results');
            if (el) { el.style.display = 'none'; el.innerHTML = ''; }
        }
    });
}

export async function loadChangedFiles(agentName, sessionId) {
    if (!agentName) return;
    try {
        const params = new URLSearchParams();
        const sid = sessionId || (state.currentSession && state.currentSession.session_id);
        if (sid) params.set("session_id", sid);
        const qs = params.toString() ? `?${params}` : "";
        const resp = await fetch(`/api/sessions/live/${encodeURIComponent(agentName)}/files${qs}`);
        if (!resp.ok) throw new Error(`files fetch failed: ${resp.status}`);
        const data = await resp.json();
        _currentFiles = data.files || [];
    } catch (e) {
        _currentFiles = [];
    }
    renderStarredFiles();
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

let _cmView = null;       // active EditorView instance (edit mode)
let _cmMergeView = null;  // active merge view instance (diff mode)

/** Get the CodeMirror module (loaded via IIFE script tag as window.CoralCM). */
function _getCm() {
    if (!window.CoralCM) {
        console.error('[coral] CodeMirror not available — codemirror-bundle.js may not have loaded');
        return null;
    }
    return window.CoralCM;
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

/** Create an editable CodeMirror editor. Returns false if CM is unavailable. */
function _createCmEditor(container, content, langName) {
    const cm = _getCm();
    if (!cm) return false;

    try {
        const extensions = [
            cm.basicSetup,
            cm.oneDark,
            cm.search(),
            cm.EditorView.lineWrapping,
            cm.EditorView.theme({ '&': { height: '100%' }, '.cm-scroller': { overflow: 'auto' } }),
        ];

        const langExt = _getLangExtension(cm, langName);
        if (langExt) extensions.push(langExt);

        _cmView = new cm.EditorView({
            state: cm.EditorState.create({ doc: content, extensions }),
            parent: container,
        });
        return true;
    } catch (e) {
        console.error('[coral] CodeMirror editor creation failed:', e);
        return false;
    }
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

/** Create a read-only CodeMirror merge view. Returns false if CM is unavailable. */
function _createCmMergeView(container, originalContent, currentContent, langName) {
    const cm = _getCm();
    if (!cm) return false;

    try {
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
        return true;
    } catch (e) {
        console.error('[coral] CodeMirror merge view creation failed:', e);
        return false;
    }
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

/** Create a new file via the API and open it in the editor. */
// Exposed on window for onclick in rendered search results.
window._createFile = async function(filePath) {
    if (!filePath) return;
    const s = state.currentSession;
    if (!s || s.type !== 'live') return;

    try {
        const qs = new URLSearchParams({ filepath: filePath, session_id: s.session_id || '' });
        const resp = await fetch(`/api/sessions/live/${encodeURIComponent(s.name)}/file-content?${qs}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ content: '' }),
        });
        const data = await resp.json();
        if (data.error) {
            showToast(data.error, true);
            return;
        }
        // Clear search, open the new file in editor, refresh file list
        const searchInput = document.getElementById('files-search-input');
        if (searchInput) searchInput.value = '';
        const searchResults = document.getElementById('files-search-results');
        if (searchResults) searchResults.style.display = 'none';
        openFileEdit(filePath);
        refreshChangedFiles();
    } catch (e) {
        showToast('Failed to create file', true);
    }
};

async function _openInlinePane(filepath, initialView) {
    // On mobile, use a full-screen overlay instead of the sidebar pane
    const isMobile = window.innerWidth <= 767;
    let panel;

    if (isMobile) {
        // Remove any existing overlay
        document.querySelectorAll('.mobile-file-preview-overlay').forEach(el => el.remove());
        const overlay = document.createElement('div');
        overlay.className = 'mobile-file-preview-overlay';
        document.body.appendChild(overlay);
        panel = overlay;
    } else {
        panel = document.getElementById('agentic-panel-files');
        if (!panel) return;
        // Switch to files tab if not active
        if (window.switchAgenticTab) window.switchAgenticTab('files', 'top');
    }

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

        // Try to show merge view; fall back to plain content if CM unavailable
        const cmContainer = document.getElementById('inline-preview-cm');
        if (cmContainer) {
            const langName = _getLangFromPath(filepath);
            const ok = await _createCmMergeView(cmContainer, originalContent, currentContent, langName);
            if (ok) {
                body.style.display = 'none';
                cmContainer.style.display = 'block';
            } else {
                // Fallback: show current content with syntax highlighting
                _renderContentView(body, currentContent, filepath);
            }
        }
    } catch (e) {
        console.error('[coral] _loadDiffView failed for', filepath, e);
        if (_isStale(gen)) return;
        body.innerHTML = '<div class="inline-preview-error">Failed to load diff</div>';
    }
}

/** Render file content with syntax highlighting into the given container. */
function _renderContentView(container, content, filepath) {
    const lang = _getLangFromPath(filepath);
    // Render markdown files as formatted HTML
    if (lang === 'markdown' && typeof marked !== 'undefined') {
        const html = marked.parse(content);
        container.innerHTML = `<div class="notes-rendered" style="padding:12px 14px;overflow-y:auto">${typeof DOMPurify !== 'undefined' ? DOMPurify.sanitize(html) : html}</div>`;
        return;
    }
    const escaped = escapeHtml(content);
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
    const fallbackEl = document.getElementById('fallback-editor');
    if (cmContent != null) _previewState.content = cmContent;
    else if (fallbackEl) _previewState.content = fallbackEl.value;
    _destroyCmEditor();

    _previewState.mode = targetMode;
    _updateModeButtons(targetMode);
    const langName = _getLangFromPath(_previewState.filepath);

    if (targetMode === 'edit') {
        body.style.display = 'none';
        cmContainer.style.display = 'block';
        const ok = await _createCmEditor(cmContainer, _previewState.content, langName);
        if (!ok) {
            // Fallback: plain textarea
            cmContainer.style.display = 'none';
            body.style.display = '';
            body.innerHTML = `<textarea class="inline-preview-fallback-editor" id="fallback-editor">${escapeHtml(_previewState.content)}</textarea>`;
        }
    } else if (targetMode === 'diff') {
        if (_previewState.hasDiff && _previewState.originalContent != null) {
            body.style.display = 'none';
            cmContainer.style.display = 'block';
            const ok = await _createCmMergeView(cmContainer, _previewState.originalContent, _previewState.content, langName);
            if (!ok) {
                // Fallback: show plain content
                cmContainer.style.display = 'none';
                body.style.display = '';
                _renderContentView(body, _previewState.content, _previewState.filepath);
            }
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
    const fallbackEditor = document.getElementById('fallback-editor');
    const content = _getCmContent() ?? (fallbackEditor ? fallbackEditor.value : _previewState.content);

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

    // Remove mobile overlay if present
    document.querySelectorAll('.mobile-file-preview-overlay').forEach(el => el.remove());

    const panel = document.getElementById('agentic-panel-files');
    if (panel) {
        panel.innerHTML = `
            <div class="changed-files-header" id="changed-files-header">
                <div class="files-search-row">
                    <input type="search" id="files-search-input" class="files-search-input" placeholder="Search or create files..." autocomplete="off">
                    <button class="refresh-files-btn" onclick="refreshChangedFiles()" title="Refresh git diff">&#x21bb;</button>
                </div>
                <span class="changed-files-title" id="changed-files-title">Loading...</span>
            </div>
            <div id="files-search-results" class="changed-files-list" style="display:none"></div>
            <div id="starred-files-list" class="changed-files-list" style="display:none"></div>
            <div class="changed-files-list" id="changed-files-list">
                <div class="file-empty">Loading...</div>
            </div>
        `;
        initFileSearch();
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
            renderStarredFiles();
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

    const files = _currentFiles.slice().sort((a, b) => a.filepath.localeCompare(b.filepath));

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

    const starred = new Set(_getStarredFiles());
    list.innerHTML = files.map((f) => {
        const { dir, name } = splitPath(f.filepath);
        const statusCls = getStatusClass(f.status);
        const statusLabel = getStatusLabel(f.status);
        const adds = f.additions > 0 ? `<span class="file-adds">+${f.additions}</span>` : '';
        const dels = f.deletions > 0 ? `<span class="file-dels">-${f.deletions}</span>` : '';
        const stats = (adds || dels) ? `<span class="file-stats">${adds}${dels}</span>` : '';
        const statusIcon = f.status === '??' ? '?' : f.status === 'A' || f.status === 'AM' ? '+' : f.status === 'D' ? '-' : '~';
        const escapedPath = escapeHtml(f.filepath).replace(/'/g, "\\'");
        const isStarred = starred.has(f.filepath);
        const starBtn = `<button class="file-star-btn ${isStarred ? 'starred' : ''}" data-filepath="${escapeHtml(f.filepath)}" onclick="event.stopPropagation(); toggleStarFile('${escapedPath}')" title="${isStarred ? 'Unstar' : 'Star'}">${isStarred ? '★' : '☆'}</button>`;
        const diffBtn = `<button class="file-action-btn" onclick="event.stopPropagation(); openFileDiff('${escapedPath}')" title="Diff"><span class="material-icons">difference</span></button>`;
        const previewBtn = `<button class="file-action-btn" onclick="event.stopPropagation(); openFilePreview('${escapedPath}')" title="Preview"><span class="material-icons">visibility</span></button>`;
        const editBtn = `<button class="file-action-btn" onclick="event.stopPropagation(); openFileEdit('${escapedPath}')" title="Edit"><span class="material-icons">edit</span></button>`;

        return `<div class="file-item ${statusCls}" title="${escapeHtml(f.filepath)} (${statusLabel})"
                     onclick="openFileDiff('${escapedPath}')">
            ${starBtn}
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
