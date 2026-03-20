import pytest
from httpx import AsyncClient, ASGITransport

from coral.web_server import app

@pytest.mark.asyncio
async def test_index_renders_templates():
    """
    Ensure the root index page renders correctly with all Jinja includes,
    and returns a 200 OK status without throwing TemplateNotFound errors.
    """
    async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as client:
        response = await client.get("/")
        
        # Verify a 200 OK
        assert response.status_code == 200
        
        # Verify some text that is included from the base layout or includes
        assert "Coral" in response.text
        assert "Workspace" in response.text
        assert "Chat History" in response.text
