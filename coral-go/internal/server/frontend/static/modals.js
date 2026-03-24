/* Modal management: launch and info dialogs */

import { state } from './state.js';
import { showToast, escapeHtml, escapeAttr } from './utils.js';
import { loadLiveSessions } from './api.js';
import { loadBoardProjects } from './message_board.js';
import { getEngineNames, getEngineName, setRendererOverride } from './renderers.js';
import { renderCaptureText, syncPaneWidth } from './capture.js';
import { hideRestartModal } from './controls.js';
import { updateTerminalTheme, getTerminal } from './xterm_renderer.js';

export function toggleFlag(inputId, flag) {
    const input = document.getElementById(inputId);
    const current = input.value.trim();
    const flags = current ? current.split(/\s+/) : [];
    const idx = flags.indexOf(flag);
    if (idx >= 0) {
        flags.splice(idx, 1);
    } else {
        flags.push(flag);
    }
    input.value = flags.join(" ");
    // Update button active states
    input.dispatchEvent(new Event("input"));
}

// Track which launch mode is active: null (chooser), 'agent', or 'terminal'
let _launchMode = null;
let _existingTeamBoardNames = new Set();

function _syncMobileLaunchSections(step) {
    const isMobile = window.innerWidth <= 767;
    const root = step ? document.getElementById(`launch-step-${step}`) : document.getElementById("launch-modal");
    if (!root) return;
    root.querySelectorAll(".mobile-launch-section").forEach(section => {
        section.open = !isMobile;
    });
}

function _showLaunchStep(step) {
    document.getElementById("launch-step-chooser").style.display = step === "chooser" ? "flex" : "none";
    document.getElementById("launch-step-agent").style.display = step === "agent" ? "flex" : "none";
    document.getElementById("launch-step-terminal").style.display = step === "terminal" ? "flex" : "none";
    document.getElementById("launch-step-team").style.display = step === "team" ? "flex" : "none";
    // Widen the modal for multi-column layouts
    const content = document.querySelector("#launch-modal .modal-content");
    if (content) {
        content.classList.toggle("modal-content-agent-wide", step === "agent");
        content.classList.toggle("modal-content-extra-wide", step === "team");
        content.classList.toggle("modal-content-wide", step !== "team" && step !== "agent");
    }
    _syncMobileLaunchSections(step);
}

function _selectLaunchType(type) {
    if (type === "back") {
        _launchMode = null;
        _showLaunchStep("chooser");
        return;
    }
    _launchMode = type;
    _showLaunchStep(type);
    if (type === "agent") {
        _loadAgentBoardProjects();
        _initAgentPresets();
        // Check CLI availability for the selected agent type
        const agentType = document.getElementById("launch-type")?.value;
        if (agentType) _checkAgentCLI(agentType);
    } else if (type === "team") {
        _initTeamForm();
    } else {
        document.getElementById("launch-terminal-name").focus();
    }
}
window._selectLaunchType = _selectLaunchType;

export function showLaunchModal() {
    _launchMode = null;
    document.getElementById("launch-modal").style.display = "flex";
    _showLaunchStep("chooser");

    // Reset agent form
    document.getElementById("launch-agent-name").value = "";
    document.getElementById("launch-flags").value = _getPermFlag('launch-type');
    document.getElementById("launch-agent-prompt").value = "";
    document.getElementById("launch-board-name").value = "";
    document.getElementById("launch-board-server").value = "";
    const agentBoardSelect = document.getElementById("launch-board-select");
    if (agentBoardSelect) agentBoardSelect.value = "";
    document.getElementById("launch-new-board-row").style.display = "none";
    document.getElementById("launch-board-server-row").style.display = "none";
    _syncFlagButtons("launch-flags");
    // Clear preset selection and unlock fields
    document.querySelectorAll("#agent-preset-selector .agent-preset-btn").forEach(btn => btn.classList.remove("active"));
    const agentNameInput = document.getElementById("launch-agent-name");
    const agentPromptInput = document.getElementById("launch-agent-prompt");
    agentNameInput.readOnly = false;
    agentPromptInput.readOnly = false;
    agentNameInput.classList.remove("preset-locked");
    agentPromptInput.classList.remove("preset-locked");
    const savePersonaBtn = document.getElementById("agent-save-persona-btn");
    if (savePersonaBtn) savePersonaBtn.style.display = "";

    // Reset terminal form
    document.getElementById("launch-terminal-name").value = "";

    // Reset team form
    document.getElementById("team-agents-list").innerHTML = "";
    document.getElementById("team-board-name").value = "";
    const teamFlagsEl = document.getElementById("team-flags");
    if (teamFlagsEl) teamFlagsEl.value = "";
    const teamAutoPermsEl = document.getElementById("team-auto-permissions");
    if (teamAutoPermsEl) teamAutoPermsEl.checked = true;

    // Pre-fill from global settings
    const s = state.settings || {};
    const dirInput = document.getElementById("launch-dir");
    const termDirInput = document.getElementById("launch-terminal-dir");
    if (s.default_working_dir) {
        if (dirInput) dirInput.value = s.default_working_dir;
        if (termDirInput) termDirInput.value = s.default_working_dir;
    }
    const typeSelect = document.getElementById("launch-type");
    if (s.default_agent_type && typeSelect) {
        typeSelect.value = s.default_agent_type;
    }

    _syncMobileLaunchSections();
}

export function hideLaunchModal() {
    document.getElementById("launch-modal").style.display = "none";
}

// ── CLI Availability Check ────────────────────────────────────────────────

const CLI_INSTALL_INSTRUCTIONS = {
    claude: { name: 'claude', cmd: 'npm install -g @anthropic-ai/claude-code' },
    gemini: { name: 'gemini', cmd: 'pip install google-gemini-cli' },
    codex: { name: 'codex', cmd: 'npm install -g @openai/codex' },
};

let _cliCheckCache = {}; // { type: {available, checkedAt} }

async function _checkAgentCLI(agentType) {
    const warning = document.getElementById('cli-check-warning');
    if (!warning) return;

    // Check cache (valid for 30s)
    const cached = _cliCheckCache[agentType];
    if (cached && (Date.now() - cached.checkedAt) < 30000) {
        _showCLIWarning(warning, agentType, cached.available);
        return;
    }

    try {
        const resp = await fetch(`/api/system/cli-check?type=${encodeURIComponent(agentType)}`);
        const data = await resp.json();
        const available = data.available !== false;
        _cliCheckCache[agentType] = { available, checkedAt: Date.now() };
        _showCLIWarning(warning, agentType, available);
    } catch {
        // Network error — don't show warning
        warning.style.display = 'none';
    }
}
window._checkAgentCLI = _checkAgentCLI;

/** Map agent type to its permission bypass flag. */
const PERM_FLAGS = {
    claude: '--dangerously-skip-permissions',
    codex: '--full-auto',
    gemini: '--dangerously-skip-permissions',
};

/** Update permission flag shortcut buttons in the same modal as the agent type select. */
function _updatePermFlagButtons(selectEl) {
    const agentType = selectEl.value || 'claude';
    const flag = PERM_FLAGS[agentType] || PERM_FLAGS.claude;

    // Find the enclosing modal content and update all flag-shortcut-btn that reference skip-permissions/full-auto
    const modal = selectEl.closest('.modal-content') || selectEl.closest('.modal');
    if (!modal) return;

    modal.querySelectorAll('.flag-shortcut-btn').forEach(btn => {
        const onclick = btn.getAttribute('onclick') || '';
        // Match buttons that toggle a dangerously- or --full-auto flag
        if (onclick.includes('dangerously-') || onclick.includes('full-auto')) {
            // Extract the flag input ID from the onclick
            const match = onclick.match(/toggleFlag\('([^']+)'/);
            if (match) {
                const flagInputId = match[1];
                btn.setAttribute('onclick', `toggleFlag('${flagInputId}','${flag}')`);
                btn.textContent = flag;
            }
        }
    });

    // Also update the flags input value — replace any known perm flag with the new one
    const allFlags = Object.values(PERM_FLAGS);
    modal.querySelectorAll('input[id*=flags]').forEach(input => {
        let val = input.value;
        const hadFlag = allFlags.some(f => val.includes(f));
        if (hadFlag) {
            allFlags.forEach(f => { val = val.replace(f, ''); });
            val = val.trim();
            input.value = val ? val + ' ' + flag : flag;
        }
    });
}
window._updatePermFlagButtons = _updatePermFlagButtons;

/** Get the permission flag for a given agent type select element ID. */
function _getPermFlag(selectId) {
    const el = document.getElementById(selectId);
    const agentType = el ? el.value : 'claude';
    return PERM_FLAGS[agentType] || PERM_FLAGS.claude;
}

/** Check if a flags string contains ANY known permission flag. */
function _hasPermFlag(flagsStr) {
    return Object.values(PERM_FLAGS).some(f => flagsStr.includes(f));
}

/** Verify a CLI path by calling the backend check endpoint. */
async function _checkOneCLI(inputId, resultId) {
    const input = document.getElementById(inputId);
    const resultEl = document.getElementById(resultId);
    if (!input || !resultEl) return;

    const binary = input.value.trim() || input.placeholder;
    resultEl.textContent = '...';
    resultEl.className = 'cli-verify-result';

    try {
        const resp = await fetch(`/api/system/cli-check?binary=${encodeURIComponent(binary)}`);
        const data = await resp.json();
        if (data.found) {
            resultEl.textContent = data.version || 'Found';
            resultEl.classList.add('cli-verify-ok');
        } else {
            resultEl.textContent = 'Not found';
            resultEl.classList.add('cli-verify-fail');
        }
    } catch {
        resultEl.textContent = 'Error';
        resultEl.classList.add('cli-verify-fail');
    }
}

window._verifyAllCLIs = async function() {
    const btn = document.getElementById('verify-all-btn');
    if (btn) { btn.disabled = true; btn.textContent = 'Checking...'; }
    await Promise.all([
        _checkOneCLI('settings-cli-path-claude', 'cli-result-claude'),
        _checkOneCLI('settings-cli-path-codex', 'cli-result-codex'),
        _checkOneCLI('settings-cli-path-gemini', 'cli-result-gemini'),
    ]);
    if (btn) { btn.disabled = false; btn.textContent = 'Verify All'; }
};

function _showCLIWarning(el, agentType, available) {
    if (available) {
        el.style.display = 'none';
        return;
    }
    const info = CLI_INSTALL_INSTRUCTIONS[agentType] || { name: agentType, cmd: '' };
    el.innerHTML = `<span class="cli-warning-icon" title="${info.name} CLI not found">&#x26A0;</span> <code>${info.name}</code> not found`;
    el.style.display = '';
}

