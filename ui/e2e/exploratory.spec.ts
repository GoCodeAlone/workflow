import { test, expect } from '@playwright/test';
import path from 'path';
import fs from 'fs';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

/**
 * Helper: Simulate dragging a palette item to the canvas.
 * Scrolls the palette item into view before dragging to handle sidebar overflow.
 */
async function dragModuleToCanvas(
  page: import('@playwright/test').Page,
  moduleLabel: string,
  canvasX: number,
  canvasY: number,
) {
  const paletteItem = page.getByText(moduleLabel, { exact: true }).first();
  await paletteItem.scrollIntoViewIfNeeded();
  await expect(paletteItem).toBeVisible({ timeout: 5000 });
  await page.waitForTimeout(200);

  const sourceBounds = await paletteItem.boundingBox();
  if (!sourceBounds) throw new Error(`Could not find palette item: ${moduleLabel}`);

  const canvas = page.locator('.react-flow').first();
  const canvasBounds = await canvas.boundingBox();
  if (!canvasBounds) throw new Error('Could not find canvas');

  const dropX = canvasBounds.x + canvasX;
  const dropY = canvasBounds.y + canvasY;

  const sourceX = sourceBounds.x + sourceBounds.width / 2;
  const sourceY = sourceBounds.y + sourceBounds.height / 2;

  await page.evaluate(
    ({ srcX, srcY, dstX, dstY, label }) => {
      const source = document.elementFromPoint(srcX, srcY) as HTMLElement;
      const target = document.elementFromPoint(dstX, dstY) as HTMLElement;
      if (!source || !target) return;

      let draggable = source;
      while (draggable && draggable.getAttribute('draggable') !== 'true') {
        draggable = draggable.parentElement as HTMLElement;
      }

      const moduleTypeMap: Record<string, string> = {
        'HTTP Server': 'http.server',
        'HTTP Router': 'http.router',
        'HTTP Handler': 'http.handler',
        'HTTP Proxy': 'http.proxy',
        'API Handler': 'api.handler',
        'Auth Middleware': 'http.middleware.auth',
        'Logging Middleware': 'http.middleware.logging',
        'Rate Limiter': 'http.middleware.ratelimit',
        'CORS Middleware': 'http.middleware.cors',
        'Message Broker': 'messaging.broker',
        'Message Handler': 'messaging.handler',
        'State Machine': 'statemachine.engine',
        'State Tracker': 'state.tracker',
        'State Connector': 'state.connector',
        'Scheduler': 'scheduler.modular',
        'Auth Service': 'auth.modular',
        'Event Bus': 'eventbus.modular',
        'Cache': 'cache.modular',
        'Chi Mux Router': 'chimux.router',
        'Event Logger': 'eventlogger.modular',
        'HTTP Client': 'httpclient.modular',
        'Database': 'database.modular',
        'JSON Schema Validator': 'jsonschema.modular',
        'Workflow Database': 'database.workflow',
        'Metrics Collector': 'metrics.collector',
        'Health Checker': 'health.checker',
        'Request ID Middleware': 'http.middleware.requestid',
        'Data Transformer': 'data.transformer',
        'Webhook Sender': 'webhook.sender',
      };

      const modType = moduleTypeMap[label];
      if (!modType) return;

      const dt = new DataTransfer();
      dt.setData('application/workflow-module-type', modType);

      const dragStartEvent = new DragEvent('dragstart', {
        bubbles: true, cancelable: true, dataTransfer: dt, clientX: srcX, clientY: srcY,
      });
      (draggable || source).dispatchEvent(dragStartEvent);

      const dragOverEvent = new DragEvent('dragover', {
        bubbles: true, cancelable: true, dataTransfer: dt, clientX: dstX, clientY: dstY,
      });
      target.dispatchEvent(dragOverEvent);

      const dropEvent = new DragEvent('drop', {
        bubbles: true, cancelable: true, dataTransfer: dt, clientX: dstX, clientY: dstY,
      });
      target.dispatchEvent(dropEvent);
    },
    { srcX: sourceX, srcY: sourceY, dstX: dropX, dstY: dropY, label: moduleLabel },
  );

  await page.waitForTimeout(500);
}

/**
 * Helper: Connect two nodes by dragging from source bottom handle to target top handle.
 */
