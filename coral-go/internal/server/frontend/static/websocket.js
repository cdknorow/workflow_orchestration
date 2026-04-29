/* WebSocket connection for real-time coral updates */

import { state } from './state.js';
import { renderLiveSessions, updateSessionStatus, updateSessionSummary, updateSessionBranch, updateWaitingIndicator } from './render.js';
import { renderLiveJobs } from './live_jobs.js';
import { updateChangedFileCount } from './changed_files.js';
import { updateSectionVisibility } from './sidebar.js';
import { showNotificationToast, showWorkflowNotification, showAlertNotification, showToast, escapeHtml, dbg } from './utils.js';

export function connectCoralWs() {
    dbg('connectCoralWs: establishing connection');
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    const url = `${proto}//${location.host}/ws/coral`;

    state.coralWs = new WebSocket(url);

    state.coralWs.onmessage = (event) => {
        const data = JSON.parse(event.data);

        // Handle diff updates: merge changed/removed into existing session list
        if (data.type === "coral_diff") {
            let sessions = [...(state.liveSessions || [])];

            // Apply changed sessions (update existing or add new)
            if (data.changed) {
                for (const changed of data.changed) {
                    const key = changed.session_id || changed.name;
                    const idx = sessions.findIndex(s => (s.session_id || s.name) === key);
                    if (idx >= 0) {
                        // Preserve fields not sent in WS diff updates
                        if (!changed.commands && sessions[idx].commands) {
                            changed.commands = sessions[idx].commands;
                        }
                        if (!changed.icon && sessions[idx].icon) {
                            changed.icon = sessions[idx].icon;
                        }
                        if (changed.token_input === undefined && sessions[idx].token_input !== undefined) {
                            changed.token_input = sessions[idx].token_input;
                            changed.token_output = sessions[idx].token_output;
                            changed.token_cost_usd = sessions[idx].token_cost_usd;
                        }
                        if (changed.context_pct === undefined && sessions[idx].context_pct !== undefined) {
                            changed.context_pct = sessions[idx].context_pct;
                            changed.context_window = sessions[idx].context_window;
                        }
                        sessions[idx] = changed;
                    } else {
                        sessions.push(changed);
                    }
                }
            }

            // Remove sessions that no longer exist
            if (data.removed) {
                const removedSet = new Set(data.removed);
                sessions = sessions.filter(s => !removedSet.has(s.session_id || s.name));
            }

            // Treat merged list as a full update for the rest of the handler
            data.type = "coral_update";
            data.sessions = sessions;
        }

        if (data.type === "coral_update") {
            // Detect sessions that just transitioned to "needs input"
            for (const s of data.sessions) {
                const id = s.session_id || s.name;
                const wasWaiting = state.prevWaitingState[id];
                const notifyEnabled = state.settings.notify_needs_input !== false;
                if (notifyEnabled && s.waiting_for_input && !wasWaiting) {
                    const label = escapeHtml(s.display_name || s.name);
                    const detail = s.waiting_summary ? escapeHtml(s.waiting_summary) : null;
                    const sessionName = s.name;
                    const agentType = s.agent_type;
                    const sessionId = s.session_id;
                    showNotificationToast(label, detail, () => {
                        import('./sessions.js').then(m => m.selectLiveSession(sessionName, agentType, sessionId));
                    });
                }
                state.prevWaitingState[id] = !!s.waiting_for_input;

                // Detect goal (summary) changes — disabled for now, needs design polish
                // const prevSummary = state.prevSummaryState && state.prevSummaryState[id];
                // if (s.summary && s.summary !== prevSummary) {
                //     const goalLabel = s.display_name || s.board_job_title || s.name;
                //     showToast(`${goalLabel}: ${s.summary}`);
                // }
                // if (!state.prevSummaryState) state.prevSummaryState = {};
                // state.prevSummaryState[id] = s.summary || null;
            }

            // Preserve commands and branch from previous data when not included in WS update
            if (state.liveSessions && state.liveSessions.length) {
                const prevMap = {};
                for (const s of state.liveSessions) {
                    const key = s.session_id || s.name;
                    prevMap[key] = s;
                }
                for (const s of data.sessions) {
                    const key = s.session_id || s.name;
                    const prev = prevMap[key];
                    if (prev) {
                        if (!s.commands && prev.commands) s.commands = prev.commands;
                        if (!s.branch && prev.branch) s.branch = prev.branch;
                        if (!s.repo_name && prev.repo_name) s.repo_name = prev.repo_name;
                    }
                }
            }
            state.liveSessions = data.sessions;
            renderLiveSessions(data.sessions);

            // Show notifications pushed via POST /api/notifications
            if (data.notifications) {
                for (const n of data.notifications) {
                    if (n.type === 'alert') {
                        showAlertNotification(n.title, n.message, n.level, n.link || null);
                    } else {
                        showWorkflowNotification(n.title, n.message, n.level);
                    }
                }
            }

            // Update Jobs sidebar
            if (data.active_runs) {
                renderLiveJobs(data.active_runs);
            }

            // Update status/summary/branch if we're viewing a live session
            if (state.currentSession && state.currentSession.type === "live") {
                const sid = state.currentSession.session_id;
                // Try matching by session_id first, then fall back to name
                let s = sid
                    ? data.sessions.find(s => s.session_id === sid)
                    : null;
                if (!s) {
                    s = data.sessions.find(s => s.name === state.currentSession.name);
                }
                if (s) {
                    // Keep state in sync with backend (handles restarts
                    // where session_id or name may change).
                    // Only update session_id if we matched by session_id (not
                    // by name fallback), to avoid switching to the wrong
                    // session when multiple sessions share the same directory.
                    const matchedById = sid && s.session_id === sid;
                    if (matchedById && s.name !== state.currentSession.name) {
                        state.currentSession.name = s.name;
                    }
                    if (!matchedById && s.session_id && s.session_id !== state.currentSession.session_id) {
                        // Matched by name only — adopt new session_id
                        // (only safe when there's a single session with this name)
                        const sameNameCount = data.sessions.filter(x => x.name === state.currentSession.name).length;
                        if (sameNameCount === 1) {
                            state.currentSession.session_id = s.session_id;
                        }
                    }
                    // Sync display_name and update header
                    const headerName = s.display_name || s.name;
                    state.currentSession.display_name = s.display_name || null;
                    document.getElementById("session-name").textContent = headerName;
                    updateSessionStatus(s.status);
                    updateSessionSummary(s.summary);
                    updateSessionBranch(s.branch);
                    updateWaitingIndicator(s);
                    updateChangedFileCount(s.changed_file_count || 0);
                    // Update terminal header status dot
                    const termDot = document.getElementById('terminal-status-dot');
                    if (termDot) termDot.className = `terminal-status-dot ${s.working ? 'working' : s.waiting_for_input ? 'waiting' : s.sleeping ? 'sleeping' : s.done ? 'done' : 'stale'}`;
                }
            }
        }
    };

    state.coralWs.onclose = (ev) => {
        dbg('coralWs CLOSE', { code: ev.code, reason: ev.reason });
        setTimeout(connectCoralWs, 5000);
    };

    state.coralWs.onerror = (ev) => {
        dbg('coralWs ERROR', ev);
        // Will trigger onclose
    };
}