function _showCLINotFoundModal(agentType) {
    const info = CLI_INSTALL_INSTRUCTIONS[agentType] || { name: agentType, cmd: `Install ${agentType} CLI` };
    const modal = document.createElement('div');
    modal.className = 'modal';
    modal.style.display = 'flex';
    modal.innerHTML = `
        <div class="modal-content" style="width:480px">
            <h3>${info.name} CLI Not Found</h3>
            <p style="color:var(--text-secondary);font-size:13px;line-height:1.5;margin:8px 0 12px">
                The <code>${info.name}</code> command was not found on this system. Install it to launch ${info.name} agents.
            </p>
            <div style="background:var(--bg-tertiary);border:1px solid var(--border);border-radius:6px;padding:10px 14px;font-family:var(--font-mono);font-size:13px;display:flex;align-items:center;gap:8px">
                <code style="flex:1;user-select:all">${escapeHtml(info.cmd)}</code>
                <button class="btn btn-small" onclick="navigator.clipboard.writeText('${info.cmd.replace(/'/g, "\\'")}'); this.textContent='Copied!'; setTimeout(()=>this.textContent='Copy',1500)">Copy</button>
            </div>
            <div class="modal-actions" style="margin-top:16px">
                <button class="btn" data-action="settings">Set CLI Path</button>
                <button class="btn btn-primary" data-action="close">OK</button>
            </div>
        </div>
    `;
    document.body.appendChild(modal);
    modal.addEventListener('click', (e) => {
        if (e.target.dataset?.action === 'close' || e.target === modal) modal.remove();
        if (e.target.dataset?.action === 'settings') { modal.remove(); showSettingsModal(); }
    });
}

function _showDemoLimitModal(message) {
    const modal = document.createElement('div');
    modal.className = 'modal';
    modal.style.display = 'flex';
    modal.innerHTML = `
        <div class="modal-content" style="width:480px;text-align:center">
            <h3 style="margin-bottom:12px">Demo Limit Reached</h3>
            <p style="color:var(--text-secondary);font-size:14px;line-height:1.6;margin:0 0 16px">
                This is a demo edition with the following limits:<br>
                <strong>Max Live Sessions:</strong> 10<br>
                <strong>Max Agent Teams:</strong> 2
            </p>
            <p style="color:var(--text-secondary);font-size:13px;margin:0 0 20px">
                Please stop existing sessions before launching new ones.
            </p>
            <div class="modal-actions" style="justify-content:center">
                <button class="btn btn-primary" data-action="close">OK</button>
            </div>
        </div>
    `;
    document.body.appendChild(modal);
    modal.addEventListener('click', (e) => {
        if (e.target.dataset?.action === 'close' || e.target === modal) modal.remove();
    });
}

function _checkPermissionFlag(flagsStr, agentType) {
    const permFlag = PERM_FLAGS[agentType] || PERM_FLAGS.claude;
    return new Promise((resolve) => {
        if (flagsStr && _hasPermFlag(flagsStr)) {
            resolve('skip');
            return;
        }
        const modal = document.createElement('div');
        modal.className = 'modal';
        modal.style.display = 'flex';
        modal.innerHTML = `
            <div class="modal-content" style="width:460px">
                <h3>Permission Prompt Warning</h3>
                <p style="color:var(--text-secondary);font-size:13px;line-height:1.5;margin:8px 0 16px">
                    Agents may get stuck on permission prompts without <code>${escapeHtml(permFlag)}</code>. Would you like to enable it?
                </p>
                <div class="modal-actions" style="gap:8px">
                    <button class="btn" data-action="cancel">Cancel</button>
                    <button class="btn" data-action="skip">Launch Without</button>
                    <button class="btn btn-primary" data-action="enable">Enable &amp; Launch</button>
                </div>
            </div>
        `;
        document.body.appendChild(modal);
        modal.addEventListener('click', (e) => {
            const action = e.target.dataset?.action;
            if (action) {
                modal.remove();
                resolve(action === 'cancel' ? null : action);
            } else if (e.target === modal) {
                modal.remove();
                resolve(null);
            }
        });
    });
}

export async function launchSession() {
    let dir, type, agentName, flagsStr, backend;

    if (_launchMode === "terminal") {
        dir = document.getElementById("launch-terminal-dir").value.trim();
        type = "terminal";
        agentName = document.getElementById("launch-terminal-name").value.trim();
        flagsStr = "";
        backend = document.getElementById("launch-terminal-backend")?.value;
    } else {
        dir = document.getElementById("launch-dir").value.trim();
        type = document.getElementById("launch-type").value;
        agentName = document.getElementById("launch-agent-name").value.trim();
        flagsStr = document.getElementById("launch-flags").value.trim();
        backend = document.getElementById("launch-backend")?.value;
    }

    if (!dir) {
        showToast("Working directory is required", true);
        return;
    }

    // Permission flag check (skip for terminals)
    if (_launchMode !== "terminal") {
        const permResult = await _checkPermissionFlag(flagsStr, type);
        if (permResult === null) return;
        if (permResult === 'enable') {
            const permFlag = PERM_FLAGS[type] || PERM_FLAGS.claude;
            flagsStr = (flagsStr ? flagsStr + ' ' : '') + permFlag;
            const flagsInput = document.getElementById("launch-flags");
            if (flagsInput) flagsInput.value = flagsStr;
        }
    }

    const payload = { working_dir: dir, agent_type: type };
    if (agentName) payload.display_name = agentName;
    if (backend) payload.backend = backend;

    if (flagsStr) {
        payload.flags = flagsStr.split(/\s+/);
    }

    // Agent capabilities (only for agent mode)
    if (_launchMode !== "terminal") {
        const capabilities = _getPermissions('launch-perms');
        if (capabilities) payload.capabilities = capabilities;
    }

    // Agent prompt and message board (only for agent mode)
    if (_launchMode !== "terminal") {
        const prompt = document.getElementById("launch-agent-prompt").value.trim();
        if (prompt) payload.prompt = prompt;

        const boardSelect = document.getElementById("launch-board-select");
        if (boardSelect && boardSelect.value) {
            if (boardSelect.value === "__new__") {
                const boardName = document.getElementById("launch-board-name").value.trim();
                if (boardName) payload.board_name = boardName;
            } else {
                payload.board_name = boardSelect.value;
            }
            const boardServer = document.getElementById("launch-board-server").value.trim();
            if (boardServer) payload.board_server = boardServer;
        }
    }

    // Disable launch button to prevent double-clicks
    const stepId = _launchMode === "terminal" ? "launch-step-terminal" : "launch-step-agent";
    const launchBtn = document.querySelector(`#${stepId} .btn-primary`);
    if (launchBtn) { launchBtn.disabled = true; launchBtn.textContent = 'Launching...'; }

    try {
        const resp = await fetch("/api/sessions/launch", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(payload),
        });
        if (resp.status === 403) {
            const err = await resp.json();
            _showDemoLimitModal(err.error || 'Demo limit reached');
            return;
        }
        const result = await resp.json();
        if (result.error) {
            if (result.error.includes('not found') && result.error.includes('CLI')) {
                _showCLINotFoundModal(type);
            } else {
                showToast(result.error, true);
            }
        } else {
            showToast(`Launched: ${result.session_name}`);
            hideLaunchModal();
            setTimeout(loadLiveSessions, 2000);
        }
    } catch (e) {
        showToast("Failed to launch session", true);
    } finally {
        if (launchBtn) { launchBtn.disabled = false; launchBtn.textContent = 'Launch'; }
    }
}

// ── Agent Board Select ────────────────────────────────────────────────────

async function _loadAgentBoardProjects() {
    const select = document.getElementById("launch-board-select");
    if (!select) return;
    select.innerHTML = '<option value="">None</option><option value="__new__">Create new board...</option>';
    try {
        const resp = await fetch("/api/board/projects");
        const projects = await resp.json();
        for (const p of projects) {
            const opt = document.createElement("option");
            opt.value = p.project;
            opt.textContent = p.project;
            select.appendChild(opt);
        }
    } catch (_) {}
}

function _onAgentBoardChange() {
    const select = document.getElementById("launch-board-select");
    const newRow = document.getElementById("launch-new-board-row");
    const serverRow = document.getElementById("launch-board-server-row");
    const hasBoard = select.value && select.value !== "";
    newRow.style.display = select.value === "__new__" ? "" : "none";
    serverRow.style.display = hasBoard ? "" : "none";
    if (!hasBoard) document.getElementById("launch-board-server").value = "";
}
window._onAgentBoardChange = _onAgentBoardChange;

// ── Saved Personas & Team Templates ──────────────────────────────────────

function _getSavedPersonas() {
    try { return JSON.parse(state.settings.saved_personas || "[]"); }
    catch { return []; }
}

async function _setSavedPersonas(personas) {
    state.settings.saved_personas = JSON.stringify(personas);
    await fetch("/api/settings", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ saved_personas: state.settings.saved_personas }),
    });
}

function _getSavedTeamTemplates() {
    try { return JSON.parse(state.settings.saved_team_templates || "[]"); }
    catch { return []; }
}

async function _setSavedTeamTemplates(templates) {
    state.settings.saved_team_templates = JSON.stringify(templates);
    await fetch("/api/settings", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ saved_team_templates: state.settings.saved_team_templates }),
    });
}

/** Render preset buttons (built-in + saved) into a container. */
function _renderPresetButtons(containerId, onClickFn, opts = {}) {
    const container = document.getElementById(containerId);
    if (!container) return;
    const saved = _getSavedPersonas();

    let html = `<select class="agent-preset-select" onchange="${onClickFn}(this.value); this.value='';">`;
    html += `<option value="" disabled selected>Choose a preset or custom...</option>`;
    html += `<optgroup label="Built-in">`;
    for (const preset of AGENT_PRESETS) {
        html += `<option value="${escapeAttr(preset.name)}">${escapeHtml(preset.name)}</option>`;
    }
    html += `</optgroup>`;
    if (saved.length > 0) {
        html += `<optgroup label="Saved Personas">`;
        for (const p of saved) {
            html += `<option value="${escapeAttr(p.name)}">${escapeHtml(p.name)}</option>`;
        }
        html += `</optgroup>`;
    }
    html += `<option value="">Custom</option>`;
    html += `</select>`;
    container.innerHTML = html;
}

/** Find a persona by name in built-in + saved lists. */
function _findPersona(name) {
    return AGENT_PRESETS.find(p => p.name === name) || _getSavedPersonas().find(p => p.name === name);
}

async function _saveCurrentPersona(nameInputId, promptInputId, flagsInputId) {
    const name = document.getElementById(nameInputId).value.trim();
    const prompt = document.getElementById(promptInputId).value.trim();
    const flags = flagsInputId ? document.getElementById(flagsInputId).value.trim() : "";
    if (!name) { showToast("Enter a name before saving", "error"); return; }
    if (AGENT_PRESETS.find(p => p.name === name)) { showToast("Can't overwrite a built-in preset", "error"); return; }

    const capabilities = _getPermissions('launch-perms');
    const saved = _getSavedPersonas();
    const idx = saved.findIndex(p => p.name === name);
    const entry = { name, prompt, flags };
    if (capabilities) entry.capabilities = capabilities;
    if (idx >= 0) saved[idx] = entry; else saved.push(entry);
    await _setSavedPersonas(saved);
    showToast(`Saved persona "${name}"`);

    // Re-render all preset containers
    _renderPresetButtons("agent-preset-selector", "window._selectAgentPreset");
    _renderPresetButtons("add-agent-board-presets", "window._selectBoardAgentPreset");
}
window._saveCurrentPersona = _saveCurrentPersona;

