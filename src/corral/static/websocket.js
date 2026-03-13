/* WebSocket connection for real-time corral updates */

import { state } from './state.js';
import { renderLiveSessions, updateSessionStatus, updateSessionSummary, updateSessionBranch, updateWaitingIndicator } from './render.js';
import { renderLiveJobs } from './live_jobs.js';
import { updateChangedFileCount } from './changed_files.js';
import { updateSectionVisibility } from './sidebar.js';
import { showNotificationToast, escapeHtml } from './utils.js';

export function connectCorralWs() {
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    const url = `${proto}//${location.host}/ws/corral`;

    state.corralWs = new WebSocket(url);

    state.corralWs.onmessage = (event) => {
        const data = JSON.parse(event.data);
        if (data.type === "corral_update") {
            // Detect sessions that just transitioned to "needs input"
            for (const s of data.sessions) {
                const id = s.session_id || s.name;
                const wasWaiting = state.prevWaitingState[id];
                const notifyEnabled = state.settings.notify_needs_input !== false;
                if (notifyEnabled && s.waiting_for_input && s.waiting_reason !== "stop" && !wasWaiting) {
                    const label = escapeHtml(s.display_name || s.name);
                    const detail = s.waiting_summary ? escapeHtml(s.waiting_summary) : null;
                    const sessionName = s.name;
                    const agentType = s.agent_type;
                    const sessionId = s.session_id;
                    showNotificationToast(label, detail, () => {
                        import('./sessions.js').then(m => m.selectLiveSession(sessionName, agentType, sessionId));
                    });
                }
                state.prevWaitingState[id] = s.waiting_for_input && s.waiting_reason !== "stop";
            }

            state.liveSessions = data.sessions;
            renderLiveSessions(data.sessions);

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
                    updateWaitingIndicator(s.waiting_for_input, s.working, s.waiting_reason);
                    updateChangedFileCount(s.changed_file_count || 0);
                }
            }
        }
    };

    state.corralWs.onclose = () => {
        setTimeout(connectCorralWs, 5000);
    };

    state.corralWs.onerror = () => {
        // Will trigger onclose
    };
}
