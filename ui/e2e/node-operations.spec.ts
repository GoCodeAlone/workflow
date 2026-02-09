import { test, expect } from '@playwright/test';

/**
 * Helper: Simulate dragging a palette item to the canvas.
 * Since HTML5 drag-and-drop is hard to automate, we use the dataTransfer API
 * via page.evaluate to dispatch the events programmatically.
 */
async function dragModuleToCanvas(
  page: import('@playwright/test').Page,
  moduleLabel: string,
  canvasX: number,
  canvasY: number,
) {
  // Find the palette item
  const paletteItem = page.getByText(moduleLabel, { exact: true }).first();
  await paletteItem.scrollIntoViewIfNeeded();
  await expect(paletteItem).toBeVisible();

  const sourceBounds = await paletteItem.boundingBox();
  if (!sourceBounds) throw new Error(`Could not find palette item: ${moduleLabel}`);

  // Find the canvas drop target (the div wrapping ReactFlow)
  const canvas = page.locator('.react-flow').first();
  const canvasBounds = await canvas.boundingBox();
  if (!canvasBounds) throw new Error('Could not find canvas');

  const dropX = canvasBounds.x + canvasX;
  const dropY = canvasBounds.y + canvasY;

  // Dispatch drag events programmatically using evaluate
  // The palette items set 'application/workflow-module-type' on drag
  // We need to find the moduleType from the label
  const moduleType = await page.evaluate((label: string) => {
    // Look through all draggable elements to find the one with matching text
    const elements = document.querySelectorAll('[draggable="true"]');
    for (const el of elements) {
      if (el.textContent?.trim() === label) {
        // The parent structure has event handlers; we need to trigger drag start to get the type
        return el.textContent?.trim();
      }
    }
    return null;
  }, moduleLabel);

  // Use mouse-based drag simulation
  const sourceX = sourceBounds.x + sourceBounds.width / 2;
  const sourceY = sourceBounds.y + sourceBounds.height / 2;

  // Programmatically create and dispatch drag events with proper dataTransfer
  await page.evaluate(
    ({ srcX, srcY, dstX, dstY, label }) => {
      const source = document.elementFromPoint(srcX, srcY) as HTMLElement;
      const target = document.elementFromPoint(dstX, dstY) as HTMLElement;
      if (!source || !target) return;

      // Find the actual draggable element
      let draggable = source;
      while (draggable && draggable.getAttribute('draggable') !== 'true') {
        draggable = draggable.parentElement as HTMLElement;
      }

      // We need to figure out the module type from the palette structure
      // The CATEGORIES structure maps labels to types
      // Simpler approach: look at the text and match against known module types
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

      // Create a proper DataTransfer
      const dt = new DataTransfer();
      dt.setData('application/workflow-module-type', modType);

      // Dispatch dragstart on source
      const dragStartEvent = new DragEvent('dragstart', {
        bubbles: true,
        cancelable: true,
        dataTransfer: dt,
        clientX: srcX,
        clientY: srcY,
      });
      (draggable || source).dispatchEvent(dragStartEvent);

      // Dispatch dragover on target (needed for drop to work)
      const dragOverEvent = new DragEvent('dragover', {
        bubbles: true,
        cancelable: true,
        dataTransfer: dt,
        clientX: dstX,
        clientY: dstY,
      });
      target.dispatchEvent(dragOverEvent);

      // Dispatch drop on target
      const dropEvent = new DragEvent('drop', {
        bubbles: true,
        cancelable: true,
        dataTransfer: dt,
        clientX: dstX,
        clientY: dstY,
      });
      target.dispatchEvent(dropEvent);
    },
    { srcX: sourceX, srcY: sourceY, dstX: dropX, dstY: dropY, label: moduleLabel },
  );

  // Wait for the node to appear
  await page.waitForTimeout(500);
}

