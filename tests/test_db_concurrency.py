"""Stress tests for SQLite concurrent write access.

Verifies that the pragma tuning (synchronous=NORMAL, WAL mode, busy_timeout)
prevents 'database is locked' errors under heavy concurrent writes.
"""

import asyncio
import pytest
import pytest_asyncio

from coral.store import CoralStore


@pytest_asyncio.fixture
async def store(tmp_path):
    s = CoralStore(db_path=tmp_path / "test.db")
    yield s
    await s.close()


@pytest.mark.asyncio
async def test_pragmas_are_set(store):
    """Verify performance pragmas are applied on connection."""
    conn = await store._get_conn()

    row = await (await conn.execute("PRAGMA journal_mode")).fetchone()
    assert row[0] == "wal"

    row = await (await conn.execute("PRAGMA synchronous")).fetchone()
    # NORMAL = 1
    assert row[0] == 1

    row = await (await conn.execute("PRAGMA temp_store")).fetchone()
    # MEMORY = 2
    assert row[0] == 2

    row = await (await conn.execute("PRAGMA cache_size")).fetchone()
    assert row[0] == -8000


@pytest.mark.asyncio
async def test_concurrent_event_inserts(store):
    """Many concurrent insert_agent_event calls should not raise 'database is locked'."""
    # Register a session so foreign key constraints don't bite
    await store.register_live_session("sid-1", "claude", "agent-1", "/tmp/wt1")

    errors = []

    async def insert_events(agent_name: str, count: int):
        for i in range(count):
            try:
                await store.insert_agent_event(
                    agent_name=agent_name,
                    event_type="tool_use",
                    summary=f"Event {i} from {agent_name}",
                    session_id="sid-1",
                )
            except Exception as e:
                errors.append((agent_name, i, str(e)))

    # 5 agents × 20 events each = 100 concurrent inserts
    tasks = [insert_events(f"agent-{n}", 20) for n in range(5)]
    await asyncio.gather(*tasks)

    assert len(errors) == 0, f"Got {len(errors)} errors: {errors[:5]}"


@pytest.mark.asyncio
async def test_concurrent_mixed_operations(store):
    """Concurrent reads and writes should not deadlock or raise errors."""
    await store.register_live_session("sid-1", "claude", "agent-1", "/tmp/wt1")

    errors = []

    async def writer(agent_name: str):
        for i in range(15):
            try:
                await store.insert_agent_event(
                    agent_name=agent_name,
                    event_type="status",
                    summary=f"Status {i}",
                    session_id="sid-1",
                )
            except Exception as e:
                errors.append(("write", agent_name, str(e)))

    async def reader():
        for _ in range(15):
            try:
                await store.get_all_live_sessions()
            except Exception as e:
                errors.append(("read", "", str(e)))

    tasks = [
        writer("agent-1"),
        writer("agent-2"),
        writer("agent-3"),
        reader(),
        reader(),
    ]
    await asyncio.gather(*tasks)

    assert len(errors) == 0, f"Got {len(errors)} errors: {errors[:5]}"


@pytest.mark.asyncio
async def test_concurrent_session_register_unregister(store):
    """Rapid register/unregister cycles should not corrupt the table."""
    errors = []

    async def churn(agent_id: int):
        for i in range(10):
            sid = f"sid-{agent_id}-{i}"
            try:
                await store.register_live_session(
                    sid, "claude", f"agent-{agent_id}", f"/tmp/wt{agent_id}",
                )
                await store.unregister_live_session(sid)
            except Exception as e:
                errors.append((agent_id, i, str(e)))

    tasks = [churn(n) for n in range(5)]
    await asyncio.gather(*tasks)

    assert len(errors) == 0, f"Got {len(errors)} errors: {errors[:5]}"

    # Table should be empty — all sessions unregistered
    sessions = await store.get_all_live_sessions()
    assert len(sessions) == 0


@pytest.mark.asyncio
async def test_wal_checkpoint_passive(store):
    """PASSIVE checkpoint should succeed without blocking."""
    conn = await store._get_conn()

    # Insert some data to generate WAL entries
    for i in range(10):
        await store.register_live_session(f"sid-{i}", "claude", f"wt{i}", f"/tmp/wt{i}")

    # Passive checkpoint should not raise
    result = await conn.execute("PRAGMA wal_checkpoint(PASSIVE)")
    row = await result.fetchone()
    # Returns (busy, log_pages, checkpointed_pages)
    assert row is not None
    assert row[0] == 0  # 0 = not blocked by concurrent reader


@pytest.mark.asyncio
async def test_multiple_store_instances_concurrent_writes(tmp_path):
    """Multiple CoralStore instances sharing the same DB file should not deadlock.

    This simulates the ad-hoc store instance pattern that caused the original bug.
    """
    db_path = tmp_path / "shared.db"
    stores = [CoralStore(db_path=db_path) for _ in range(3)]

    errors = []

    async def write_from_store(store_instance, store_id: int):
        for i in range(10):
            try:
                await store_instance.register_live_session(
                    f"sid-{store_id}-{i}", "claude",
                    f"agent-{store_id}", f"/tmp/wt{store_id}",
                )
            except Exception as e:
                errors.append((store_id, i, str(e)))

    try:
        tasks = [write_from_store(s, idx) for idx, s in enumerate(stores)]
        await asyncio.gather(*tasks)

        assert len(errors) == 0, f"Got {len(errors)} errors: {errors[:5]}"
    finally:
        for s in stores:
            await s.close()
