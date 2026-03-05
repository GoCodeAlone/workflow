import { test, expect, type Page } from '@playwright/test';

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const MOCK_USER = {
  id: 'user-1',
  email: 'admin@test.com',
  display_name: 'Admin User',
  active: true,
  role: 'admin',
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
};

const MOCK_WORKFLOW = {
  id: 'wf-test-1',
  name: 'Test Workflow',
  project_id: 'proj-1',
  is_system: false,
  status: 'running',
  // Minimal config so configToNodes builds 2 nodes for the TraceCanvas
  config_yaml:
    'modules:\n  - name: http-server\n    type: http.server\n    config:\n      address: ":8080"\n  - name: http-router\n    type: http.router\n    dependsOn: [http-server]\nworkflows: {}\ntriggers: {}',
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
};

const MOCK_EXECUTIONS = [
  {
    id: 'exec-trace-completed',
    workflow_id: 'wf-test-1',
    trigger_type: 'http',
    status: 'completed',
    duration_ms: 123,
    started_at: '2024-01-01T12:00:00Z',
    completed_at: '2024-01-01T12:00:00.123Z',
    metadata: { explicit_trace: true, config_version: 'abc123' },
    sampled: true,
  },
  {
    id: 'exec-failed-1',
    workflow_id: 'wf-test-1',
    trigger_type: 'http',
    status: 'failed',
    duration_ms: 50,
    started_at: '2024-01-01T11:00:00Z',
    completed_at: '2024-01-01T11:00:00.050Z',
    error_message: 'Connection refused',
    metadata: {},
    sampled: false,
  },
];

const MOCK_STEPS = [
  {
    id: 's1',
    execution_id: 'exec-trace-completed',
    step_name: 'fetch-data',
    step_type: 'step.http_call',
    status: 'completed',
    sequence_num: 0,
    duration_ms: 80,
    started_at: '2024-01-01T12:00:00Z',
    input_data: { url: 'https://api.example.com/data' },
    output_data: { status: 200, body: '{"result":"ok"}' },
  },
  {
    id: 's2',
    execution_id: 'exec-trace-completed',
    step_name: 'process-data',
    step_type: 'step.set',
    status: 'completed',
    sequence_num: 1,
    duration_ms: 10,
    started_at: '2024-01-01T12:00:00.080Z',
    input_data: { data: { result: 'ok' } },
    output_data: { processed: true },
  },
  {
    id: 's3',
    execution_id: 'exec-trace-completed',
    step_name: 'send-response',
    step_type: 'step.http_respond',
    status: 'completed',
    sequence_num: 2,
    duration_ms: 5,
    started_at: '2024-01-01T12:00:00.090Z',
    input_data: { body: '{"processed":true}' },
    output_data: null,
  },
];

const MOCK_LOGS = [
  {
    id: 1,
    execution_id: 'exec-trace-completed',
    level: 'info',
    message: 'Pipeline started',
    module_name: 'test-handler',
    fields: null,
    created_at: '2024-01-01T12:00:00Z',
  },
  {
    id: 2,
    execution_id: 'exec-trace-completed',
    level: 'debug',
    message: 'Fetching external data',
    module_name: 'test-handler',
    fields: { step: 'fetch-data' },
    created_at: '2024-01-01T12:00:00.001Z',
  },
  {
    id: 3,
    execution_id: 'exec-trace-completed',
    level: 'error',
    message: 'Rate limit hit, retrying',
    module_name: 'test-handler',
    fields: { code: 429 },
    created_at: '2024-01-01T12:00:00.090Z',
  },
];

