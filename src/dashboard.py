"""Claude Fleet Dashboard — monitors parallel AI coding agents via log files."""

from __future__ import annotations

import asyncio
import json
import re
from glob import glob
from pathlib import Path

from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Horizontal, Vertical, VerticalScroll
from textual.reactive import reactive
from textual.widgets import Footer, Header, Input, Label, ListItem, ListView, Static

TASKS_FILE = Path("fleet_tasks.json")
LOG_PATTERN = "/tmp/claude_fleet_*.log"
SUMMARY_FILE = Path("/tmp/fleet_task_summary.txt")
STATUS_RE = re.compile(r"\|\|STATUS:\s*(.+?)\|\|")
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


def load_tasks() -> dict[str, list[str]]:
    if TASKS_FILE.exists():
        try:
            return json.loads(TASKS_FILE.read_text())
        except (json.JSONDecodeError, OSError):
            pass
    return {}


def save_tasks(data: dict[str, list[str]]) -> None:
    TASKS_FILE.write_text(json.dumps(data, indent=2))


class LogTail(Static):
    """Displays a history of ||STATUS:|| lines from a log file."""

    tail_text: reactive[str] = reactive("Waiting for output...")

    def __init__(self, log_path: Path, max_lines: int = 18, **kwargs) -> None:
        super().__init__(**kwargs)
        self.log_path = log_path
        self.max_lines = max_lines

    def on_mount(self) -> None:
        self.set_interval(1.0, self._refresh_log)

    def _refresh_log(self) -> None:
        try:
            raw = self.log_path.read_text(errors="replace")
            text = strip_ansi(raw)
            matches = STATUS_RE.findall(text)
            if matches:
                tail = matches[-self.max_lines :]
                self.tail_text = "\n".join(f"• {s.strip()}" for s in tail)
            else:
                self.tail_text = "Waiting for output..."
        except OSError:
            self.tail_text = "Log file not found."

    def watch_tail_text(self, value: str) -> None:
        self.update(value)


class StatusBox(Static):
    """Displays the latest ||STATUS: ...|| from a log file."""

    status_text: reactive[str] = reactive("Idle")

    def __init__(self, log_path: Path, **kwargs) -> None:
        super().__init__(**kwargs)
        self.log_path = log_path

    def on_mount(self) -> None:
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
        self.update(f"STATUS: {value}")


class TaskPanel(Vertical):
    """Per-agent task list with add/delete."""

    def __init__(self, agent_name: str, **kwargs) -> None:
        super().__init__(**kwargs)
        self.agent_name = agent_name

    def compose(self) -> ComposeResult:
        tasks = load_tasks().get(self.agent_name, [])
        yield Label("Tasks", classes="section-label")
        yield ListView(
            *[ListItem(Label(t)) for t in tasks],
            id=f"tasks-{self.agent_name}",
        )
        yield Input(
            placeholder="Add task + Enter (select item + d to delete)",
            id=f"input-{self.agent_name}",
        )

    def on_input_submitted(self, event: Input.Submitted) -> None:
        input_id = f"input-{self.agent_name}"
        if event.input.id != input_id:
            return
        text = event.value.strip()
        if not text:
            return
        event.input.value = ""
        lv: ListView = self.query_one(f"#tasks-{self.agent_name}", ListView)
        lv.append(ListItem(Label(text)))
        self._persist(lv)

    def delete_selected(self) -> None:
        lv: ListView = self.query_one(f"#tasks-{self.agent_name}", ListView)
        if lv.index is not None and len(lv.children) > 0:
            lv.children[lv.index].remove()
            self._persist(lv)

    def _persist(self, lv: ListView) -> None:
        all_tasks = load_tasks()
        items: list[str] = []
        for child in lv.children:
            labels = child.query(Label)
            if labels:
                items.append(str(labels.first.renderable))
        all_tasks[self.agent_name] = items
        save_tasks(all_tasks)


class TaskSummaryPanel(Vertical):
    """Live display of /tmp/fleet_task_summary.txt, refreshed every 2 s."""

    def compose(self) -> ComposeResult:
        yield Label(" Fleet Task Summary ", classes="agent-header")
        yield VerticalScroll(
            Static("Waiting for /tmp/fleet_task_summary.txt…", id="summary-content"),
            id="summary-scroll",
        )

    def on_mount(self) -> None:
        self.set_interval(2.0, self._refresh)

    def _refresh(self) -> None:
        try:
            content = SUMMARY_FILE.read_text(errors="replace").strip()
            self.query_one("#summary-content", Static).update(
                content if content else "(file exists but is empty)"
            )
        except OSError:
            self.query_one("#summary-content", Static).update(
                "Waiting for /tmp/fleet_task_summary.txt…"
            )


class AgentCard(Vertical):
    """One card per agent: header, status, log tail, tasks."""

    def __init__(self, agent_name: str, log_path: Path, **kwargs) -> None:
        super().__init__(**kwargs)
        self.agent_name = agent_name
        self.log_path = log_path

    def compose(self) -> ComposeResult:
        yield Label(f" {self.agent_name} ", classes="agent-header")
        yield StatusBox(self.log_path, classes="status-box")
        yield TaskPanel(self.agent_name)


class FleetDashboard(App):
    CSS = """
    Screen {
        layout: vertical;
        padding: 1;
    }

    #main-area {
        layout: horizontal;
        height: 1fr;
    }

    /* Left pane: grid of agent cards */
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
    }

    .status-box {
        border: dashed $warning;
        padding: 0 1;
        margin: 1 0;
        min-height: 1;
        color: $warning;
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

    ListView {
        min-height: 3;
        max-height: 8;
        margin: 0 0 0 0;
    }

    Input {
        margin: 0;
    }

    /* Right pane: task summary */
    .summary-panel {
        width: 52;
        height: 100%;
        border: solid $success;
        padding: 1;
        margin-left: 1;
    }

    #summary-scroll {
        height: 1fr;
    }

    #summary-content {
        padding: 0 1;
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
        Binding("d", "delete_task", "Delete selected task"),
        Binding("q", "quit", "Quit"),
    ]

    def compose(self) -> ComposeResult:
        agents = discover_agents()

        yield Header()
        with Horizontal(id="main-area"):
            with Vertical(id="agents-grid"):
                if not agents:
                    yield Static(
                        "No agent logs found. Run launch_fleet.sh first.\n"
                        f"Expected log files matching: {LOG_PATTERN}"
                    )
                else:
                    for name, log_path in agents:
                        yield AgentCard(name, log_path, classes="agent-card")
            yield TaskSummaryPanel(classes="summary-panel")
        yield Footer()

    def on_mount(self) -> None:
        self.sub_title = (
            "To intervene: quit (q), then run: tmux attach -t claude-fleet"
        )
        agents = discover_agents()
        if agents:
            cols = min(len(agents), 3)
            self.query_one("#agents-grid").styles.grid_size_columns = cols

    def action_delete_task(self) -> None:
        for card in self.query(AgentCard):
            panel = card.query_one(TaskPanel)
            lv = panel.query_one(ListView)
            if lv.has_focus or any(
                c.has_focus for c in lv.walk_children()
            ):
                panel.delete_selected()
                return


if __name__ == "__main__":
    FleetDashboard().run()
