import { test, expect } from '@playwright/test';

/**
 * Helper: Simulate dragging a palette item to the canvas via dispatching drag events.
 */
async function dragModuleToCanvas(
  page: import('@playwright/test').Page,
  moduleLabel: string,
  canvasX: number,
  canvasY: number,
) {
  const paletteItem = page.getByText(moduleLabel, { exact: true }).first();
  await paletteItem.scrollIntoViewIfNeeded();
  await expect(paletteItem).toBeVisible();

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
        'EventBus Bridge': 'messaging.broker.eventbus',
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
 * Connect two nodes by mouse-dragging from source handle (bottom) to target handle (top).
 * Uses Playwright's mouse API with careful coordinate calculation.
 */
async function connectNodes(
  page: import('@playwright/test').Page,
  sourceNodeIndex: number,
  targetNodeIndex: number,
) {
  // Get source node's bottom (source) handle
  const sourceNode = page.locator('.react-flow__node').nth(sourceNodeIndex);
  const sourceHandle = sourceNode.locator('.react-flow__handle-bottom');

  // Get target node's top (target) handle
  const targetNode = page.locator('.react-flow__node').nth(targetNodeIndex);
  const targetHandle = targetNode.locator('.react-flow__handle-top');

  // Wait for handles to be attached
  await sourceHandle.waitFor({ state: 'attached', timeout: 5000 });
  await targetHandle.waitFor({ state: 'attached', timeout: 5000 });

  const srcBox = await sourceHandle.boundingBox();
  const tgtBox = await targetHandle.boundingBox();

  if (!srcBox || !tgtBox) {
    throw new Error(`Handle bounding boxes not found. Source: ${!!srcBox}, Target: ${!!tgtBox}`);
  }

  const srcX = srcBox.x + srcBox.width / 2;
  const srcY = srcBox.y + srcBox.height / 2;
  const tgtX = tgtBox.x + tgtBox.width / 2;
  const tgtY = tgtBox.y + tgtBox.height / 2;

  // Perform mouse-based drag from source handle to target handle
  // Use small steps so ReactFlow picks up the connection intent
  await page.mouse.move(srcX, srcY);
  await page.waitForTimeout(100);
  await page.mouse.down();
  await page.waitForTimeout(100);

  // Move in many small steps
  const steps = 20;
  for (let i = 1; i <= steps; i++) {
    const x = srcX + (tgtX - srcX) * (i / steps);
    const y = srcY + (tgtY - srcY) * (i / steps);
    await page.mouse.move(x, y);
    await page.waitForTimeout(20);
  }

  await page.waitForTimeout(100);
  await page.mouse.up();
  await page.waitForTimeout(500);
}

test.describe('Connections', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForSelector('.react-flow');
  });

  test('should connect two nodes with an edge', async ({ page }) => {
    // Add two nodes with large vertical separation
    await dragModuleToCanvas(page, 'HTTP Server', 200, 50);
    await dragModuleToCanvas(page, 'HTTP Router', 200, 400);

    // Verify both nodes exist
    await expect(page.locator('.react-flow__node')).toHaveCount(2);

    // Deselect by clicking empty pane
    await page.locator('.react-flow__pane').click({ position: { x: 50, y: 250 } });
    await page.waitForTimeout(300);

    // Connect source (node 0) bottom handle to target (node 1) top handle
    await connectNodes(page, 0, 1);

    // Check for edge
    const edges = page.locator('.react-flow__edge');
    await expect(edges.first()).toBeVisible({ timeout: 10000 });

    await page.screenshot({ path: 'e2e/screenshots/12-two-nodes-connected.png', fullPage: true });
  });

  test('should create a multi-node workflow with connections', async ({ page }) => {
    // Add three nodes
    await dragModuleToCanvas(page, 'HTTP Server', 200, 30);
    await dragModuleToCanvas(page, 'Auth Middleware', 200, 250);
    await dragModuleToCanvas(page, 'HTTP Router', 200, 470);

    await expect(page.locator('.react-flow__node')).toHaveCount(3);

    // Deselect
    await page.locator('.react-flow__pane').click({ position: { x: 50, y: 600 } });
    await page.waitForTimeout(300);

    // Connect: HTTP Server -> Auth Middleware
    await connectNodes(page, 0, 1);

    // Connect: Auth Middleware -> HTTP Router
    await connectNodes(page, 1, 2);

    // Should have edges
    const edges = page.locator('.react-flow__edge');
    await expect(edges.first()).toBeVisible({ timeout: 10000 });

    await page.screenshot({ path: 'e2e/screenshots/13-multi-node-workflow.png', fullPage: true });
  });

  test('should show edge in the DOM structure', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 200, 50);
    await dragModuleToCanvas(page, 'Message Broker', 200, 400);

    await page.locator('.react-flow__pane').click({ position: { x: 50, y: 250 } });
    await page.waitForTimeout(300);

    await connectNodes(page, 0, 1);

    // Verify the edge container has SVG path elements
    const edgePath = page.locator('.react-flow__edge path');
    await expect(edgePath.first()).toBeVisible({ timeout: 10000 });

    await page.screenshot({ path: 'e2e/screenshots/14-edge-dom.png', fullPage: true });
  });
});
