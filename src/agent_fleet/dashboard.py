"""Multi-Agent Fleet Dashboard — monitors parallel AI coding agents via log files."""

from __future__ import annotations

import asyncio
import re
import time
from datetime import datetime
from glob import glob
from pathlib import Path

from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Vertical, Horizontal
from textual.reactive import reactive
from textual.widgets import Footer, Header, Label, Static, Input, Button

LOG_PATTERN = "/tmp/*_fleet_*.log"
STATUS_RE = re.compile(r"\|\|STATUS:\s*(.+?)\|\|")
SUMMARY_RE = re.compile(r"\|\|SUMMARY:\s*(.+?)\|\|")
ANSI_RE = re.compile(r"\x1b(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\))")

# Model-specific commands
COMMAND_MAP = {
    "claude": {
        "compress": "/compact",
        "clear": "/clear",
    },
    "gemini": {
        "compress": "/compress",
        "clear": "/clear",
    }
}


def strip_ansi(text: str) -> str:
    """Remove ANSI escape sequences from terminal output.

    Replaces each sequence with a space so that cursor-movement codes
    (e.g. ESC[C used instead of literal spaces) don't merge adjacent words.
    """
    return ANSI_RE.sub(" ", text)


def clean_match(text: str) -> str:
    """Normalize whitespace in a regex match extracted from terminal output.

    Collapses runs of spaces (introduced when ANSI sequences are replaced
    with spaces) into a single space and strips leading/trailing whitespace.
    """
    return " ".join(text.split())


def discover_agents() -> list[tuple[str, str, Path]]:
    """Return (agent_type, agent_name, log_path) tuples sorted by name."""
    results = []
    for log_path in sorted(glob(LOG_PATTERN)):
        p = Path(log_path)
        # name will be e.g. "worktree1" from "claude_fleet_worktree1.log"
        # we extract the part before 'fleet_' as agent_type and after as agent_name
        match = re.search(r'([^_]+)_fleet_(.+)', p.stem)
        if match:
            agent_type = match.group(1)
            agent_name = match.group(2)
            results.append((agent_type, agent_name, p))
    return results


def _status_css_class(status: str) -> str:
    """Return a CSS class name based on the status text keywords."""
    lower = status.lower()
    if any(k in lower for k in ("complete", "done", "finished", "success")):
        return "status-complete"
    if any(k in lower for k in ("error", "fail", "failed", "exception", "crash")):
        return "status-error"
    if any(k in lower for k in ("waiting", "idle", "paused", "blocked")):
        return "status-waiting"
    return "status-active"


class LogTail(Static):
    """Displays a history of ||STATUS:|| lines from a log file with timestamps."""

    tail_text: reactive[str] = reactive("Waiting for output...")

    def __init__(self, log_path: Path, max_lines: int = 18, **kwargs) -> None:
        super().__init__(**kwargs)
        self.log_path = log_path
        self.max_lines = max_lines
        self._timestamped: list[tuple[str, str]] = []
        self._seen_count: int = 0

    def on_mount(self) -> None:
        self.set_interval(1.0, self._refresh_log)

    def _refresh_log(self) -> None:
        try:
            raw = self.log_path.read_text(errors="replace")
            text = strip_ansi(raw)
            matches = STATUS_RE.findall(text)
            if matches:
                if len(matches) > self._seen_count:
                    new_entries = matches[self._seen_count:]
                    ts = datetime.now().strftime("%H:%M:%S")
                    for entry in new_entries:
                        self._timestamped.append((ts, clean_match(entry)))
                    self._seen_count = len(matches)

                tail = self._timestamped[-self.max_lines:]
                lines = [f"[dim]{ts}[/dim]  {msg}" for ts, msg in tail]
                self.tail_text = "\n".join(lines)
            else:
                self.tail_text = "Waiting for output..."
        except OSError:
            self.tail_text = "Log file not found."

    def watch_tail_text(self, value: str) -> None:
        self.update(value)


