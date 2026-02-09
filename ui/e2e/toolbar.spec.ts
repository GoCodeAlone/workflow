import { test, expect } from '@playwright/test';

/**
 * Helper: Simulate dragging a palette item to the canvas.
 */
async function dragModuleToCanvas(
  page: import('@playwright/test').Page,
  moduleLabel: string,
  canvasX: number,
  canvasY: number,
) {
  const paletteItem = page.getByText(moduleLabel, { exact: true }).first();
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

test.describe('Toolbar', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForSelector('.react-flow');
  });

  test('should validate an empty workflow and show error toast', async ({ page }) => {
    // With no modules, the Validate button should be disabled
    const validateBtn = page.getByRole('button', { name: 'Validate' });
    await expect(validateBtn).toBeDisabled();
  });

  test('should validate a workflow with nodes and show result', async ({ page }) => {
    // Add a node first
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await expect(page.getByText('1 modules')).toBeVisible();

    // Click Validate
    const validateBtn = page.getByRole('button', { name: 'Validate' });
    await expect(validateBtn).toBeEnabled();
    await validateBtn.click();

    // Wait for validation toast to appear
    // Server isn't running, so the API call will eventually fail.
    // The catch block shows "Workflow is valid (local check only)".
    // The proxy error may be slow, so give it a generous timeout.
    await expect(page.getByText(/Workflow is valid/)).toBeVisible({ timeout: 30000 });

    await page.screenshot({ path: 'e2e/screenshots/15-validation-result.png', fullPage: true });
  });

  test('should export YAML when clicking Export YAML', async ({ page }) => {
    // Add a node
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await expect(page.getByText('1 modules')).toBeVisible();

    // Set up download listener before clicking
    const downloadPromise = page.waitForEvent('download', { timeout: 5000 });

    // Click Export YAML
    await page.getByRole('button', { name: 'Export YAML' }).click();

    // Verify download was triggered
    const download = await downloadPromise;
    expect(download.suggestedFilename()).toBe('workflow.yaml');

    await page.screenshot({ path: 'e2e/screenshots/16-after-export.png', fullPage: true });
  });

  test('should clear canvas when clicking Clear', async ({ page }) => {
    // Add some nodes
    await dragModuleToCanvas(page, 'HTTP Server', 200, 200);
    await dragModuleToCanvas(page, 'HTTP Router', 500, 200);

    await expect(page.locator('.react-flow__node')).toHaveCount(2);
    await expect(page.getByText('2 modules')).toBeVisible();

    // Click Clear
    await page.getByRole('button', { name: 'Clear' }).click();

    // Canvas should be empty
    await expect(page.locator('.react-flow__node')).toHaveCount(0);
    await expect(page.getByText('0 modules')).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/17-cleared-state.png', fullPage: true });
  });

  test('should disable Export, Save, Validate, Clear when no nodes', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'Export YAML' })).toBeDisabled();
    await expect(page.getByRole('button', { name: 'Save' })).toBeDisabled();
    await expect(page.getByRole('button', { name: 'Validate' })).toBeDisabled();
    await expect(page.getByRole('button', { name: 'Clear' })).toBeDisabled();
  });

  test('should enable toolbar buttons after adding a node', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);

    await expect(page.getByRole('button', { name: 'Export YAML' })).toBeEnabled();
    await expect(page.getByRole('button', { name: 'Save' })).toBeEnabled();
    await expect(page.getByRole('button', { name: 'Validate' })).toBeEnabled();
    await expect(page.getByRole('button', { name: 'Clear' })).toBeEnabled();
  });

  test('should undo and redo node addition', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await expect(page.locator('.react-flow__node')).toHaveCount(1);

    // Undo should be enabled now
    const undoBtn = page.getByRole('button', { name: 'Undo' });
    await expect(undoBtn).toBeEnabled();

    // Click Undo
    await undoBtn.click();
    await expect(page.locator('.react-flow__node')).toHaveCount(0);

    // Redo should be enabled
    const redoBtn = page.getByRole('button', { name: 'Redo' });
    await expect(redoBtn).toBeEnabled();

    // Click Redo
    await redoBtn.click();
    await expect(page.locator('.react-flow__node')).toHaveCount(1);

    await page.screenshot({ path: 'e2e/screenshots/18-undo-redo.png', fullPage: true });
  });
});
