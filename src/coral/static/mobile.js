/* Mobile navigation and view management */

import { state } from './state.js';

const MOBILE_BREAKPOINT = 767;

let _currentMobileTab = 'agents';

function isMobile() {
    return window.innerWidth <= MOBILE_BREAKPOINT;
}

// ── Bottom Tab Bar Navigation ─────────────────────────────────────────────

function switchMobileTab(tab) {
    _currentMobileTab = tab;

    // Update tab active states
    document.querySelectorAll('.mobile-tab').forEach(t => {
        t.classList.toggle('active', t.dataset.tab === tab);
    });

    // Hide all mobile-level views
    const agentList = document.getElementById('mobile-agent-list');
    const welcomeScreen = document.getElementById('welcome-screen');
    const liveView = document.getElementById('live-session-view');
    const historyView = document.getElementById('history-session-view');
    const boardView = document.getElementById('messageboard-view');
    const schedulerView = document.getElementById('scheduler-view');

    if (agentList) agentList.style.display = 'none';
    if (welcomeScreen) welcomeScreen.style.display = 'none';
    if (liveView) liveView.style.display = 'none';
    if (historyView) historyView.style.display = 'none';
    if (boardView) boardView.style.display = 'none';
    if (schedulerView) schedulerView.style.display = 'none';

    switch (tab) {
        case 'agents':
            if (agentList) agentList.style.display = 'flex';
            break;
        case 'board':
            if (boardView) {
                boardView.style.display = 'flex';
                // Trigger board project list load if needed
                if (window.showMessageBoardProjects) {
                    window.showMessageBoardProjects();
                }
            }
            break;
        case 'history':
            // Show the history section from sidebar as a full-screen list
            if (agentList) {
                agentList.style.display = 'flex';
                agentList.dataset.mode = 'history';
            }
            _showMobileHistory();
            break;
        case 'jobs':
            if (schedulerView) {
                schedulerView.style.display = 'flex';
            }
            break;
        case 'settings':
            if (window.showSettingsModal) {
                window.showSettingsModal();
            }
            // Show welcome screen as fallback
            if (welcomeScreen) welcomeScreen.style.display = 'flex';
            break;
    }
}
window.switchMobileTab = switchMobileTab;

function _showMobileHistory() {
    const agentList = document.getElementById('mobile-agent-list');
    if (!agentList) return;

    // Copy the history sidebar content into mobile view
    const historyBody = document.querySelector('#sidebar-history .sidebar-section-body');
    if (historyBody) {
        agentList.innerHTML = '';
        // Clone history search/filters
        const filters = document.querySelector('.history-filters');
        if (filters) agentList.appendChild(filters.cloneNode(true));
        // Clone history session list
        const histList = document.getElementById('history-list');
        if (histList) {
            const clone = histList.cloneNode(true);
            clone.id = 'mobile-history-list';
            agentList.appendChild(clone);
            // Wire up click handlers on cloned items
            clone.querySelectorAll('li').forEach(li => {
                const sessionId = li.dataset.sessionId;
                if (sessionId) {
                    li.addEventListener('click', () => {
                        if (window.selectHistorySession) {
                            window.selectHistorySession(sessionId);
                        }
                    });
                }
            });
        }
    }
}

// ── Mobile Back Navigation ────────────────────────────────────────────────

function mobileBack() {
    if (!isMobile()) return;

    // Go back to agent list
    const liveView = document.getElementById('live-session-view');
    const historyView = document.getElementById('history-session-view');
    const agentList = document.getElementById('mobile-agent-list');

    if (liveView) liveView.style.display = 'none';
    if (historyView) historyView.style.display = 'none';
    if (agentList) agentList.style.display = 'flex';

    _currentMobileTab = 'agents';
    document.querySelectorAll('.mobile-tab').forEach(t => {
        t.classList.toggle('active', t.dataset.tab === 'agents');
    });
}
window.mobileBack = mobileBack;

// ── Sync Agent List to Mobile View ────────────────────────────────────────

export function syncMobileAgentList() {
    if (!isMobile()) return;

    const agentList = document.getElementById('mobile-agent-list');
    if (!agentList || agentList.dataset.mode === 'history') return;

    // Copy the live sessions list from sidebar
    const sidebarList = document.getElementById('live-session-list');
    if (sidebarList && agentList) {
        // Clear and clone
        agentList.innerHTML = '';

        // Add header with New button
        const header = document.createElement('div');
        header.style.cssText = 'display:flex;justify-content:space-between;align-items:center;padding:12px 16px;';
        header.innerHTML = `
            <h2 style="font-size:16px;font-weight:600;color:var(--text-primary);margin:0">Live Sessions</h2>
            <button class="btn btn-small btn-primary" onclick="showLaunchModal()">+ New</button>
        `;
        agentList.appendChild(header);

        const clone = sidebarList.cloneNode(true);
        clone.id = 'mobile-session-list';

        // Re-wire click handlers on cloned items
        clone.querySelectorAll('.session-group-item').forEach(item => {
            const onclick = item.getAttribute('onclick');
            if (onclick) {
                item.removeAttribute('onclick');
                item.addEventListener('click', () => {
                    // Execute the original onclick
                    eval(onclick);
                });
            }
        });

        // Re-wire group collapse handlers
        clone.querySelectorAll('.session-group-header').forEach(item => {
            const onclick = item.getAttribute('onclick');
            if (onclick) {
                item.removeAttribute('onclick');
                item.addEventListener('click', () => {
                    eval(onclick);
                });
            }
        });

        agentList.appendChild(clone);
    }
}

// ── Intercept Session Selection on Mobile ─────────────────────────────────

const _origSelectLiveSession = window.selectLiveSession;
export function wrapSelectLiveSession() {
    const orig = window.selectLiveSession;
    if (!orig) return;

    window.selectLiveSession = function(name, agentType, sessionId) {
        // Call original
        orig(name, agentType, sessionId);

        // On mobile, hide agent list and show the session view
        if (isMobile()) {
            const agentList = document.getElementById('mobile-agent-list');
            if (agentList) agentList.style.display = 'none';
        }
    };
}

// ── Initialize Mobile ─────────────────────────────────────────────────────

export function initMobile() {
    // Set default tab
    if (isMobile()) {
        switchMobileTab('agents');
    }

    // Wrap selectLiveSession for mobile navigation
    wrapSelectLiveSession();

    // Listen for resize to toggle mobile/desktop
    window.addEventListener('resize', () => {
        const tabBar = document.querySelector('.mobile-tab-bar');
        if (!tabBar) return;

        if (isMobile()) {
            tabBar.style.display = 'flex';
        } else {
            tabBar.style.display = 'none';
            // Restore sidebar visibility
            const sidebar = document.querySelector('.sidebar');
            const handle = document.querySelector('.sidebar-resize-handle');
            if (sidebar) sidebar.style.display = '';
            if (handle) handle.style.display = '';
        }
    });
}
