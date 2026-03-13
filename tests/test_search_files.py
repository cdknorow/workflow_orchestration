"""Tests for the /api/sessions/live/{name}/search-files endpoint."""

import pytest
import pytest_asyncio
from unittest.mock import AsyncMock, patch
from httpx import ASGITransport, AsyncClient

from corral.web_server import app
from corral.store import CorralStore as SessionStore


# ── Fixtures ──────────────────────────────────────────────────────────────


@pytest_asyncio.fixture
async def tmp_store(tmp_path):
    db_path = tmp_path / "test.db"
    s = SessionStore(db_path=db_path)
    await s._get_conn()
    yield s
    await s.close()


@pytest_asyncio.fixture
async def client(tmp_store, monkeypatch):
    import corral.web_server as ws
    ws._set_store(tmp_store)
    monkeypatch.setattr(ws, "store", tmp_store)
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as c:
        yield c


# Sample file list returned by git ls-files
SAMPLE_FILES = "\n".join([
    "src/main.py",
    "src/utils.py",
    "src/api/routes.py",
    "src/api/models.py",
    "tests/test_main.py",
    "tests/test_utils.py",
    "tests/test_api.py",
    "README.md",
    "setup.py",
    "docs/guide.md",
])


@pytest.fixture
def mock_pane_and_git():
    """Patch _find_pane to return a fake workdir and run_cmd to return sample files."""
    find_pane = AsyncMock(return_value={"current_path": "/fake/workdir"})
    run_cmd = AsyncMock(return_value=(0, SAMPLE_FILES, ""))
    with patch("corral.api.live_sessions._find_pane", find_pane), \
         patch("corral.tools.utils.run_cmd", run_cmd):
        yield find_pane, run_cmd


# ── Tests ─────────────────────────────────────────────────────────────────


@pytest.mark.asyncio
async def test_search_no_query_returns_all(client, mock_pane_and_git):
    """With no query, returns up to 50 files."""
    resp = await client.get("/api/sessions/live/agent-1/search-files")
    assert resp.status_code == 200
    data = resp.json()
    assert len(data["files"]) == 10  # all sample files


@pytest.mark.asyncio
async def test_search_filters_by_query(client, mock_pane_and_git):
    """Query filters to matching files only."""
    resp = await client.get("/api/sessions/live/agent-1/search-files?q=test")
    assert resp.status_code == 200
    files = resp.json()["files"]
    assert all("test" in f.lower() for f in files)
    assert "tests/test_main.py" in files
    assert "tests/test_utils.py" in files
    assert "tests/test_api.py" in files


@pytest.mark.asyncio
async def test_search_basename_exact_match_ranked_first(client, mock_pane_and_git):
    """Exact basename match should rank above partial basename match."""
    resp = await client.get("/api/sessions/live/agent-1/search-files?q=setup.py")
    assert resp.status_code == 200
    files = resp.json()["files"]
    assert files[0] == "setup.py"


@pytest.mark.asyncio
async def test_search_basename_contains_ranked_above_path(client, mock_pane_and_git):
    """Basename-contains matches rank above path-only matches."""
    resp = await client.get("/api/sessions/live/agent-1/search-files?q=main")
    assert resp.status_code == 200
    files = resp.json()["files"]
    # "src/main.py" and "tests/test_main.py" have "main" in basename — should come first
    basename_matches = [f for f in files if "main" in f.split("/")[-1].lower()]
    path_only_matches = [f for f in files if "main" not in f.split("/")[-1].lower()]
    if path_only_matches:
        first_path_idx = files.index(path_only_matches[0])
        last_basename_idx = files.index(basename_matches[-1])
        assert last_basename_idx < first_path_idx


@pytest.mark.asyncio
async def test_search_case_insensitive(client, mock_pane_and_git):
    """Search is case-insensitive."""
    resp = await client.get("/api/sessions/live/agent-1/search-files?q=README")
    assert resp.status_code == 200
    files = resp.json()["files"]
    assert "README.md" in files


@pytest.mark.asyncio
async def test_search_no_matches_returns_empty(client, mock_pane_and_git):
    """Non-matching query returns empty list."""
    resp = await client.get("/api/sessions/live/agent-1/search-files?q=nonexistent_xyz")
    assert resp.status_code == 200
    assert resp.json()["files"] == []


@pytest.mark.asyncio
async def test_search_no_workdir_returns_empty(client):
    """If working directory can't be resolved, returns empty."""
    with patch("corral.api.live_sessions._find_pane", AsyncMock(return_value=None)):
        resp = await client.get("/api/sessions/live/agent-1/search-files?q=test")
    assert resp.status_code == 200
    assert resp.json()["files"] == []


@pytest.mark.asyncio
async def test_search_git_failure_returns_empty(client):
    """If git ls-files fails, returns empty."""
    with patch("corral.api.live_sessions._find_pane", AsyncMock(return_value={"current_path": "/fake"})), \
         patch("corral.tools.utils.run_cmd", AsyncMock(return_value=(1, None, "fatal: not a git repo"))):
        resp = await client.get("/api/sessions/live/agent-1/search-files?q=test")
    assert resp.status_code == 200
    assert resp.json()["files"] == []


@pytest.mark.asyncio
async def test_search_empty_repo_returns_empty(client):
    """Empty git output returns empty list."""
    with patch("corral.api.live_sessions._find_pane", AsyncMock(return_value={"current_path": "/fake"})), \
         patch("corral.tools.utils.run_cmd", AsyncMock(return_value=(0, "", ""))):
        resp = await client.get("/api/sessions/live/agent-1/search-files?q=anything")
    assert resp.status_code == 200
    assert resp.json()["files"] == []


@pytest.mark.asyncio
async def test_search_path_query_matches_directory(client, mock_pane_and_git):
    """Query matching a directory path component finds files in that dir."""
    resp = await client.get("/api/sessions/live/agent-1/search-files?q=api")
    assert resp.status_code == 200
    files = resp.json()["files"]
    assert "tests/test_api.py" in files
    assert "src/api/routes.py" in files
    assert "src/api/models.py" in files


@pytest.mark.asyncio
async def test_search_max_50_results(client):
    """Results are capped at 50."""
    many_files = "\n".join([f"src/file_{i:03d}.py" for i in range(100)])
    with patch("corral.api.live_sessions._find_pane", AsyncMock(return_value={"current_path": "/fake"})), \
         patch("corral.tools.utils.run_cmd", AsyncMock(return_value=(0, many_files, ""))):
        resp = await client.get("/api/sessions/live/agent-1/search-files?q=file")
    assert resp.status_code == 200
    assert len(resp.json()["files"]) == 50


@pytest.mark.asyncio
async def test_search_whitespace_query_treated_as_empty(client, mock_pane_and_git):
    """Whitespace-only query is treated as empty (returns all files)."""
    resp = await client.get("/api/sessions/live/agent-1/search-files?q=%20%20")
    assert resp.status_code == 200
    assert len(resp.json()["files"]) == 10


@pytest.mark.asyncio
async def test_search_dot_extension_query(client, mock_pane_and_git):
    """Query for file extension works."""
    resp = await client.get("/api/sessions/live/agent-1/search-files?q=.md")
    assert resp.status_code == 200
    files = resp.json()["files"]
    assert "README.md" in files
    assert "docs/guide.md" in files
    assert all(".md" in f for f in files)
