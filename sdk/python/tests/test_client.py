"""Unit tests for the Cyntr Python SDK.

Pure stdlib: mocks ``urllib.request.urlopen`` via ``unittest.mock.patch``
so no live server (and no ``aiohttp``) is required.
"""

from __future__ import annotations

import io
import json
import os
import sys
import unittest
from unittest.mock import MagicMock, patch
from urllib.error import HTTPError

# Ensure the sibling ``cyntr`` package is importable when running
# `python3 -m unittest discover sdk/python/tests/` from the repo root.
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.dirname(HERE))

from cyntr import CyntrClient, CyntrError, EvalCase  # noqa: E402
from cyntr.models import Agent, EvalRun, WorkflowRun  # noqa: E402


def _envelope(data):
    """Build a Cyntr API envelope as raw bytes."""
    return json.dumps({
        "data": data,
        "meta": {"request_id": "req_test", "timestamp": "2026-01-01T00:00:00Z"},
        "error": None,
    }).encode()


def _mock_response(payload, status: int = 200):
    """Build a mock object behaving like the urllib context manager return."""
    body = _envelope(payload) if status < 400 else payload
    resp = MagicMock()
    resp.read.return_value = body if isinstance(body, bytes) else str(body).encode()
    resp.status = status
    resp.__enter__ = MagicMock(return_value=resp)
    resp.__exit__ = MagicMock(return_value=False)
    return resp


class CapturingMockUrlopen:
    """Captures the Request passed to urlopen and returns a canned payload."""

    def __init__(self, payload, status: int = 200):
        self.payload = payload
        self.status = status
        self.last_request = None

    def __call__(self, request, timeout=None):
        self.last_request = request
        return _mock_response(self.payload, self.status)


