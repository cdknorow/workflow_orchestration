/* Live history view — renders JSONL messages as a read-only conversation log */

import { state } from './state.js';
import { escapeHtml } from './utils.js';

let historyPollInterval = null;
let historyMessageCount = 0;

function renderMarkdown(text) {
    if (typeof marked !== 'undefined') {
        try { return marked.parse(text); } catch (e) { /* fall through */ }
    }
    return escapeHtml(text);
}

const TOOL_ICONS = {
    Read: "\u{1F4C4}",
    Edit: "\u{270F}\uFE0F",
    Write: "\u{1F4DD}",
    Bash: "\u{1F4BB}",
    Grep: "\u{1F50D}",
    Glob: "\u{1F4C2}",
    Agent: "\u{1F916}",
    WebSearch: "\u{1F310}",
    WebFetch: "\u{1F310}",
    TaskCreate: "\u{2611}\uFE0F",
    TaskUpdate: "\u{2611}\uFE0F",
    NotebookEdit: "\u{1F4D3}",
    AskUserQuestion: "\u{2753}",
};

function getToolIcon(name) {
    return TOOL_ICONS[name] || "\u{1F527}";
}

function renderDiffLines(oldStr, newStr) {
    const oldLines = oldStr.split("\n");
    const newLines = newStr.split("\n");
    let html = "";
    for (const line of oldLines) {
        html += `<div class="diff-line diff-del">- ${escapeHtml(line)}</div>`;
    }
    for (const line of newLines) {
        html += `<div class="diff-line diff-add">+ ${escapeHtml(line)}</div>`;
    }
    return html;
}

/** Render inline output preview: first 3 lines visible, rest expandable */
function renderInlineOutput(content, isError) {
    const lines = content.split("\n");
    const errorClass = isError ? " tool-result-error" : "";
    const preview = lines.slice(0, 3).join("\n");

    if (lines.length <= 3) {
        return `<pre class="tool-output-inline${errorClass}">${escapeHtml(preview)}</pre>`;
    }

    const full = content;
    return `<div class="tool-output-wrapper">
        <pre class="tool-output-inline tool-output-collapsed${errorClass}">${escapeHtml(preview)}</pre>
        <pre class="tool-output-inline tool-output-full${errorClass}" style="display:none">${escapeHtml(full)}</pre>
        <button class="tool-output-toggle" onclick="this.parentElement.querySelector('.tool-output-collapsed').style.display='none';this.parentElement.querySelector('.tool-output-full').style.display='';this.textContent='';this.style.display='none'">... ${lines.length - 3} more lines</button>
    </div>`;
}

function renderToolCard(tool) {
    const icon = getToolIcon(tool.name);
    const toolUseId = tool.tool_use_id || "";
    let bodyHtml = "";

    // Show description if present (Claude's prompt to the user)
    if (tool.description) {
        bodyHtml += `<div class="tool-card-description">${escapeHtml(tool.description)}</div>`;
    }

    if (tool.name === "Bash" && tool.command) {
        // Bash: show full command in a code block
        bodyHtml += `<pre class="tool-card-command">${escapeHtml(tool.command)}</pre>`;
    } else if (tool.input_summary) {
        bodyHtml += `<div class="tool-card-summary">${escapeHtml(tool.input_summary)}</div>`;
    }

    // AskUserQuestion: render structured question with options
    if (tool.name === "AskUserQuestion" && tool.questions) {
        for (const q of tool.questions) {
            bodyHtml += `<div class="tool-question">`;
            bodyHtml += `<div class="tool-question-text">${escapeHtml(q.question)}</div>`;
            if (q.options && q.options.length > 0) {
                bodyHtml += `<div class="tool-question-options">`;
                for (const opt of q.options) {
                    bodyHtml += `<div class="tool-question-option">`;
                    bodyHtml += `<span class="tool-question-option-label">${escapeHtml(opt.label)}</span>`;
                    if (opt.description) {
                        bodyHtml += `<span class="tool-question-option-desc">${escapeHtml(opt.description)}</span>`;
                    }
                    bodyHtml += `</div>`;
                }
                bodyHtml += `</div>`;
            }
            bodyHtml += `</div>`;
        }
    }

    // Edit tool: show diff
    if (tool.name === "Edit" && (tool.old_string || tool.new_string)) {
        const diffHtml = renderDiffLines(tool.old_string || "", tool.new_string || "");
        bodyHtml += `<details class="tool-card-diff"><summary>Diff</summary><div class="diff-content">${diffHtml}</div></details>`;
    }
    // Write tool: show content preview
    if (tool.name === "Write" && tool.write_content) {
        bodyHtml += `<details class="tool-card-diff"><summary>Content</summary><pre class="tool-result-content">${escapeHtml(tool.write_content)}</pre></details>`;
    }

    return `<div class="tool-card" data-tool-use-id="${escapeHtml(toolUseId)}">
        ${bodyHtml}
    </div>`;
}