async function connectNodes(
  page: import('@playwright/test').Page,
  sourceNodeIndex: number,
  targetNodeIndex: number,
) {
  const sourceNode = page.locator('.react-flow__node').nth(sourceNodeIndex);
  const sourceHandle = sourceNode.locator('.react-flow__handle-bottom');
  const targetNode = page.locator('.react-flow__node').nth(targetNodeIndex);
  const targetHandle = targetNode.locator('.react-flow__handle-top');

  await sourceHandle.waitFor({ state: 'attached', timeout: 5000 });
  await targetHandle.waitFor({ state: 'attached', timeout: 5000 });

  const srcBox = await sourceHandle.boundingBox();
  const tgtBox = await targetHandle.boundingBox();
  if (!srcBox || !tgtBox) throw new Error('Handle bounding boxes not found');

  const srcX = srcBox.x + srcBox.width / 2;
  const srcY = srcBox.y + srcBox.height / 2;
  const tgtX = tgtBox.x + tgtBox.width / 2;
  const tgtY = tgtBox.y + tgtBox.height / 2;

  await page.mouse.move(srcX, srcY);
  await page.waitForTimeout(100);
  await page.mouse.down();
  await page.waitForTimeout(100);
  const steps = 20;
  for (let i = 1; i <= steps; i++) {
    await page.mouse.move(
      srcX + (tgtX - srcX) * (i / steps),
      srcY + (tgtY - srcY) * (i / steps),
    );
    await page.waitForTimeout(20);
  }
  await page.waitForTimeout(100);
  await page.mouse.up();
  await page.waitForTimeout(500);
}

