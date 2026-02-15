"""Workflow engine client implementation using httpx."""

from __future__ import annotations

from collections.abc import Iterator
from dataclasses import asdict
from typing import Any

import httpx

from workflow_sdk.types import (
    DLQEntry,
    DLQFilter,
    Execution,
    ExecutionFilter,
    HealthCheck,
    HealthStatus,
    SSEEvent,
    StepExecution,
    Workflow,
)


class WorkflowError(Exception):
    """Raised when the Workflow API returns a non-2xx response."""

    def __init__(self, status_code: int, message: str, body: str = "") -> None:
        self.status_code = status_code
        self.body = body
        super().__init__(f"Workflow API error {status_code}: {message}")


class WorkflowClient:
    """Client for communicating with the GoCodeAlone Workflow engine API.

    Args:
        base_url: Base URL of the workflow engine (e.g., "http://localhost:8080").
        api_key: Optional API key for authentication.
        timeout: Request timeout in seconds (default: 30).
    """

    def __init__(
        self,
        base_url: str,
        api_key: str | None = None,
        timeout: float = 30.0,
    ) -> None:
        self._base_url = base_url.rstrip("/")
        self._api_key = api_key
        self._timeout = timeout
        self._client = httpx.Client(
            base_url=self._base_url,
            timeout=timeout,
            headers=self._build_headers(),
        )

    def _build_headers(self) -> dict[str, str]:
        headers: dict[str, str] = {
            "Content-Type": "application/json",
            "Accept": "application/json",
        }
        if self._api_key:
            headers["Authorization"] = f"Bearer {self._api_key}"
        return headers

    def _request(
        self,
        method: str,
        path: str,
        *,
        json: Any = None,
        params: dict[str, Any] | None = None,
    ) -> Any:
        """Make an HTTP request and return the JSON response."""
        # Filter out None values from params
        if params:
            params = {k: v for k, v in params.items() if v is not None}

        resp = self._client.request(method, path, json=json, params=params)
        if resp.status_code >= 400:
            raise WorkflowError(
                status_code=resp.status_code,
                message=resp.reason_phrase or "Unknown error",
                body=resp.text,
            )
        if resp.status_code == 204:
            return None
        return resp.json()

    def close(self) -> None:
        """Close the underlying HTTP client."""
        self._client.close()

    def __enter__(self) -> WorkflowClient:
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()

    # ---- Workflows ----

    def list_workflows(self) -> list[Workflow]:
        """List all workflows."""
        data = self._request("GET", "/api/v1/workflows")
        return [_parse_workflow(w) for w in data]

    def get_workflow(self, workflow_id: str) -> Workflow:
        """Get a single workflow by ID."""
        data = self._request("GET", f"/api/v1/workflows/{workflow_id}")
        return _parse_workflow(data)

    def create_workflow(self, config: dict[str, Any]) -> Workflow:
        """Create a new workflow from a configuration dict."""
        data = self._request("POST", "/api/v1/workflows", json=config)
        return _parse_workflow(data)

    def delete_workflow(self, workflow_id: str) -> None:
        """Delete a workflow by ID."""
        self._request("DELETE", f"/api/v1/workflows/{workflow_id}")

    # ---- Executions ----

    def execute_workflow(
        self, workflow_id: str, data: dict[str, Any]
    ) -> Execution:
        """Execute a workflow with the given input data."""
        resp = self._request(
            "POST", f"/api/v1/workflows/{workflow_id}/execute", json=data
        )
        return _parse_execution(resp)

    def get_execution(self, execution_id: str) -> Execution:
        """Get a single execution by ID."""
        data = self._request("GET", f"/api/v1/executions/{execution_id}")
        return _parse_execution(data)

    def list_executions(
        self, filter: ExecutionFilter | None = None
    ) -> list[Execution]:
        """List executions with optional filtering."""
        params: dict[str, Any] = {}
        if filter:
            params = {k: v for k, v in asdict(filter).items() if v is not None}
        data = self._request("GET", "/api/v1/executions", params=params)
        return [_parse_execution(e) for e in data]

    # ---- Live Tracing (SSE) ----

    def stream_execution(self, execution_id: str) -> Iterator[SSEEvent]:
        """Stream execution events via Server-Sent Events.

        Yields SSEEvent objects as they arrive. The iterator completes when
        the server closes the connection.

        Args:
            execution_id: The execution ID to stream events for.

        Yields:
            SSEEvent with id, event type, and data fields.
        """
        url = f"{self._base_url}/api/v1/executions/{execution_id}/stream"
        headers: dict[str, str] = {"Accept": "text/event-stream"}
        if self._api_key:
            headers["Authorization"] = f"Bearer {self._api_key}"

        with httpx.stream("GET", url, headers=headers, timeout=None) as resp:
            if resp.status_code >= 400:
                resp.read()
                raise WorkflowError(
                    status_code=resp.status_code,
                    message=resp.reason_phrase or "Unknown error",
                    body=resp.text,
                )

            current_id = ""
            current_event = ""
            current_data = ""

            for line in resp.iter_lines():
                if line == "":
                    # Empty line = end of event
                    if current_data or current_event or current_id:
                        yield SSEEvent(
                            id=current_id,
                            event=current_event,
                            data=current_data,
                        )
                        current_id = ""
                        current_event = ""
                        current_data = ""
                elif line.startswith("id: "):
                    current_id = line[4:]
                elif line.startswith("event: "):
                    current_event = line[7:]
                elif line.startswith("data: "):
                    current_data = line[6:]
                elif line.startswith(":"):
                    # Comment line, ignore
                    pass

            # Flush any remaining event
            if current_data or current_event or current_id:
                yield SSEEvent(
                    id=current_id,
                    event=current_event,
                    data=current_data,
                )

    # ---- DLQ ----

    def list_dlq_entries(
        self, filter: DLQFilter | None = None
    ) -> list[DLQEntry]:
        """List dead-letter queue entries."""
        params: dict[str, Any] = {}
        if filter:
            params = {k: v for k, v in asdict(filter).items() if v is not None}
        data = self._request("GET", "/api/v1/dlq", params=params)
        return [_parse_dlq_entry(e) for e in data]

    def retry_dlq_entry(self, entry_id: str) -> None:
        """Retry a failed DLQ entry."""
        self._request("POST", f"/api/v1/dlq/{entry_id}/retry")

    # ---- Health ----

    def health(self) -> HealthStatus:
        """Check the system health status."""
        data = self._request("GET", "/healthz")
        checks = {
            name: HealthCheck(
                status=check.get("status", "unknown"),
                message=check.get("message"),
            )
            for name, check in data.get("checks", {}).items()
        }
        return HealthStatus(status=data["status"], checks=checks)


