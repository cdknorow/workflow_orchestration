"""Claude Fleet Dashboard — monitors parallel AI coding agents via log files."""

from __future__ import annotations

import asyncio
import re
from datetime import datetime
from glob import glob
from pathlib import Path

from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Vertical
from textual.reactive import reactive
from textual.widgets import Footer, Header, Label, Static

LOG_PATTERN = "/tmp/claude_fleet_*.log"
STATUS_RE = re.compile(r"\|\|STATUS:\s*(.+?)\|\|")
SUMMARY_RE = re.compile(r"\|\|SUMMARY:\s*(.+?)\|\|")
ANSI_RE = re.compile(r"\x1b(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\))")


def strip_ansi(text: str) -> str:
    """Remove ANSI escape sequences from terminal output."""
    return ANSI_RE.sub("", text)


def discover_agents() -> list[tuple[str, Path]]:
    """Return (agent_name, log_path) pairs sorted by name."""
    results = []
    for log_path in sorted(glob(LOG_PATTERN)):
        p = Path(log_path)
        name = p.stem.removeprefix("claude_fleet_")
        results.append((name, p))
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
                        self._timestamped.append((ts, entry.strip()))
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
                self.status_text = matches[-1].strip()
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
                self.summary_text = matches[-1]
        except OSError:
            pass

    def watch_summary_text(self, value: str) -> None:
        if value:
            self.display = True
            self.update(f"[bold]Goal:[/bold] {value}")
        else:
            self.display = False


class AgentCard(Vertical):
    """One card per agent: header, status, log tail, tasks."""

    def __init__(self, agent_name: str, log_path: Path, **kwargs) -> None:
        super().__init__(**kwargs)
        self.agent_name = agent_name
        self.log_path = log_path

    def compose(self) -> ComposeResult:
        yield Label(f" {self.agent_name.upper()} ", classes="agent-header")
        yield SummaryBox(self.log_path, classes="summary-box")   # hidden until agent emits a summary
        yield StatusBox(self.log_path, classes="status-box")
        yield Label("History", classes="section-label")
        yield LogTail(self.log_path, classes="log-tail")


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
        margin-bottom: 1;
    }

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

    TITLE = "Claude Fleet Dashboard"
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
                for name, log_path in agents:
                    self._known_agents.add(name)
                    yield AgentCard(name, log_path, classes="agent-card")
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
        current_names = {name for name, _ in current}
        new_agents = [(name, path) for name, path in current if name not in self._known_agents]

        if not new_agents:
            return

        grid = self.query_one("#agents-grid")

        # Remove the "no agents" placeholder if present
        try:
            placeholder = self.query_one("#no-agents-placeholder", Static)
            await placeholder.remove()
        except Exception:
            pass

        for name, log_path in new_agents:
            self._known_agents.add(name)
            await grid.mount(AgentCard(name, log_path, classes="agent-card"))

        # Update grid columns
        cols = min(len(self._known_agents), 3)
        grid.styles.grid_size_columns = cols

        # Update subtitle
        n = len(self._known_agents)
        self.sub_title = f"{n} agent(s) active  •  q=quit  r=refresh"

    async def action_refresh_agents(self) -> None:
        """Manually trigger agent discovery."""
        await self._auto_refresh_agents()


if __name__ == "__main__":
    FleetDashboard().run()