async function _deletePersona(name) {
    const saved = _getSavedPersonas().filter(p => p.name !== name);
    await _setSavedPersonas(saved);
    showToast(`Deleted persona "${name}"`);
    _renderPresetButtons("agent-preset-selector", "window._selectAgentPreset");
    _renderPresetButtons("add-agent-board-presets", "window._selectBoardAgentPreset");
}
window._deletePersona = _deletePersona;

// ── Export / Import (Personas & Team Templates) ──────────────────────────

function _downloadJson(data, filename) {
    const blob = new Blob([JSON.stringify(data, null, 2)], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = filename;
    a.click();
    URL.revokeObjectURL(url);
}

function _openFilePicker() {
    return new Promise((resolve) => {
        const input = document.createElement("input");
        input.type = "file";
        input.accept = ".json";
        input.onchange = () => {
            const file = input.files[0];
            if (!file) { resolve(null); return; }
            const reader = new FileReader();
            reader.onload = () => {
                try { resolve(JSON.parse(reader.result)); }
                catch { showToast("Invalid JSON file", "error"); resolve(null); }
            };
            reader.readAsText(file);
        };
        input.click();
    });
}

export function exportPersonas() {
    const saved = _getSavedPersonas();
    if (saved.length === 0) { showToast("No saved personas to export", "error"); return; }
    _downloadJson({ version: 1, type: "coral-personas", personas: saved }, "coral-personas.json");
    showToast(`Exported ${saved.length} persona(s)`);
}

export async function importPersonas() {
    const data = await _openFilePicker();
    if (!data) return;
    const personas = data.personas || (Array.isArray(data) ? data : null);
    if (!personas || !Array.isArray(personas)) { showToast("Invalid persona file format", "error"); return; }

    const existing = _getSavedPersonas();
    const existingNames = new Set(existing.map(p => p.name));
    let added = 0;
    for (const p of personas) {
        if (!p.name) continue;
        if (AGENT_PRESETS.find(bp => bp.name === p.name)) continue;
        if (existingNames.has(p.name)) {
            const idx = existing.findIndex(e => e.name === p.name);
            existing[idx] = { name: p.name, prompt: p.prompt || "", flags: p.flags || "" };
        } else {
            existing.push({ name: p.name, prompt: p.prompt || "", flags: p.flags || "" });
        }
        added++;
    }
    await _setSavedPersonas(existing);
    showToast(`Imported ${added} persona(s)`);
    _renderPresetButtons("agent-preset-selector", "window._selectAgentPreset");
    _renderPresetButtons("add-agent-board-presets", "window._selectBoardAgentPreset");
}

export function exportTeamTemplates() {
    const templates = _getSavedTeamTemplates();
    if (templates.length === 0) { showToast("No saved templates to export", "error"); return; }
    _downloadJson({ version: 1, type: "coral-team-templates", templates }, "coral-team-templates.json");
    showToast(`Exported ${templates.length} template(s)`);
}

export async function importTeamTemplates() {
    const data = await _openFilePicker();
    if (!data) return;
    const templates = data.templates || (Array.isArray(data) ? data : null);
    if (!templates || !Array.isArray(templates)) { showToast("Invalid template file format", "error"); return; }

    const existing = _getSavedTeamTemplates();
    const existingNames = new Set(existing.map(t => t.name));
    let added = 0;
    for (const t of templates) {
        if (!t.name || !Array.isArray(t.agents)) continue;
        if (existingNames.has(t.name)) {
            const idx = existing.findIndex(e => e.name === t.name);
            existing[idx] = t;
        } else {
            existing.push(t);
        }
        added++;
    }
    await _setSavedTeamTemplates(existing);
    showToast(`Imported ${added} template(s)`);
    _renderTeamTemplateSelector();
}

// ── Agent Preset Selector (single-agent modal) ───────────────────────────

function _initAgentPresets() {
    _renderPresetButtons("agent-preset-selector", "window._selectAgentPreset");
}

function _selectAgentPreset(name) {
    const nameInput = document.getElementById("launch-agent-name");
    const promptInput = document.getElementById("launch-agent-prompt");
    const saveBtn = document.getElementById("agent-save-persona-btn");

    if (name) {
        const persona = _findPersona(name);
        if (persona) {
            nameInput.value = persona.name;
            promptInput.value = persona.prompt;
        }
    } else {
        nameInput.value = "";
        promptInput.value = "";
    }

    // Populate permissions from preset
    const persona = name ? _findPersona(name) : null;
    _setPermissions('launch-perms', persona?.capabilities || null);

    // Lock fields for built-in presets (they can't be saved/overwritten)
    const isBuiltIn = !!AGENT_PRESETS.find(p => p.name === name);
    nameInput.readOnly = isBuiltIn;
    promptInput.readOnly = isBuiltIn;
    nameInput.classList.toggle("preset-locked", isBuiltIn);
    promptInput.classList.toggle("preset-locked", isBuiltIn);
    if (saveBtn) saveBtn.style.display = isBuiltIn ? "none" : "";

    document.querySelectorAll("#agent-preset-selector .agent-preset-btn").forEach(btn => {
        btn.classList.toggle("active", btn.dataset.preset === name);
    });

    if (!isBuiltIn) nameInput.focus();
}
window._selectAgentPreset = _selectAgentPreset;

// ── Add Agent to Board ───────────────────────────────────────────────────

export function showAddAgentToBoard(boardName, workDir) {
    const modal = document.getElementById("add-agent-board-modal");
    document.getElementById("add-agent-board-name").value = boardName;
    document.getElementById("add-agent-board-workdir").value = workDir;
    document.getElementById("add-agent-board-subtitle").textContent = `Board: ${boardName}`;
    document.getElementById("add-agent-board-agent-name").value = "";
    document.getElementById("add-agent-board-prompt").value = "";
    document.getElementById("add-agent-board-flags").value = _getPermFlag('add-agent-board-type');
    _syncFlagButtons("add-agent-board-flags");

    _renderPresetButtons("add-agent-board-presets", "window._selectBoardAgentPreset");

    modal.style.display = "flex";
}

export function hideAddAgentBoardModal() {
    document.getElementById("add-agent-board-modal").style.display = "none";
}

function _selectBoardAgentPreset(name) {
    const nameInput = document.getElementById("add-agent-board-agent-name");
    const promptInput = document.getElementById("add-agent-board-prompt");

    if (name) {
        const persona = _findPersona(name);
        if (persona) {
            nameInput.value = persona.name;
            promptInput.value = persona.prompt;
        }
    } else {
        nameInput.value = "";
        promptInput.value = "";
    }

    document.querySelectorAll("#add-agent-board-presets .agent-preset-btn").forEach(btn => {
        btn.classList.toggle("active", btn.dataset.preset === name);
    });
    nameInput.focus();
}
window._selectBoardAgentPreset = _selectBoardAgentPreset;

export async function launchAgentToBoard() {
    const boardName = document.getElementById("add-agent-board-name").value;
    const workDir = document.getElementById("add-agent-board-workdir").value;
    const agentName = document.getElementById("add-agent-board-agent-name").value.trim();
    const prompt = document.getElementById("add-agent-board-prompt").value.trim();
    let flagsStr = document.getElementById("add-agent-board-flags").value.trim();

    if (!agentName) {
        showToast("Agent name is required", "error");
        return;
    }

    // Permission flag check
    const boardAgentType = document.getElementById("add-agent-board-type")?.value || "claude";
    const permResult = await _checkPermissionFlag(flagsStr, boardAgentType);
    if (permResult === null) return;
    if (permResult === 'enable') {
        const permFlag = PERM_FLAGS[boardAgentType] || PERM_FLAGS.claude;
        flagsStr = (flagsStr ? flagsStr + ' ' : '') + permFlag;
        const flagsInput = document.getElementById("add-agent-board-flags");
        if (flagsInput) flagsInput.value = flagsStr;
    }

    const flags = flagsStr ? flagsStr.split(/\s+/) : [];

    // Disable launch button to prevent double-clicks
    const launchBtn = document.querySelector('#add-agent-board-modal .btn-primary');
    if (launchBtn) { launchBtn.disabled = true; launchBtn.textContent = 'Launching...'; }

    try {
        const resp = await fetch("/api/sessions/launch", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
                working_dir: workDir,
                agent_type: document.getElementById("add-agent-board-type")?.value || "claude",
                display_name: agentName,
                flags,
                prompt,
                board_name: boardName,
            }),
        });
        if (resp.status === 403) {
            const err = await resp.json();
            _showDemoLimitModal(err.error || 'Demo limit reached');
            return;
        }
        const result = await resp.json();
        if (result.error) {
            if (result.error.includes('not found') && result.error.includes('CLI')) {
                _showCLINotFoundModal(boardAgentType);
            } else {
                showToast(result.error, "error");
            }
        } else {
            showToast(`Launched ${agentName} on board ${boardName}`);
            hideAddAgentBoardModal();
        }
    } catch (e) {
        showToast("Failed to launch agent", "error");
    } finally {
        if (launchBtn) { launchBtn.disabled = false; launchBtn.textContent = 'Launch'; }
    }
}

// ── Agent Team ────────────────────────────────────────────────────────────

let _teamAgentCounter = 0;

// Predefined agent presets
// All known Coral capabilities
const ALL_CAPABILITIES = ['file_read', 'file_write', 'shell', 'web_access', 'git_write', 'agent_spawn', 'notebook'];