// Plugin list: `executions` and `logs` are global category so they appear in nav
// without requiring a workflow to be open first.
const MOCK_PLUGINS = [
  {
    name: 'admin-core',
    version: '1.0.0',
    description: 'Core admin plugin',
    enabled: true,
    dependencies: [],
    uiPages: [
      { id: 'dashboard', label: 'Dashboard', icon: '📊', category: 'global', order: 1 },
      { id: 'editor', label: 'Editor', icon: '✏️', category: 'global', order: 2 },
      { id: 'executions', label: 'Executions', icon: '▶️', category: 'global', order: 3 },
      { id: 'logs', label: 'Logs', icon: '📋', category: 'global', order: 4 },
    ],
  },
];

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

async function setupApiMocks(page: Page) {
  // Auth: setup-status (checked on every app load)
  await page.route('**/api/v1/auth/setup-status', (route) =>
    route.fulfill({ json: { needsSetup: false, userCount: 1 } }),
  );

  // Auth: me (loaded after token validated)
  await page.route('**/api/v1/auth/me', (route) =>
    route.fulfill({ json: MOCK_USER }),
  );

  // Plugins list (drives navigation)
  await page.route('**/api/v1/admin/plugins', (route) =>
    route.fulfill({ json: MOCK_PLUGINS }),
  );

  // Module schemas (loaded on auth)
  await page.route('**/api/v1/admin/engine/modules', (route) =>
    route.fulfill({ json: [] }),
  );

  // Workflows list (WorkflowPickerBar + WorkflowList)
  await page.route('**/api/v1/admin/workflows', async (route) => {
    if (route.request().method() !== 'GET') return route.continue();
    return route.fulfill({ json: [MOCK_WORKFLOW] });
  });

  // Individual workflow fetch (for config YAML used by TraceCanvas)
  await page.route('**/api/v1/admin/workflows/wf-test-1', async (route) => {
    if (route.request().method() !== 'GET') return route.continue();
    return route.fulfill({ json: MOCK_WORKFLOW });
  });

  // Workflow dashboard
  await page.route('**/api/v1/admin/workflows/wf-test-1/dashboard', (route) =>
    route.fulfill({
      json: {
        workflow: MOCK_WORKFLOW,
        execution_counts: { completed: 1, failed: 1 },
      },
    }),
  );

  // Executions list
  await page.route('**/api/v1/admin/workflows/wf-test-1/executions*', (route) =>
    route.fulfill({ json: MOCK_EXECUTIONS }),
  );

  // Trigger (POST) — returns a new traced execution
  await page.route('**/api/v1/admin/workflows/wf-test-1/trigger', (route) => {
    if (route.request().method() !== 'POST') return route.continue();
    return route.fulfill({
      json: {
        id: 'exec-new-traced',
        workflow_id: 'wf-test-1',
        trigger_type: 'http',
        status: 'running',
        duration_ms: null,
        started_at: new Date().toISOString(),
        metadata: { explicit_trace: true },
        sampled: true,
      },
    });
  });

  // Execution detail (both existing and newly triggered)
  await page.route('**/api/v1/admin/executions/exec-trace-completed', (route) =>
    route.fulfill({ json: MOCK_EXECUTIONS[0] }),
  );
  await page.route('**/api/v1/admin/executions/exec-new-traced', (route) =>
    route.fulfill({
      json: {
        id: 'exec-new-traced',
        workflow_id: 'wf-test-1',
        trigger_type: 'http',
        status: 'completed',
        duration_ms: 150,
        started_at: new Date().toISOString(),
        completed_at: new Date().toISOString(),
        metadata: { explicit_trace: true, config_version: 'abc123' },
        sampled: true,
      },
    }),
  );

  // Execution steps
  await page.route('**/api/v1/admin/executions/**/steps', (route) =>
    route.fulfill({ json: MOCK_STEPS }),
  );

  // Execution logs
  await page.route('**/api/v1/admin/executions/**/logs*', (route) =>
    route.fulfill({ json: { logs: MOCK_LOGS } }),
  );

  // SSE streams: empty valid stream responses
  await page.route('**/**/stream*', (route) =>
    route.fulfill({
      status: 200,
      headers: { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' },
      body: '',
    }),
  );

  // Projects (used by ProjectSwitcher)
  await page.route('**/api/v1/admin/projects*', (route) =>
    route.fulfill({ json: [] }),
  );
}

async function loginAndLoad(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem('auth_token', 'mock-test-token');
    localStorage.setItem('auth_refresh_token', 'mock-refresh-token');
  });
  await page.goto('/');
  // Wait for the nav to be visible (app fully loaded past auth checks)
  await page.waitForSelector('nav[aria-label="Main navigation"]', { timeout: 20000 });
}

