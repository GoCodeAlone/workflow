"""Type definitions for the Workflow SDK."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any


@dataclass
class Workflow:
    """Represents a configured workflow definition."""

    id: str
    name: str
    version: int
    status: str  # "active", "inactive", "draft", "error"
    config: dict[str, Any]
    created_at: str
    updated_at: str
    description: str = ""


@dataclass
class StepExecution:
    """Represents a single step within a workflow execution."""

    name: str
    status: str  # "pending", "running", "completed", "failed", "cancelled", "timeout"
    started_at: str
    completed_at: str | None = None
    duration_ms: int | None = None
    input: dict[str, Any] | None = None
    output: dict[str, Any] | None = None
    error: str | None = None


@dataclass
class Execution:
    """Represents a running or completed workflow execution."""

    id: str
    workflow_id: str
    status: str  # "pending", "running", "completed", "failed", "cancelled", "timeout"
    input: dict[str, Any]
    started_at: str
    steps: list[StepExecution] = field(default_factory=list)
    output: dict[str, Any] | None = None
    error: str | None = None
    completed_at: str | None = None
    duration_ms: int | None = None


@dataclass
class SSEEvent:
    """Represents a Server-Sent Event from execution streaming."""

    id: str
    event: str
    data: str


@dataclass
class DLQEntry:
    """Represents a dead-letter queue entry for failed events."""

    id: str
    workflow_id: str
    execution_id: str
    error: str
    payload: dict[str, Any]
    retry_count: int
    max_retries: int
    created_at: str
    last_retry_at: str | None = None


@dataclass
class HealthCheck:
    """Represents the result of a single health check."""

    status: str
    message: str | None = None


@dataclass
class HealthStatus:
    """Represents the overall system health."""

    status: str  # "healthy", "degraded", "unhealthy"
    checks: dict[str, HealthCheck] = field(default_factory=dict)


@dataclass
class ExecutionFilter:
    """Filter parameters for listing executions."""

    workflow_id: str | None = None
    status: str | None = None
    since: str | None = None
    until: str | None = None
    limit: int | None = None
    offset: int | None = None


@dataclass
class DLQFilter:
    """Filter parameters for listing DLQ entries."""

    workflow_id: str | None = None
    since: str | None = None
    limit: int | None = None
    offset: int | None = None