const AGENT_PRESETS = [
    {
        name: "Lead Developer",
        prompt: "You are the lead developer. Implement features, write code, and coordinate with the team via the message board.",
        capabilities: { allow: ['file_read', 'file_write', 'shell', 'git_write', 'agent_spawn'] },
    },
    {
        name: "QA Engineer",
        prompt: "You are an expert QA engineer. Review the work of other agents, create test plans, write tests, and ask probing questions about complex areas. Coordinate with the team via the message board.",
        capabilities: { allow: ['file_read'], deny: ['file_write', 'shell'] },
    },
    {
        name: "Orchestrator",
        prompt: "You are the orchestrator. Coordinate the team, break down tasks, assign work via the message board, and track progress. Do not write code yourself — delegate to the other agents. Coordinate with the team via the message board.",
        capabilities: { allow: ['file_read', 'shell:coral-board *', 'agent_spawn', 'web_access'] },
    },
    {
        name: "Frontend Dev",
        prompt: "You are a frontend developer. Build UI components, style pages, and ensure a great user experience. Coordinate with the team via the message board.",
        capabilities: { allow: ['file_read', 'file_write', 'shell:npm *', 'shell:npx *', 'web_access'] },
    },
    {
        name: "Backend Dev",
        prompt: "You are a backend developer. Build APIs, services, and data models. Coordinate with the team via the message board.",
        capabilities: { allow: ['file_read', 'file_write', 'shell', 'git_write'] },
    },
    {
        name: "DevOps Engineer",
        prompt: "You are a DevOps engineer. Handle CI/CD, infrastructure, deployment, and monitoring. Coordinate with the team via the message board.",
        capabilities: { allow: ['file_read', 'file_write', 'shell', 'git_write'] },
    },
    {
        name: "Security Reviewer",
        prompt: "You are a security reviewer. Audit code for vulnerabilities, review auth flows, and ensure OWASP best practices. Report findings via the message board.",
        capabilities: { allow: ['file_read', 'web_access'] },
    },
    {
        name: "Technical Writer",
        prompt: "You are a technical writer. Write documentation, API guides, and READMEs. Coordinate with the team via the message board.",
        capabilities: { allow: ['file_read', 'file_write'] },
    },
    {
        name: "Content Writer",
        prompt: "You are an expert content writer. Write blog posts, social media copy, email campaigns, and landing page content. Focus on clear, engaging writing that drives action. Coordinate with the team via the message board.",
        capabilities: { allow: ['file_read', 'file_write', 'web_access'] },
    },
    {
        name: "SEO Strategist",
        prompt: "You are an SEO and analytics expert. Research keywords, analyze competitors, optimize content for search engines, and provide data-driven recommendations. Coordinate with the team via the message board.",
        capabilities: { allow: ['file_read', 'web_access'] },
    },
    {
        name: "Design Director",
        prompt: "You are a creative director focused on visual design. Create design briefs, review visual assets, ensure brand consistency, and provide art direction. Coordinate with the team via the message board.",
        capabilities: { allow: ['file_read', 'file_write', 'web_access'] },
    },
    {
        name: "Research Analyst",
        prompt: "You are a thorough research analyst. Find information, summarize documents, compare options, and provide well-sourced answers. Coordinate with the team via the message board.",
        capabilities: { allow: ['file_read', 'web_access'] },
    },
    {
        name: "Writer & Editor",
        prompt: "You are a skilled writer and editor. Draft emails, documents, presentations, and reports. Polish and proofread content for clarity and professionalism. Coordinate with the team via the message board.",
        capabilities: { allow: ['file_read', 'file_write'] },
    },
    {
        name: "Scheduler & Planner",
        prompt: "You are an organizational expert. Help plan projects, create timelines, track deadlines, and organize information into actionable plans. Coordinate with the team via the message board.",
        capabilities: { allow: ['file_read', 'file_write'] },
    },
];

// ── Permissions Editor ─────────────────────────────────────────────────

const CAPABILITY_LABELS = {
    file_read: 'Read Files',
    file_write: 'Write Files',
    shell: 'Shell',
    web_access: 'Web Access',
    git_write: 'Git Write',
    agent_spawn: 'Spawn Agents',
    notebook: 'Notebooks',
};

function _togglePermissions(containerId) {
    const el = document.getElementById(containerId);
    if (!el) return;
    const isHidden = el.style.display === 'none';
    el.style.display = isHidden ? '' : 'none';
    const chevron = el.previousElementSibling?.querySelector('.permissions-chevron');
    if (chevron) chevron.style.transform = isHidden ? 'rotate(90deg)' : '';
}
window._togglePermissions = _togglePermissions;

function _renderPermChips(containerId, type, capabilities) {
    const container = document.getElementById(containerId);
    if (!container) return;
    const caps = capabilities?.[type] || [];
    // Separate base capabilities from shell patterns
    const baseCaps = caps.filter(c => !c.startsWith('shell:'));
    const shellPatterns = caps.filter(c => c.startsWith('shell:')).map(c => c.slice(6));

    let html = '';
    for (const cap of ALL_CAPABILITIES) {
        const isActive = baseCaps.includes(cap);
        const cls = `perm-chip${isActive ? ' active' : ''}`;
        html += `<button class="${cls}" data-cap="${cap}" data-type="${type}" onclick="window._togglePermChip(this)">${CAPABILITY_LABELS[cap] || cap}</button>`;
    }
    container.innerHTML = html;

    // Show shell patterns input if shell is in allow
    const shellSection = container.closest('.permissions-editor')?.querySelector('.perms-shell-patterns');
    const shellInput = container.closest('.permissions-editor')?.querySelector('.perms-shell-input');
    if (shellSection && shellInput && type === 'allow') {
        const hasShell = baseCaps.includes('shell');
        const hasShellPatterns = shellPatterns.length > 0;
        shellSection.style.display = (hasShell || hasShellPatterns) ? '' : 'none';
        shellInput.value = shellPatterns.join(', ');
    }
}

function _togglePermChip(btn) {
    btn.classList.toggle('active');
    // Toggle shell patterns visibility
    const cap = btn.dataset.cap;
    const type = btn.dataset.type;
    if (cap === 'shell' && type === 'allow') {
        const shellSection = btn.closest('.permissions-editor')?.querySelector('.perms-shell-patterns');
        if (shellSection) {
            shellSection.style.display = btn.classList.contains('active') ? '' : 'none';
        }
    }
}
window._togglePermChip = _togglePermChip;

function _getPermissions(editorId) {
    const editor = document.getElementById(editorId);
    if (!editor || editor.style.display === 'none') return null;

    const result = {};
    for (const type of ['allow', 'deny']) {
        const chips = editor.querySelectorAll(`[data-type="${type}"].active`);
        if (chips.length > 0) {
            result[type] = [...chips].map(c => c.dataset.cap);
        }
    }

    // Add shell patterns to allow list
    const shellInput = editor.querySelector('.perms-shell-input');
    if (shellInput && shellInput.value.trim()) {
        if (!result.allow) result.allow = [];
        const patterns = shellInput.value.split(',').map(p => p.trim()).filter(Boolean);
        for (const p of patterns) {
            result.allow.push(`shell:${p}`);
        }
    }

    return Object.keys(result).length > 0 ? result : null;
}

function _setPermissions(editorId, capabilities) {
    const editor = document.getElementById(editorId);
    if (!editor) return;
    _renderPermChips(editorId + '-allow', 'allow', capabilities || {});
    _renderPermChips(editorId + '-deny', 'deny', capabilities || {});
}

// Default team: first 3 presets
const DEFAULT_TEAM_PRESETS = AGENT_PRESETS.slice(0, 3);

async function _initTeamForm() {
    // Reset form
    document.getElementById("team-board-name").value = "";
    document.getElementById("team-board-server").value = "";
    _existingTeamBoardNames = new Set();
    _validateTeamName();
    const tfEl = document.getElementById("team-flags");
    if (tfEl) tfEl.value = _getPermFlag('team-agent-type');
    const tapEl = document.getElementById("team-auto-permissions");
    if (tapEl) tapEl.checked = true;
    _teamAgentCounter = 0;
    const list = document.getElementById("team-agents-list");
    list.innerHTML = "";

    // Pre-fill working dir from settings
    const s = state.settings || {};
    const dirInput = document.getElementById("launch-team-dir");
    if (s.default_working_dir && dirInput) dirInput.value = s.default_working_dir;
    const typeSelect = document.getElementById("team-agent-type");
    if (s.default_agent_type && typeSelect) typeSelect.value = s.default_agent_type;

    // Fetch existing boards for uniqueness validation
    try {
        const resp = await fetch("/api/board/projects");
        const projects = await resp.json();
        for (const p of projects) {
            if (p.project) _existingTeamBoardNames.add(String(p.project).trim().toLowerCase());
        }
    } catch (_) {}

    // Render team template selector
    _renderTeamTemplateSelector();

    // Add three default agents
    for (const preset of DEFAULT_TEAM_PRESETS) {
        _addTeamAgent(preset.name, preset.prompt);
    }

    document.getElementById("team-board-name").focus();
    _validateTeamName();
}

const BUILTIN_TEAM_TEMPLATES = [
    {
        name: "Coding Team",
        builtin: true,
        agents: [
            { name: "Orchestrator", prompt: "You are the orchestrator. Break down the task, assign work to the team via the message board, and track progress. Do not write code yourself — delegate to the other agents. Discuss your plan with the operator before posting assignments." },
            { name: "Lead Developer", prompt: "You are the lead developer. Implement features, write code, and coordinate with the team via the message board. Wait for instructions from the Orchestrator before starting." },
            { name: "QA Engineer", prompt: "You are a QA engineer. Review code, write tests, and verify the work of other agents. Wait for instructions from the Orchestrator before starting." },
            { name: "Frontend Dev", prompt: "You are a frontend developer. Build UI components, style pages, and ensure a great user experience. Wait for instructions from the Orchestrator before starting." },
            { name: "Security Reviewer", prompt: "You are a security expert. Review code for vulnerabilities (OWASP top 10, injection, auth issues, data exposure), audit dependencies, and recommend security best practices. Wait for instructions from the Orchestrator before starting." },
        ],
        flags: "",
    },
    {
        name: "Marketing Team",
        builtin: true,
        agents: [
            { name: "Orchestrator", prompt: "You are the orchestrator. Coordinate the marketing team, break down campaigns, assign tasks via the message board, and track progress. Do not write content yourself — delegate to the other agents." },
            { name: "Content Writer", prompt: "You are an expert content writer. Write blog posts, social media copy, email campaigns, and landing page content. Focus on clear, engaging writing that drives action. Coordinate with the team via the message board." },
            { name: "SEO Strategist", prompt: "You are an SEO and analytics expert. Research keywords, analyze competitors, optimize content for search engines, and provide data-driven recommendations. Coordinate with the team via the message board." },
            { name: "Design Director", prompt: "You are a creative director focused on visual design. Create design briefs, review visual assets, ensure brand consistency, and provide art direction. Coordinate with the team via the message board." },
        ],
        flags: "",
    },
    {
        name: "Personal Assistant",
        builtin: true,
        agents: [
            { name: "Orchestrator", prompt: "You are the orchestrator. Coordinate the assistant team, prioritize tasks, delegate work via the message board, and ensure nothing falls through the cracks." },
            { name: "Research Analyst", prompt: "You are a thorough research analyst. Find information, summarize documents, compare options, and provide well-sourced answers. Coordinate with the team via the message board." },
            { name: "Writer & Editor", prompt: "You are a skilled writer and editor. Draft emails, documents, presentations, and reports. Polish and proofread content for clarity and professionalism. Coordinate with the team via the message board." },
            { name: "Scheduler & Planner", prompt: "You are an organizational expert. Help plan projects, create timelines, track deadlines, and organize information into actionable plans. Coordinate with the team via the message board." },
        ],
        flags: "",
    },
];

function _renderTeamTemplateSelector() {
    const select = document.getElementById("team-template-selector");
    if (!select) return;
    const saved = _getSavedTeamTemplates();
    const all = [...BUILTIN_TEAM_TEMPLATES, ...saved];

    let html = '<option value="">Select a template...</option>';
    for (const t of all) {
        const suffix = t.builtin ? '' : ' (saved)';
        html += `<option value="${escapeAttr(t.name)}">${escapeHtml(t.name)} (${t.agents.length} agents)${suffix}</option>`;
    }
    select.innerHTML = html;
}