test.describe('Node Operations', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForSelector('.react-flow');
  });

  test('should add a node to the canvas via drag and drop', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);

    // Verify node appeared - should show "HTTP Server 1" as the label
    await expect(page.locator('.react-flow__node').first()).toBeVisible();

    // Module count should update
    await expect(page.getByText('1 modules')).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/06-node-on-canvas.png', fullPage: true });
  });

  test('should show correct label on the added node', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);

    // The node should contain the label text
    const node = page.locator('.react-flow__node').first();
    await expect(node).toBeVisible();

    // Node should show "HTTP Server 1" and the type badge
    await expect(node.getByText(/HTTP Server/)).toBeVisible();
    await expect(node.getByText('http.server')).toBeVisible();
  });

  test('should select a node and show property panel', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);

    // Click on the node
    const node = page.locator('.react-flow__node').first();
    await node.click();

    // Property panel should now show "Properties" header
    await expect(page.getByText('Properties', { exact: true })).toBeVisible();

    // Should show the Name field with the node name
    const nameInput = page.locator('label').filter({ hasText: 'Name' }).locator('input');
    await expect(nameInput).toBeVisible();

    // Should show Type badge
    await expect(page.locator('text=http.server').last()).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/07-property-panel.png', fullPage: true });
  });

  test('should edit node name in property panel', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);

    // Click on node
    const node = page.locator('.react-flow__node').first();
    await node.click();

    // Find the name input in property panel
    const nameInput = page.locator('label').filter({ hasText: 'Name' }).locator('input');
    await expect(nameInput).toBeVisible();

    // Clear and type a new name
    await nameInput.fill('My Web Server');

    // Verify the node label updated on canvas
    await expect(page.locator('.react-flow__node').first().getByText('My Web Server')).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/08-node-name-edited.png', fullPage: true });
  });

  test('should edit a config field in property panel', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);

    // Click on node
    const node = page.locator('.react-flow__node').first();
    await node.click();

    // The HTTP Server has an "Address" config field with default ":8080"
    const addressInput = page.locator('label').filter({ hasText: 'Address' }).locator('input');
    await expect(addressInput).toBeVisible();
    await expect(addressInput).toHaveValue(':8080');

    // Change the address
    await addressInput.fill(':9090');
    await expect(addressInput).toHaveValue(':9090');

    await page.screenshot({ path: 'e2e/screenshots/09-config-field-edited.png', fullPage: true });
  });

  test('should delete a node', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);

    // Verify node exists
    await expect(page.locator('.react-flow__node')).toHaveCount(1);
    await expect(page.getByText('1 modules')).toBeVisible();

    // Click on node to select it
    const node = page.locator('.react-flow__node').first();
    await node.click();

    // Click the "Delete Node" button in property panel
    await page.getByRole('button', { name: 'Delete Node' }).click();

    // Verify node is gone
    await expect(page.locator('.react-flow__node')).toHaveCount(0);
    await expect(page.getByText('0 modules')).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/10-node-deleted.png', fullPage: true });
  });

  test('should add multiple different node types', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 150, 100);
    await dragModuleToCanvas(page, 'Message Broker', 450, 100);
    await dragModuleToCanvas(page, 'Scheduler', 150, 350);

    // Should have 3 nodes on canvas
    await expect(page.locator('.react-flow__node')).toHaveCount(3, { timeout: 10000 });
    await expect(page.getByText('3 modules')).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/11-multiple-nodes.png', fullPage: true });
  });

  test('should add a Metrics Collector node to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'Metrics Collector', 300, 200);

    // Verify node appeared
    const node = page.locator('.react-flow__node').first();
    await expect(node).toBeVisible();

    // Node should show the label and type badge
    await expect(node.getByText(/Metrics Collector/)).toBeVisible();
    await expect(node.getByText('metrics.collector')).toBeVisible();

    // Module count should update
    await expect(page.getByText('1 modules')).toBeVisible();
  });

  test('should add a Workflow Database node and show correct property fields', async ({ page }) => {
    await dragModuleToCanvas(page, 'Workflow Database', 300, 200);

    // Verify node appeared
    const node = page.locator('.react-flow__node').first();
    await expect(node).toBeVisible();
    await expect(node.getByText('database.workflow')).toBeVisible();

    // Click on node to open property panel
    await node.click();

    // Property panel should show correct config fields for Workflow Database
    await expect(page.getByText('Properties', { exact: true })).toBeVisible();
    await expect(page.locator('text=database.workflow').last()).toBeVisible();

    // Should show Driver select field
    await expect(page.locator('label').filter({ hasText: 'Driver' })).toBeVisible();

    // Should show DSN field
    await expect(page.locator('label').filter({ hasText: 'DSN' })).toBeVisible();

    // Should show Max Open Connections field
    await expect(page.locator('label').filter({ hasText: 'Max Open Connections' })).toBeVisible();

    // Should show Max Idle Connections field
    await expect(page.locator('label').filter({ hasText: 'Max Idle Connections' })).toBeVisible();
  });
});
