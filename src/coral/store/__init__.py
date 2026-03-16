"""Coral store package — modular database layer.

The CoralStore class composes all domain-specific stores (sessions, git, tasks)
behind a single shared SQLite connection. It is the primary interface used by
the web server and background services.
"""

from __future__ import annotations

from pathlib import Path
from typing import Any

from coral.store.connection import DatabaseManager, DB_PATH
from coral.store.sessions import SessionStore
from coral.store.git import GitStore
from coral.store.tasks import TaskStore
from coral.store.schedule import ScheduleStore
from coral.store.webhooks import WebhookStore


class CoralStore(DatabaseManager):
    """Unified store that delegates to domain-specific sub-stores sharing one connection.

    All sub-stores share the same _conn and _db_path so there is only one
    SQLite connection per CoralStore instance.
    """

    def __init__(self, db_path: Path = DB_PATH) -> None:
        super().__init__(db_path)
        # Create sub-stores that share our connection state
        self._sessions = SessionStore(db_path)
        self._git = GitStore(db_path)
        self._tasks = TaskStore(db_path)
        self._schedule = ScheduleStore(db_path)
        self._webhooks = WebhookStore(db_path)

    async def _get_conn(self):
        """Ensure all sub-stores share the same connection."""
        conn = await super()._get_conn()
        # Share the connection with sub-stores
        self._sessions._conn = self._conn
        self._sessions._schema_ensured = True
        self._git._conn = self._conn
        self._git._schema_ensured = True
        self._tasks._conn = self._conn
        self._tasks._schema_ensured = True
        self._schedule._conn = self._conn
        self._schedule._schema_ensured = True
        self._webhooks._conn = self._conn
        self._webhooks._schema_ensured = True
        return conn

    async def close(self) -> None:
        await super().close()
        # Clear sub-store connections
        self._sessions._conn = None
        self._git._conn = None
        self._tasks._conn = None
        self._schedule._conn = None
        self._webhooks._conn = None

    # ── Delegate: SessionStore methods ─────────────────────────────────────

    # User Settings
    async def get_settings(self) -> dict[str, str]:
        await self._get_conn()
        return await self._sessions.get_settings()

    async def set_setting(self, key: str, value: str) -> None:
        await self._get_conn()
        return await self._sessions.set_setting(key, value)

    # Session Notes
    async def get_session_notes(self, session_id: str) -> dict[str, Any]:
        await self._get_conn()
        return await self._sessions.get_session_notes(session_id)

    async def save_session_notes(self, session_id: str, notes_md: str) -> None:
        await self._get_conn()
        return await self._sessions.save_session_notes(session_id, notes_md)

    async def save_auto_summary(self, session_id: str, summary: str) -> None:
        await self._get_conn()
        return await self._sessions.save_auto_summary(session_id, summary)

    # Display Names
    async def get_display_name(self, session_id: str) -> str | None:
        await self._get_conn()
        return await self._sessions.get_display_name(session_id)

    async def set_display_name(self, session_id: str, display_name: str) -> None:
        await self._get_conn()
        return await self._sessions.set_display_name(session_id, display_name)

    async def get_display_names(self, session_ids: list[str]) -> dict[str, str]:
        await self._get_conn()
        return await self._sessions.get_display_names(session_ids)

    async def migrate_display_name(self, old_session_id: str, new_session_id: str) -> None:
        await self._get_conn()
        return await self._sessions.migrate_display_name(old_session_id, new_session_id)

    # Tags
    async def list_tags(self) -> list[dict[str, Any]]:
        await self._get_conn()
        return await self._sessions.list_tags()

    async def create_tag(self, name: str, color: str = "#58a6ff") -> dict[str, Any]:
        await self._get_conn()
        return await self._sessions.create_tag(name, color)

    async def delete_tag(self, tag_id: int) -> None:
        await self._get_conn()
        return await self._sessions.delete_tag(tag_id)

    async def get_session_tags(self, session_id: str) -> list[dict[str, Any]]:
        await self._get_conn()
        return await self._sessions.get_session_tags(session_id)

    async def add_session_tag(self, session_id: str, tag_id: int) -> None:
        await self._get_conn()
        return await self._sessions.add_session_tag(session_id, tag_id)

    async def remove_session_tag(self, session_id: str, tag_id: int) -> None:
        await self._get_conn()
        return await self._sessions.remove_session_tag(session_id, tag_id)

    # Session Index & FTS
    async def upsert_session_index(self, session_id: str, source_type: str, source_file: str,
                                    first_timestamp: str | None, last_timestamp: str | None,
                                    message_count: int, display_summary: str, file_mtime: float) -> None:
        await self._get_conn()
        return await self._sessions.upsert_session_index(
            session_id, source_type, source_file, first_timestamp, last_timestamp,
            message_count, display_summary, file_mtime)

    async def upsert_fts(self, session_id: str, body: str) -> None:
        await self._get_conn()
        return await self._sessions.upsert_fts(session_id, body)

    async def enqueue_for_summarization(self, session_id: str) -> None:
        await self._get_conn()
        return await self._sessions.enqueue_for_summarization(session_id)

    async def mark_summarized(self, session_id: str, status: str, error: str | None = None) -> None:
        await self._get_conn()
        return await self._sessions.mark_summarized(session_id, status, error)

    async def get_pending_summaries(self, limit: int = 5) -> list[str]:
        await self._get_conn()
        return await self._sessions.get_pending_summaries(limit)

    async def get_indexed_mtimes(self) -> dict[str, float]:
        await self._get_conn()
        return await self._sessions.get_indexed_mtimes()

    async def list_sessions_paged(self, page: int = 1, page_size: int = 50,
                                   search: str | None = None, **kwargs) -> dict[str, Any]:
        await self._get_conn()
        return await self._sessions.list_sessions_paged(page, page_size, search, **kwargs)

    # Agent Live State
    async def get_agent_session_id(self, agent_name: str) -> str | None:
        await self._get_conn()
        return await self._sessions.get_agent_session_id(agent_name)

    async def set_agent_session_id(self, agent_name: str, session_id: str) -> None:
        await self._get_conn()
        return await self._sessions.set_agent_session_id(agent_name, session_id)

    async def clear_agent_session_id(self, agent_name: str) -> None:
        await self._get_conn()
        return await self._sessions.clear_agent_session_id(agent_name)

    # Live Sessions
    async def register_live_session(self, session_id: str, agent_type: str, agent_name: str,
                                     working_dir: str, display_name: str | None = None,
                                     resume_from_id: str | None = None,
                                     flags: list[str] | None = None,
                                     is_job: bool = False,
                                     prompt: str | None = None,
                                     board_name: str | None = None,
                                     board_server: str | None = None) -> None:
        await self._get_conn()
        return await self._sessions.register_live_session(
            session_id, agent_type, agent_name, working_dir, display_name, resume_from_id, flags,
            is_job=is_job, prompt=prompt, board_name=board_name, board_server=board_server)

    async def update_live_session_display_name(self, session_id: str, display_name: str) -> None:
        await self._get_conn()
        return await self._sessions.update_live_session_display_name(session_id, display_name)

    async def unregister_live_session(self, session_id: str) -> None:
        await self._get_conn()
        return await self._sessions.unregister_live_session(session_id)

    async def replace_live_session(self, old_session_id: str, new_session_id: str, agent_type: str,
                                    agent_name: str, working_dir: str, display_name: str | None = None,
                                    resume_from_id: str | None = None,
                                    flags: list[str] | None = None) -> None:
        await self._get_conn()
        return await self._sessions.replace_live_session(
            old_session_id, new_session_id, agent_type, agent_name, working_dir,
            display_name, resume_from_id, flags)

    async def get_live_session_prompt_info(self, session_id: str) -> dict[str, str | None] | None:
        await self._get_conn()
        return await self._sessions.get_live_session_prompt_info(session_id)

    async def get_agent_type_for_session(self, session_id: str) -> str:
        await self._get_conn()
        return await self._sessions.get_agent_type_for_session(session_id)

    async def get_all_live_sessions(self) -> list[dict[str, Any]]:
        await self._get_conn()
        return await self._sessions.get_all_live_sessions()

    async def get_all_session_metadata(self) -> dict[str, dict[str, Any]]:
        await self._get_conn()
        return await self._sessions.get_all_session_metadata()

    # ── Delegate: GitStore methods ─────────────────────────────────────────

    async def upsert_git_snapshot(self, agent_name: str, agent_type: str, working_directory: str,
                                   branch: str, commit_hash: str, commit_subject: str,
                                   commit_timestamp: str | None, session_id: str | None = None,
                                   remote_url: str | None = None) -> None:
        await self._get_conn()
        return await self._git.upsert_git_snapshot(
            agent_name, agent_type, working_directory, branch, commit_hash,
            commit_subject, commit_timestamp, session_id, remote_url)

    async def get_git_snapshots(self, agent_name: str, limit: int = 20) -> list[dict[str, Any]]:
        await self._get_conn()
        return await self._git.get_git_snapshots(agent_name, limit)

    async def get_latest_git_state(self, agent_name: str) -> dict[str, Any] | None:
        await self._get_conn()
        return await self._git.get_latest_git_state(agent_name)

    async def get_latest_git_state_by_session(self, session_id: str) -> dict[str, Any] | None:
        await self._get_conn()
        return await self._git.get_latest_git_state_by_session(session_id)

    async def get_all_latest_git_state(self) -> dict[str, dict[str, Any]]:
        await self._get_conn()
        return await self._git.get_all_latest_git_state()

    async def get_git_snapshots_for_session(self, session_id: str, limit: int = 100) -> list[dict[str, Any]]:
        await self._get_conn()
        return await self._git.get_git_snapshots_for_session(session_id, limit)

    async def replace_changed_files(self, agent_name: str, working_directory: str,
                                     files: list[dict[str, Any]],
                                     session_id: str | None = None) -> None:
        await self._get_conn()
        return await self._git.replace_changed_files(agent_name, working_directory, files, session_id)

    async def get_changed_files(self, agent_name: str, session_id: str | None = None) -> list[dict[str, Any]]:
        await self._get_conn()
        return await self._git.get_changed_files(agent_name, session_id)

    async def get_all_changed_file_counts(self) -> dict[str, int]:
        await self._get_conn()
        return await self._git.get_all_changed_file_counts()

    # ── Delegate: TaskStore methods ────────────────────────────────────────

    async def list_agent_tasks(self, agent_name: str, session_id: str | None = None) -> list[dict[str, Any]]:
        await self._get_conn()
        return await self._tasks.list_agent_tasks(agent_name, session_id)

    async def create_agent_task(self, agent_name: str, title: str, session_id: str | None = None) -> dict[str, Any]:
        await self._get_conn()
        return await self._tasks.create_agent_task(agent_name, title, session_id)

    async def create_agent_task_if_not_exists(self, agent_name: str, title: str, session_id: str | None = None) -> dict[str, Any] | None:
        await self._get_conn()
        return await self._tasks.create_agent_task_if_not_exists(agent_name, title, session_id)

    async def update_agent_task(self, task_id: int, title: str | None = None,
                                 completed: int | None = None, sort_order: int | None = None) -> None:
        await self._get_conn()
        return await self._tasks.update_agent_task(task_id, title, completed, sort_order)

    async def complete_agent_task_by_title(self, agent_name: str, title: str, session_id: str | None = None) -> None:
        await self._get_conn()
        return await self._tasks.complete_agent_task_by_title(agent_name, title, session_id)

    async def delete_agent_task(self, task_id: int) -> None:
        await self._get_conn()
        return await self._tasks.delete_agent_task(task_id)

    async def reorder_agent_tasks(self, agent_name: str, task_ids: list[int]) -> None:
        await self._get_conn()
        return await self._tasks.reorder_agent_tasks(agent_name, task_ids)

    async def list_agent_notes(self, agent_name: str, session_id: str | None = None) -> list[dict[str, Any]]:
        await self._get_conn()
        return await self._tasks.list_agent_notes(agent_name, session_id)

    async def create_agent_note(self, agent_name: str, content: str, session_id: str | None = None) -> dict[str, Any]:
        await self._get_conn()
        return await self._tasks.create_agent_note(agent_name, content, session_id)

    async def update_agent_note(self, note_id: int, content: str) -> None:
        await self._get_conn()
        return await self._tasks.update_agent_note(note_id, content)

    async def delete_agent_note(self, note_id: int) -> None:
        await self._get_conn()
        return await self._tasks.delete_agent_note(note_id)

    async def insert_agent_event(self, agent_name: str, event_type: str, summary: str,
                                  tool_name: str | None = None, session_id: str | None = None,
                                  detail_json: str | None = None) -> dict[str, Any]:
        await self._get_conn()
        return await self._tasks.insert_agent_event(
            agent_name, event_type, summary, tool_name, session_id, detail_json)

    async def list_agent_events(self, agent_name: str, limit: int = 50, session_id: str | None = None) -> list[dict[str, Any]]:
        await self._get_conn()
        return await self._tasks.list_agent_events(agent_name, limit, session_id)

    async def get_agent_event_counts(self, agent_name: str, session_id: str | None = None) -> list[dict[str, Any]]:
        await self._get_conn()
        return await self._tasks.get_agent_event_counts(agent_name, session_id)

    async def get_latest_event_types(self, session_ids: list[str]) -> dict[str, tuple[str, str]]:
        await self._get_conn()
        return await self._tasks.get_latest_event_types(session_ids)

    async def get_latest_goals(self, session_ids: list[str]) -> dict[str, str]:
        await self._get_conn()
        return await self._tasks.get_latest_goals(session_ids)

    async def clear_agent_events(self, agent_name: str, session_id: str | None = None) -> None:
        await self._get_conn()
        return await self._tasks.clear_agent_events(agent_name, session_id)

    async def get_last_known_status_summary(self) -> dict[str, dict[str, str | None]]:
        await self._get_conn()
        return await self._tasks.get_last_known_status_summary()

    async def list_tasks_by_session(self, session_id: str) -> list[dict[str, Any]]:
        await self._get_conn()
        return await self._tasks.list_tasks_by_session(session_id)

    async def list_notes_by_session(self, session_id: str) -> list[dict[str, Any]]:
        await self._get_conn()
        return await self._tasks.list_notes_by_session(session_id)

    async def list_events_by_session(self, session_id: str, limit: int = 200) -> list[dict[str, Any]]:
        await self._get_conn()
        return await self._tasks.list_events_by_session(session_id, limit)

    # ── Delegate: WebhookStore methods ──────────────────────────────────────

    async def list_webhook_configs(self, enabled_only: bool = False) -> list[dict[str, Any]]:
        await self._get_conn()
        return await self._webhooks.list_webhook_configs(enabled_only)

    async def get_webhook_config(self, webhook_id: int) -> dict[str, Any] | None:
        await self._get_conn()
        return await self._webhooks.get_webhook_config(webhook_id)

    async def create_webhook_config(self, name: str, platform: str, url: str,
                                     agent_filter: str | None = None) -> dict[str, Any]:
        await self._get_conn()
        return await self._webhooks.create_webhook_config(
            name, platform, url, agent_filter)

    async def update_webhook_config(self, webhook_id: int, **fields) -> None:
        await self._get_conn()
        return await self._webhooks.update_webhook_config(webhook_id, **fields)

    async def delete_webhook_config(self, webhook_id: int) -> None:
        await self._get_conn()
        return await self._webhooks.delete_webhook_config(webhook_id)

    async def create_webhook_delivery(self, webhook_id: int, agent_name: str, event_type: str,
                                       event_summary: str, session_id: str | None = None) -> dict[str, Any]:
        await self._get_conn()
        return await self._webhooks.create_webhook_delivery(
            webhook_id, agent_name, event_type, event_summary, session_id)

    async def mark_webhook_delivery(self, delivery_id: int, status: str,
                                     http_status: int | None = None, error_msg: str | None = None,
                                     attempt_count: int | None = None,
                                     next_retry_at: str | None = None) -> None:
        await self._get_conn()
        return await self._webhooks.mark_webhook_delivery(
            delivery_id, status, http_status, error_msg, attempt_count, next_retry_at)

    async def get_pending_webhook_deliveries(self, limit: int = 50) -> list[dict[str, Any]]:
        await self._get_conn()
        return await self._webhooks.get_pending_webhook_deliveries(limit)

    async def list_webhook_deliveries(self, webhook_id: int, limit: int = 50) -> list[dict[str, Any]]:
        await self._get_conn()
        return await self._webhooks.list_webhook_deliveries(webhook_id, limit)

    async def increment_consecutive_failures(self, webhook_id: int) -> int:
        await self._get_conn()
        return await self._webhooks.increment_consecutive_failures(webhook_id)

    async def reset_consecutive_failures(self, webhook_id: int) -> None:
        await self._get_conn()
        return await self._webhooks.reset_consecutive_failures(webhook_id)

    async def auto_disable_webhook(self, webhook_id: int, reason: str) -> None:
        await self._get_conn()
        return await self._webhooks.auto_disable_webhook(webhook_id, reason)
