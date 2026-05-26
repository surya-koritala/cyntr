"""Cyntr Python SDK — client library for the Cyntr AI Agent Platform API.

Stdlib-only: uses ``urllib`` for sync HTTP and ``http.client`` for streaming.
No third-party dependencies.

Example::

    from cyntr import CyntrClient

    client = CyntrClient("http://localhost:7700", api_key="cyntr_...")
    tenants = client.list_tenants()
    client.create_agent("demo", name="bot", model="claude", prompt="be helpful")
    reply = client.chat("demo", "bot", "Hello!")
    print(reply["content"])
"""

from __future__ import annotations

import json
import time
from typing import Any, Iterable, Iterator
from urllib.parse import urlencode, urlparse, quote
from urllib.request import Request, urlopen
from urllib.error import HTTPError, URLError

from .models import Agent, EvalCase, EvalRun, Session, WorkflowRun


class CyntrError(Exception):
    """Raised for any non-2xx response from the Cyntr API."""

    def __init__(self, status: int, code: str, message: str, request_id: str = ""):
        self.status = status
        self.code = code
        self.message = message
        self.request_id = request_id
        super().__init__(f"Cyntr API error {status} ({code}): {message}")


class CyntrClient:
    """Synchronous client for the Cyntr API.

    Parameters
    ----------
    base_url: API base URL (default ``http://localhost:7700``).
    api_key: Bearer token. Optional.
    timeout: Per-request timeout in seconds (default 60).
    max_retries: Number of retries for 5xx / connection errors (default 3).
    retry_backoff: Base seconds for exponential backoff (default 1.0).

    All response-returning methods return the parsed ``data`` field from
    the API envelope. Typed-dataclass variants (``get_agent_typed`` etc.)
    are provided where useful; the default methods return raw dicts.
    """

    def __init__(
        self,
        base_url: str = "http://localhost:7700",
        api_key: str | None = None,
        timeout: float = 60.0,
        max_retries: int = 3,
        retry_backoff: float = 1.0,
    ):
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.timeout = timeout
        self.max_retries = max_retries
        self.retry_backoff = retry_backoff

    # ------------------------------------------------------------------ #
    # Transport
    # ------------------------------------------------------------------ #

    def _headers(self) -> dict[str, str]:
        h = {"Content-Type": "application/json", "Accept": "application/json"}
        if self.api_key:
            h["Authorization"] = f"Bearer {self.api_key}"
        return h

    def _request(self, method: str, path: str, body: Any = None) -> Any:
        url = self.base_url + path
        data = json.dumps(body).encode() if body is not None else None
        last_error: Exception | None = None

        for attempt in range(self.max_retries + 1):
            try:
                req = Request(url, data=data, method=method)
                for k, v in self._headers().items():
                    req.add_header(k, v)
                with urlopen(req, timeout=self.timeout) as resp:
                    raw = resp.read().decode() or "{}"
                payload = json.loads(raw)
                return payload.get("data", payload) if isinstance(payload, dict) else payload
            except HTTPError as e:
                if e.code >= 500 and attempt < self.max_retries:
                    time.sleep(self.retry_backoff * (2 ** attempt))
                    last_error = e
                    continue
                body_bytes = e.read()
                try:
                    err = json.loads(body_bytes.decode())
                    err_obj = err.get("error") or {}
                    code = err_obj.get("code", "")
                    msg = err_obj.get("message") or body_bytes.decode()
                    req_id = (err.get("meta") or {}).get("request_id", "")
                except Exception:
                    code, msg, req_id = "", body_bytes.decode(), ""
                raise CyntrError(e.code, code, msg, req_id) from None
            except URLError as e:
                if attempt < self.max_retries:
                    time.sleep(self.retry_backoff * (2 ** attempt))
                    last_error = e
                    continue
                raise CyntrError(0, "CONNECTION_ERROR", str(e)) from None

        raise CyntrError(0, "MAX_RETRIES", f"max retries exceeded: {last_error}")

    @staticmethod
    def _qs(params: dict[str, Any]) -> str:
        filtered = {k: v for k, v in params.items() if v not in (None, "", 0)}
        return "?" + urlencode(filtered) if filtered else ""

    # ------------------------------------------------------------------ #
    # System
    # ------------------------------------------------------------------ #

    def health(self) -> dict:
        return self._request("GET", "/api/v1/system/health")

    def version(self) -> dict:
        return self._request("GET", "/api/v1/system/version")

    # ------------------------------------------------------------------ #
    # Tenants
    # ------------------------------------------------------------------ #

    def list_tenants(self) -> list:
        return self._request("GET", "/api/v1/tenants")

    def get_tenant(self, tid: str) -> dict:
        return self._request("GET", f"/api/v1/tenants/{tid}")

    def create_tenant(self, name: str, isolation: str = "namespace", policy: str = "default") -> dict:
        return self._request("POST", "/api/v1/tenants", {
            "name": name, "isolation": isolation, "policy": policy,
        })

    def delete_tenant(self, tid: str) -> dict:
        return self._request("DELETE", f"/api/v1/tenants/{tid}")

    # ------------------------------------------------------------------ #
    # Agents
    # ------------------------------------------------------------------ #

    def list_agents(self, tenant: str) -> list:
        return self._request("GET", f"/api/v1/tenants/{tenant}/agents")

    def create_agent(
        self,
        tenant: str,
        name: str | None = None,
        model: str | None = None,
        prompt: str | None = None,
        tools: list[str] | None = None,
        skills: list[str] | None = None,
        **extra: Any,
    ) -> dict:
        """Create an agent.

        Backward-compatible: if called with a single dict positional argument
        (the old ``create_agent(tenant, config_dict)`` form), it is treated
        as the body verbatim.
        """
        if isinstance(name, dict) and model is None and prompt is None:
            body = name  # legacy form
        else:
            body: dict[str, Any] = {}
            if name is not None: body["name"] = name
            if model is not None: body["model"] = model
            if prompt is not None: body["system_prompt"] = prompt
            if tools is not None: body["tools"] = tools
            if skills is not None: body["skills"] = skills
            body.update(extra)
        return self._request("POST", f"/api/v1/tenants/{tenant}/agents", body)

    def get_agent(self, tenant: str, name: str) -> dict:
        return self._request("GET", f"/api/v1/tenants/{tenant}/agents/{name}")

    def update_agent(self, tenant: str, name: str, **fields_: Any) -> dict:
        # Legacy ``update_agent(t, n, config_dict)`` — first kwarg ``config`` is honored.
        config = fields_.pop("config", None)
        body = config if isinstance(config, dict) else fields_
        return self._request("PUT", f"/api/v1/tenants/{tenant}/agents/{name}", body)

    def delete_agent(self, tenant: str, name: str) -> dict:
        return self._request("DELETE", f"/api/v1/tenants/{tenant}/agents/{name}")

    def chat(
        self,
        tenant: str,
        agent: str,
        message: str,
        *,
        user: str | None = None,
        channel: str | None = None,
    ) -> dict:
        body: dict[str, Any] = {"message": message}
        if user is not None: body["user"] = user
        if channel is not None: body["channel"] = channel
        return self._request("POST", f"/api/v1/tenants/{tenant}/agents/{agent}/chat", body)

    def chat_stream(self, tenant: str, agent: str, message: str) -> Iterator[dict]:
        """Stream chat tokens via Server-Sent Events.

        Yields a parsed dict for every ``data:`` line. Uses ``http.client``
        so we can read the response incrementally without buffering.
        """
        import http.client

        parsed = urlparse(self.base_url)
        conn_cls = http.client.HTTPSConnection if parsed.scheme == "https" else http.client.HTTPConnection
        conn = conn_cls(parsed.hostname, parsed.port, timeout=self.timeout)
        path = f"/api/v1/tenants/{tenant}/agents/{agent}/stream?message={quote(message)}"
        headers = {"Accept": "text/event-stream"}
        if self.api_key:
            headers["Authorization"] = f"Bearer {self.api_key}"
        try:
            conn.request("GET", path, headers=headers)
            resp = conn.getresponse()
            if resp.status >= 400:
                raise CyntrError(resp.status, "STREAM_FAILED", resp.read().decode())
            buf = b""
            while True:
                chunk = resp.read(1)
                if not chunk:
                    break
                buf += chunk
                while b"\n\n" in buf:
                    raw, buf = buf.split(b"\n\n", 1)
                    for line in raw.split(b"\n"):
                        if line.startswith(b"data:"):
                            data = line[5:].strip()
                            if not data:
                                continue
                            try:
                                yield json.loads(data.decode())
                            except json.JSONDecodeError:
                                yield {"raw": data.decode()}
        finally:
            conn.close()

    # ------------------------------------------------------------------ #
    # Sessions
    # ------------------------------------------------------------------ #

    def list_sessions(self, tenant: str, agent: str) -> list:
        return self._request("GET", f"/api/v1/tenants/{tenant}/agents/{agent}/sessions")

    def get_session(self, tenant: str, agent: str, session_id: str) -> list:
        return self._request(
            "GET",
            f"/api/v1/tenants/{tenant}/agents/{agent}/sessions/{session_id}/messages",
        )

    # Legacy alias
    get_session_messages = get_session

    def clear_session(self, tenant: str, agent: str, session_id: str = "current") -> dict:
        return self._request(
            "DELETE",
            f"/api/v1/tenants/{tenant}/agents/{agent}/sessions/{session_id}",
        )

    # ------------------------------------------------------------------ #
    # Memories
    # ------------------------------------------------------------------ #

    def list_memories(self, tenant: str, agent: str) -> list:
        return self._request("GET", f"/api/v1/tenants/{tenant}/agents/{agent}/memories")

    def delete_memory(self, tenant: str, agent: str, memory_id: str) -> dict:
        return self._request(
            "DELETE",
            f"/api/v1/tenants/{tenant}/agents/{agent}/memories/{memory_id}",
        )

    # ------------------------------------------------------------------ #
    # Skills
    # ------------------------------------------------------------------ #

    def list_skills(self) -> list:
        return self._request("GET", "/api/v1/skills")

    def install_skill(self, name: str) -> dict:
        return self._request("POST", "/api/v1/skills", {"path": name})

    def uninstall_skill(self, name: str) -> dict:
        return self._request("DELETE", f"/api/v1/skills/{name}")

    def search_skills(self, query: str) -> list:
        return self._request("GET", f"/api/v1/skills/marketplace/search?q={quote(query)}")

    # ------------------------------------------------------------------ #
    # Workflows
    # ------------------------------------------------------------------ #

    def list_workflows(self, tenant: str | None = None) -> list:
        # API has no tenant filter; ignore arg but accept it for spec parity.
        _ = tenant
        return self._request("GET", "/api/v1/workflows")

    def get_workflow(self, workflow_id: str) -> dict:
        return self._request("GET", f"/api/v1/workflows/{workflow_id}")

    def register_workflow(self, definition: dict) -> dict:
        return self._request("POST", "/api/v1/workflows", definition)

    def run_workflow(self, tenant: str, name: str, inputs: dict | None = None) -> dict:
        """Run a workflow by name. ``tenant`` is accepted for parity but
        the workflow ID identifies the run on the server. ``inputs`` is
        forwarded as the request body."""
        _ = tenant
        return self._request("POST", f"/api/v1/workflows/{name}/run", inputs or {})

    def list_workflow_runs(self) -> list:
        return self._request("GET", "/api/v1/workflows/runs")

    def get_workflow_run(self, run_id: str) -> dict:
        return self._request("GET", f"/api/v1/workflows/runs/{run_id}")

    # ------------------------------------------------------------------ #
    # Knowledge
    # ------------------------------------------------------------------ #

    def list_knowledge(self) -> list:
        return self._request("GET", "/api/v1/knowledge")

    def ingest_knowledge(
        self,
        title: str,
        content: str,
        tags: str | list[str] | None = None,
    ) -> dict:
        if isinstance(tags, list):
            tags = ",".join(tags)
        return self._request(
            "POST",
            "/api/v1/knowledge",
            {"title": title, "content": content, "tags": tags or ""},
        )

    def search_knowledge(self, query: str, limit: int = 10) -> list:
        path = f"/api/v1/knowledge/search?q={quote(query)}&limit={limit}"
        return self._request("GET", path)

    def delete_knowledge(self, kid: str) -> dict:
        return self._request("DELETE", f"/api/v1/knowledge/{kid}")

    # ------------------------------------------------------------------ #
    # Audit
    # ------------------------------------------------------------------ #

    def query_audit(
        self,
        tenant: str | None = None,
        user: str | None = None,
        action: str | None = None,
        since: str | None = None,
        limit: int = 100,
    ) -> list:
        qs = self._qs({
            "tenant": tenant, "user": user, "action": action,
            "since": since, "limit": limit,
        })
        return self._request("GET", f"/api/v1/audit{qs}")

    # ------------------------------------------------------------------ #
    # Eval
    # ------------------------------------------------------------------ #

    def run_eval(
        self,
        agent: str,
        tenant: str,
        cases: Iterable[EvalCase | dict],
    ) -> str:
        """Submit an eval run; returns the run ID."""
        serialized: list[dict] = []
        for c in cases:
            if isinstance(c, EvalCase):
                serialized.append(c.to_dict())
            else:
                serialized.append(c)
        body = {"agent": agent, "tenant": tenant, "cases": serialized}
        resp = self._request("POST", "/api/v1/eval/run", body)
        if isinstance(resp, dict):
            return resp.get("run_id") or resp.get("id") or ""
        return ""

    def get_eval_run(self, run_id: str) -> EvalRun | None:
        data = self._request("GET", f"/api/v1/eval/runs/{run_id}")
        return EvalRun.from_dict(data if isinstance(data, dict) else None)

    def list_eval_runs(self) -> list:
        return self._request("GET", "/api/v1/eval/runs")

    # ------------------------------------------------------------------ #
    # Policies
    # ------------------------------------------------------------------ #

    def list_policy_rules(self) -> list:
        return self._request("GET", "/api/v1/policies/rules")

    def test_policy(self, tenant: str, action: str, tool: str = "", agent: str = "", user: str = "") -> dict:
        return self._request("POST", "/api/v1/policies/test", {
            "tenant": tenant, "action": action, "tool": tool, "agent": agent, "user": user,
        })

    # ------------------------------------------------------------------ #
    # Schedules / Federation / Approvals / Channels (kept for completeness)
    # ------------------------------------------------------------------ #

    def list_schedules(self) -> list:
        return self._request("GET", "/api/v1/schedules")

    def add_schedule(self, interval: str, tenant: str, agent: str, message: str, channel: str = "", channel_id: str = "") -> dict:
        body: dict[str, Any] = {
            "interval": interval, "tenant": tenant, "agent": agent, "message": message,
        }
        if channel:
            body["channel"] = channel
            body["channel_id"] = channel_id
        return self._request("POST", "/api/v1/schedules", body)

    def remove_schedule(self, schedule_id: str) -> dict:
        return self._request("POST", f"/api/v1/schedules/{schedule_id}/remove")

    def list_peers(self) -> list:
        return self._request("GET", "/api/v1/federation/peers")

    def join_peer(self, name: str, endpoint: str, secret: str = "") -> dict:
        return self._request("POST", "/api/v1/federation/peers", {
            "name": name, "endpoint": endpoint, "secret": secret,
        })

    def remove_peer(self, name: str) -> dict:
        return self._request("DELETE", f"/api/v1/federation/peers/{name}")

    def list_approvals(self) -> list:
        return self._request("GET", "/api/v1/approvals")

    def approve(self, approval_id: str, decided_by: str = "") -> dict:
        return self._request("POST", f"/api/v1/approvals/{approval_id}/approve", {"decided_by": decided_by})

    def deny(self, approval_id: str, decided_by: str = "") -> dict:
        return self._request("POST", f"/api/v1/approvals/{approval_id}/deny", {"decided_by": decided_by})

    def list_channels(self) -> list:
        return self._request("GET", "/api/v1/channels")

    # ------------------------------------------------------------------ #
    # Typed conveniences
    # ------------------------------------------------------------------ #

    def get_agent_typed(self, tenant: str, name: str) -> Agent | None:
        return Agent.from_dict(self.get_agent(tenant, name))

    def get_workflow_run_typed(self, run_id: str) -> WorkflowRun | None:
        return WorkflowRun.from_dict(self.get_workflow_run(run_id))


__all__ = ["CyntrClient", "CyntrError"]
