"""Typed response models for the Cyntr SDK.

Lightweight dataclasses backed by the stdlib only — no pydantic.
Every model exposes a ``from_dict`` classmethod that tolerates missing
or extra keys so the SDK doesn't break when the server evolves.
"""

from __future__ import annotations

from dataclasses import dataclass, field, fields
from typing import Any


def _coerce(cls, data: Any):
    """Coerce a dict into a dataclass, ignoring unknown keys."""
    if data is None:
        return None
    if not isinstance(data, dict):
        return data
    known = {f.name for f in fields(cls)}
    return cls(**{k: v for k, v in data.items() if k in known})


@dataclass
class Agent:
    """A Cyntr agent definition."""
    name: str = ""
    tenant: str = ""
    model: str = ""
    system_prompt: str = ""
    tools: list[str] = field(default_factory=list)
    skills: list[str] = field(default_factory=list)
    max_turns: int = 0
    max_history: int = 0
    rate_limit: int = 0

    @classmethod
    def from_dict(cls, data: dict | None) -> "Agent | None":
        return _coerce(cls, data)


@dataclass
class Session:
    """A conversation session with an agent."""
    id: str = ""
    tenant: str = ""
    agent: str = ""
    messages: list[dict] = field(default_factory=list)
    created_at: str = ""
    updated_at: str = ""

    @classmethod
    def from_dict(cls, data: dict | None) -> "Session | None":
        return _coerce(cls, data)


@dataclass
class WorkflowRun:
    """An execution of a workflow."""
    id: str = ""
    workflow_id: str = ""
    status: str = ""
    current_step: str = ""
    started_at: str = ""
    completed_at: str = ""
    error: str = ""

    @classmethod
    def from_dict(cls, data: dict | None) -> "WorkflowRun | None":
        if data is None:
            return None
        # Server uses TitleCase (ID, WorkflowID, Status…); normalize to snake_case.
        norm = {
            "id": data.get("id") or data.get("ID") or data.get("run_id", ""),
            "workflow_id": data.get("workflow_id") or data.get("WorkflowID", ""),
            "status": data.get("status") or data.get("Status", ""),
            "current_step": data.get("current_step") or data.get("CurrentStep", ""),
            "started_at": data.get("started_at") or data.get("StartedAt", ""),
            "completed_at": data.get("completed_at") or data.get("CompletedAt", ""),
            "error": data.get("error") or data.get("Error", ""),
        }
        return cls(**norm)


@dataclass
class EvalCase:
    """A single eval test case."""
    name: str = ""
    agent: str = ""
    tenant: str = ""
    prompt: str = ""
    expected: str = ""
    match: str = "contains"

    def to_dict(self) -> dict:
        return {f.name: getattr(self, f.name) for f in fields(self)}


@dataclass
class EvalRun:
    """An eval run summary."""
    id: str = ""
    agent: str = ""
    tenant: str = ""
    status: str = ""
    total: int = 0
    passed: int = 0
    failed: int = 0
    started_at: str = ""
    completed_at: str = ""
    results: list[dict] = field(default_factory=list)

    @classmethod
    def from_dict(cls, data: dict | None) -> "EvalRun | None":
        if data is None:
            return None
        # Be lenient about server casing.
        norm = {
            "id": data.get("id") or data.get("ID") or data.get("run_id", ""),
            "agent": data.get("agent") or data.get("Agent", ""),
            "tenant": data.get("tenant") or data.get("Tenant", ""),
            "status": data.get("status") or data.get("Status", ""),
            "total": int(data.get("total") or data.get("Total") or 0),
            "passed": int(data.get("passed") or data.get("Passed") or 0),
            "failed": int(data.get("failed") or data.get("Failed") or 0),
            "started_at": data.get("started_at") or data.get("StartedAt", ""),
            "completed_at": data.get("completed_at") or data.get("CompletedAt", ""),
            "results": data.get("results") or data.get("Results") or [],
        }
        return cls(**norm)


__all__ = ["Agent", "Session", "WorkflowRun", "EvalCase", "EvalRun"]
