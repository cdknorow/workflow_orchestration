/* Mobile navigation and view management */

import { state } from './state.js';

const MOBILE_BREAKPOINT = 767;

let _currentMobileTab = 'agents';

function isMobile() {
    return window.innerWidth <= MOBILE_BREAKPOINT;
}

// ── Tablet Sidebar Toggle ─────────────────────────────────────────────────

function toggleTabletSidebar() {
    const sidebar = document.querySelector('.sidebar');
    const backdrop = document.querySelector('.tablet-sidebar-backdrop');
    if (!sidebar) return;

    const isOpen = sidebar.classList.toggle('tablet-open');
    if (backdrop) backdrop.classList.toggle('active', isOpen);
}
window.toggleTabletSidebar = toggleTabletSidebar;

// ── Bottom Tab Bar Navigation ─────────────────────────────────────────────

function switchMobileTab(tab) {
    _currentMobileTab = tab;

    // Restore tab bar visibility when switching tabs
    const tabBar = document.querySelector('.mobile-tab-bar');
    if (tabBar) tabBar.style.display = 'flex';

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

    // Hide agentic panel overlay when leaving panel tab
    const agenticState = document.getElementById('agentic-state');
    if (agenticState) agenticState.classList.remove('mobile-panel-overlay');

    switch (tab) {
        case 'agents':
            if (agentList) agentList.style.display = 'flex';
            break;
        case 'history':
            // Show the sidebar history section as a full-screen view
            _showMobileHistory(agentList);
            break;
        case 'jobs':
            if (schedulerView) {
                schedulerView.style.display = 'flex';
            }
            break;
        case 'panel':
            // Show agentic panel as full-screen overlay
            if (agenticState) {
                agenticState.classList.add('mobile-panel-overlay');
            }
            // Also show the live session view underneath (for context)
            if (liveView) liveView.style.display = 'flex';
            break;
        case 'settings':
            if (window.showSettingsModal) {
                window.showSettingsModal();
            }
            // Re-activate the agents tab (settings is a modal, not a view)
            _currentMobileTab = 'agents';
            document.querySelectorAll('.mobile-tab').forEach(t => {
                t.classList.toggle('active', t.dataset.tab === 'agents');
            });
            if (agentList) agentList.style.display = 'flex';
            break;
    }
}
window.switchMobileTab = switchMobileTab;