# ---- Parsing helpers ----


def _parse_workflow(data: dict[str, Any]) -> Workflow:
    return Workflow(
        id=data["id"],
        name=data["name"],
        version=data.get("version", 0),
        status=data.get("status", "draft"),
        config=data.get("config", {}),
        created_at=data.get("created_at", ""),
        updated_at=data.get("updated_at", ""),
        description=data.get("description", ""),
    )


def _parse_step_execution(data: dict[str, Any]) -> StepExecution:
    return StepExecution(
        name=data["name"],
        status=data["status"],
        started_at=data.get("started_at", ""),
        completed_at=data.get("completed_at"),
        duration_ms=data.get("duration_ms"),
        input=data.get("input"),
        output=data.get("output"),
        error=data.get("error"),
    )


def _parse_execution(data: dict[str, Any]) -> Execution:
    steps = [_parse_step_execution(s) for s in data.get("steps", [])]
    return Execution(
        id=data["id"],
        workflow_id=data["workflow_id"],
        status=data["status"],
        input=data.get("input", {}),
        started_at=data.get("started_at", ""),
        steps=steps,
        output=data.get("output"),
        error=data.get("error"),
        completed_at=data.get("completed_at"),
        duration_ms=data.get("duration_ms"),
    )


def _parse_dlq_entry(data: dict[str, Any]) -> DLQEntry:
    return DLQEntry(
        id=data["id"],
        workflow_id=data["workflow_id"],
        execution_id=data["execution_id"],
        error=data["error"],
        payload=data.get("payload", {}),
        retry_count=data.get("retry_count", 0),
        max_retries=data.get("max_retries", 0),
        created_at=data.get("created_at", ""),
        last_retry_at=data.get("last_retry_at"),
    )