window._loadTeamTemplateFromSelect = function(name) {
    if (!name) return;
    _loadTeamTemplate(name);
    // Reset dropdown to placeholder after loading
    const select = document.getElementById("team-template-selector");
    if (select) select.value = "";
};

/** Launch a team preset from the welcome screen — opens the team modal pre-filled. */
window._launchTeamPreset = function(templateName) {
    const modal = document.getElementById('quick-launch-modal');
    if (!modal) return;
    document.getElementById('quick-launch-template').value = templateName;
    document.getElementById('quick-launch-title').textContent = `Launch ${templateName}`;
    document.getElementById('quick-launch-board-name').value = '';
    document.getElementById('quick-launch-board-name').placeholder = templateName.toLowerCase().replace(/\s+/g, '-');
    modal.style.display = '';
};

window._quickLaunchTeam = async function() {
    const templateName = document.getElementById('quick-launch-template').value;
    const boardName = document.getElementById('quick-launch-board-name').value.trim() ||
        document.getElementById('quick-launch-board-name').placeholder;
    const workDir = document.getElementById('quick-launch-workdir').value.trim();

    if (!workDir) { showToast('Working directory is required', 'error'); return; }

    const all = [...BUILTIN_TEAM_TEMPLATES, ..._getSavedTeamTemplates()];
    const tmpl = all.find(t => t.name === templateName);
    if (!tmpl) { showToast('Template not found', 'error'); return; }

    // Disable launch button to prevent double-clicks
    const launchBtn = document.querySelector('#quick-launch-modal .btn-primary');
    if (launchBtn) { launchBtn.disabled = true; launchBtn.textContent = 'Launching...'; }

    const agentType = 'claude';
    const permFlag = PERM_FLAGS[agentType] || PERM_FLAGS.claude;

    try {
        const resp = await fetch('/api/sessions/launch-team', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                board_name: boardName,
                working_dir: workDir,
                agent_type: agentType,
                flags: [permFlag],
                agents: tmpl.agents.map(a => ({ name: a.name, prompt: a.prompt, capabilities: a.capabilities })),
            }),
        });
        if (resp.status === 403) {
            const err = await resp.json();
            _showDemoLimitModal(err.error || 'Demo limit reached');
            return;
        }
        const data = await resp.json();
        if (data.error) { showToast(data.error, 'error'); return; }
        document.getElementById('quick-launch-modal').style.display = 'none';
        showToast(`Launched ${templateName} on ${boardName}`);
    } catch (e) {
        showToast('Launch failed: ' + e.message, 'error');
    } finally {
        if (launchBtn) { launchBtn.disabled = false; launchBtn.textContent = 'Launch'; }
    }
};

function _loadTeamTemplate(name) {
    const all = [...BUILTIN_TEAM_TEMPLATES, ..._getSavedTeamTemplates()];
    const tmpl = all.find(t => t.name === name);
    if (!tmpl) { console.warn('[coral] Template not found:', name); return; }

    // Clear current agents and load from template
    _teamAgentCounter = 0;
    document.getElementById("team-agents-list").innerHTML = "";

    for (const agent of tmpl.agents) {
        const agentName = agent.name || agent.role || '';
        const agentPrompt = agent.prompt || agent.description || '';
        _addTeamAgent(agentName, agentPrompt, agent.capabilities);
    }
    if (tmpl.flags) {
        const tfEl = document.getElementById("team-flags");
        if (tfEl) { tfEl.value = tmpl.flags; _syncFlagButtons("team-flags"); }
    }
    showToast(`Loaded template "${name}"`);
}
window._loadTeamTemplate = _loadTeamTemplate;

async function _saveTeamTemplate() {
    // Collect current agents
    const rows = document.querySelectorAll("#team-agents-list .team-agent-row");
    if (rows.length === 0) { showToast("Add agents before saving", "error"); return; }

    const agents = [];
    for (const row of rows) {
        const name = row.querySelector(".team-agent-name").value.trim();
        const prompt = row.querySelector(".team-agent-prompt").value.trim();
        if (name) {
            const entry = { name, prompt };
            const permsEditor = row.querySelector('.permissions-editor');
            if (permsEditor) {
                const caps = _getPermissions(permsEditor.id);
                if (caps) entry.capabilities = caps;
            }
            agents.push(entry);
        }
    }
    if (agents.length === 0) { showToast("At least one agent needs a name", "error"); return; }

    const templateName = prompt("Template name:");
    if (!templateName) return;

    const flags = document.getElementById("team-flags")?.value.trim() || '';
    const templates = _getSavedTeamTemplates();
    const idx = templates.findIndex(t => t.name === templateName);
    const entry = { name: templateName, agents, flags };
    if (idx >= 0) templates[idx] = entry; else templates.push(entry);
    await _setSavedTeamTemplates(templates);
    showToast(`Saved template "${templateName}"`);
    _renderTeamTemplateSelector();
}
window._saveTeamTemplate = _saveTeamTemplate;

async function _deleteTeamTemplate(name) {
    const templates = _getSavedTeamTemplates().filter(t => t.name !== name);
    await _setSavedTeamTemplates(templates);
    showToast(`Deleted template "${name}"`);
    _renderTeamTemplateSelector();
}
window._deleteTeamTemplate = _deleteTeamTemplate;

function _truncatePrompt(text, maxLen) {
    if (!text || text.length <= maxLen) return text || "";
    return text.substring(0, maxLen) + "\u2026";
}

function _addTeamAgent(defaultName, defaultPrompt, defaultCapabilities) {
    _teamAgentCounter++;
    const idx = _teamAgentCounter;
    const list = document.getElementById("team-agents-list");
    const row = document.createElement("div");
    row.className = "team-agent-row";
    row.draggable = true;
    row.dataset.idx = idx;

    const hasContent = !!(defaultName || defaultPrompt);
    const collapsed = hasContent;

    row.innerHTML = `
        <div class="team-agent-card ${collapsed ? '' : 'editing'}">
            <div class="team-agent-summary" onclick="this.closest('.team-agent-card').classList.toggle('editing')">
                <div class="team-agent-summary-left">
                    <span class="team-agent-role-name">${escapeHtml(defaultName || 'New Agent')}</span>
                    <span class="team-agent-prompt-preview">${escapeHtml(_truncatePrompt(defaultPrompt, 200))}</span>
                </div>
                <div class="team-agent-summary-actions">
                    <button class="team-agent-reorder-btn" title="Move up" onclick="event.stopPropagation(); window._moveTeamAgent(this, -1)">
                        <svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 3v10M4 7l4-4 4 4"/></svg>
                    </button>
                    <button class="team-agent-reorder-btn" title="Move down" onclick="event.stopPropagation(); window._moveTeamAgent(this, 1)">
                        <svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 13V3M4 9l4 4 4-4"/></svg>
                    </button>
                    <button class="team-agent-edit-btn" title="Edit" onclick="event.stopPropagation(); this.closest('.team-agent-card').classList.add('editing')">
                        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
                    </button>
                    <button class="team-agent-remove" onclick="event.stopPropagation(); this.closest('.team-agent-row').remove()" title="Remove">&times;</button>
                </div>
            </div>
            <div class="team-agent-form">
                <div style="display:flex;gap:8px;align-items:end">
                    <label style="flex:1">Name / Role:
                        <input type="text" class="team-agent-name" placeholder="e.g. QA Engineer, Frontend Dev" value="${escapeAttr(defaultName || '')}"
                            oninput="const card=this.closest('.team-agent-row'); card.querySelector('.team-agent-role-name').textContent=this.value||'New Agent'">
                    </label>
                    <div style="flex-shrink:0;text-align:center">
                        <div style="font-size:11px;color:var(--text-secondary);margin-bottom:2px">Icon</div>
                        <button type="button" class="team-agent-icon-btn" onclick="openTeamIconPicker(this)" style="width:36px;height:36px;font-size:18px;border-radius:6px;border:1px solid var(--border);background:var(--bg-tertiary);cursor:pointer" data-icon="">🤖</button>
                        <input type="hidden" class="team-agent-icon" value="">
                    </div>
                </div>
                <label>Behavior Prompt:
                    <textarea class="team-agent-prompt" rows="3" placeholder="Describe this agent's role and behavior..."
                        oninput="const card=this.closest('.team-agent-row'); card.querySelector('.team-agent-prompt-preview').textContent=this.value.substring(0,200)+(this.value.length>200?'\u2026':'')">${escapeHtml(defaultPrompt || '')}</textarea>
                </label>
                <div class="permissions-section">
                    <button type="button" class="permissions-toggle-btn" onclick="window._togglePermissions('team-perms-${idx}')">
                        <svg class="permissions-chevron" width="10" height="10" viewBox="0 0 16 16" fill="currentColor"><path d="M6 4l4 4-4 4z"/></svg>
                        Permissions <span style="color:var(--text-muted);font-weight:normal;font-size:11px">(optional)</span>
                    </button>
                    <div id="team-perms-${idx}" class="permissions-editor" style="display:none">
                        <div class="perms-group">
                            <label class="perms-label">Allow:</label>
                            <div id="team-perms-${idx}-allow" class="perms-chips"></div>
                        </div>
                        <div class="perms-group">
                            <label class="perms-label">Deny:</label>
                            <div id="team-perms-${idx}-deny" class="perms-chips"></div>
                        </div>
                        <div class="perms-shell-patterns" style="display:none">
                            <label class="perms-label">Shell patterns <span style="color:var(--text-muted);font-weight:normal">(comma-separated)</span>:</label>
                            <input type="text" class="perms-shell-input team-perms-shell" placeholder="e.g. git *, npm *, npx *">
                        </div>
                    </div>
                </div>
                <div style="display:flex;gap:6px">
                    <button class="btn btn-small" onclick="browseAgentTemplates(this)" title="Browse community agent templates from aitmpl.com">Browse Templates</button>
                    <button class="btn btn-small team-agent-done-btn" onclick="this.closest('.team-agent-card').classList.remove('editing')">Done</button>
                </div>
            </div>
        </div>
    `;
    list.appendChild(row);

    // Initialize permissions chips from preset defaults
    const caps = defaultCapabilities || _findPersona(defaultName)?.capabilities || null;
    _setPermissions(`team-perms-${idx}`, caps);

    // Drag-and-drop reorder handlers
    row.addEventListener('dragstart', (e) => {
        e.dataTransfer.effectAllowed = 'move';
        e.dataTransfer.setData('text/plain', idx.toString());
        row.classList.add('dragging');
    });
    row.addEventListener('dragend', () => {
        row.classList.remove('dragging');
        list.querySelectorAll('.team-agent-row.drag-over').forEach(el => el.classList.remove('drag-over'));
    });
    row.addEventListener('dragover', (e) => {
        e.preventDefault();
        e.dataTransfer.dropEffect = 'move';
        row.classList.add('drag-over');
    });
    row.addEventListener('dragleave', () => {
        row.classList.remove('drag-over');
    });
    row.addEventListener('drop', (e) => {
        e.preventDefault();
        row.classList.remove('drag-over');
        const draggedIdx = e.dataTransfer.getData('text/plain');
        const draggedRow = list.querySelector(`.team-agent-row[data-idx="${draggedIdx}"]`);
        if (draggedRow && draggedRow !== row) {
            const rows = [...list.children];
            const fromPos = rows.indexOf(draggedRow);
            const toPos = rows.indexOf(row);
            if (fromPos < toPos) {
                list.insertBefore(draggedRow, row.nextSibling);
            } else {
                list.insertBefore(draggedRow, row);
            }
        }
    });

    // Focus the name input if it's a new empty agent
    if (!hasContent) {
        const nameInput = row.querySelector('.team-agent-name');
        if (nameInput) setTimeout(() => nameInput.focus(), 50);
    }
}

