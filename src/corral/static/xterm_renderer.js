/* xterm.js terminal renderer — streams raw ANSI output via WebSocket */

import { state } from './state.js';

let terminal = null;
let fitAddon = null;
let terminalWs = null;
let _selectionDisposable = null;

// Buffered content for when updates are paused due to text selection
let _pendingContent = null;
let _xtermSelecting = false;

// Track which session_id the terminal WS is currently connected to,
// and a generation counter to suppress stale onclose reconnects.
let _connectedSessionId = null;
let _wsGeneration = 0;

function _setPauseBadge(visible) {
    const badge = document.getElementById("selection-pause-badge");
    if (badge) badge.style.display = visible ? "" : "none";
}

/** Write buffered content to the terminal (called when selection clears). */
function _flushPending() {
    if (_pendingContent !== null && terminal) {
        terminal.reset();
        terminal.write(_pendingContent.replace(/\n/g, '\r\n'));
        if (state.autoScroll) {
            terminal.scrollToBottom();
        }
        _pendingContent = null;
    }
}

export function createTerminal(containerEl) {
    if (terminal) {
        disposeTerminal();
    }

    if (typeof Terminal === 'undefined') {
        console.warn('xterm.js not loaded, falling back to semantic renderer');
        return null;
    }

    terminal = new Terminal({
        cursorBlink: false,
        cursorStyle: 'block',
        disableStdin: true,
        scrollback: 1000,
        fontSize: 13,
        fontFamily: "'SF Mono', 'Fira Code', 'Cascadia Code', Menlo, monospace",
        theme: {
            background: '#0d1117',
            foreground: '#e6edf3',
            cursor: '#e6edf3',
            selectionBackground: '#264f78',
            black: '#484f58',
            red: '#f85149',
            green: '#3fb950',
            yellow: '#d29922',
            blue: '#58a6ff',
            magenta: '#bc8cff',
            cyan: '#39d2c0',
            white: '#e6edf3',
            brightBlack: '#6e7681',
            brightRed: '#ffa198',
            brightGreen: '#56d364',
            brightYellow: '#e3b341',
            brightBlue: '#79c0ff',
            brightMagenta: '#d2a8ff',
            brightCyan: '#56d4dd',
            brightWhite: '#f0f6fc',
        },
    });

    fitAddon = new FitAddon.FitAddon();
    terminal.loadAddon(fitAddon);

    if (typeof WebLinksAddon !== 'undefined') {
        const webLinksAddon = new WebLinksAddon.WebLinksAddon();
        terminal.loadAddon(webLinksAddon);
    }

    // Track selection state: pause updates while user has text selected
    _selectionDisposable = terminal.onSelectionChange(() => {
        const hasSelection = terminal.hasSelection();
        _xtermSelecting = hasSelection;
        state.isSelecting = hasSelection;
        _setPauseBadge(hasSelection);
        if (!hasSelection) {
            _flushPending();
        }
    });

    terminal.open(containerEl);
    fitAddon.fit();

    return terminal;
}

export function connectTerminalWs(name, agentType, sessionId) {
    // Skip if already connected to this exact session
    if (_connectedSessionId === sessionId && terminalWs && terminalWs.readyState === WebSocket.OPEN) {
        return;
    }

    disconnectTerminalWs();

    // Bump generation so any pending onclose from the old WS is suppressed
    const myGeneration = ++_wsGeneration;
    _connectedSessionId = sessionId;

    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    const params = new URLSearchParams();
    if (agentType) params.set("agent_type", agentType);
    if (sessionId) params.set("session_id", sessionId);
    const qs = params.toString() ? `?${params}` : "";

    terminalWs = new WebSocket(
        `${proto}//${location.host}/ws/terminal/${encodeURIComponent(name)}${qs}`
    );

    terminalWs.onmessage = (event) => {
        const data = JSON.parse(event.data);
        if (data.type === "terminal_update" && terminal) {
            // Buffer the update if user has text selected
            if (_xtermSelecting) {
                _pendingContent = data.content;
                return;
            }
            terminal.reset();
            // tmux outputs \n but xterm.js needs \r\n for proper line breaks
            terminal.write(data.content.replace(/\n/g, '\r\n'));
            if (state.autoScroll) {
                terminal.scrollToBottom();
            }
        }
    };

    terminalWs.onclose = () => {
        // Only reconnect if this WS is still the current generation.
        // If disconnectTerminalWs() was called (intentional close) or
        // connectTerminalWs() was called for a different session, the
        // generation will have been bumped and we should NOT reconnect.
        if (myGeneration !== _wsGeneration) return;

        if (state.currentSession && state.currentSession.type === "live"
            && state.currentSession.session_id === sessionId) {
            setTimeout(() => {
                // Re-check: generation still current AND session still matches
                if (myGeneration === _wsGeneration
                    && state.currentSession
                    && state.currentSession.session_id === sessionId) {
                    connectTerminalWs(
                        state.currentSession.name,
                        state.currentSession.agent_type,
                        state.currentSession.session_id,
                    );
                }
            }, 3000);
        }
    };
}

export function disconnectTerminalWs() {
    // Bump generation BEFORE closing so the old onclose handler is suppressed
    _wsGeneration++;
    _connectedSessionId = null;
    if (terminalWs) {
        terminalWs.close();
        terminalWs = null;
    }
}

export function disposeTerminal() {
    disconnectTerminalWs();
    if (_selectionDisposable) {
        _selectionDisposable.dispose();
        _selectionDisposable = null;
    }
    _xtermSelecting = false;
    _pendingContent = null;
    if (terminal) {
        terminal.dispose();
        terminal = null;
        fitAddon = null;
    }
}

export function fitTerminal() {
    if (fitAddon) {
        fitAddon.fit();
    }
}

export function getTerminal() {
    return terminal;
}
