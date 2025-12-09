"""Integration tests using a live HTTP server."""

import pytest
import requests


@pytest.mark.integration
class TestLiveServer:
    """Test the API using a real HTTP server."""

    def test_health_check_live(self, live_server):
        """Test health check endpoint with live server."""
        response = requests.get(f"{live_server}/api/v1/health")

        assert response.status_code == 200
        data = response.json()

        assert data["status"] == "healthy"
        assert "version" in data
        assert "uptime_seconds" in data

    def test_list_providers_live(self, live_server):
        """Test listing providers with live server."""
        response = requests.get(f"{live_server}/api/v1/evaluations/providers")

        assert response.status_code == 200
        data = response.json()

        assert "items" in data
        assert "total_count" in data