// Move a team agent card up or down
function _moveTeamAgent(btn, direction) {
    const row = btn.closest('.team-agent-row');
    if (!row) return;
    const list = row.parentElement;
    if (direction === -1 && row.previousElementSibling) {
        list.insertBefore(row, row.previousElementSibling);
    } else if (direction === 1 && row.nextElementSibling) {
        list.insertBefore(row.nextElementSibling, row);
    }
}
window._moveTeamAgent = _moveTeamAgent;

function _showAddAgentPicker() {
    const picker = document.getElementById("team-agent-picker");
    if (!picker) return;

    // Get names of agents already added
    const existingNames = new Set();
    document.querySelectorAll("#team-agents-list .team-agent-name").forEach(input => {
        const n = input.value.trim().toLowerCase();
        if (n) existingNames.add(n);
    });

    // Build picker items: presets not already in the list
    const available = AGENT_PRESETS.filter(p => !existingNames.has(p.name.toLowerCase()));
    const savedAvailable = _getSavedPersonas().filter(p => !existingNames.has(p.name.toLowerCase()));

    let html = '';
    for (const preset of available) {
        html += `<button class="agent-picker-item" onclick="window._addPresetAgent('${escapeAttr(preset.name)}')">${escapeHtml(preset.name)}</button>`;
    }
    if (savedAvailable.length > 0) {
        if (available.length > 0) html += '<div class="agent-picker-divider"></div>';
        for (const persona of savedAvailable) {
            html += `<button class="agent-picker-item agent-preset-saved" onclick="window._addPresetAgent('${escapeAttr(persona.name)}')">${escapeHtml(persona.name)}</button>`;
        }
    }
    if (available.length > 0 || savedAvailable.length > 0) {
        html += '<div class="agent-picker-divider"></div>';
    }
    html += `<button class="agent-picker-item agent-picker-custom" onclick="window._addTeamAgent()">+ Create Custom</button>`;
    html += '<div class="agent-picker-divider"></div>';
    html += `<button class="agent-picker-item agent-picker-browse" onclick="browseAgentTemplatesNew(); document.getElementById('team-agent-picker').style.display='none'">Browse Community Templates</button>`;

    picker.innerHTML = html;
    picker.style.display = "";

    // Close picker on outside click
    const closeHandler = (e) => {
        if (!picker.contains(e.target) && !e.target.closest('.team-add-agent-btn')) {
            picker.style.display = "none";
            document.removeEventListener("click", closeHandler);
        }
    };
    setTimeout(() => document.addEventListener("click", closeHandler), 0);
}
window._showAddAgentPicker = _showAddAgentPicker;

function _addPresetAgent(name) {
    const persona = _findPersona(name);
    if (persona) {
        _addTeamAgent(persona.name, persona.prompt);
    }
    const picker = document.getElementById("team-agent-picker");
    if (picker) picker.style.display = "none";
}
window._addPresetAgent = _addPresetAgent;
window._addTeamAgent = () => {
    _addTeamAgent("", "");
    const picker = document.getElementById("team-agent-picker");
    if (picker) picker.style.display = "none";
};

function _validateTeamName() {
    const input = document.getElementById("team-board-name");
    const hint = document.getElementById("team-board-name-hint");
    if (!input || !hint) return true;
    const value = input.value.trim();
    if (!value) {
        hint.textContent = "This will also be the message board name.";
        hint.classList.remove("error");
        return false;
    }
    if (_existingTeamBoardNames.has(value.toLowerCase())) {
        hint.textContent = "That team name is already in use. Pick a unique name.";
        hint.classList.add("error");
        return false;
    }
    hint.textContent = "Looks good. This will also be the message board name.";
    hint.classList.remove("error");
    return true;
}
window._validateTeamName = _validateTeamName;

async function launchTeam() {
    const boardName = document.getElementById("team-board-name").value.trim();
    if (!boardName) {
        showToast("Team name is required", true);
        return;
    }
    if (!_validateTeamName()) {
        showToast("Team name must be unique", true);
        return;
    }

    const workingDir = document.getElementById("launch-team-dir").value.trim();
    if (!workingDir) {
        showToast("Working directory is required", true);
        return;
    }

    const agentType = document.getElementById("team-agent-type").value;
    const autoPerms = document.getElementById("team-auto-permissions")?.checked;

    // Build flags from checkbox
    const flags = [];
    if (autoPerms) {
        const permFlag = PERM_FLAGS[agentType] || PERM_FLAGS.claude;
        flags.push(permFlag);
    }

    // Collect agent definitions
    const rows = document.querySelectorAll("#team-agents-list .team-agent-row");
    if (rows.length === 0) {
        showToast("Add at least one agent", true);
        return;
    }

    const agents = [];
    for (const row of rows) {
        const name = row.querySelector(".team-agent-name").value.trim();
        const prompt = row.querySelector(".team-agent-prompt").value.trim();
        const icon = row.querySelector(".team-agent-icon")?.value.trim() || "";
        if (!name) {
            showToast("Each agent needs a name", true);
            return;
        }
        const agent = { name, prompt };
        if (icon) agent.icon = icon;
        // Collect per-agent permissions
        const permsEditor = row.querySelector('.permissions-editor');
        if (permsEditor) {
            const caps = _getPermissions(permsEditor.id);
            if (caps) agent.capabilities = caps;
        }
        agents.push(agent);
    }

    const boardServer = document.getElementById("team-board-server").value.trim();
    const payload = {
        board_name: boardName,
        working_dir: workingDir,
        agent_type: agentType,
        flags,
        agents,
    };
    if (boardServer) payload.board_server = boardServer;

    // Disable launch button to prevent double-clicks
    const launchBtn = document.querySelector('#launch-step-team .btn-primary');
    if (launchBtn) { launchBtn.disabled = true; launchBtn.textContent = 'Launching...'; }

    try {
        const resp = await fetch("/api/sessions/launch-team", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(payload),
        });
        if (resp.status === 403) {
            const err = await resp.json();
            _showDemoLimitModal(err.error || 'Demo limit reached');
            return;
        }
        const result = await resp.json();
        if (result.error) {
            showToast(result.error, true);
        } else {
            // Check for per-agent errors in the launched array
            const launched = result.launched || [];
            const failed = launched.filter(a => a.error);
            if (failed.length > 0) {
                const successCount = launched.length - failed.length;
                const failList = failed.map(a => `<li><strong>${escapeHtml(a.name)}</strong>: ${escapeHtml(a.error)}</li>`).join('');
                const modal = document.createElement('div');
                modal.className = 'modal';
                modal.style.display = 'flex';
                modal.innerHTML = `
                    <div class="modal-content" style="width:500px">
                        <h3>Team Launch — ${failed.length} agent${failed.length > 1 ? 's' : ''} failed</h3>
                        <p style="color:var(--text-secondary);font-size:13px;margin:8px 0">${successCount} launched successfully, ${failed.length} failed:</p>
                        <ul style="color:var(--error);font-size:13px;line-height:1.6;margin:8px 0 16px;padding-left:20px">${failList}</ul>
                        <div class="modal-actions">
                            <button class="btn" data-action="settings">Set CLI Paths</button>
                            <button class="btn btn-primary" data-action="close">OK</button>
                        </div>
                    </div>
                `;
                document.body.appendChild(modal);
                modal.addEventListener('click', (e) => {
                    if (e.target.dataset?.action === 'close' || e.target === modal) modal.remove();
                    if (e.target.dataset?.action === 'settings') { modal.remove(); showSettingsModal(); }
                });
            }
            if (launched.length - failed.length > 0) {
                showToast(`Launched team: ${launched.length - failed.length} agents on "${boardName}"`);
            }
            hideLaunchModal();
            setTimeout(loadLiveSessions, 2000);
            setTimeout(loadBoardProjects, 4000);
        }
    } catch (e) {
        showToast("Failed to launch team", true);
    } finally {
        if (launchBtn) { launchBtn.disabled = false; launchBtn.textContent = 'Launch Team'; }
    }
}
window.launchTeam = launchTeam;

export async function showInfoModal() {
    if (!state.currentSession || state.currentSession.type !== "live") {
        showToast("No live session selected", true);
        return;
    }

    const name = state.currentSession.name;
    const agentType = state.currentSession.agent_type || "";

    try {
        const params = new URLSearchParams();
        if (agentType) params.set("agent_type", agentType);
        const sid = state.currentSession.session_id;
        if (sid) params.set("session_id", sid);
        const qs = params.toString() ? `?${params}` : "";
        const resp = await fetch(`/api/sessions/live/${encodeURIComponent(name)}/info${qs}`);
        const info = await resp.json();

        if (info.error) {
            showToast(info.error, true);
            return;
        }

        document.getElementById("info-agent-name").textContent = info.agent_name || "";
        document.getElementById("info-agent-type").textContent = info.agent_type || "";
        document.getElementById("info-tmux-session").textContent = info.tmux_session_name || "";
        document.getElementById("info-tmux-command").textContent = info.tmux_command || "";
        document.getElementById("info-working-dir").textContent = info.working_directory || "";
        document.getElementById("info-log-path").textContent = info.log_path || "";
        document.getElementById("info-pane-title").textContent = info.pane_title || "";
        document.getElementById("info-git-branch").textContent = info.git_branch || "—";
        const commitHash = info.git_commit_hash ? info.git_commit_hash.substring(0, 8) : "";
        const commitSubject = info.git_commit_subject || "";
        document.getElementById("info-git-commit").textContent = commitHash ? `${commitHash} ${commitSubject}` : "—";

        // Prompt
        const promptLabel = document.getElementById("info-prompt-label");
        const promptVal = document.getElementById("info-prompt");
        if (info.prompt) {
            promptLabel.style.display = "";
            promptVal.style.display = "";
            promptVal.textContent = info.prompt;
        } else {
            promptLabel.style.display = "none";
            promptVal.style.display = "none";
        }

        // Message Board (clickable link)
        const boardLabel = document.getElementById("info-board-label");
        const boardVal = document.getElementById("info-board");
        if (info.board_name) {
            boardLabel.style.display = "";
            boardVal.style.display = "";
            const boardLink = document.createElement("a");
            boardLink.href = "#";
            boardLink.textContent = info.board_name;
            boardLink.style.cssText = "color:var(--accent);cursor:pointer;text-decoration:underline";
            boardLink.onclick = (e) => {
                e.preventDefault();
                hideInfoModal();
                if (window.selectBoardProject) window.selectBoardProject(info.board_name);
            };
            boardVal.innerHTML = "";
            boardVal.appendChild(boardLink);
        } else {
            boardLabel.style.display = "none";
            boardVal.style.display = "none";
        }

        document.getElementById("info-modal").style.display = "flex";
    } catch (e) {
        showToast("Failed to load session info", true);
        console.error("showInfoModal exception:", e);
    }
}

