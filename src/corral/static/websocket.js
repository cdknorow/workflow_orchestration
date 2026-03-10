/* WebSocket connection for real-time corral updates */

import { state } from './state.js';
import { renderLiveSessions, updateSessionStatus, updateSessionSummary, updateSessionBranch, updateWaitingIndicator } from './render.js';
import { renderLiveJobs } from './live_jobs.js';

export function connectCorralWs() {
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    const url = `${proto}//${location.host}/ws/corral`;

    state.corralWs = new WebSocket(url);

    state.corralWs.onmessage = (event) => {
        const data = JSON.parse(event.data);
        if (data.type === "corral_update") {
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
                    // where session_id or name may change)
                    if (s.session_id && s.session_id !== state.currentSession.session_id) {
                        state.currentSession.session_id = s.session_id;
                    }
                    if (s.name !== state.currentSession.name) {
                        state.currentSession.name = s.name;
                    }
                    // Sync display_name and update header
                    const headerName = s.display_name || s.name;
                    state.currentSession.display_name = s.display_name || null;
                    document.getElementById("session-name").textContent = headerName;
                    updateSessionStatus(s.status);
                    updateSessionSummary(s.summary);
                    updateSessionBranch(s.branch);
                    updateWaitingIndicator(s.waiting_for_input, s.working);
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
