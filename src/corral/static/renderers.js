/* Rendering engine abstraction for terminal capture output.
 *
 * Each RenderEngine takes raw terminal text and produces DOM nodes.
 * Engines can be assigned per agent type, with runtime overrides.
 */

import {
    isSeparatorLine, isUserPromptLine, highlightCodeLine,
    CODE_FENCE_RE, DIFF_ADD_RE, DIFF_DEL_RE, DIFF_SUMMARY_RE,
    TOOL_HEADER_RE, TOOL_RESULT_RE,
} from './syntax.js';

// ── Regex constants shared across renderers ──────────────────────────

const TOOL_CALL_RE = /^[\s●]*⏺\s/;
const PROGRESS_RE = /^\s*\S\s+\S.*[…\.]\s*\((\d+[ms]|\d+\w*\s+\d+\w*\s+·|thinking)/;
const STATUS_BAR_RE = /^\s*(worktree:|⏵)/;
const PULSE_RE = /\|\|PULSE:(STATUS|SUMMARY|CONFIDENCE)\s/;

// ── Shared line rendering ────────────────────────────────────────────

/** Render a single line div with syntax highlighting. */
function renderLine(line) {
    const div = document.createElement("div");
    div.className = "capture-line";

    const diffAddMatch = line.match(DIFF_ADD_RE);
    const diffDelMatch = !diffAddMatch && line.match(DIFF_DEL_RE);
    const isDiffSummary = DIFF_SUMMARY_RE.test(line);
    const isNumberedCode = !diffAddMatch && !diffDelMatch && CODE_FENCE_RE.test(line);
    const isToolHeader = TOOL_HEADER_RE.test(line) || TOOL_CALL_RE.test(line);
    const isToolResult = TOOL_RESULT_RE.test(line);

    if (isSeparatorLine(line)) {
        div.classList.add("capture-separator");
        div.textContent = line;
    } else if (isUserPromptLine(line)) {
        div.classList.add("capture-user-input");
        div.textContent = line;
    } else if (isDiffSummary) {
        div.classList.add("capture-diff-summary");
        div.textContent = line;
    } else if (diffAddMatch) {
        div.classList.add("capture-diff-add");
        const gutter = diffAddMatch[1];
        const code = line.slice(gutter.length);
        const gutterSpan = document.createElement("span");
        gutterSpan.className = "sh-diff-gutter-add";
        gutterSpan.textContent = gutter;
        div.appendChild(gutterSpan);
        const highlighted = highlightCodeLine(code);
        if (highlighted) {
            div.appendChild(highlighted);
        } else {
            div.appendChild(document.createTextNode(code));
        }
    } else if (diffDelMatch) {
        div.classList.add("capture-diff-del");
        const gutter = diffDelMatch[1];
        const code = line.slice(gutter.length);
        const gutterSpan = document.createElement("span");
        gutterSpan.className = "sh-diff-gutter-del";
        gutterSpan.textContent = gutter;
        div.appendChild(gutterSpan);
        div.appendChild(document.createTextNode(code));
    } else if (isToolHeader) {
        div.classList.add("capture-tool-header");
        div.textContent = line;
    } else if (isToolResult) {
        div.classList.add("capture-tool-result");
        div.textContent = line;
    } else if (isNumberedCode) {
        div.classList.add("capture-code");
        const match = line.match(CODE_FENCE_RE);
        const gutter = match[1];
        const code = line.slice(gutter.length);
        const gutterSpan = document.createElement("span");
        gutterSpan.className = "sh-gutter";
        gutterSpan.textContent = gutter;
        div.appendChild(gutterSpan);
        const highlighted = highlightCodeLine(code);
        if (highlighted) {
            div.appendChild(highlighted);
        } else {
            div.appendChild(document.createTextNode(code));
        }
    } else {
        div.textContent = line;
    }

    return div;
}

// ── Base class ───────────────────────────────────────────────────────

class RenderEngine {
    /** Human-readable name for UI display. */
    get name() { return "base"; }

    /** Render raw terminal text into the given DOM element. */
    render(el, text) {
        throw new Error("RenderEngine.render() not implemented");
    }
}

// ── Block-group renderer (current) ───────────────────────────────────

function classifyLine(line, i, lines) {
    if (line.trim() === "") return "empty";

    if (isSeparatorLine(line)) {
        const prevIsUser = i > 0 && isUserPromptLine(lines[i - 1]);
        const nextIsUser = i < lines.length - 1 && isUserPromptLine(lines[i + 1]);
        if (prevIsUser || nextIsUser) return "user";
        return "separator";
    }
    if (isUserPromptLine(line)) return "user";
    if (STATUS_BAR_RE.test(line)) return "statusbar";
    if (PULSE_RE.test(line)) return "pulse";
    if (TOOL_CALL_RE.test(line)) return "tool-header";
    if (TOOL_RESULT_RE.test(line)) return "tool-body";
    if (DIFF_ADD_RE.test(line) || DIFF_DEL_RE.test(line)) return "tool-body";
    if (DIFF_SUMMARY_RE.test(line)) return "tool-body";
    if (CODE_FENCE_RE.test(line)) return "tool-body";
    if (PROGRESS_RE.test(line)) return "status";

    return "text";
}

function groupIntoBlocks(lines) {
    const blocks = [];
    let current = null;

    function finishBlock() {
        if (current && current.lines.length > 0) {
            while (current.lines.length > 0 && lines[current.lines[current.lines.length - 1]].trim() === "") {
                current.lines.pop();
            }
            if (current.lines.length > 0) {
                blocks.push(current);
            }
        }
        current = null;
    }

    for (let i = 0; i < lines.length; i++) {
        const cls = classifyLine(lines[i], i, lines);

        if (cls === "empty") {
            if (current && current.type === "text") {
                finishBlock();
            }
            continue;
        }

        if (cls === "user") {
            if (!current || current.type !== "user") {
                finishBlock();
                current = { type: "user", lines: [] };
            }
            current.lines.push(i);
        } else if (cls === "tool-header") {
            finishBlock();
            current = { type: "tool", lines: [i] };
        } else if (cls === "tool-body") {
            if (current && (current.type === "tool" || current.type === "status")) {
                current.lines.push(i);
            } else {
                finishBlock();
                current = { type: "tool", lines: [i] };
            }
        } else if (cls === "text") {
            if (current && (current.type === "text" || current.type === "tool" || current.type === "user" || current.type === "status")) {
                current.lines.push(i);
            } else {
                finishBlock();
                current = { type: "text", lines: [i] };
            }
        } else if (cls === "separator") {
            finishBlock();
        } else if (cls === "status") {
            if (current && current.type === "status") {
                current.lines.push(i);
            } else {
                finishBlock();
                current = { type: "status", lines: [i] };
            }
        } else if (cls === "pulse") {
            finishBlock();
            blocks.push({ type: "pulse", lines: [i] });
        } else if (cls === "statusbar") {
            if (current && current.type === "statusbar") {
                current.lines.push(i);
            } else {
                finishBlock();
                current = { type: "statusbar", lines: [i] };
            }
        }
    }

    finishBlock();
    return blocks;
}

class BlockGroupRenderer extends RenderEngine {
    get name() { return "block-group"; }

    render(el, text) {
        el.innerHTML = "";
        const lines = text.split("\n");
        const blocks = groupIntoBlocks(lines);

        for (const block of blocks) {
            const container = document.createElement("div");
            container.className = `capture-block capture-block-${block.type}`;

            for (const idx of block.lines) {
                container.appendChild(renderLine(lines[idx]));
            }

            el.appendChild(container);
        }
    }
}

// ── Plain renderer (previous flat line-by-line) ──────────────────────

class PlainRenderer extends RenderEngine {
    get name() { return "plain"; }

    render(el, text) {
        el.innerHTML = "";
        const lines = text.split("\n");

        for (const line of lines) {
            if (line.trim() === "") continue;
            el.appendChild(renderLine(line));
        }
    }
}

// ── Registry ─────────────────────────────────────────────────────────

const ENGINES = {
    "block-group": new BlockGroupRenderer(),
    "plain": new PlainRenderer(),
};

/** Default renderer name per agent type. */
const AGENT_DEFAULTS = {
    claude: "block-group",
    gemini: "plain",
};

/** Per-agent overrides stored at runtime (sessionId -> engineName). */
const agentOverrides = {};

/** Get all registered engine names. */
export function getEngineNames() {
    return Object.keys(ENGINES);
}

/** Get the current engine name for a given agent. */
export function getEngineName(agentType, sessionId) {
    if (sessionId && agentOverrides[sessionId]) {
        return agentOverrides[sessionId];
    }
    return AGENT_DEFAULTS[agentType] || AGENT_DEFAULTS.claude;
}

/** Get the renderer instance for a given agent. */
export function getRenderer(agentType, sessionId) {
    const name = getEngineName(agentType, sessionId);
    return ENGINES[name] || ENGINES["block-group"];
}

/** Override the renderer for a specific agent session. */
export function setRendererOverride(sessionId, engineName) {
    if (!ENGINES[engineName]) {
        console.warn(`Unknown render engine: ${engineName}`);
        return;
    }
    agentOverrides[sessionId] = engineName;
}

/** Clear the renderer override for a specific agent session. */
export function clearRendererOverride(sessionId) {
    delete agentOverrides[sessionId];
}

/** Set the default renderer for an agent type. */
export function setAgentDefault(agentType, engineName) {
    if (!ENGINES[engineName]) {
        console.warn(`Unknown render engine: ${engineName}`);
        return;
    }
    AGENT_DEFAULTS[agentType] = engineName;
}
