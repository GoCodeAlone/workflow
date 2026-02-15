import type {
  ClientOptions,
  DLQEntry,
  DLQFilter,
  Execution,
  ExecutionFilter,
  HealthStatus,
  SSEEvent,
  Workflow,
  WorkflowConfig,
} from "./types.js";

/** WorkflowError is thrown for non-2xx API responses. */
export class WorkflowError extends Error {
  public readonly status: number;
  public readonly statusText: string;
  public readonly body: string;

  constructor(status: number, statusText: string, body: string) {
    super(`Workflow API error ${status} ${statusText}: ${body}`);
    this.name = "WorkflowError";
    this.status = status;
    this.statusText = statusText;
    this.body = body;
  }
}

/**
 * WorkflowClient provides a typed interface to the Workflow engine REST API
 * and SSE streaming endpoints.
 */
export interface WorkflowClient {
  // Workflows
  listWorkflows(): Promise<Workflow[]>;
  getWorkflow(id: string): Promise<Workflow>;
  createWorkflow(config: WorkflowConfig): Promise<Workflow>;
  deleteWorkflow(id: string): Promise<void>;

  // Executions
  executeWorkflow(
    id: string,
    data: Record<string, unknown>,
  ): Promise<Execution>;
  getExecution(id: string): Promise<Execution>;
  listExecutions(filter?: ExecutionFilter): Promise<Execution[]>;

  // Live tracing
  streamExecution(executionId: string): AsyncGenerator<SSEEvent>;

  // DLQ
  listDLQEntries(filter?: DLQFilter): Promise<DLQEntry[]>;
  retryDLQEntry(id: string): Promise<void>;

  // Health
  health(): Promise<HealthStatus>;
}

/**
 * Creates a new WorkflowClient that communicates with the Workflow engine API.
 *
 * @example
 * ```ts
 * const client = createClient({ baseURL: "http://localhost:8080" });
 * const workflows = await client.listWorkflows();
 * ```
 */
export function createClient(options: ClientOptions): WorkflowClient {
  const baseURL = options.baseURL.replace(/\/+$/, "");
  const apiKey = options.apiKey;
  const fetchFn = options.fetch ?? globalThis.fetch;
  const timeout = options.timeout ?? 30_000;

  function buildHeaders(): HeadersInit {
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      Accept: "application/json",
    };
    if (apiKey) {
      headers["Authorization"] = `Bearer ${apiKey}`;
    }
    return headers;
  }

  async function request<T>(
    method: string,
    path: string,
    body?: unknown,
  ): Promise<T> {
    const url = `${baseURL}${path}`;
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeout);

    try {
      const resp = await fetchFn(url, {
        method,
        headers: buildHeaders(),
        body: body !== undefined ? JSON.stringify(body) : undefined,
        signal: controller.signal,
      });

      if (!resp.ok) {
        const text = await resp.text().catch(() => "");
        throw new WorkflowError(resp.status, resp.statusText, text);
      }

      // 204 No Content
      if (resp.status === 204) {
        return undefined as T;
      }

      return (await resp.json()) as T;
    } finally {
      clearTimeout(timer);
    }
  }

  function buildQueryString(params: Record<string, unknown>): string {
    const parts: string[] = [];
    for (const [key, value] of Object.entries(params)) {
      if (value !== undefined && value !== null) {
        parts.push(
          `${encodeURIComponent(key)}=${encodeURIComponent(String(value))}`,
        );
      }
    }
    return parts.length > 0 ? `?${parts.join("&")}` : "";
  }

  // SSE streaming via ReadableStream (works in browsers and Node 18+)
  async function* streamSSE(
    executionId: string,
  ): AsyncGenerator<SSEEvent, void, undefined> {
    const url = `${baseURL}/api/v1/executions/${encodeURIComponent(executionId)}/stream`;
    const headers: Record<string, string> = {
      Accept: "text/event-stream",
    };
    if (apiKey) {
      headers["Authorization"] = `Bearer ${apiKey}`;
    }

    const resp = await fetchFn(url, { headers });
    if (!resp.ok) {
      const text = await resp.text().catch(() => "");
      throw new WorkflowError(resp.status, resp.statusText, text);
    }

    if (!resp.body) {
      throw new WorkflowError(0, "No body", "Response body is null");
    }

    const reader = resp.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";

    // Current event being assembled
    let currentId = "";
    let currentEvent = "";
    let currentData = "";

    try {
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split("\n");
        // Keep the last incomplete line in the buffer
        buffer = lines.pop() ?? "";

        for (const line of lines) {
          if (line === "") {
            // Empty line = end of event
            if (currentData || currentEvent || currentId) {
              yield {
                id: currentId,
                event: currentEvent,
                data: currentData,
              };
              currentId = "";
              currentEvent = "";
              currentData = "";
            }
          } else if (line.startsWith("id: ")) {
            currentId = line.slice(4);
          } else if (line.startsWith("event: ")) {
            currentEvent = line.slice(7);
          } else if (line.startsWith("data: ")) {
            currentData = line.slice(6);
          } else if (line.startsWith(":")) {
            // Comment line, ignore
          }
        }
      }

      // Flush any remaining event in the buffer
      if (currentData || currentEvent || currentId) {
        yield {
          id: currentId,
          event: currentEvent,
          data: currentData,
        };
      }
    } finally {
      reader.releaseLock();
    }
  }

  return {
    // ---- Workflows ----

    async listWorkflows(): Promise<Workflow[]> {
      return request<Workflow[]>("GET", "/api/v1/workflows");
    },

    async getWorkflow(id: string): Promise<Workflow> {
      return request<Workflow>(
        "GET",
        `/api/v1/workflows/${encodeURIComponent(id)}`,
      );
    },

    async createWorkflow(config: WorkflowConfig): Promise<Workflow> {
      return request<Workflow>("POST", "/api/v1/workflows", config);
    },

    async deleteWorkflow(id: string): Promise<void> {
      return request<void>(
        "DELETE",
        `/api/v1/workflows/${encodeURIComponent(id)}`,
      );
    },

    // ---- Executions ----

    async executeWorkflow(
      id: string,
      data: Record<string, unknown>,
    ): Promise<Execution> {
      return request<Execution>(
        "POST",
        `/api/v1/workflows/${encodeURIComponent(id)}/execute`,
        data,
      );
    },

    async getExecution(id: string): Promise<Execution> {
      return request<Execution>(
        "GET",
        `/api/v1/executions/${encodeURIComponent(id)}`,
      );
    },

    async listExecutions(filter?: ExecutionFilter): Promise<Execution[]> {
      const qs = filter ? buildQueryString(filter as Record<string, unknown>) : "";
      return request<Execution[]>("GET", `/api/v1/executions${qs}`);
    },

    // ---- Live Tracing ----

    streamExecution(executionId: string): AsyncGenerator<SSEEvent> {
      return streamSSE(executionId);
    },

    // ---- DLQ ----

    async listDLQEntries(filter?: DLQFilter): Promise<DLQEntry[]> {
      const qs = filter ? buildQueryString(filter as Record<string, unknown>) : "";
      return request<DLQEntry[]>("GET", `/api/v1/dlq${qs}`);
    },

    async retryDLQEntry(id: string): Promise<void> {
      return request<void>(
        "POST",
        `/api/v1/dlq/${encodeURIComponent(id)}/retry`,
      );
    },

    // ---- Health ----

    async health(): Promise<HealthStatus> {
      return request<HealthStatus>("GET", "/healthz");
    },
  };
}