class TestCyntrClient(unittest.TestCase):
    def setUp(self):
        self.client = CyntrClient("http://localhost:7700", api_key="cyntr_test")

    # -- agents ----------------------------------------------------------

    def test_list_agents(self):
        mock = CapturingMockUrlopen(["agent_a", "agent_b"])
        with patch("cyntr.client.urlopen", mock):
            agents = self.client.list_agents("acme")
        self.assertEqual(agents, ["agent_a", "agent_b"])
        self.assertEqual(mock.last_request.method, "GET")
        self.assertTrue(mock.last_request.full_url.endswith("/api/v1/tenants/acme/agents"))
        # Auth header sent.
        self.assertEqual(mock.last_request.get_header("Authorization"), "Bearer cyntr_test")

    def test_create_agent(self):
        mock = CapturingMockUrlopen({"status": "created", "agent": "bot"})
        with patch("cyntr.client.urlopen", mock):
            out = self.client.create_agent(
                "acme", name="bot", model="claude", prompt="be terse",
                tools=["*"], skills=["search"],
            )
        self.assertEqual(out["status"], "created")
        body = json.loads(mock.last_request.data.decode())
        self.assertEqual(body["name"], "bot")
        self.assertEqual(body["model"], "claude")
        self.assertEqual(body["system_prompt"], "be terse")
        self.assertEqual(body["tools"], ["*"])
        self.assertEqual(body["skills"], ["search"])

    def test_chat_non_streaming(self):
        mock = CapturingMockUrlopen({"agent": "bot", "content": "hi", "tools_used": None})
        with patch("cyntr.client.urlopen", mock):
            resp = self.client.chat("acme", "bot", "hello", user="u1", channel="slack")
        self.assertEqual(resp["content"], "hi")
        body = json.loads(mock.last_request.data.decode())
        self.assertEqual(body, {"message": "hello", "user": "u1", "channel": "slack"})
        self.assertTrue(mock.last_request.full_url.endswith("/agents/bot/chat"))

    # -- workflows -------------------------------------------------------

    def test_run_workflow(self):
        mock = CapturingMockUrlopen({"run_id": "wfr_1"})
        with patch("cyntr.client.urlopen", mock):
            out = self.client.run_workflow("acme", "nightly", {"x": 1})
        self.assertEqual(out["run_id"], "wfr_1")
        self.assertTrue(mock.last_request.full_url.endswith("/workflows/nightly/run"))
        self.assertEqual(json.loads(mock.last_request.data.decode()), {"x": 1})

    def test_get_workflow_run_typed(self):
        mock = CapturingMockUrlopen({
            "ID": "wfr_1", "WorkflowID": "nightly", "Status": "completed",
            "StartedAt": "t1", "CompletedAt": "t2",
        })
        with patch("cyntr.client.urlopen", mock):
            run = self.client.get_workflow_run_typed("wfr_1")
        self.assertIsInstance(run, WorkflowRun)
        self.assertEqual(run.id, "wfr_1")
        self.assertEqual(run.status, "completed")

    # -- eval ------------------------------------------------------------

    def test_run_eval(self):
        mock = CapturingMockUrlopen({"run_id": "ev_42"})
        with patch("cyntr.client.urlopen", mock):
            rid = self.client.run_eval(
                "bot", "acme",
                [EvalCase(name="c1", prompt="2+2?", expected="4")],
            )
        self.assertEqual(rid, "ev_42")
        body = json.loads(mock.last_request.data.decode())
        self.assertEqual(body["agent"], "bot")
        self.assertEqual(body["tenant"], "acme")
        self.assertEqual(len(body["cases"]), 1)
        self.assertEqual(body["cases"][0]["name"], "c1")

    def test_get_eval_run(self):
        mock = CapturingMockUrlopen({
            "id": "ev_42", "status": "passed", "total": 3, "passed": 3, "failed": 0,
        })
        with patch("cyntr.client.urlopen", mock):
            run = self.client.get_eval_run("ev_42")
        self.assertIsInstance(run, EvalRun)
        self.assertEqual(run.id, "ev_42")
        self.assertEqual(run.total, 3)
        self.assertEqual(run.passed, 3)

    # -- knowledge / skills ---------------------------------------------

    def test_search_knowledge(self):
        mock = CapturingMockUrlopen([{"id": "kb1", "title": "doc"}])
        with patch("cyntr.client.urlopen", mock):
            out = self.client.search_knowledge("postgres", limit=5)
        self.assertEqual(len(out), 1)
        self.assertIn("q=postgres", mock.last_request.full_url)
        self.assertIn("limit=5", mock.last_request.full_url)

    # -- error handling --------------------------------------------------

    def test_error_response(self):
        err_payload = json.dumps({
            "data": None,
            "meta": {"request_id": "req_err"},
            "error": {"code": "NOT_FOUND", "message": "agent missing"},
        }).encode()
        http_err = HTTPError(
            url="http://x/", code=404, msg="Not Found", hdrs=None,
            fp=io.BytesIO(err_payload),
        )

        def raising(*a, **kw):
            raise http_err

        with patch("cyntr.client.urlopen", side_effect=raising):
            with self.assertRaises(CyntrError) as cm:
                self.client.get_agent("acme", "missing")
        self.assertEqual(cm.exception.status, 404)
        self.assertEqual(cm.exception.code, "NOT_FOUND")
        self.assertEqual(cm.exception.request_id, "req_err")
        self.assertIn("agent missing", cm.exception.message)

    # -- audit / system -------------------------------------------------

    def test_query_audit_filters(self):
        mock = CapturingMockUrlopen([])
        with patch("cyntr.client.urlopen", mock):
            self.client.query_audit(tenant="acme", action="chat", limit=50)
        url = mock.last_request.full_url
        self.assertIn("tenant=acme", url)
        self.assertIn("action=chat", url)
        self.assertIn("limit=50", url)

    def test_health_no_auth_optional(self):
        mock = CapturingMockUrlopen({"ok": True})
        c = CyntrClient("http://localhost:7700")  # no api key
        with patch("cyntr.client.urlopen", mock):
            self.assertEqual(c.health(), {"ok": True})
        self.assertIsNone(mock.last_request.get_header("Authorization"))


if __name__ == "__main__":
    unittest.main()