export function hideInfoModal() {
    document.getElementById("info-modal").style.display = "none";
}

export function copyInfoCommand() {
    const text = document.getElementById("info-tmux-command").textContent;
    navigator.clipboard.writeText(text).then(() => {
        showToast("Copied to clipboard");
    }).catch(() => {
        showToast("Failed to copy", true);
    });
}

// ── Resume Modal ───────────────────────────────────────────────────────────

export function showResumeModal() {
    const list = document.getElementById("resume-session-list");
    list.innerHTML = "";

    if (!state.liveSessions || state.liveSessions.length === 0) {
        list.innerHTML = '<p style="color:var(--text-muted);font-size:13px;text-align:center;padding:16px 0">No live agents available</p>';
        document.getElementById("resume-modal").style.display = "flex";
        return;
    }

    for (const agent of state.liveSessions) {
        const item = document.createElement("div");
        item.className = "resume-session-item";
        const typeBadge = (agent.agent_type || "claude").toLowerCase();
        const goal = agent.summary ? escapeHtml(agent.summary) : '<span style="color:var(--text-muted)">No goal set</span>';
        item.innerHTML = `
            <div class="resume-item-header">
                <span class="resume-item-name">${escapeHtml(agent.name)}</span>
                <span class="badge ${typeBadge}">${typeBadge}</span>
            </div>
            <div class="resume-item-goal">${goal}</div>
        `;
        item.addEventListener("click", () => resumeIntoSession(agent.name, agent.agent_type, agent.session_id));
        list.appendChild(item);
    }

    document.getElementById("resume-modal").style.display = "flex";
}

export function hideResumeModal() {
    document.getElementById("resume-modal").style.display = "none";
}

export async function resumeIntoSession(agentName, agentType, currentSessionId) {
    if (!state.currentSession || state.currentSession.type !== "history") {
        showToast("No history session selected", true);
        return;
    }

    const sessionId = state.currentSession.name;
    const displayType = (agentType || "claude").toLowerCase();

    window.showConfirmModal('Resume Session', `This will kill the current session in "${agentName}" and resume session ${sessionId.substring(0, 8)}... in its place. Continue?`, async () => {
        hideResumeModal();
        try {
            const resp = await fetch(`/api/sessions/live/${encodeURIComponent(agentName)}/resume`, {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ session_id: sessionId, agent_type: agentType, current_session_id: currentSessionId }),
            });
            const result = await resp.json();
            if (result.error) {
                showToast(result.error, true);
            } else {
                showToast(`Resumed session in ${agentName}`);
                if (window.selectLiveSession) {
                    window.selectLiveSession(agentName, displayType, sessionId);
                }
            }
        } catch (e) {
            showToast("Failed to resume session", true);
            console.error("resumeIntoSession error:", e);
        }
    });
}

// ── Settings Modal ────────────────────────────────────────────────────────

export async function loadSettings() {
    try {
        const resp = await fetch("/api/settings");
        const data = await resp.json();
        const s = data.settings || {};
        // Boolean settings are stored as strings in SQLite — coerce them
        if (typeof s.fit_pane_width === "string") {
            s.fit_pane_width = s.fit_pane_width === "True";
        }
        // Default fit_pane_width to true if not yet set
        if (s.fit_pane_width === undefined) {
            s.fit_pane_width = true;
        }
        if (typeof s.notify_needs_input === "string") {
            s.notify_needs_input = s.notify_needs_input === "True";
        }
        if (s.notify_needs_input === undefined) {
            s.notify_needs_input = true;
        }
        // Coerce show_scrollbars
        if (typeof s.show_scrollbars === "string") {
            s.show_scrollbars = s.show_scrollbars === "True";
        }
        if (s.show_scrollbars === undefined) {
            s.show_scrollbars = true;
        }
        // Coerce refresh_files_on_switch (default: disabled)
        if (typeof s.refresh_files_on_switch === "string") {
            s.refresh_files_on_switch = s.refresh_files_on_switch === "True" || s.refresh_files_on_switch === "true";
        }
        if (s.refresh_files_on_switch === undefined) {
            s.refresh_files_on_switch = false;
        }
        state.settings = s;

        // Apply scrollbar visibility
        document.body.classList.toggle('no-scrollbars', !s.show_scrollbars);

        // Apply theme from settings (default to GhostV3 if no theme configured)
        const themeName = s.custom_theme || "GhostV3";
        await applyCustomThemeByName(themeName);
        if (!s.custom_theme) {
            state.settings.custom_theme = "GhostV3";
        }
    } catch (e) {
        console.error("Failed to load settings:", e);
    }
}

// ── Theme ────────────────────────────────────────────────────────────────

/** Resolve "system" to the actual dark/light value */
function resolveTheme(theme) {
    if (theme === "system") {
        return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
    }
    return theme;
}

/** Apply a theme to the document */
export function applyTheme(theme) {
    const resolved = resolveTheme(theme);
    document.documentElement.setAttribute("data-theme", resolved);

    // Store the *preference* (dark/light/system) so other pages can read it
    try { localStorage.setItem("coral-theme", theme); } catch (_) {}

    // Update xterm.js terminal colors to match
    updateTerminalTheme();
}

/** Remove all custom CSS variables from inline styles (reset before switching themes) */
function clearCustomThemeVars() {
    const style = document.documentElement.style;
    // Remove any --* properties that were set inline
    const toRemove = [];
    for (let i = 0; i < style.length; i++) {
        const prop = style[i];
        if (prop.startsWith("--")) toRemove.push(prop);
    }
    for (const prop of toRemove) style.removeProperty(prop);
}

/** Load a custom theme by name: apply its base theme + overlay its CSS variables */
async function applyCustomThemeByName(name) {
    try {
        const resp = await fetch(`/api/themes/${encodeURIComponent(name)}`);
        const data = await resp.json();
        if (data.error || !data.theme?.variables) return;

        // Apply the base theme (dark/light)
        const base = data.theme.base || "dark";
        applyTheme(base);

        // Overlay custom variables
        for (const [cssVar, value] of Object.entries(data.theme.variables)) {
            if (value) document.documentElement.style.setProperty(cssVar, value);
        }

        // Refresh terminal colors
        updateTerminalTheme();
    } catch {
        // Fall back to default
    }
}

// Listen for OS theme changes when in "system" mode
window.matchMedia("(prefers-color-scheme: light)").addEventListener("change", () => {
    const pref = (state.settings && state.settings.theme) || "dark";
    if (pref === "system") applyTheme("system");
});

export async function showSettingsModal() {
    const s = state.settings || {};

    // Theme — populate with built-in options + saved custom themes
    const themeSelect = document.getElementById("settings-theme");
    if (themeSelect) {
        // Fetch saved custom themes
        let customThemes = [];
        try {
            const resp = await fetch("/api/themes");
            const data = await resp.json();
            customThemes = data.themes || [];
        } catch (_) {}

        // Build options: built-in first, then custom
        let options = `<option value="dark">Dark</option>
            <option value="light">Light</option>
            <option value="system">System</option>`;
        if (customThemes.length > 0) {
            options += `<option disabled>──────────</option>`;
            for (const t of customThemes) {
                const name = escapeAttr(t.name);
                const label = escapeHtml(t.name);
                options += `<option value="custom:${name}">${label}</option>`;
            }
        }
        themeSelect.innerHTML = options;

        // Set current value
        const currentTheme = s.custom_theme ? `custom:${s.custom_theme}` : (s.theme || "dark");
        themeSelect.value = currentTheme;
    }

    // Default Render Engine
    const currentEngine = s.default_renderer || "block-group";
    const options = getEngineNames()
        .map(name => `<option value="${escapeAttr(name)}"${name === currentEngine ? " selected" : ""}>${escapeHtml(name)}</option>`)
        .join("");
    document.getElementById("settings-renderer-select").innerHTML = options;

    // Default Agent Type
    const currentAgentType = s.default_agent_type || "claude";
    const agentTypeSelect = document.getElementById("settings-agent-type");
    if (agentTypeSelect) agentTypeSelect.value = currentAgentType;

    // Default Working Directory
    const dirInput = document.getElementById("settings-working-dir");
    if (dirInput) {
        dirInput.value = s.default_working_dir || "";
        dirInput.placeholder = dirInput.dataset.coralRoot || "/path/to/project";
    }

    // API Key
    const apiKeyInput = document.getElementById("settings-api-key");
    if (apiKeyInput) {
        fetch('/api/system/api-key').then(r => r.ok ? r.json() : null).then(data => {
            if (data && data.key) apiKeyInput.value = data.key;
            else apiKeyInput.value = 'Not configured';
        }).catch(() => { apiKeyInput.value = 'Not available'; });
    }

    // CLI Paths
    const cliClaude = document.getElementById("settings-cli-path-claude");
    const cliCodex = document.getElementById("settings-cli-path-codex");
    const cliGemini = document.getElementById("settings-cli-path-gemini");
    if (cliClaude) cliClaude.value = s.cli_path_claude || "";
    if (cliCodex) cliCodex.value = s.cli_path_codex || "";
    if (cliGemini) cliGemini.value = s.cli_path_gemini || "";

    // Group by Team
    const groupByTeamCheck = document.getElementById("settings-group-by-team");
    if (groupByTeamCheck) {
        groupByTeamCheck.checked = localStorage.getItem('coral-group-by-team') !== 'false';
    }

    // Terminal Scrollback
    const scrollbackSelect = document.getElementById("settings-terminal-scrollback");
    if (scrollbackSelect) scrollbackSelect.value = s.terminal_scrollback || "1000";

    // Fit Pane Width
    const fitPaneCheck = document.getElementById("settings-fit-pane-width");
    if (fitPaneCheck) fitPaneCheck.checked = !!s.fit_pane_width;

    // Notify Needs Input
    const notifyCheck = document.getElementById("settings-notify-needs-input");
    if (notifyCheck) notifyCheck.checked = !!s.notify_needs_input;

    // Check for Updates (stored in localStorage, defaults to on)
    const updateCheck = document.getElementById("settings-check-updates");
    if (updateCheck) updateCheck.checked = localStorage.getItem("coral-update-check-enabled") !== "false";

    // Show Scrollbars (defaults to true)
    const scrollbarsCheck = document.getElementById("settings-show-scrollbars");
    if (scrollbarsCheck) scrollbarsCheck.checked = s.show_scrollbars !== false;

    document.getElementById("settings-modal").style.display = "flex";
}

export function hideSettingsModal() {
    document.getElementById("settings-modal").style.display = "none";
}

