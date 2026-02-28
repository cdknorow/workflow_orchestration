/* Capture text rendering and auto-refresh */

import { state, CAPTURE_REFRESH_MS } from './state.js';
import {
    isSeparatorLine, isUserPromptLine, highlightCodeLine,
    CODE_FENCE_RE, DIFF_ADD_RE, DIFF_DEL_RE, DIFF_SUMMARY_RE,
    TOOL_HEADER_RE, TOOL_RESULT_RE,
} from './syntax.js';
import { loadAgentTasks } from './tasks.js';
import { loadAgentEvents } from './agentic_state.js';

// Matches any line starting with ⏺ (U+23FA) — all Claude tool calls
const TOOL_CALL_RE = /^[\s●]*⏺\s/;
// Matches progress/thinking lines by content pattern rather than icon,
// since Claude Code rotates through many decorative Unicode characters.
// Looks for: <any single char> <text>… (<duration> · ↓ <tokens>)
// or:        <any single char> <text>… (thinking)
const PROGRESS_RE = /^\s*\S\s+\S.*[…\.]\s*\((\d+[ms]|\d+\w*\s+\d+\w*\s+·|thinking)/;
// Matches the status bar at the bottom (worktree:... | session:... | ...)
const STATUS_BAR_RE = /^\s*(worktree:|⏵)/;
// Matches ||PULSE:TYPE payload|| protocol lines
const PULSE_RE = /\|\|PULSE:(STATUS|SUMMARY|CONFIDENCE)\s/;

/**
 * Classify a single line into a block type.
 * Returns: "user", "tool-header", "tool-body", "text", "separator", "empty", "status", "statusbar", "pulse"
 */
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
    // Any ⏺ line is a tool call header
    if (TOOL_CALL_RE.test(line)) return "tool-header";
    if (TOOL_RESULT_RE.test(line)) return "tool-body";
    if (DIFF_ADD_RE.test(line) || DIFF_DEL_RE.test(line)) return "tool-body";
    if (DIFF_SUMMARY_RE.test(line)) return "tool-body";
    if (CODE_FENCE_RE.test(line)) return "tool-body";
    if (PROGRESS_RE.test(line)) return "status";

    return "text";
}

/**
 * Group classified lines into blocks. Each block has a type and a list of line indices.
 */
function groupIntoBlocks(lines) {
    const blocks = [];
    let current = null;

    function finishBlock() {
        if (current && current.lines.length > 0) {
            // Trim trailing empty lines from the block
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
            // Empty lines end text blocks but not tool/user blocks
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
            // New ⏺ tool header always starts a fresh tool block
            finishBlock();
            current = { type: "tool", lines: [i] };
        } else if (cls === "tool-body") {
            // ⎿ results, code, diff lines join the current tool or status block
            if (current && (current.type === "tool" || current.type === "status")) {
                current.lines.push(i);
            } else {
                finishBlock();
                current = { type: "tool", lines: [i] };
            }
        } else if (cls === "text") {
            // Text continues current block (text/tool/user/status), or starts new text block
            if (current && (current.type === "text" || current.type === "tool" || current.type === "user" || current.type === "status")) {
                current.lines.push(i);
            } else {
                finishBlock();
                current = { type: "text", lines: [i] };
            }
        } else if (cls === "separator") {
            finishBlock();
        } else if (cls === "status") {
            // Thinking/progress lines — group consecutive ones together
            if (current && current.type === "status") {
                current.lines.push(i);
            } else {
                finishBlock();
                current = { type: "status", lines: [i] };
            }
        } else if (cls === "pulse") {
            // Each PULSE line gets its own block
            finishBlock();
            blocks.push({ type: "pulse", lines: [i] });
        } else if (cls === "statusbar") {
            // Bottom status bar lines group together
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

/**
 * Render a single line div with syntax highlighting (extracted from original logic).
 */
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

export function renderCaptureText(el, text) {
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

export async function refreshCapture() {
    if (!state.currentSession || state.currentSession.type !== "live") return;

    try {
        const params = new URLSearchParams();
        if (state.currentSession.agent_type) params.set("agent_type", state.currentSession.agent_type);
        if (state.currentSession.session_id) params.set("session_id", state.currentSession.session_id);
        const qs = params.toString() ? `?${params}` : "";
        let captureUrl = `/api/sessions/live/${encodeURIComponent(state.currentSession.name)}/capture${qs}`;
        const resp = await fetch(captureUrl);
        const data = await resp.json();
        const el = document.getElementById("pane-capture");
        const text = data.capture || data.error || "No capture available";

        // Only update if content changed to avoid scroll jank
        if (el._lastCapture !== text) {
            el._lastCapture = text;
            renderCaptureText(el, text);
            if (state.autoScroll) {
                el.scrollTop = el.scrollHeight;
            }
        }
    } catch (e) {
        console.error("Failed to refresh capture:", e);
    }

    // Poll tasks and events on the same interval
    if (state.currentSession && state.currentSession.type === "live") {
        const sid = state.currentSession.session_id;
        loadAgentTasks(state.currentSession.name, sid);
        loadAgentEvents(state.currentSession.name, sid);
    }
}

export function startCaptureRefresh() {
    stopCaptureRefresh();
    refreshCapture();
    state.captureInterval = setInterval(refreshCapture, CAPTURE_REFRESH_MS);
}

export function stopCaptureRefresh() {
    if (state.captureInterval) {
        clearInterval(state.captureInterval);
        state.captureInterval = null;
    }
}
