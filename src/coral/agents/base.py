"""Base agent class defining the interface for all agent implementations."""

from __future__ import annotations

import os
from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any


@dataclass
class ExtractedSession:
    """Data extracted from a history file for a single session.

    Returned by ``BaseAgent.extract_sessions`` and consumed by the
    ``SessionIndexer`` to insert into the store.
    """

    session_id: str
    source_type: str
    first_timestamp: str | None = None
    last_timestamp: str | None = None
    message_count: int = 0
    display_summary: str = "(no messages)"
    fts_body: str = ""


class BaseAgent(ABC):
    """Abstract base class for agent implementations (Claude, Gemini, etc.).

    Each subclass encapsulates the agent-specific logic for:
    - Building CLI launch commands
    - Loading / indexing history files
    - Installing hooks
    - Preparing for session resume
    - Processing hook payloads (tool summaries, task sync, events)
    """

    @property
    @abstractmethod
    def agent_type(self) -> str:
        """Short identifier, e.g. 'claude', 'gemini'."""

    @property
    @abstractmethod
    def supports_resume(self) -> bool:
        """Whether the agent supports resuming a previous session."""

    @property
    @abstractmethod
    def history_base_path(self) -> Path:
        """Root directory where this agent stores its history files."""

    @property
    @abstractmethod
    def history_glob_pattern(self) -> str:
        """Glob pattern (relative to *history_base_path*) for history files."""

    @property
    def available_commands(self) -> dict[str, str]:
        """Map of command names to slash-commands the agent supports."""
        return {"compress": "/compact", "clear": "/clear"}

    @staticmethod
    def _build_board_system_prompt(board_name: str | None, role: str | None, prompt: str | None) -> str:
        """Build the board + behavior portion of the system prompt."""
        parts = []
        if prompt:
            parts.append(prompt)
        if board_name:
            is_orchestrator = role and "orchestrator" in role.lower()
            board_intro = (
                f"You are part of an Agent Team and can communicate with your teammates using the coral-board CLI. "
                f"You have already been subscribed to message board \"{board_name}\"."
                + (f" Your role is: {role}." if role else "")
                + "\n\nUse the coral-board CLI to communicate with your teammates:\n"
                "  coral-board read          — read new messages from teammates\n"
                "  coral-board post \"msg\"    — post a message to the board\n"
                "  coral-board read --last 5 — see the 5 most recent messages\n"
                "  coral-board subscribers   — see who is on the board\n"
                "Check the board periodically for updates from your teammates.\n\n"
            )
            if is_orchestrator:
                board_intro += (
                    "Introduce yourself by posting to the message board, then discuss your proposed plan "
                    "with the operator (the human user) before posting assignments to the team."
                )
            else:
                board_intro += (
                    "Introduce yourself by posting to the message board, then wait for instructions from the Orchestrator."
                )
            parts.append(board_intro)
        return "\n\n".join(parts)

    @abstractmethod
    def build_launch_command(
        self,
        session_id: str,
        protocol_path: Path | None,
        resume_session_id: str | None = None,
        flags: list[str] | None = None,
        working_dir: str | None = None,
        board_name: str | None = None,
        role: str | None = None,
        prompt: str | None = None,
    ) -> str:
        """Build the shell command string to launch this agent."""

    @abstractmethod
    def load_history_sessions(self) -> list[dict[str, Any]]:
        """Scan history files and return a list of session summary dicts."""

    @abstractmethod
    def load_session_messages(self, session_id: str) -> list[dict[str, Any]]:
        """Load all messages for a specific historical session."""

    @abstractmethod
    def extract_sessions(self, path: Path) -> list[ExtractedSession]:
        """Extract session data from a history file.

        Returns a list of ``ExtractedSession`` objects — one per session found
        in the file. The caller (``SessionIndexer``) is responsible for
        inserting them into the store.
        """

    def prepare_resume(self, session_id: str, working_dir: str) -> None:
        """Prepare for resuming a session (e.g. copy files). Default: no-op."""

    # ── Hook Processing Interface ─────────────────────────────────────────

    def resolve_agent_name(self, hook_data: dict) -> str:
        """Extract the agent/worktree name from hook payload data.

        Default implementation uses the basename of the cwd field.
        """
        cwd = hook_data.get("cwd", "")
        return os.path.basename(cwd.rstrip("/"))

    def parse_agentic_event(self, hook_data: dict) -> dict | None:
        """Parse a hook payload into an agentic event dict for the dashboard.

        Returns a dict with keys: event_type, summary, tool_name, detail_json, session_id.
        Returns None if the payload doesn't represent a recognized event.

        Subclasses override this to handle agent-specific hook formats.
        """
        return None

    def parse_task_event(self, hook_data: dict) -> dict | None:
        """Parse a hook payload into a task sync event dict.

        Returns a dict with keys: action ("create"|"update"), subject, task_id, status,
        session_id.
        Returns None if the payload doesn't represent a task event.

        Subclasses override this to handle agent-specific task formats.
        """
        return None

    def parse_task_response(self, resp) -> dict:
        """Parse a tool response to extract task id and subject.

        Returns a dict with keys: task_id, subject.
        Default returns empty strings.
        """
        return {"task_id": "", "subject": ""}

    # ── Transcript Parsing Interface ──────────────────────────────────────

    def resolve_transcript_path(self, session_id: str, working_directory: str = "") -> Path | None:
        """Find the transcript file for a session.

        *working_directory* is an optional hint for fast lookup.
        Returns the Path to the transcript file, or None if not found.
        """
        return None

    def parse_transcript_entry(
        self, entry: dict[str, Any], tool_use_names: dict[str, str]
    ) -> list[dict[str, Any]] | dict[str, Any] | None:
        """Convert a raw transcript entry into normalized frontend message(s).

        *tool_use_names* maps tool_use_id → tool_name for labeling results.
        The method should also populate this dict when it encounters tool_use blocks.

        Return format (each message dict must include a "type" key):
        - ``{"type": "user", "timestamp": str, "content": str}``
        - ``{"type": "assistant", "timestamp": str, "text": str, "tool_uses": list}``
        - ``{"type": "tool_result", "timestamp": str, "content": str, ...}``
        - A list of the above (for entries that expand to multiple messages)
        - None to skip the entry
        """
        return None
