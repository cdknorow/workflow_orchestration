/* REST API fetch functions */

import { state } from './state.js';
import { renderLiveSessions, renderHistorySessions } from './render.js';
import { buildApiParams } from './search_filters.js';

export async function loadLiveSessions() {
    try {
        const resp = await fetch("/api/sessions/live");
        state.liveSessions = await resp.json();
        renderLiveSessions(state.liveSessions);
    } catch (e) {
        console.error("Failed to load live sessions:", e);
    }
}

export async function loadHistorySessions() {
    try {
        const resp = await fetch("/api/sessions/history");
        const data = await resp.json();
        // Handle new paginated response shape
        const sessions = data.sessions || data;
        renderHistorySessions(sessions, data.total, data.page, data.page_size);
    } catch (e) {
        console.error("Failed to load history sessions:", e);
    }
}

export async function loadHistorySessionsPaged(page = 1, pageSize = 50) {
    try {
        const params = buildApiParams(page, pageSize);
        const resp = await fetch(`/api/sessions/history?${params}`);
        const data = await resp.json();
        const sessions = data.sessions || data;
        renderHistorySessions(sessions, data.total, data.page, data.page_size);
        return data;
    } catch (e) {
        console.error("Failed to load paged history sessions:", e);
        return null;
    }
}

export async function loadLiveSessionDetail(name, agentType, sessionId) {
    try {
        const params = new URLSearchParams();
        if (agentType) params.set("agent_type", agentType);
        if (sessionId) params.set("session_id", sessionId);
        const qs = params.toString() ? `?${params}` : "";
        const resp = await fetch(`/api/sessions/live/${encodeURIComponent(name)}${qs}`);
        return await resp.json();
    } catch (e) {
        console.error("Failed to load session detail:", e);
        return null;
    }
}

export async function loadHistoryMessages(sessionId) {
    try {
        const resp = await fetch(`/api/sessions/history/${encodeURIComponent(sessionId)}`);
        return await resp.json();
    } catch (e) {
        console.error("Failed to load history messages:", e);
        return null;
    }
}