function _showMobileHistory(agentList) {
    if (!agentList) return;
    agentList.style.display = 'flex';
    agentList.dataset.mode = 'history';
    agentList.innerHTML = '';

    // Header
    const header = document.createElement('div');
    header.style.cssText = 'padding:12px 16px;';
    header.innerHTML = '<h2 style="font-size:16px;font-weight:600;color:var(--text-primary);margin:0">Session History</h2>';
    agentList.appendChild(header);

    // Move the sidebar's history section body content into mobile view
    // We reference the actual sidebar elements so search/filters work
    const historySection = document.querySelector('[data-section="history"]');
    if (historySection) {
        const body = historySection.querySelector('.sidebar-section-body');
        if (body) {
            // Clone to avoid removing from sidebar
            const clone = body.cloneNode(true);
            clone.style.cssText = 'padding:0 12px;overflow-y:auto;flex:1;';

            // Re-wire history item clicks
            clone.querySelectorAll('.session-list li').forEach(li => {
                const onclick = li.getAttribute('onclick');
                if (onclick) {
                    li.removeAttribute('onclick');
                    li.addEventListener('click', () => { eval(onclick); });
                }
            });

            // Re-wire search input
            const searchInput = clone.querySelector('input[type="search"]');
            if (searchInput) {
                const origInput = document.getElementById('history-search');
                if (origInput) {
                    searchInput.addEventListener('input', () => {
                        origInput.value = searchInput.value;
                        origInput.dispatchEvent(new Event('input'));
                    });
                }
            }

            agentList.appendChild(clone);
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

    // Hide panel overlay and panel tab
    const agenticState = document.getElementById('agentic-state');
    if (agenticState) agenticState.classList.remove('mobile-panel-overlay');
    const panelTab = document.getElementById('mobile-tab-panel');
    if (panelTab) panelTab.style.display = 'none';

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
    const sidebarList = document.getElementById('live-sessions-list');
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

        // On mobile, hide agent list and tab bar to show full session view
        if (isMobile()) {
            const agentList = document.getElementById('mobile-agent-list');
            if (agentList) agentList.style.display = 'none';
            const tabBar = document.querySelector('.mobile-tab-bar');
            if (tabBar) tabBar.style.display = 'none';
            // Show the Panel tab now that a session is selected
            const panelTab = document.getElementById('mobile-tab-panel');
            if (panelTab) panelTab.style.display = '';
        }

        // On tablet, close the sidebar overlay
        const sidebar = document.querySelector('.sidebar');
        const backdrop = document.querySelector('.tablet-sidebar-backdrop');
        if (sidebar) sidebar.classList.remove('tablet-open');
        if (backdrop) backdrop.classList.remove('active');
    };
}

// ── Pull-to-Refresh ───────────────────────────────────────────────────────

function _initPullToRefresh() {
    const agentList = document.getElementById('mobile-agent-list');
    if (!agentList) return;

    let startY = 0;
    let pulling = false;
    let refreshIndicator = null;

    agentList.addEventListener('touchstart', (e) => {
        if (agentList.scrollTop === 0) {
            startY = e.touches[0].clientY;
            pulling = true;
        }
    }, { passive: true });

    agentList.addEventListener('touchmove', (e) => {
        if (!pulling) return;
        const dy = e.touches[0].clientY - startY;
        if (dy > 10 && agentList.scrollTop === 0) {
            if (!refreshIndicator) {
                refreshIndicator = document.createElement('div');
                refreshIndicator.className = 'pull-refresh-indicator';
                refreshIndicator.textContent = 'Pull to refresh...';
                agentList.insertBefore(refreshIndicator, agentList.firstChild);
            }
            const progress = Math.min(dy / 80, 1);
            refreshIndicator.style.height = Math.min(dy * 0.5, 50) + 'px';
            refreshIndicator.style.opacity = progress;
            if (dy > 80) {
                refreshIndicator.textContent = 'Release to refresh';
            } else {
                refreshIndicator.textContent = 'Pull to refresh...';
            }
        }
    }, { passive: true });

    agentList.addEventListener('touchend', (e) => {
        if (!pulling) return;
        pulling = false;
        const dy = (e.changedTouches[0]?.clientY || 0) - startY;
        if (dy > 80 && refreshIndicator) {
            refreshIndicator.textContent = 'Refreshing...';
            // Trigger session reload
            if (window._coralLoadLiveSessions) {
                window._coralLoadLiveSessions();
            }
            setTimeout(() => {
                if (refreshIndicator && refreshIndicator.parentNode) {
                    refreshIndicator.remove();
                }
                refreshIndicator = null;
            }, 1000);
        } else {
            if (refreshIndicator && refreshIndicator.parentNode) {
                refreshIndicator.remove();
            }
            refreshIndicator = null;
        }
    }, { passive: true });
}

// ── Swipe Between Agents ──────────────────────────────────────────────────

function _initSwipeNavigation() {
    let touchStartX = 0;
    let touchStartY = 0;

    document.addEventListener('touchstart', (e) => {
        if (!isMobile()) return;
        touchStartX = e.touches[0].clientX;
        touchStartY = e.touches[0].clientY;
    }, { passive: true });

    document.addEventListener('touchend', (e) => {
        if (!isMobile()) return;

        const dx = e.changedTouches[0].clientX - touchStartX;
        const dy = e.changedTouches[0].clientY - touchStartY;

        // Only detect horizontal swipes (dx much larger than dy)
        if (Math.abs(dx) < 60 || Math.abs(dy) > Math.abs(dx) * 0.7) return;

        // Only swipe when viewing a live session
        const liveView = document.getElementById('live-session-view');
        if (!liveView || liveView.style.display === 'none') return;

        const sessions = state.liveSessions || [];
        if (sessions.length < 2) return;

        const currentId = state.currentSession?.session_id;
        const currentIdx = sessions.findIndex(s => s.session_id === currentId);
        if (currentIdx < 0) return;

        let nextIdx;
        if (dx > 0) {
            // Swipe right → previous agent
            nextIdx = currentIdx > 0 ? currentIdx - 1 : sessions.length - 1;
        } else {
            // Swipe left → next agent
            nextIdx = currentIdx < sessions.length - 1 ? currentIdx + 1 : 0;
        }

        const next = sessions[nextIdx];
        if (next && window.selectLiveSession) {
            window.selectLiveSession(next.name, next.agent_type, next.session_id);
        }
    }, { passive: true });
}

// ── Virtual Keyboard Detection ────────────────────────────────────────────

function _initKeyboardDetection() {
    if (!window.visualViewport) return;

    // Track the initial viewport height to detect keyboard open/close
    let initialHeight = window.visualViewport.height;

    window.visualViewport.addEventListener('resize', () => {
        if (!isMobile()) return;

        // Keyboard is open when viewport shrinks significantly (>100px)
        const heightDiff = initialHeight - window.visualViewport.height;
        const keyboardOpen = heightDiff > 100;

        document.body.classList.toggle('keyboard-open', keyboardOpen);
    });

    // Update initial height on orientation change
    window.addEventListener('orientationchange', () => {
        setTimeout(() => {
            initialHeight = window.visualViewport.height;
        }, 300);
    });
}

// ── Initialize Mobile ─────────────────────────────────────────────────────

export function initMobile() {
    // Set default tab
    if (isMobile()) {
        switchMobileTab('agents');
    }

    // Wrap selectLiveSession for mobile navigation
    wrapSelectLiveSession();

    // Initialize touch gestures
    _initPullToRefresh();
    _initSwipeNavigation();

    // Detect virtual keyboard for hiding tab bar
    _initKeyboardDetection();

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
