"""Tests for the message board API layer."""

import pytest
import pytest_asyncio
from httpx import AsyncClient, ASGITransport

from coral.messageboard.app import create_app


@pytest_asyncio.fixture
async def client(tmp_path):
    app = create_app(db_path=tmp_path / "test_board.db")
    async with AsyncClient(
        transport=ASGITransport(app=app), base_url="http://test"
    ) as c:
        yield c


@pytest.mark.asyncio
async def test_subscribe(client):
    resp = await client.post(
        "/proj1/subscribe",
        json={"session_id": "a1", "job_title": "Backend Dev"},
    )
    assert resp.status_code == 200
    data = resp.json()
    assert data["session_id"] == "a1"
    assert data["job_title"] == "Backend Dev"


@pytest.mark.asyncio
async def test_post_and_read_messages(client):
    # Subscribe two agents
    await client.post("/proj1/subscribe", json={"session_id": "a1", "job_title": "Backend"})
    await client.post("/proj1/subscribe", json={"session_id": "a2", "job_title": "Frontend"})

    # a1 posts
    resp = await client.post(
        "/proj1/messages",
        json={"session_id": "a1", "content": "Found 401 errors"},
    )
    assert resp.status_code == 200
    msg = resp.json()
    assert msg["id"] is not None

    # a2 reads — sees a1's message
    resp = await client.get("/proj1/messages", params={"session_id": "a2"})
    assert resp.status_code == 200
    msgs = resp.json()
    assert len(msgs) == 1
    assert msgs[0]["content"] == "Found 401 errors"
    assert msgs[0]["job_title"] == "Backend"

    # a2 reads again — empty
    resp = await client.get("/proj1/messages", params={"session_id": "a2"})
    assert resp.status_code == 200
    assert resp.json() == []


@pytest.mark.asyncio
async def test_list_subscribers(client):
    await client.post("/proj1/subscribe", json={"session_id": "a1", "job_title": "Backend"})
    await client.post("/proj1/subscribe", json={"session_id": "a2", "job_title": "Frontend"})

    resp = await client.get("/proj1/subscribers")
    assert resp.status_code == 200
    subs = resp.json()
    assert len(subs) == 2


@pytest.mark.asyncio
async def test_list_projects(client):
    await client.post("/proj1/subscribe", json={"session_id": "a1", "job_title": "Dev"})
    await client.post("/proj2/subscribe", json={"session_id": "a2", "job_title": "Dev"})

    resp = await client.get("/projects")
    assert resp.status_code == 200
    projects = resp.json()
    assert len(projects) == 2


@pytest.mark.asyncio
async def test_unsubscribe(client):
    await client.post("/proj1/subscribe", json={"session_id": "a1", "job_title": "Dev"})

    resp = await client.request(
        "DELETE", "/proj1/subscribe", json={"session_id": "a1"}
    )
    assert resp.status_code == 200

    # Verify removed
    resp = await client.get("/proj1/subscribers")
    assert resp.json() == []


@pytest.mark.asyncio
async def test_unsubscribe_not_found(client):
    resp = await client.request(
        "DELETE", "/proj1/subscribe", json={"session_id": "nonexistent"}
    )
    assert resp.status_code == 404


@pytest.mark.asyncio
async def test_delete_project(client):
    await client.post("/proj1/subscribe", json={"session_id": "a1", "job_title": "Dev"})
    await client.post("/proj1/messages", json={"session_id": "a1", "content": "hi"})

    resp = await client.delete("/proj1")
    assert resp.status_code == 200

    resp = await client.get("/projects")
    assert resp.json() == []


@pytest.mark.asyncio
async def test_webhook_dispatch(client, tmp_path):
    """Test that webhook is dispatched on message post (best-effort)."""
    # We test by subscribing with a webhook URL that won't connect —
    # the post should still succeed (fire-and-forget).
    await client.post(
        "/proj1/subscribe",
        json={"session_id": "a1", "job_title": "Dev", "webhook_url": "http://127.0.0.1:19999/hook"},
    )
    await client.post(
        "/proj1/subscribe",
        json={"session_id": "a2", "job_title": "Frontend"},
    )

    # a2 posts — should trigger webhook to a1 (which will fail, but post succeeds)
    resp = await client.post(
        "/proj1/messages",
        json={"session_id": "a2", "content": "hello"},
    )
    assert resp.status_code == 200
    assert resp.json()["content"] == "hello"
