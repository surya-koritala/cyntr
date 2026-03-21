"""Cyntr Python SDK — client library for the Cyntr AI Agent Platform API."""

import json
from urllib.request import Request, urlopen
from urllib.error import HTTPError


class CyntrClient:
    """Client for the Cyntr API.

    Usage:
        client = CyntrClient("http://localhost:7700", api_key="cyntr_...")

        # List tenants
        tenants = client.list_tenants()

        # Create and chat with an agent
        client.create_agent("demo", {"name": "bot", "model": "claude", "tools": ["*"]})
        response = client.chat("demo", "bot", "Hello!")
        print(response["content"])
    """

    def __init__(self, base_url: str = "http://localhost:7700", api_key: str | None = None, max_retries: int = 3, retry_backoff: float = 1.0):
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.max_retries = max_retries
        self.retry_backoff = retry_backoff

    def _request(self, method: str, path: str, body: dict | None = None) -> dict:
        url = self.base_url + path
        data = json.dumps(body).encode() if body else None
        last_error = None

        for attempt in range(self.max_retries + 1):
            try:
                req = Request(url, data=data, method=method)
                req.add_header("Content-Type", "application/json")
                if self.api_key:
                    req.add_header("Authorization", f"Bearer {self.api_key}")
                resp = urlopen(req, timeout=30)
                result = json.loads(resp.read().decode())
                return result.get("data", result)
            except HTTPError as e:
                if e.code >= 500 and attempt < self.max_retries:
                    import time
                    time.sleep(self.retry_backoff * (2 ** attempt))
                    last_error = e
                    continue
                error_body = e.read().decode()
                try:
                    err = json.loads(error_body)
                    msg = err.get("error", {}).get("message", error_body)
                except Exception:
                    msg = error_body
                raise Exception(f"Cyntr API error {e.code}: {msg}")
        raise Exception(f"Max retries exceeded: {last_error}")

    # System
    def health(self) -> dict:
        return self._request("GET", "/api/v1/system/health")

    def version(self) -> dict:
        return self._request("GET", "/api/v1/system/version")

    # Tenants
    def list_tenants(self) -> list:
        return self._request("GET", "/api/v1/tenants")

    def get_tenant(self, tenant: str) -> dict:
        return self._request("GET", f"/api/v1/tenants/{tenant}")

    def create_tenant(self, name: str, isolation: str = "namespace", policy: str = "default") -> dict:
        return self._request("POST", "/api/v1/tenants", {"name": name, "isolation": isolation, "policy": policy})

    def delete_tenant(self, tenant: str) -> dict:
        return self._request("DELETE", f"/api/v1/tenants/{tenant}")

    # Agents
    def list_agents(self, tenant: str) -> list:
        return self._request("GET", f"/api/v1/tenants/{tenant}/agents")

    def create_agent(self, tenant: str, config: dict) -> dict:
        return self._request("POST", f"/api/v1/tenants/{tenant}/agents", config)

    def get_agent(self, tenant: str, name: str) -> dict:
        return self._request("GET", f"/api/v1/tenants/{tenant}/agents/{name}")

    def update_agent(self, tenant: str, name: str, config: dict) -> dict:
        return self._request("PUT", f"/api/v1/tenants/{tenant}/agents/{name}", config)

    def delete_agent(self, tenant: str, name: str) -> dict:
        return self._request("DELETE", f"/api/v1/tenants/{tenant}/agents/{name}")

    def chat(self, tenant: str, agent: str, message: str) -> dict:
        return self._request("POST", f"/api/v1/tenants/{tenant}/agents/{agent}/chat", {"message": message})

    # Sessions
    def list_sessions(self, tenant: str, agent: str) -> list:
        return self._request("GET", f"/api/v1/tenants/{tenant}/agents/{agent}/sessions")

    def get_session_messages(self, tenant: str, agent: str, session_id: str) -> list:
        return self._request("GET", f"/api/v1/tenants/{tenant}/agents/{agent}/sessions/{session_id}/messages")

    # Memories
    def list_memories(self, tenant: str, agent: str) -> list:
        return self._request("GET", f"/api/v1/tenants/{tenant}/agents/{agent}/memories")

    def delete_memory(self, tenant: str, agent: str, memory_id: str) -> dict:
        return self._request("DELETE", f"/api/v1/tenants/{tenant}/agents/{agent}/memories/{memory_id}")

    # Skills
    def list_skills(self) -> list:
        return self._request("GET", "/api/v1/skills")

    def install_skill(self, path: str) -> dict:
        return self._request("POST", "/api/v1/skills", {"path": path})

    def uninstall_skill(self, name: str) -> dict:
        return self._request("DELETE", f"/api/v1/skills/{name}")

    # Policies
    def list_policy_rules(self) -> list:
        return self._request("GET", "/api/v1/policies/rules")

    def test_policy(self, tenant: str, action: str, tool: str = "", agent: str = "", user: str = "") -> dict:
        return self._request("POST", "/api/v1/policies/test", {
            "tenant": tenant, "action": action, "tool": tool, "agent": agent, "user": user
        })

    # Workflows
    def list_workflows(self) -> list:
        return self._request("GET", "/api/v1/workflows")

    def get_workflow(self, workflow_id: str) -> dict:
        return self._request("GET", f"/api/v1/workflows/{workflow_id}")

    def register_workflow(self, definition: dict) -> dict:
        return self._request("POST", "/api/v1/workflows", definition)

    def run_workflow(self, workflow_id: str) -> dict:
        return self._request("POST", f"/api/v1/workflows/{workflow_id}/run", {})

    def list_workflow_runs(self) -> list:
        return self._request("GET", "/api/v1/workflows/runs")

    def get_workflow_run(self, run_id: str) -> dict:
        return self._request("GET", f"/api/v1/workflows/runs/{run_id}")

    # Schedules
    def list_schedules(self) -> list:
        return self._request("GET", "/api/v1/schedules")

    def add_schedule(self, interval: str, tenant: str, agent: str, message: str, channel: str = "", channel_id: str = "") -> dict:
        body = {"interval": interval, "tenant": tenant, "agent": agent, "message": message}
        if channel:
            body["channel"] = channel
            body["channel_id"] = channel_id
        return self._request("POST", "/api/v1/schedules", body)

    def remove_schedule(self, schedule_id: str) -> dict:
        return self._request("POST", f"/api/v1/schedules/{schedule_id}/remove")

    # Audit
    def query_audit(self, tenant: str = "", user: str = "", action: str = "", agent: str = "", limit: int = 0) -> list:
        params = []
        if tenant: params.append(f"tenant={tenant}")
        if user: params.append(f"user={user}")
        if action: params.append(f"action={action}")
        if agent: params.append(f"agent={agent}")
        if limit: params.append(f"limit={limit}")
        qs = "&".join(params)
        path = f"/api/v1/audit?{qs}" if qs else "/api/v1/audit"
        return self._request("GET", path)

    # Federation
    def list_peers(self) -> list:
        return self._request("GET", "/api/v1/federation/peers")

    def join_peer(self, name: str, endpoint: str, secret: str = "") -> dict:
        return self._request("POST", "/api/v1/federation/peers", {"name": name, "endpoint": endpoint, "secret": secret})

    def remove_peer(self, name: str) -> dict:
        return self._request("DELETE", f"/api/v1/federation/peers/{name}")

    # Approvals
    def list_approvals(self) -> list:
        return self._request("GET", "/api/v1/approvals")

    def approve(self, approval_id: str, decided_by: str = "") -> dict:
        return self._request("POST", f"/api/v1/approvals/{approval_id}/approve", {"decided_by": decided_by})

    def deny(self, approval_id: str, decided_by: str = "") -> dict:
        return self._request("POST", f"/api/v1/approvals/{approval_id}/deny", {"decided_by": decided_by})

    # Channels
    def list_channels(self) -> list:
        return self._request("GET", "/api/v1/channels")

    # Knowledge
    def list_knowledge(self) -> list:
        return self._request("GET", "/api/v1/knowledge")

    def ingest_knowledge(self, title: str, content: str, tags: str = "") -> dict:
        return self._request("POST", "/api/v1/knowledge", {"title": title, "content": content, "tags": tags})

    def delete_knowledge(self, doc_id: str) -> dict:
        return self._request("DELETE", f"/api/v1/knowledge/{doc_id}")
