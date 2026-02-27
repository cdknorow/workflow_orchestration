/* Capture text rendering and auto-refresh */

import { state, CAPTURE_REFRESH_MS } from './state.js';
import {
    isSeparatorLine, isUserPromptLine, highlightCodeLine,
    CODE_FENCE_RE, DIFF_ADD_RE, DIFF_DEL_RE, DIFF_SUMMARY_RE,
    TOOL_HEADER_RE, TOOL_RESULT_RE,
} from './syntax.js';
import { loadAgentTasks } from './tasks.js';
import { loadAgentEvents } from './agentic_state.js';

export function renderCaptureText(el, text) {
    el.innerHTML = "";
    const lines = text.split("\n");
    let inUserBlock = false;
    let inCodeBlock = false;

    for (let i = 0; i < lines.length; i++) {
        const line = lines[i];
        const div = document.createElement("div");
        div.className = "capture-line";

        const diffAddMatch = line.match(DIFF_ADD_RE);
        const diffDelMatch = !diffAddMatch && line.match(DIFF_DEL_RE);
        const isDiffSummary = DIFF_SUMMARY_RE.test(line);
        const isNumberedCode = !diffAddMatch && !diffDelMatch && CODE_FENCE_RE.test(line);
        const isToolHeader = TOOL_HEADER_RE.test(line);
        const isToolResult = TOOL_RESULT_RE.test(line);

        if (isSeparatorLine(line)) {
            const prevIsUser = i > 0 && isUserPromptLine(lines[i - 1]);
            const nextIsUser = i < lines.length - 1 && isUserPromptLine(lines[i + 1]);
            if (prevIsUser || nextIsUser || inUserBlock) {
                div.classList.add("capture-separator");
                inUserBlock = nextIsUser;
            }
            inCodeBlock = false;
        } else if (isUserPromptLine(line)) {
            div.classList.add("capture-user-input");
            inUserBlock = true;
            inCodeBlock = false;
        } else if (inUserBlock && line.trim() !== "") {
            div.classList.add("capture-user-input");
        } else if (isDiffSummary) {
            div.classList.add("capture-diff-summary");
            inUserBlock = false;
            inCodeBlock = false;
        } else if (diffAddMatch) {
            div.classList.add("capture-diff-add");
            inUserBlock = false;
            inCodeBlock = false;
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
            el.appendChild(div);
            continue;
        } else if (diffDelMatch) {
            div.classList.add("capture-diff-del");
            inUserBlock = false;
            inCodeBlock = false;
            const gutter = diffDelMatch[1];
            const code = line.slice(gutter.length);
            const gutterSpan = document.createElement("span");
            gutterSpan.className = "sh-diff-gutter-del";
            gutterSpan.textContent = gutter;
            div.appendChild(gutterSpan);
            div.appendChild(document.createTextNode(code));
            el.appendChild(div);
            continue;
        } else if (isToolHeader) {
            div.classList.add("capture-tool-header");
            inUserBlock = false;
            inCodeBlock = false;
        } else if (isToolResult) {
            div.classList.add("capture-tool-result");
            inUserBlock = false;
        } else if (isNumberedCode) {
            div.classList.add("capture-code");
            inUserBlock = false;
            inCodeBlock = true;
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
            el.appendChild(div);
            continue;
        } else {
            if (inUserBlock && line.trim() === "") {
                inUserBlock = false;
            }
            inCodeBlock = false;
        }

        div.textContent = line;
        el.appendChild(div);
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
