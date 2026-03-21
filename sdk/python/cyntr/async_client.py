"""Async Cyntr client using asyncio."""
import asyncio
import json
from urllib.request import Request, urlopen
from urllib.error import HTTPError
from typing import Any


class AsyncCyntrClient:
    """Async wrapper around CyntrClient using run_in_executor."""

    def __init__(self, base_url: str = "http://localhost:7700", api_key: str | None = None):
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key

    def _sync_request(self, method: str, path: str, body: dict | None = None) -> Any:
        url = self.base_url + path
        data = json.dumps(body).encode() if body else None
        req = Request(url, data=data, method=method)
        req.add_header("Content-Type", "application/json")
        if self.api_key:
            req.add_header("Authorization", f"Bearer {self.api_key}")
        resp = urlopen(req, timeout=30)
        result = json.loads(resp.read().decode())
        return result.get("data", result)

    async def _request(self, method: str, path: str, body: dict | None = None) -> Any:
        loop = asyncio.get_event_loop()
        return await loop.run_in_executor(None, self._sync_request, method, path, body)

    async def health(self) -> dict: return await self._request("GET", "/api/v1/system/health")
    async def list_tenants(self) -> list: return await self._request("GET", "/api/v1/tenants")
    async def list_agents(self, tenant: str) -> list: return await self._request("GET", f"/api/v1/tenants/{tenant}/agents")
    async def chat(self, tenant: str, agent: str, message: str) -> dict:
        return await self._request("POST", f"/api/v1/tenants/{tenant}/agents/{agent}/chat", {"message": message})
    async def create_agent(self, tenant: str, config: dict) -> dict:
        return await self._request("POST", f"/api/v1/tenants/{tenant}/agents", config)
    async def list_skills(self) -> list: return await self._request("GET", "/api/v1/skills")
    async def list_workflows(self) -> list: return await self._request("GET", "/api/v1/workflows")
    async def list_knowledge(self) -> list: return await self._request("GET", "/api/v1/knowledge")
    async def ingest_knowledge(self, title: str, content: str, tags: str = "") -> dict:
        return await self._request("POST", "/api/v1/knowledge", {"title": title, "content": content, "tags": tags})