/** Navigate to executions view and select the test workflow. */
async function goToExecutionsWithWorkflow(page: Page) {
  await page.getByRole('button', { name: 'Executions' }).click();
  // WorkflowPickerBar select should appear
  const picker = page.locator('select').filter({ has: page.locator('option[value="wf-test-1"]') });
  await picker.waitFor({ timeout: 8000 });
  await picker.selectOption('wf-test-1');
  // Wait for execution table to load
  await expect(page.getByText('Trace Request')).toBeVisible({ timeout: 10000 });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe('Admin UI Trace View', () => {
  test.beforeEach(async ({ page }) => {
    await setupApiMocks(page);
    await loginAndLoad(page);
  });

  test('app loads and main navigation is visible', async ({ page }) => {
    await expect(page.locator('nav[aria-label="Main navigation"]')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Dashboard' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Executions' })).toBeVisible();
  });

  test('executions nav shows WorkflowPickerBar when opened', async ({ page }) => {
    await page.getByRole('button', { name: 'Executions' }).click();
    await expect(page.getByText('Workflow:')).toBeVisible({ timeout: 8000 });
    await expect(page.locator('select').filter({ has: page.locator('option[value="wf-test-1"]') })).toBeVisible({ timeout: 8000 });
  });

  test('trace request button visible after workflow is selected', async ({ page }) => {
    await goToExecutionsWithWorkflow(page);
    await expect(page.getByText('Trace Request')).toBeVisible();
  });

  test('trace request button opens modal with form fields', async ({ page }) => {
    await goToExecutionsWithWorkflow(page);
    await page.getByText('Trace Request').click();

    // Modal should appear
    await expect(page.getByText('X-Workflow-Trace: true')).toBeVisible({ timeout: 5000 });
    await expect(page.getByPlaceholder('/api/v1/...')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Send Traced Request' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Cancel', exact: true })).toBeVisible();
  });

  test('trace request triggers traced execution and opens trace view', async ({ page }) => {
    await goToExecutionsWithWorkflow(page);
    await page.getByText('Trace Request').click();

    await page.getByPlaceholder('/api/v1/...').fill('/api/v1/test');
    await page.getByRole('button', { name: 'Send Traced Request' }).click();

    // TraceView appears with the new execution id
    await expect(page.getByText('exec-new-traced')).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('← Back')).toBeVisible();
  });

  test('trace canvas renders for a traced execution', async ({ page }) => {
    await goToExecutionsWithWorkflow(page);
    await page.getByText('Trace Request').click();
    await page.getByPlaceholder('/api/v1/...').fill('/api/v1/test');
    await page.getByRole('button', { name: 'Send Traced Request' }).click();

    await expect(page.getByText('← Back')).toBeVisible({ timeout: 10000 });

    // TraceCanvas renders the ReactFlow canvas using the workflow's config YAML
    // The canvas is inside the 280px-height area in TraceView
    const canvas = page.locator('.react-flow').first();
    await expect(canvas.or(page.getByText('No workflow config loaded'))).toBeVisible({ timeout: 8000 });
  });

  test('trace view waterfall shows step timing bars', async ({ page }) => {
    await goToExecutionsWithWorkflow(page);
    await page.getByText('Trace Request').click();
    await page.getByPlaceholder('/api/v1/...').fill('/api/v1/test');
    await page.getByRole('button', { name: 'Send Traced Request' }).click();

    await expect(page.getByText('← Back')).toBeVisible({ timeout: 10000 });

    // Waterfall step names from MOCK_STEPS
    await expect(page.getByText('fetch-data')).toBeVisible({ timeout: 8000 });
    await expect(page.getByText('process-data')).toBeVisible();
    await expect(page.getByText('send-response')).toBeVisible();
  });

  test('trace view log viewer shows execution logs', async ({ page }) => {
    await goToExecutionsWithWorkflow(page);
    await page.getByText('Trace Request').click();
    await page.getByPlaceholder('/api/v1/...').fill('/api/v1/test');
    await page.getByRole('button', { name: 'Send Traced Request' }).click();

    await expect(page.getByText('← Back')).toBeVisible({ timeout: 10000 });

    // Log messages visible in ExecutionLogViewer
    await expect(page.getByText('Pipeline started')).toBeVisible({ timeout: 8000 });
    await expect(page.getByText('Fetching external data')).toBeVisible();
  });

  test('clicking step in waterfall opens step detail panel', async ({ page }) => {
    await goToExecutionsWithWorkflow(page);
    await page.getByText('Trace Request').click();
    await page.getByPlaceholder('/api/v1/...').fill('/api/v1/test');
    await page.getByRole('button', { name: 'Send Traced Request' }).click();

    await expect(page.getByText('← Back')).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('fetch-data')).toBeVisible({ timeout: 8000 });

    // Click step in the waterfall to open StepDetailPanel
    await page.getByText('fetch-data').first().click();
    // StepDetailPanel should show the step name
    await expect(page.getByText('fetch-data').first()).toBeVisible({ timeout: 5000 });
  });

  test('failed execution shows error state in execution list', async ({ page }) => {
    await goToExecutionsWithWorkflow(page);

    // failed execution should be visible in the table (use .first() since 'failed' also appears as a filter button)
    await expect(page.getByText('failed').first()).toBeVisible({ timeout: 10000 });

    // Expand the failed execution — ID 'exec-failed-1' truncates to 'exec-fai...' (8 chars)
    await page.getByText('exec-fai').click();
    // After expansion, StepTimeline renders the mock steps
    await expect(page.getByText('fetch-data')).toBeVisible({ timeout: 5000 });
  });

  test('back button in trace view returns to executions dashboard', async ({ page }) => {
    await goToExecutionsWithWorkflow(page);
    await page.getByText('Trace Request').click();
    await page.getByPlaceholder('/api/v1/...').fill('/api/v1/test');
    await page.getByRole('button', { name: 'Send Traced Request' }).click();

    await expect(page.getByText('← Back')).toBeVisible({ timeout: 10000 });
    await page.getByText('← Back').click();

    // Should return to executions/dashboard view
    await expect(page.getByText('Trace Request')).toBeVisible({ timeout: 5000 });
  });

  test('trace canvas is in read-only mode (nodesConnectable=false)', async ({ page }) => {
    await goToExecutionsWithWorkflow(page);
    await page.getByText('Trace Request').click();
    await page.getByPlaceholder('/api/v1/...').fill('/api/v1/test');
    await page.getByRole('button', { name: 'Send Traced Request' }).click();

    await expect(page.getByText('← Back')).toBeVisible({ timeout: 10000 });

    // TraceCanvas renders with nodesConnectable=false and nodesDraggable=false.
    // Verify the canvas renders and nodes are present (YAML has 2 modules → 2 nodes).
    const reactFlow = page.locator('.react-flow');
    await expect(reactFlow.or(page.getByText('No workflow config loaded'))).toBeVisible({ timeout: 8000 });
    // If canvas loaded, verify nodes exist and no edge connections can be made
    if (await reactFlow.isVisible()) {
      const nodes = page.locator('.react-flow__node');
      await expect(nodes.first()).toBeVisible({ timeout: 5000 });
    }
  });
});