class StatusBox(Static):
    """Displays the latest ||STATUS: ...|| from a log file with color coding."""

    status_text: reactive[str] = reactive("Idle")

    def __init__(self, log_path: Path, **kwargs) -> None:
        super().__init__(**kwargs)
        self.log_path = log_path

    def on_mount(self) -> None:
        self.add_class("status-waiting")
        self.set_interval(1.0, self._refresh_status)

    def _refresh_status(self) -> None:
        try:
            raw = self.log_path.read_text(errors="replace")
            text = strip_ansi(raw)
            matches = STATUS_RE.findall(text)
            if matches:
                self.status_text = clean_match(matches[-1])
        except OSError:
            pass

    def watch_status_text(self, value: str) -> None:
        # Remove all status classes, then add the appropriate one
        for cls in ("status-active", "status-complete", "status-error", "status-waiting"):
            self.remove_class(cls)
        self.add_class(_status_css_class(value))
        self.update(f"● {value}")


class SummaryBox(Static):
    """Displays the latest ||SUMMARY: ...|| from a log file.

    Hidden entirely (zero height) until an agent emits a summary.
    """

    summary_text: reactive[str] = reactive("")

    def __init__(self, log_path: Path, **kwargs) -> None:
        super().__init__(**kwargs)
        self.log_path = log_path

    def on_mount(self) -> None:
        self.display = False          # hidden until first summary arrives
        self.set_interval(1.0, self._refresh_summary)

    def _refresh_summary(self) -> None:
        try:
            raw = self.log_path.read_text(errors="replace")
            text = strip_ansi(raw)
            matches = SUMMARY_RE.findall(text)
            if matches:
                self.summary_text = clean_match(matches[-1])
        except OSError:
            pass

    def watch_summary_text(self, value: str) -> None:
        if value:
            self.display = True
            self.update(f"[bold]Goal:[/bold] {value}")
        else:
            self.display = False