test.describe('Exploratory Usability Tests', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForSelector('.react-flow', { timeout: 15000 });
    await page.waitForTimeout(500);
  });

  test('Test 1: Full application layout verification', async ({ page }) => {
    // Verify sidebar with Modules heading
    await expect(page.getByText('Modules', { exact: true })).toBeVisible();

    // Verify canvas
    const canvas = page.locator('.react-flow');
    await expect(canvas).toBeVisible();

    // Verify toolbar buttons
    const toolbarButtons = ['Import', 'Export YAML', 'Save', 'Validate', 'Undo', 'Redo', 'Clear'];
    for (const btn of toolbarButtons) {
      await expect(page.getByRole('button', { name: btn })).toBeVisible();
    }

    // Verify property panel placeholder
    await expect(page.getByText('Select a node to edit its properties')).toBeVisible();

    // Verify module count shows 0
    await expect(page.getByText('0 modules')).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/exp-01-full-layout.png', fullPage: true });
  });

  test('Test 2: All 10 categories visible and interactive', async ({ page }) => {
    const categories = [
      'HTTP',
      'Middleware',
      'Messaging',
      'State Machine',
      'Events',
      'Integration',
      'Scheduling',
      'Infrastructure',
      'Database',
      'Observability',
    ];

    // Verify all 10 categories render by checking for module items from each category
    // The categories are rendered in the sidebar; we look for their labels
    // Use a locator scoped to avoid ambiguity
    for (const category of categories) {
      const label = page.locator('[style*="cursor: pointer"]').filter({ hasText: category }).first();
      await label.scrollIntoViewIfNeeded();
      await expect(label).toBeVisible({ timeout: 5000 });
    }

    // Collapse the HTTP category by clicking its header
    const httpHeader = page.locator('[style*="cursor: pointer"]').filter({ hasText: 'HTTP' }).first();
    await httpHeader.scrollIntoViewIfNeeded();
    await httpHeader.click();
    await page.waitForTimeout(400);

    // After collapsing HTTP, "HTTP Server" draggable items should be gone from DOM
    // (the NodePalette uses conditional rendering: expanded[cat.key] && ...)
    const httpServerDraggable = page.locator('[draggable="true"]').filter({ hasText: 'HTTP Server' });
    await expect(httpServerDraggable).toHaveCount(0, { timeout: 3000 });

    // Re-expand by clicking HTTP again
    await httpHeader.click();
    await page.waitForTimeout(400);

    // Items should be visible again
    await expect(page.getByText('HTTP Server')).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/exp-02-categories.png', fullPage: true });
  });

  test('Test 3: Drag new module types to canvas', async ({ page }) => {
    // Drag one module from Database category
    await dragModuleToCanvas(page, 'Workflow Database', 200, 100);
    await expect(page.locator('.react-flow__node')).toHaveCount(1, { timeout: 5000 });

    // Drag one module from Observability category
    await dragModuleToCanvas(page, 'Metrics Collector', 450, 100);
    await expect(page.locator('.react-flow__node')).toHaveCount(2, { timeout: 5000 });

    // Drag one new Integration module
    await dragModuleToCanvas(page, 'Data Transformer', 200, 300);
    await expect(page.locator('.react-flow__node')).toHaveCount(3, { timeout: 5000 });

    // Drag one new Middleware module
    await dragModuleToCanvas(page, 'Request ID Middleware', 450, 300);
    await expect(page.locator('.react-flow__node')).toHaveCount(4, { timeout: 5000 });

    // Verify all 4 nodes appear on canvas
    await expect(page.getByText('4 modules')).toBeVisible();

    // Verify each node is visible with its label
    await expect(page.locator('.react-flow__node').filter({ hasText: /Workflow Database/ })).toBeVisible();
    await expect(page.locator('.react-flow__node').filter({ hasText: /Metrics Collector/ })).toBeVisible();
    await expect(page.locator('.react-flow__node').filter({ hasText: /Data Transformer/ })).toBeVisible();
    await expect(page.locator('.react-flow__node').filter({ hasText: /Request ID Middleware/ })).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/exp-03-new-module-types.png', fullPage: true });
  });

  test('Test 4: Complex multi-category workflow', async ({ page }) => {
    // Build a 6-node workflow using modules from different categories
    // Use a 2-column layout with generous spacing
    await dragModuleToCanvas(page, 'HTTP Server', 200, 30);
    await expect(page.locator('.react-flow__node')).toHaveCount(1, { timeout: 5000 });

    await dragModuleToCanvas(page, 'Auth Middleware', 200, 250);
    await expect(page.locator('.react-flow__node')).toHaveCount(2, { timeout: 5000 });

    await dragModuleToCanvas(page, 'HTTP Router', 200, 470);
    await expect(page.locator('.react-flow__node')).toHaveCount(3, { timeout: 5000 });

    await dragModuleToCanvas(page, 'Event Bus', 500, 30);
    await expect(page.locator('.react-flow__node')).toHaveCount(4, { timeout: 5000 });

    await dragModuleToCanvas(page, 'Scheduler', 500, 250);
    await expect(page.locator('.react-flow__node')).toHaveCount(5, { timeout: 5000 });

    await dragModuleToCanvas(page, 'Metrics Collector', 500, 470);
    await expect(page.locator('.react-flow__node')).toHaveCount(6, { timeout: 5000 });

    await expect(page.getByText('6 modules')).toBeVisible();

    // Deselect by clicking empty pane before connecting
    await page.locator('.react-flow__pane').click({ position: { x: 350, y: 600 } });
    await page.waitForTimeout(300);

    // Connect first pair: node 0 (HTTP Server) -> node 1 (Auth Middleware)
    // Use generous spacing (220px apart) matching the working connections spec
    await connectNodes(page, 0, 1);
    await page.waitForTimeout(300);

    // Try second pair in second column: node 3 (Event Bus) -> node 4 (Scheduler)
    await connectNodes(page, 3, 4);
    await page.waitForTimeout(300);

    // Verify at least some edges exist
    const edgeCount = await page.locator('.react-flow__edge').count();
    expect(edgeCount).toBeGreaterThanOrEqual(1);

    await page.screenshot({ path: 'e2e/screenshots/exp-04-complex-workflow.png', fullPage: true });
  });

  test('Test 5: Property panel for new module types', async ({ page }) => {
    // Drag a database module
    await dragModuleToCanvas(page, 'Workflow Database', 300, 150);
    await expect(page.locator('.react-flow__node')).toHaveCount(1, { timeout: 5000 });

    // Click the database node to show property panel
    const dbNode = page.locator('.react-flow__node').first();
    await dbNode.click();
    await page.waitForTimeout(500);

    // Verify property panel shows relevant fields
    await expect(page.getByText('Properties', { exact: true })).toBeVisible();
    // The type badge appears on both node and property panel; use .last() for property panel
    await expect(page.getByText('database.workflow').last()).toBeVisible();

    // Check for database-specific config fields
    await expect(page.locator('label').filter({ hasText: 'Driver' })).toBeVisible();
    await expect(page.locator('label').filter({ hasText: 'DSN' })).toBeVisible();

    // Now drag a metrics module in a different position
    await dragModuleToCanvas(page, 'Metrics Collector', 300, 400);
    await expect(page.locator('.react-flow__node')).toHaveCount(2, { timeout: 5000 });

    // Click the metrics node (second node)
    const metricsNode = page.locator('.react-flow__node').nth(1);
    await metricsNode.click();
    await page.waitForTimeout(500);

    // Verify property panel updates to show metrics type
    await expect(page.getByText('metrics.collector').last()).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/exp-05-property-panels.png', fullPage: true });
  });

  test('Test 6: YAML export and import with new module types', async ({ page }) => {
    // Build a workflow with new module types
    await dragModuleToCanvas(page, 'Workflow Database', 300, 100);
    await expect(page.locator('.react-flow__node')).toHaveCount(1, { timeout: 5000 });

    await dragModuleToCanvas(page, 'Metrics Collector', 300, 300);
    await expect(page.locator('.react-flow__node')).toHaveCount(2, { timeout: 5000 });

    await dragModuleToCanvas(page, 'Data Transformer', 300, 500);
    await expect(page.locator('.react-flow__node')).toHaveCount(3, { timeout: 5000 });

    // Export to YAML
    const downloadPromise = page.waitForEvent('download', { timeout: 10000 });
    await page.getByRole('button', { name: 'Export YAML' }).click();
    const download = await downloadPromise;

    // Read the exported file
    const filePath = await download.path();
    expect(filePath).toBeTruthy();
    const content = fs.readFileSync(filePath!, 'utf8');

    // Verify it contains the new type strings
    expect(content).toContain('database.workflow');
    expect(content).toContain('metrics.collector');
    expect(content).toContain('data.transformer');
    expect(content).toContain('modules:');

    // Clear canvas
    await page.getByRole('button', { name: 'Clear' }).click();
    await expect(page.locator('.react-flow__node')).toHaveCount(0, { timeout: 5000 });
    await expect(page.getByText('0 modules')).toBeVisible();

    // Write exported content to temp file for re-import
    const tmpDir = path.join(__dirname, 'fixtures');
    fs.mkdirSync(tmpDir, { recursive: true });
    const tmpPath = path.join(tmpDir, 'exploratory-roundtrip.yaml');
    fs.writeFileSync(tmpPath, content, 'utf8');

    // Import the YAML back
    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Import' }).click();
    const fileChooser = await fileChooserPromise;
    await fileChooser.setFiles(tmpPath);

    // Verify nodes reappear
    await expect(page.locator('.react-flow__node')).toHaveCount(3, { timeout: 10000 });
    await expect(page.getByText('3 modules')).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/exp-06-yaml-roundtrip.png', fullPage: true });
  });

  test('Test 7: Undo/Redo across multiple operations', async ({ page }) => {
    // Add 3 nodes one by one, verifying each
    await dragModuleToCanvas(page, 'HTTP Server', 200, 100);
    await expect(page.locator('.react-flow__node')).toHaveCount(1, { timeout: 5000 });

    await dragModuleToCanvas(page, 'HTTP Router', 400, 100);
    await expect(page.locator('.react-flow__node')).toHaveCount(2, { timeout: 5000 });

    await dragModuleToCanvas(page, 'Scheduler', 300, 350);
    await expect(page.locator('.react-flow__node')).toHaveCount(3, { timeout: 5000 });

    // Undo twice (should go from 3 -> 2 -> 1 node)
    const undoBtn = page.getByRole('button', { name: 'Undo' });
    await expect(undoBtn).toBeEnabled();
    await undoBtn.click();
    await expect(page.locator('.react-flow__node')).toHaveCount(2, { timeout: 5000 });

    await undoBtn.click();
    await expect(page.locator('.react-flow__node')).toHaveCount(1, { timeout: 5000 });

    // Redo once (should go from 1 -> 2 nodes)
    const redoBtn = page.getByRole('button', { name: 'Redo' });
    await expect(redoBtn).toBeEnabled();
    await redoBtn.click();
    await expect(page.locator('.react-flow__node')).toHaveCount(2, { timeout: 5000 });

    await page.screenshot({ path: 'e2e/screenshots/exp-07-undo-redo.png', fullPage: true });
  });

  test('Test 8: Canvas controls and navigation', async ({ page }) => {
    // Add several nodes, verify incrementally
    await dragModuleToCanvas(page, 'HTTP Server', 200, 100);
    await expect(page.locator('.react-flow__node')).toHaveCount(1, { timeout: 5000 });

    await dragModuleToCanvas(page, 'Message Broker', 400, 100);
    await expect(page.locator('.react-flow__node')).toHaveCount(2, { timeout: 5000 });

    await dragModuleToCanvas(page, 'Cache', 300, 350);
    await expect(page.locator('.react-flow__node')).toHaveCount(3, { timeout: 5000 });

    // Verify MiniMap is visible
    const minimap = page.locator('.react-flow__minimap');
    await expect(minimap).toBeVisible();

    // Verify controls panel is visible
    const controls = page.locator('.react-flow__controls');
    await expect(controls).toBeVisible();

    // Verify background pattern is rendered
    await expect(page.locator('.react-flow__background')).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/exp-08-canvas-controls.png', fullPage: true });
  });

  test('Test 9: Clear with many nodes', async ({ page }) => {
    // Add 5 nodes, verify incrementally
    await dragModuleToCanvas(page, 'HTTP Server', 150, 80);
    await expect(page.locator('.react-flow__node')).toHaveCount(1, { timeout: 5000 });

    await dragModuleToCanvas(page, 'Auth Middleware', 400, 80);
    await expect(page.locator('.react-flow__node')).toHaveCount(2, { timeout: 5000 });

    await dragModuleToCanvas(page, 'HTTP Router', 150, 300);
    await expect(page.locator('.react-flow__node')).toHaveCount(3, { timeout: 5000 });

    await dragModuleToCanvas(page, 'Message Broker', 400, 300);
    await expect(page.locator('.react-flow__node')).toHaveCount(4, { timeout: 5000 });

    await dragModuleToCanvas(page, 'Scheduler', 275, 500);
    await expect(page.locator('.react-flow__node')).toHaveCount(5, { timeout: 5000 });

    // Click Clear button
    await page.getByRole('button', { name: 'Clear' }).click();

    // Verify 0 modules, no nodes on canvas
    await expect(page.locator('.react-flow__node')).toHaveCount(0, { timeout: 5000 });
    await expect(page.getByText('0 modules')).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/exp-09-clear-many.png', fullPage: true });
  });

  test('Test 10: Node deletion and selection', async ({ page }) => {
    // Add 3 nodes with large separation to avoid overlap
    await dragModuleToCanvas(page, 'HTTP Server', 150, 50);
    await expect(page.locator('.react-flow__node')).toHaveCount(1, { timeout: 5000 });

    await dragModuleToCanvas(page, 'HTTP Router', 450, 50);
    await expect(page.locator('.react-flow__node')).toHaveCount(2, { timeout: 5000 });

    await dragModuleToCanvas(page, 'Scheduler', 300, 350);
    await expect(page.locator('.react-flow__node')).toHaveCount(3, { timeout: 5000 });

    await expect(page.getByText('3 modules')).toBeVisible();

    // Click on the specific node by test id to avoid overlap issues
    const firstNode = page.locator('[data-testid="rf__node-http_server_1"]');
    await firstNode.click({ force: true });
    await page.waitForTimeout(300);

    // Delete via Delete Node button in property panel
    await page.getByRole('button', { name: 'Delete Node' }).click();
    await page.waitForTimeout(300);

    // Verify 2 remaining
    await expect(page.locator('.react-flow__node')).toHaveCount(2, { timeout: 5000 });
    await expect(page.getByText('2 modules')).toBeVisible();

    // Select another node and verify property panel updates
    const remainingNode = page.locator('.react-flow__node').first();
    await remainingNode.click({ force: true });
    await page.waitForTimeout(300);

    // Property panel should show Properties header
    await expect(page.getByText('Properties', { exact: true })).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/exp-10-delete-select.png', fullPage: true });
  });
});
