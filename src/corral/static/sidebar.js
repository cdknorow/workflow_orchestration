/* Sidebar drag-to-resize functionality */

import { fitTerminal } from './xterm_renderer.js';

export function initSidebarResize() {
    const handle = document.getElementById("sidebar-resize-handle");
    const sidebar = document.querySelector(".sidebar");

    let dragging = false;

    handle.addEventListener("mousedown", (e) => {
        e.preventDefault();
        dragging = true;
        handle.classList.add("dragging");
        document.body.style.cursor = "col-resize";
        document.body.style.userSelect = "none";
    });

    document.addEventListener("mousemove", (e) => {
        if (!dragging) return;
        const newWidth = Math.min(Math.max(e.clientX, 140), window.innerWidth * 0.5);
        sidebar.style.width = newWidth + "px";
    });

    document.addEventListener("mouseup", () => {
        if (!dragging) return;
        dragging = false;
        handle.classList.remove("dragging");
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
        fitTerminal();
    });
}

/* Task bar drag-to-resize functionality */

export function initTaskBarResize() {
    const handle = document.getElementById("task-bar-resize-handle");
    const taskBar = document.getElementById("agentic-state");
    const liveBody = document.querySelector(".live-body");

    if (!handle || !taskBar || !liveBody) return;

    let dragging = false;

    handle.addEventListener("mousedown", (e) => {
        e.preventDefault();
        dragging = true;
        handle.classList.add("dragging");
        document.body.style.cursor = "col-resize";
        document.body.style.userSelect = "none";
    });

    document.addEventListener("mousemove", (e) => {
        if (!dragging) return;
        const rect = liveBody.getBoundingClientRect();
        const newWidth = rect.right - e.clientX;
        const clamped = Math.min(Math.max(newWidth, 180), 480);
        taskBar.style.width = clamped + "px";
    });

    document.addEventListener("mouseup", () => {
        if (!dragging) return;
        dragging = false;
        handle.classList.remove("dragging");
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
        fitTerminal();
    });
}

/* Command pane drag-to-resize functionality */

export function initCommandPaneResize() {
    const handle = document.getElementById("command-pane-resize-handle");
    const pane = document.getElementById("command-pane");
    const column = document.querySelector(".live-left-column");

    let dragging = false;

    handle.addEventListener("mousedown", (e) => {
        e.preventDefault();
        dragging = true;
        handle.classList.add("dragging");
        document.body.style.cursor = "row-resize";
        document.body.style.userSelect = "none";
    });

    document.addEventListener("mousemove", (e) => {
        if (!dragging) return;
        const container = column || document.body;
        const rect = container.getBoundingClientRect();
        const newHeight = rect.bottom - e.clientY;
        const clamped = Math.min(Math.max(newHeight, 80), rect.height * 0.6);
        pane.style.height = clamped + "px";
    });

    document.addEventListener("mouseup", () => {
        if (!dragging) return;
        dragging = false;
        handle.classList.remove("dragging");
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
        fitTerminal();
    });
}

/* Collapsible sidebar sections */

const STORAGE_KEY = 'corral-sidebar-collapsed';

function getCollapsedState() {
    try {
        return JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}');
    } catch {
        return {};
    }
}

function saveCollapsedState(state) {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
}

export function initSidebarCollapse() {
    const sections = document.querySelectorAll('.sidebar-section[data-section]');
    const saved = getCollapsedState();

    for (const section of sections) {
        const sectionId = section.dataset.section;
        const header = section.querySelector('[data-collapse-toggle]');
        if (!header) continue;

        // Restore saved state
        if (saved[sectionId]) {
            section.classList.add('collapsed');
            section.dataset.manualCollapse = 'true';
        }

        header.addEventListener('click', (e) => {
            // Don't toggle if clicking a button inside the header
            if (e.target.closest('button')) return;

            const isCollapsed = section.classList.toggle('collapsed');
            section.dataset.manualCollapse = isCollapsed ? 'true' : '';
            if (!isCollapsed) {
                section.dataset.manualExpand = 'true';
            } else {
                delete section.dataset.manualExpand;
            }

            const state = getCollapsedState();
            state[sectionId] = isCollapsed;
            saveCollapsedState(state);
        });

        // Add aria-expanded
        header.setAttribute('role', 'button');
        header.setAttribute('aria-expanded', !section.classList.contains('collapsed'));
    }
}

export function updateSectionVisibility(sectionId, itemCount) {
    const section = document.querySelector(`[data-section="${sectionId}"]`);
    if (!section) return;

    const badge = section.querySelector('.section-count-badge');
    if (badge) badge.textContent = itemCount;

    // Update aria-expanded
    const header = section.querySelector('[data-collapse-toggle]');
    if (header) {
        header.setAttribute('aria-expanded', !section.classList.contains('collapsed'));
    }

    // Auto-collapse empty sections (unless user manually expanded)
    if (itemCount === 0 && !section.dataset.manualExpand) {
        section.classList.add('collapsed');
    } else if (itemCount > 0 && !section.dataset.manualCollapse) {
        section.classList.remove('collapsed');
    }
}

/* Jobs subtab switching */

export function switchJobsSubtab(tab) {
    // Update subtab buttons
    document.querySelectorAll('.sidebar-subtab').forEach(btn => {
        btn.classList.toggle('active', btn.dataset.subtab === tab);
    });

    // Show/hide content
    const activeContent = document.getElementById('jobs-subtab-active');
    const scheduledContent = document.getElementById('jobs-subtab-scheduled');
    if (activeContent) activeContent.style.display = tab === 'active' ? '' : 'none';
    if (scheduledContent) scheduledContent.style.display = tab === 'scheduled' ? '' : 'none';
}

/* Agentic state panel collapse */

export function initAgenticPanelCollapse() {
    const panel = document.getElementById('agentic-state');
    if (!panel) return;

    const btn = document.getElementById('agentic-collapse-btn');
    if (!btn) return;

    // Restore state
    const collapsed = localStorage.getItem('corral-agentic-collapsed') === 'true';
    if (collapsed) panel.classList.add('collapsed');

    btn.addEventListener('click', () => {
        const isCollapsed = panel.classList.toggle('collapsed');
        localStorage.setItem('corral-agentic-collapsed', isCollapsed);
        fitTerminal();
    });
}
