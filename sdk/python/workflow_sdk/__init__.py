"""
workflow-sdk: Python client SDK for the GoCodeAlone Workflow engine.

Example usage::

    from workflow_sdk import WorkflowClient

    client = WorkflowClient("http://localhost:8080", api_key="optional-key")

    # List workflows
    workflows = client.list_workflows()

    # Execute a workflow
    execution = client.execute_workflow("my-workflow", {"order_id": "12345"})

    # Stream execution events via SSE
    for event in client.stream_execution(execution.id):
        print(f"[{event.event}] {event.data}")
"""

from workflow_sdk.client import WorkflowClient, WorkflowError
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

__all__ = [
    "WorkflowClient",
    "WorkflowError",
    "DLQEntry",
    "DLQFilter",
    "Execution",
    "ExecutionFilter",
    "HealthCheck",
    "HealthStatus",
    "SSEEvent",
    "StepExecution",
    "Workflow",
]

__version__ = "0.1.0"