class StalenessIndicator(Static):
    """Shows how recently the agent's log file was written to.

    Three states based on log file mtime:
      active  — written within the last 60 s   (green)
      recent  — written 1–5 minutes ago         (yellow)
      stale   — not written for > 5 minutes     (dim)
    """

    _ACTIVE_SECS = 60
    _RECENT_SECS = 300

    age_label: reactive[str] = reactive("")

    def __init__(self, log_path: Path, **kwargs) -> None:
        super().__init__(**kwargs)
        self.log_path = log_path

    def on_mount(self) -> None:
        self._refresh()
        self.set_interval(5.0, self._refresh)

    def _refresh(self) -> None:
        try:
            age = time.time() - self.log_path.stat().st_mtime
        except OSError:
            self._set_state("no log", "staleness-stale")
            return

        if age < self._ACTIVE_SECS:
            self._set_state("● active", "staleness-active")
        elif age < self._RECENT_SECS:
            mins = int(age // 60)
            self._set_state(f"● {mins}m ago", "staleness-recent")
        else:
            mins = int(age // 60)
            self._set_state(f"● {mins}m ago", "staleness-stale")

    def _set_state(self, label: str, css_class: str) -> None:
        for cls in ("staleness-active", "staleness-recent", "staleness-stale"):
            self.remove_class(cls)
        self.add_class(css_class)
        self.update(label)


class AgentCard(Vertical):
    """One card per agent: header, status, log tail, tasks."""

    def __init__(self, agent_type: str, agent_name: str, log_path: Path, **kwargs) -> None:
        super().__init__(**kwargs)
        self.agent_type = agent_type
        self.agent_name = agent_name
        self.log_path = log_path

    def compose(self) -> ComposeResult:
        agent_type_low = self.agent_type.lower()
        commands = COMMAND_MAP.get(agent_type_low, COMMAND_MAP["gemini"])
        compress_cmd = commands["compress"]
        clear_cmd = commands["clear"]

        yield Label(f"{self.agent_type.title()} | {self.agent_name.upper()} ", classes="agent-header")
        yield StalenessIndicator(self.log_path, classes="staleness-indicator")
        yield SummaryBox(self.log_path, classes="summary-box")   # hidden until agent emits a summary
        yield StatusBox(self.log_path, classes="status-box")
        yield Label("History", classes="section-label")
        yield LogTail(self.log_path, classes="log-tail")
        yield Label("Send command to agent", classes="section-label")
        yield Input(placeholder="Type here...", classes="command-input")
        with Horizontal(classes="command-buttons"):
            yield Button(compress_cmd, id="btn-compress", variant="primary")
            yield Button(clear_cmd, id="btn-clear", variant="warning")
            yield Button("Reset", id="btn-reset", variant="error")

    async def _send_to_tmux(self, command: str) -> str | None:
        """Send a command to the corresponding tmux pane. Returns error string if failed."""
        try:
            # list-panes -a lists all panes in all sessions
            # -F "#{pane_title}|#{session_name}|#S:#I.#P" gives us titles and target address
            proc = await asyncio.create_subprocess_exec(
                "tmux", "list-panes", "-a", "-F", "#{pane_title}|#{session_name}|#S:#I.#P",
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE
            )
            stdout, _ = await proc.communicate()
            lines = stdout.decode().splitlines()

            target = None
            # Normalize agent name for matching (e.g. worktree_2 -> worktree-2)
            norm_name = self.agent_name.replace("_", "-").lower()

            for line in lines:
                if "|" in line:
                    title, session, addr = line.split("|", 2)
                    title_low = title.lower()
                    session_low = session.lower()

                    # Match if agent name is in the title (set via OSC 2) or the session name
                    if (self.agent_name.lower() in title_low or
                        norm_name in title_low or
                        self.agent_name.lower() in session_low or
                        norm_name in session_low):
                        target = addr
                        break

            if target:
                # Send the command followed by Enter in a single call.
                await asyncio.create_subprocess_exec(
                        "tmux", "send-keys", "-t", target, command
                    )
                # 2. Give the CLI a fraction of a second to buffer the text
                await asyncio.sleep(0.1)
                # 3. Send the carriage return
                await asyncio.create_subprocess_exec(
                        "tmux", "send-keys", "-t", target, "C-m"
                    )
                return None
            else:
                return f"Pane '{self.agent_name}' not found"

        except Exception as e:
            return str(e)

    async def on_button_pressed(self, event: Button.Pressed) -> None:
        """Handle button clicks for quick commands."""
        agent_type_low = self.agent_type.lower()
        commands_map = COMMAND_MAP.get(agent_type_low, COMMAND_MAP["gemini"])
        compress_cmd = commands_map["compress"]
        clear_cmd = commands_map["clear"]

        commands_to_send = []
        if event.button.id == "btn-compress":
            commands_to_send = [compress_cmd]
        elif event.button.id == "btn-clear":
            commands_to_send = [clear_cmd]
        elif event.button.id == "btn-reset":
            commands_to_send = [compress_cmd, clear_cmd]

        if commands_to_send:
            input_widget = self.query_one(Input)
            for command in commands_to_send:
                error = await self._send_to_tmux(command)
                if error:
                    input_widget.placeholder = f"Error: {error}"
                    return
                # Short delay between commands
                if len(commands_to_send) > 1:
                    await asyncio.sleep(0.5)

            input_widget.placeholder = f"Sent: {' + '.join(commands_to_send)}"

    async def on_input_submitted(self, event: Input.Submitted) -> None:
        """Send the command to the corresponding tmux pane."""
        command = event.value.strip()
        if not command:
            return

        error = await self._send_to_tmux(command)
        if error:
            event.input.value = ""
            event.input.placeholder = f"Error: {error}"
        else:
            event.input.value = ""
            event.input.placeholder = f"Sent: {command}"


class FleetDashboard(App):
    CSS = """
    Screen {
        layout: vertical;
        padding: 1;
    }

    #agents-grid {
        layout: grid;
        grid-gutter: 1;
        width: 1fr;
        height: 100%;
    }

    .agent-card {
        border: solid $accent;
        padding: 1;
        height: 100%;
    }

    .agent-header {
        text-style: bold;
        color: $text;
        background: $accent;
        text-align: center;
        padding: 0 1;
        margin-bottom: 0;
    }

    .staleness-indicator {
        text-align: right;
        padding: 0 1;
        margin-bottom: 1;
        height: 1;
    }

    .staleness-active  { color: $success; }
    .staleness-recent  { color: $warning; }
    .staleness-stale   { color: $text-muted; }

    /* Status box color variants */
    .status-box {
        padding: 0 1;
        margin: 1 0;
        min-height: 1;
    }

    .status-box.status-active {
        border: dashed $accent;
        color: $accent;
    }

    .status-box.status-complete {
        border: dashed $success;
        color: $success;
    }

    .status-box.status-error {
        border: dashed $error;
        color: $error;
    }

    .status-box.status-waiting {
        border: dashed $warning;
        color: $warning;
    }

    .summary-box {
        padding: 0 1;
        margin: 0 0 1 0;
        border: solid $primary-darken-2;
        color: $text-muted;
        min-height: 1;
    }

    .log-tail {
        border: solid $surface-lighten-2;
        padding: 0 1;
        margin: 0 0 1 0;
        min-height: 8;
        max-height: 18;
        overflow-y: auto;
    }

    .section-label {
        text-style: bold;
        margin: 0 0 0 0;
    }

    .command-buttons {
        height: 3;
        margin: 1 0 0 0;
    }

    .command-buttons Button {
        margin-right: 2;
        min-width: 10;
        padding: 0;
    }

    .command-input {
        margin: 1 0 0 0;
        border: tall $accent;
    }

    .command-input:focus {
        border: tall $success;
    }

    .no-agents {
        color: $text-muted;
        text-align: center;
        padding: 2;
    }

    Footer {
        dock: bottom;
    }

    Header {
        dock: top;
    }
    """

    TITLE = "Multi-Agent Fleet Dashboard"
    BINDINGS = [
        Binding("q", "quit", "Quit"),
        Binding("r", "refresh_agents", "Refresh agents"),
    ]

    def __init__(self, **kwargs) -> None:
        super().__init__(**kwargs)
        self._known_agents: set[str] = set()

    def compose(self) -> ComposeResult:
        agents = discover_agents()

        yield Header()
        with Vertical(id="agents-grid"):
            if not agents:
                yield Static(
                    "No agent logs found. Run launch_fleet.sh first.\n"
                    f"Expected log files matching: {LOG_PATTERN}",
                    id="no-agents-placeholder",
                    classes="no-agents",
                )
            else:
                for agent_type, name, log_path in agents:
                    self._known_agents.add(str(log_path))
                    yield AgentCard(agent_type, name, log_path, classes="agent-card")
        yield Footer()

    def on_mount(self) -> None:
        agents = discover_agents()
        n = len(agents)
        self.sub_title = f"{n} agent(s) active  •  q=quit  r=refresh"
        if agents:
            cols = min(n, 3)
            self.query_one("#agents-grid").styles.grid_size_columns = cols
        self.set_interval(5.0, self._auto_refresh_agents)

    async def _auto_refresh_agents(self) -> None:
        """Discover new agents and mount their cards dynamically."""
        current = discover_agents()
        new_agents = [
            (atype, name, path)
            for atype, name, path in current
            if str(path) not in self._known_agents
        ]

        if not new_agents:
            return

        grid = self.query_one("#agents-grid")

        # Remove the "no agents" placeholder if present
        try:
            placeholder = self.query_one("#no-agents-placeholder", Static)
            await placeholder.remove()
        except Exception:
            pass

        for agent_type, name, log_path in new_agents:
            self._known_agents.add(str(log_path))
            await grid.mount(AgentCard(agent_type, name, log_path, classes="agent-card"))

        # Update grid columns
        cols = min(len(self._known_agents), 3)
        grid.styles.grid_size_columns = cols

        # Update subtitle
        n = len(self._known_agents)
        self.sub_title = f"{n} agent(s) active  •  q=quit  r=refresh"

    async def action_refresh_agents(self) -> None:
        """Manually trigger agent discovery."""
        await self._auto_refresh_agents()


def main():
    FleetDashboard().run()


if __name__ == "__main__":
    main()
