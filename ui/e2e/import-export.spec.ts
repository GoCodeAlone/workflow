import { test, expect } from '@playwright/test';
import path from 'path';
import fs from 'fs';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

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

// Create a test YAML fixture
const TEST_YAML = `modules:
  - name: Web Server
    type: http.server
    config:
      address: ":3000"
  - name: API Router
    type: http.router
    config:
      prefix: /api
    dependsOn:
      - Web Server
  - name: Auth
    type: http.middleware.auth
    config:
      type: jwt
    dependsOn:
      - API Router
workflows: {}
triggers: {}
`;

test.describe('Import / Export', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForSelector('.react-flow');
  });

  test('should import a YAML workflow file', async ({ page }) => {
    // Write test YAML to a temp file
    const tmpDir = path.join(__dirname, 'fixtures');
    fs.mkdirSync(tmpDir, { recursive: true });
    const yamlPath = path.join(tmpDir, 'test-workflow.yaml');
    fs.writeFileSync(yamlPath, TEST_YAML, 'utf8');

    // Use file chooser to import the YAML
    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Import' }).click();
    const fileChooser = await fileChooserPromise;
    await fileChooser.setFiles(yamlPath);

    // Wait for nodes to appear
    await expect(page.locator('.react-flow__node')).toHaveCount(3, { timeout: 5000 });

    // Verify the imported module names appear on nodes
    await expect(page.locator('.react-flow__node').filter({ hasText: 'Web Server' })).toBeVisible();
    await expect(page.locator('.react-flow__node').filter({ hasText: 'API Router' })).toBeVisible();
    await expect(page.locator('.react-flow__node').filter({ hasText: 'Auth' })).toBeVisible();

    // Should show "3 modules"
    await expect(page.getByText('3 modules')).toBeVisible();

    // Edges should be created from dependsOn relationships
    const edges = page.locator('.react-flow__edge');
    await expect(edges).toHaveCount(2, { timeout: 5000 });

    // Toast should appear
    await expect(page.getByText('Workflow imported from file')).toBeVisible({ timeout: 5000 });

    await page.screenshot({ path: 'e2e/screenshots/19-imported-workflow.png', fullPage: true });
  });

  test('should export and verify content matches', async ({ page }) => {
    // Add nodes manually
    await dragModuleToCanvas(page, 'HTTP Server', 300, 150);
    await dragModuleToCanvas(page, 'HTTP Router', 300, 350);

    await expect(page.locator('.react-flow__node')).toHaveCount(2);

    // Export
    const downloadPromise = page.waitForEvent('download');
    await page.getByRole('button', { name: 'Export YAML' }).click();
    const download = await downloadPromise;

    // Read the downloaded file content
    const filePath = await download.path();
    expect(filePath).toBeTruthy();
    const content = fs.readFileSync(filePath!, 'utf8');

    // Verify the YAML contains our modules
    expect(content).toContain('http.server');
    expect(content).toContain('http.router');
    expect(content).toContain('modules:');

    await page.screenshot({ path: 'e2e/screenshots/20-export-content.png', fullPage: true });
  });

  test('should round-trip: export then import', async ({ page }) => {
    // Add a node
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);

    // Edit the node name
    const node = page.locator('.react-flow__node').first();
    await node.click();
    const nameInput = page.locator('label').filter({ hasText: 'Name' }).locator('input');
    await nameInput.fill('Custom Server');
    await page.waitForTimeout(200);

    // Export
    const downloadPromise = page.waitForEvent('download');
    await page.getByRole('button', { name: 'Export YAML' }).click();
    const download = await downloadPromise;

    // Read exported content
    const filePath = await download.path();
    const exportedContent = fs.readFileSync(filePath!, 'utf8');
    expect(exportedContent).toContain('Custom Server');

    // Clear canvas
    await page.getByRole('button', { name: 'Clear' }).click();
    await expect(page.locator('.react-flow__node')).toHaveCount(0);

    // Write the exported content to a temp file for re-import
    const tmpDir = path.join(__dirname, 'fixtures');
    fs.mkdirSync(tmpDir, { recursive: true });
    const tmpPath = path.join(tmpDir, 'roundtrip.yaml');
    fs.writeFileSync(tmpPath, exportedContent, 'utf8');

    // Import it back
    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Import' }).click();
    const fileChooser = await fileChooserPromise;
    await fileChooser.setFiles(tmpPath);

    // Verify node reappears with the custom name
    await expect(page.locator('.react-flow__node')).toHaveCount(1, { timeout: 5000 });
    await expect(page.locator('.react-flow__node').filter({ hasText: 'Custom Server' })).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/21-roundtrip.png', fullPage: true });
  });

  test('should show error toast for invalid file import', async ({ page }) => {
    // Create an invalid YAML file
    const tmpDir = path.join(__dirname, 'fixtures');
    fs.mkdirSync(tmpDir, { recursive: true });
    const invalidPath = path.join(tmpDir, 'invalid.yaml');
    fs.writeFileSync(invalidPath, '{{invalid yaml: [}}', 'utf8');

    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Import' }).click();
    const fileChooser = await fileChooserPromise;
    await fileChooser.setFiles(invalidPath);

    // Should show error toast
    await expect(page.getByText('Failed to parse workflow file')).toBeVisible({ timeout: 5000 });

    await page.screenshot({ path: 'e2e/screenshots/22-import-error.png', fullPage: true });
  });
});