export async function applySettings() {
    const themeValue = document.getElementById("settings-theme")?.value || "dark";
    const engineName = document.getElementById("settings-renderer-select").value;
    const agentType = document.getElementById("settings-agent-type")?.value || "claude";
    const workingDir = document.getElementById("settings-working-dir")?.value.trim() || "";
    const cliPathClaude = document.getElementById("settings-cli-path-claude")?.value.trim() || "";
    const cliPathCodex = document.getElementById("settings-cli-path-codex")?.value.trim() || "";
    const cliPathGemini = document.getElementById("settings-cli-path-gemini")?.value.trim() || "";
    const fitPaneWidth = document.getElementById("settings-fit-pane-width")?.checked || false;
    const notifyNeedsInput = document.getElementById("settings-notify-needs-input")?.checked || false;
    const terminalScrollback = document.getElementById("settings-terminal-scrollback")?.value || "1000";
    const checkUpdates = document.getElementById("settings-check-updates")?.checked ?? true;
    localStorage.setItem("coral-update-check-enabled", checkUpdates ? "true" : "false");
    const showScrollbars = document.getElementById("settings-show-scrollbars")?.checked ?? true;

    // Parse theme selection — "custom:<name>" or built-in "dark"/"light"/"system"
    let theme, customTheme;
    if (themeValue.startsWith("custom:")) {
        customTheme = themeValue.slice(7);
        theme = "dark"; // will be overridden by the custom theme's base
    } else {
        theme = themeValue;
        customTheme = "";
    }

    const payload = {
        theme: theme,
        custom_theme: customTheme,
        default_renderer: engineName,
        default_agent_type: agentType,
        default_working_dir: workingDir,
        fit_pane_width: fitPaneWidth,
        notify_needs_input: notifyNeedsInput,
        terminal_scrollback: terminalScrollback,
        show_scrollbars: showScrollbars,
        cli_path_claude: cliPathClaude,
        cli_path_codex: cliPathCodex,
        cli_path_gemini: cliPathGemini,
    };

    try {
        await fetch("/api/settings", {
            method: "PUT",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(payload),
        });
        state.settings = { ...state.settings, ...payload };

        // Clear any previously applied custom CSS variables
        clearCustomThemeVars();

        if (customTheme) {
            // Load and apply the custom theme (sets base + variables)
            await applyCustomThemeByName(customTheme);
        } else {
            // Apply built-in theme
            applyTheme(theme);
        }

        // If a live session is selected, force re-render with new default
        if (state.currentSession?.session_id) {
            const el = document.getElementById("pane-capture");
            if (el && el._lastCapture) {
                renderCaptureText(el, el._lastCapture);
            }
        }

        // Trigger pane width sync if the setting was just enabled
        if (fitPaneWidth) {
            syncPaneWidth();
        }

        // Apply scrollback to existing terminal
        const term = getTerminal();
        if (term) {
            term.options.scrollback = parseInt(terminalScrollback, 10) || 1000;
        }

        // Apply scrollbar visibility
        document.body.classList.toggle('no-scrollbars', !showScrollbars);

        showToast("Settings saved");
    } catch (e) {
        showToast("Failed to save settings", true);
    }

    hideSettingsModal();
}

// ── Default Prompts Modal ──────────────────────────────────────────

// Cached default prompts fetched from the backend API.
// Populated on first open of the modal; used for "Reset to Default".
let _defaultPrompts = null;

async function _ensureDefaultPrompts() {
    if (_defaultPrompts) return _defaultPrompts;
    try {
        const res = await fetch("/api/settings/default-prompts");
        _defaultPrompts = await res.json();
    } catch {
        // Fallback — should never happen in practice
        _defaultPrompts = {
            default_prompt_orchestrator: 'IMPORTANT: You were automatically joined to message board "{board_name}". Do NOT run coral-board join. Post a message with coral-board post "<your introduction>" that introduces yourself, then discuss your proposed plan with the operator (the human user) before posting assignments. Periodically check for new messages.',
            default_prompt_worker: 'IMPORTANT: You were automatically joined to message board "{board_name}". Do NOT run coral-board join. Do not start any actions until you receive instructions from the Orchestrator on the message board. Post a message with coral-board post "<your introduction>" that introduces yourself, then periodically check for new messages.',
            team_reminder_orchestrator: 'Remember to coordinate with your team and check the message board for updates',
            team_reminder_worker: 'Remember to work with your team',
        };
    }
    return _defaultPrompts;
}

export async function showDefaultPromptsModal() {
    const defaults = await _ensureDefaultPrompts();
    const s = state.settings || {};
    const orchEl = document.getElementById("settings-prompt-orchestrator");
    const workerEl = document.getElementById("settings-prompt-worker");
    if (orchEl) orchEl.value = s.default_prompt_orchestrator || defaults.default_prompt_orchestrator;
    if (workerEl) workerEl.value = s.default_prompt_worker || defaults.default_prompt_worker;
    const teamOrchEl = document.getElementById("settings-team-reminder-orchestrator");
    const teamWorkerEl = document.getElementById("settings-team-reminder-worker");
    if (teamOrchEl) teamOrchEl.value = s.team_reminder_orchestrator || defaults.team_reminder_orchestrator;
    if (teamWorkerEl) teamWorkerEl.value = s.team_reminder_worker || defaults.team_reminder_worker;
    document.getElementById("default-prompts-modal").style.display = "flex";
}

export function hideDefaultPromptsModal() {
    document.getElementById("default-prompts-modal").style.display = "none";
}

export async function resetDefaultPrompt(type) {
    const defaults = await _ensureDefaultPrompts();
    const mapping = {
        orchestrator: ["settings-prompt-orchestrator", "default_prompt_orchestrator"],
        worker: ["settings-prompt-worker", "default_prompt_worker"],
        team_orchestrator: ["settings-team-reminder-orchestrator", "team_reminder_orchestrator"],
        team_worker: ["settings-team-reminder-worker", "team_reminder_worker"],
    };
    const entry = mapping[type];
    if (entry) {
        const el = document.getElementById(entry[0]);
        if (el) el.value = defaults[entry[1]];
    }
}

export async function saveDefaultPrompts() {
    const defaults = await _ensureDefaultPrompts();
    const orchValue = document.getElementById("settings-prompt-orchestrator")?.value || "";
    const workerValue = document.getElementById("settings-prompt-worker")?.value || "";
    const teamOrchValue = document.getElementById("settings-team-reminder-orchestrator")?.value || "";
    const teamWorkerValue = document.getElementById("settings-team-reminder-worker")?.value || "";

    // Save empty string when value matches default — so future default updates are picked up
    const payload = {
        default_prompt_orchestrator: orchValue === defaults.default_prompt_orchestrator ? "" : orchValue,
        default_prompt_worker: workerValue === defaults.default_prompt_worker ? "" : workerValue,
        team_reminder_orchestrator: teamOrchValue === defaults.team_reminder_orchestrator ? "" : teamOrchValue,
        team_reminder_worker: teamWorkerValue === defaults.team_reminder_worker ? "" : teamWorkerValue,
    };

    try {
        await fetch("/api/settings", {
            method: "PUT",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(payload),
        });
        state.settings = { ...state.settings, ...payload };
        showToast("Default prompts saved");
    } catch (e) {
        showToast("Failed to save prompts", true);
    }

    hideDefaultPromptsModal();
}

// Sync flag shortcut button active states with the text input
function _syncFlagButtons(inputId) {
    const input = document.getElementById(inputId);
    if (!input) return;
    const flags = input.value.trim() ? input.value.trim().split(/\s+/) : [];
    const container = input.closest("label")?.nextElementSibling;
    if (!container) return;
    container.querySelectorAll(".flag-shortcut-btn").forEach(btn => {
        const flag = btn.textContent.trim();
        btn.classList.toggle("active", flags.includes(flag));
    });
}

// Attach input listeners for flag sync (after DOM is ready)
document.addEventListener("DOMContentLoaded", () => {
    for (const id of ["launch-flags", "restart-flags", "job-modal-flags", "team-flags"]) {
        const el = document.getElementById(id);
        if (el) el.addEventListener("input", () => _syncFlagButtons(id));
    }
});

// Close modals on outside click
document.addEventListener("click", (e) => {
    const launchModal = document.getElementById("launch-modal");
    const infoModal = document.getElementById("info-modal");
    const resumeModal = document.getElementById("resume-modal");
    const settingsModal = document.getElementById("settings-modal");
    const restartModal = document.getElementById("restart-modal");
    if (e.target === launchModal) {
        hideLaunchModal();
    }
    if (e.target === infoModal) {
        hideInfoModal();
    }
    if (e.target === resumeModal) {
        hideResumeModal();
    }
    if (e.target === settingsModal) {
        hideSettingsModal();
    }
    if (e.target === restartModal) {
        hideRestartModal();
    }
    const macroModal = document.getElementById("macro-modal");
    if (e.target === macroModal) {
        macroModal.style.display = "none";
    }
    const webhookModal = document.getElementById("webhook-modal");
    if (e.target === webhookModal) window.hideWebhookModal?.();
    const confirmModal = document.getElementById("confirm-modal");
    if (e.target === confirmModal) window.hideConfirmModal?.();
});

// Close modals on Escape
document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") {
        hideLaunchModal();
        hideInfoModal();
        hideResumeModal();
        hideSettingsModal();
        hideRestartModal();
        const mm = document.getElementById("macro-modal");
        if (mm) mm.style.display = "none";
        window.hideWebhookModal?.();
    }
});

// ── Mobile Connect Modal ─────────────────────────────────────────────────

window._showMobileConnectModal = async function() {
    const modal = document.getElementById('mobile-connect-modal');
    if (!modal) return;

    // Detect LAN IP — prefer server-reported IP over window.location
    let baseUrl = window.location.origin;
    try {
        const netResp = await fetch('/api/system/network-info');
        if (netResp.ok) {
            const netData = await netResp.json();
            const lanIp = netData.primary || (netData.ips && netData.ips[0]);
            if (lanIp) {
                const port = netData.port || window.location.port || '8420';
                baseUrl = `http://${lanIp}:${port}`;
            }
        }
    } catch { /* fall back to window.location.origin */ }

    // Try to fetch the API key to include in the QR URL
    let connectUrl = baseUrl;
    try {
        const resp = await fetch('/api/system/api-key');
        if (resp.ok) {
            const data = await resp.json();
            if (data.key) {
                connectUrl = `${baseUrl}/auth?key=${data.key}`;
            }
        }
    } catch { /* no auth configured — just use base URL */ }

    document.getElementById('mobile-connect-url').textContent = connectUrl;

    // Generate QR code via server endpoint
    const qrContainer = document.getElementById('mobile-connect-qr');
    if (qrContainer) {
        qrContainer.innerHTML = `<img src="/api/system/qr?url=${encodeURIComponent(connectUrl)}" alt="QR Code" style="width:180px;height:180px;border-radius:8px;background:#fff;padding:8px" onerror="this.style.display='none'">`;
    }

    modal.style.display = 'flex';
};