function renderMessage(msg, container) {
    if (msg.type === "user") {
        const div = document.createElement("div");
        div.className = "chat-bubble human";
        div.innerHTML = `
            <div class="role-label">You</div>
            <div class="message-text">${renderMarkdown(msg.content)}</div>
        `;
        container.appendChild(div);
    } else if (msg.type === "assistant") {
        const tools = msg.tool_uses || [];
        const textHtml = msg.text ? `<div class="message-text">${renderMarkdown(msg.text)}</div>` : "";
        if (tools.length > 0) {
            // Text before tools (if any)
            if (textHtml) {
                const textDiv = document.createElement("div");
                textDiv.className = "chat-bubble assistant";
                textDiv.innerHTML = textHtml;
                container.appendChild(textDiv);
            }
            // Each tool gets its own bubble with tool name as the role label
            for (const tool of tools) {
                const icon = getToolIcon(tool.name);
                const toolDiv = document.createElement("div");
                toolDiv.className = "chat-bubble assistant";
                toolDiv.innerHTML = `<div class="role-label">${icon} ${escapeHtml(tool.name)}</div>` +
                    renderToolCard(tool);
                container.appendChild(toolDiv);
            }
        } else {
            // Text-only assistant message
            const div = document.createElement("div");
            div.className = "chat-bubble assistant";
            div.innerHTML = textHtml;
            container.appendChild(div);
        }
    } else if (msg.type === "tool_result") {
        // Inject output inline into the matching tool card
        const toolUseId = msg.tool_use_id || "";
        let targetCard = null;
        if (toolUseId) {
            targetCard = container.querySelector(`.tool-card[data-tool-use-id="${CSS.escape(toolUseId)}"]`);
        }
        if (targetCard) {
            // Append inline output to the tool card
            const outputDiv = document.createElement("div");
            outputDiv.className = "tool-card-output";
            outputDiv.innerHTML = renderInlineOutput(msg.content, msg.is_error);
            targetCard.appendChild(outputDiv);
        } else {
            // Fallback: render as standalone block if no matching card found
            const div = document.createElement("div");
            div.className = "tool-result-block";
            const toolName = msg.tool_name || "";
            const icon = toolName ? getToolIcon(toolName) : "";
            const label = toolName ? `${icon} ${escapeHtml(toolName)} Output` : "Output";
            div.innerHTML = `<div class="tool-result-standalone">${label}</div>` +
                renderInlineOutput(msg.content, msg.is_error);
            container.appendChild(div);
        }
    }
}

export async function refreshLiveHistory() {
    if (!state.currentSession || state.currentSession.type !== "live") return;
    if (document.hidden) return;

    const session = state.currentSession;
    if (!session.session_id) return;

    const container = document.getElementById("live-history-messages");
    if (!container) return;

    const params = new URLSearchParams();
    params.set("session_id", session.session_id);
    params.set("after", historyMessageCount);
    if (session.working_directory) {
        params.set("working_directory", session.working_directory);
    }

    try {
        const resp = await fetch(`/api/sessions/live/${encodeURIComponent(session.name)}/chat?${params}`);
        const data = await resp.json();

        if (data.messages && data.messages.length > 0) {
            for (const msg of data.messages) {
                renderMessage(msg, container);
            }
            historyMessageCount = data.total;
        }

        if (state.autoScroll) {
            container.scrollTop = container.scrollHeight;
        }
    } catch (e) {
        console.error("Failed to refresh live history:", e);
    }
}

export function startLiveHistoryPoll() {
    stopLiveHistoryPoll();
    refreshLiveHistory();
    historyPollInterval = setInterval(refreshLiveHistory, 1000);
}

export function stopLiveHistoryPoll() {
    if (historyPollInterval) {
        clearInterval(historyPollInterval);
        historyPollInterval = null;
    }
}

export function resetLiveHistory() {
    const container = document.getElementById("live-history-messages");
    if (container) container.innerHTML = "";
    historyMessageCount = 0;
}
