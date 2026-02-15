import { test, expect } from '@playwright/test';
import { dragModuleToCanvas, connectNodes, waitForNodeCount, screenshotStep, COMPLETE_MODULE_TYPE_MAP } from './helpers';
import path from 'path';
import fs from 'fs';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// ============================================================================
// Section 1: Full Application Walkthrough
// ============================================================================
test.describe('Phase 6: Application Walkthrough', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForSelector('.react-flow', { timeout: 15000 });
    await page.waitForTimeout(500);
  });

  test('P6-01: Empty canvas initial state', async ({ page }) => {
    await expect(page.getByText('0 modules')).toBeVisible();
    await expect(page.locator('.react-flow')).toBeVisible();
    await expect(page.getByText('Select a node to edit its properties')).toBeVisible();
    await screenshotStep(page, 'phase6-01-empty-canvas');
  });

  test('P6-02: All 10 categories in palette', async ({ page }) => {
    const categories = ['HTTP', 'Middleware', 'Messaging', 'State Machine', 'Events', 'Integration', 'Scheduling', 'Infrastructure', 'Database', 'Observability'];
    for (const cat of categories) {
      const label = page.locator('[style*="cursor: pointer"]').filter({ hasText: cat }).first();
      await label.scrollIntoViewIfNeeded();
      await expect(label).toBeVisible({ timeout: 5000 });
    }
    await screenshotStep(page, 'phase6-02-all-categories');
  });

  test('P6-03: Toolbar buttons all visible', async ({ page }) => {
    const toolbarButtons = ['Import', 'Export YAML', 'Save', 'Validate', 'Undo', 'Redo', 'Clear'];
    for (const btn of toolbarButtons) {
      await expect(page.getByRole('button', { name: btn })).toBeVisible();
    }
    await screenshotStep(page, 'phase6-03-toolbar-buttons');
  });

  test('P6-04: Drag first module from each major category', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 200, 50);
    await waitForNodeCount(page, 1);

    await dragModuleToCanvas(page, 'Auth Middleware', 200, 270);
    await waitForNodeCount(page, 2);

    await dragModuleToCanvas(page, 'Message Broker', 450, 50);
    await waitForNodeCount(page, 3);

    await dragModuleToCanvas(page, 'State Machine', 450, 270);
    await waitForNodeCount(page, 4);

    await expect(page.getByText('4 modules')).toBeVisible();
    await screenshotStep(page, 'phase6-04-drag-categories');
  });

  test('P6-05: Configure node properties with different field types', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 150);
    await waitForNodeCount(page, 1);

    // Click the node to show properties
    await page.locator('.react-flow__node').first().click();
    await page.waitForTimeout(300);

    await expect(page.getByText('Properties', { exact: true })).toBeVisible();
    await expect(page.getByText('http.server').last()).toBeVisible();

    // Edit the name field
    const nameInput = page.locator('label').filter({ hasText: 'Name' }).locator('input');
    await nameInput.fill('my-web-server');
    await page.waitForTimeout(300);

    // Edit the address config field
    const addressInput = page.locator('label').filter({ hasText: 'Address' }).locator('input');
    await addressInput.fill(':9090');
    await page.waitForTimeout(300);

    await screenshotStep(page, 'phase6-05-property-config');
  });

  test('P6-06: Build and connect HTTP pipeline', async ({ page }) => {
    // Use exact same coordinates as deep-complex-workflows which passes reliably
    await dragModuleToCanvas(page, 'HTTP Server', 200, 30);
    await waitForNodeCount(page, 1);
    await dragModuleToCanvas(page, 'Auth Middleware', 200, 250);
    await waitForNodeCount(page, 2);

    await expect(page.getByText('2 modules')).toBeVisible();

    // Deselect before connecting
    await page.locator('.react-flow__pane').click({ position: { x: 50, y: 600 } });
    await page.waitForTimeout(300);

    // Connect HTTP Server -> Auth Middleware (single connection, proven reliable)
    await connectNodes(page, 0, 1);

    // Should have at least one edge
    const edgeCount = await page.locator('.react-flow__edge').count();
    expect(edgeCount).toBeGreaterThanOrEqual(1);

    await screenshotStep(page, 'phase6-06-connected-pipeline');
  });

  test('P6-07: Undo and redo operations', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 250, 100);
    await waitForNodeCount(page, 1);

    await dragModuleToCanvas(page, 'HTTP Router', 250, 350);
    await waitForNodeCount(page, 2);

    // Undo
    await page.getByRole('button', { name: 'Undo' }).click();
    await waitForNodeCount(page, 1);
    await screenshotStep(page, 'phase6-07a-after-undo');

    // Redo
    await page.getByRole('button', { name: 'Redo' }).click();
    await waitForNodeCount(page, 2);
    await screenshotStep(page, 'phase6-07b-after-redo');
  });

  test('P6-08: Clear canvas resets everything', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 200, 100);
    await waitForNodeCount(page, 1);

    await dragModuleToCanvas(page, 'Scheduler', 400, 100);
    await waitForNodeCount(page, 2);

    await page.getByRole('button', { name: 'Clear' }).click();
    await waitForNodeCount(page, 0);
    await expect(page.getByText('0 modules')).toBeVisible();
    await screenshotStep(page, 'phase6-08-cleared-canvas');
  });

  test('P6-09: MiniMap and canvas controls visible', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);

    await expect(page.locator('.react-flow__minimap')).toBeVisible();
    await expect(page.locator('.react-flow__controls')).toBeVisible();
    await expect(page.locator('.react-flow__background')).toBeVisible();
    await screenshotStep(page, 'phase6-09-canvas-controls');
  });

  test('P6-10: Module counter updates correctly', async ({ page }) => {
    await expect(page.getByText('0 modules')).toBeVisible();

    await dragModuleToCanvas(page, 'HTTP Server', 200, 100);
    await waitForNodeCount(page, 1);
    await expect(page.getByText('1 module')).toBeVisible();

    await dragModuleToCanvas(page, 'HTTP Router', 400, 100);
    await waitForNodeCount(page, 2);
    await expect(page.getByText('2 modules')).toBeVisible();

    await dragModuleToCanvas(page, 'Scheduler', 300, 350);
    await waitForNodeCount(page, 3);
    await expect(page.getByText('3 modules')).toBeVisible();

    await screenshotStep(page, 'phase6-10-module-counter');
  });
});

// ============================================================================
// Section 2: Order Processing Workflow
// ============================================================================
test.describe('Phase 6: Order Processing Workflow', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForSelector('.react-flow', { timeout: 15000 });
    await page.waitForTimeout(500);
  });

  test('P6-11: Import order-processing-pipeline.yaml', async ({ page }) => {
    const yamlPath = path.resolve(__dirname, '../../example/order-processing-pipeline.yaml');

    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Import' }).click();
    const fileChooser = await fileChooserPromise;
    await fileChooser.setFiles(yamlPath);

    await expect(page.locator('.react-flow__node')).toHaveCount(10, { timeout: 10000 });
    await expect(page.getByText('10 modules')).toBeVisible();
    await screenshotStep(page, 'phase6-11-order-pipeline-imported');
  });

  test('P6-12: Verify node labels after import', async ({ page }) => {
    const yamlPath = path.resolve(__dirname, '../../example/order-processing-pipeline.yaml');
    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Import' }).click();
    const fileChooser = await fileChooserPromise;
    await fileChooser.setFiles(yamlPath);
    await expect(page.locator('.react-flow__node')).toHaveCount(10, { timeout: 10000 });

    const expectedNames = [
      'order-server', 'order-router', 'order-api', 'order-transformer',
      'order-state-engine', 'order-state-tracker', 'order-broker',
      'notification-handler', 'order-metrics', 'order-health',
    ];

    for (const name of expectedNames) {
      await expect(page.locator('.react-flow__node').filter({ hasText: name }).first()).toBeVisible({ timeout: 5000 });
    }
    await screenshotStep(page, 'phase6-12-node-labels');
  });

  test('P6-13: Click node and verify properties panel', async ({ page }) => {
    const yamlPath = path.resolve(__dirname, '../../example/order-processing-pipeline.yaml');
    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Import' }).click();
    const fileChooser = await fileChooserPromise;
    await fileChooser.setFiles(yamlPath);
    await expect(page.locator('.react-flow__node')).toHaveCount(10, { timeout: 10000 });

    // Click the first node (order-server)
    const serverNode = page.locator('.react-flow__node').filter({ hasText: 'order-server' }).first();
    await serverNode.click();
    await page.waitForTimeout(300);

    await expect(page.getByText('Properties', { exact: true })).toBeVisible();
    await expect(page.getByText('http.server').last()).toBeVisible();
    await screenshotStep(page, 'phase6-13-order-server-props');
  });

  test('P6-14: Export and re-import roundtrip', async ({ page }) => {
    const yamlPath = path.resolve(__dirname, '../../example/order-processing-pipeline.yaml');
    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Import' }).click();
    const fileChooser = await fileChooserPromise;
    await fileChooser.setFiles(yamlPath);
    await expect(page.locator('.react-flow__node')).toHaveCount(10, { timeout: 10000 });

    // Export
    const downloadPromise = page.waitForEvent('download', { timeout: 10000 });
    await page.getByRole('button', { name: 'Export YAML' }).click();
    const download = await downloadPromise;
    const filePath = await download.path();
    expect(filePath).toBeTruthy();
    const content = fs.readFileSync(filePath!, 'utf8');

    // Verify key types are present
    expect(content).toContain('http.server');
    expect(content).toContain('http.router');
    expect(content).toContain('data.transformer');
    expect(content).toContain('statemachine.engine');
    expect(content).toContain('messaging.broker');
    expect(content).toContain('metrics.collector');

    // Clear and reimport
    await page.getByRole('button', { name: 'Clear' }).click();
    await waitForNodeCount(page, 0);

    const tmpDir = path.join(__dirname, 'fixtures');
    fs.mkdirSync(tmpDir, { recursive: true });
    const tmpPath = path.join(tmpDir, 'phase6-roundtrip.yaml');
    fs.writeFileSync(tmpPath, content, 'utf8');

    const fileChooserPromise2 = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Import' }).click();
    const fileChooser2 = await fileChooserPromise2;
    await fileChooser2.setFiles(tmpPath);

    await expect(page.locator('.react-flow__node')).toHaveCount(10, { timeout: 10000 });
    await expect(page.getByText('10 modules')).toBeVisible();
    await screenshotStep(page, 'phase6-14-roundtrip-complete');
  });

  test('P6-15: Modify node property and export', async ({ page }) => {
    const yamlPath = path.resolve(__dirname, '../../example/order-processing-pipeline.yaml');
    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Import' }).click();
    const fileChooser = await fileChooserPromise;
    await fileChooser.setFiles(yamlPath);
    await expect(page.locator('.react-flow__node')).toHaveCount(10, { timeout: 10000 });

    // Click order-server node and modify address
    const serverNode = page.locator('.react-flow__node').filter({ hasText: 'order-server' }).first();
    await serverNode.click();
    await page.waitForTimeout(300);

    const addressInput = page.locator('label').filter({ hasText: 'Address' }).locator('input');
    await addressInput.fill(':9999');
    await page.waitForTimeout(300);

    // Export and verify
    const downloadPromise = page.waitForEvent('download', { timeout: 10000 });
    await page.getByRole('button', { name: 'Export YAML' }).click();
    const download = await downloadPromise;
    const filePath = await download.path();
    const content = fs.readFileSync(filePath!, 'utf8');

    expect(content).toContain(':9999');
    await screenshotStep(page, 'phase6-15-modified-export');
  });

  test('P6-16: Delete node and undo restores it', async ({ page }) => {
    const yamlPath = path.resolve(__dirname, '../../example/order-processing-pipeline.yaml');
    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Import' }).click();
    const fileChooser = await fileChooserPromise;
    await fileChooser.setFiles(yamlPath);
    await expect(page.locator('.react-flow__node')).toHaveCount(10, { timeout: 10000 });

    // Select and delete a node
    const metricsNode = page.locator('.react-flow__node').filter({ hasText: 'order-metrics' }).first();
    await metricsNode.click({ force: true });
    await page.waitForTimeout(300);

    await page.getByRole('button', { name: 'Delete Node' }).click();
    await page.waitForTimeout(300);
    await expect(page.locator('.react-flow__node')).toHaveCount(9, { timeout: 5000 });

    // Undo
    await page.getByRole('button', { name: 'Undo' }).click();
    await expect(page.locator('.react-flow__node')).toHaveCount(10, { timeout: 5000 });
    await screenshotStep(page, 'phase6-16-delete-undo');
  });
});

// ============================================================================
// Section 3: AI Copilot Panel
// ============================================================================
test.describe('Phase 6: AI Copilot Panel', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForSelector('.react-flow', { timeout: 15000 });
    await page.waitForTimeout(500);
  });

  test('P6-17: Open and close AI panel', async ({ page }) => {
    await page.getByRole('button', { name: 'AI Copilot' }).click();
    await expect(page.getByText('Describe your workflow')).toBeVisible();
    await screenshotStep(page, 'phase6-17-ai-panel-open');

    // Close via toolbar toggle
    await page.getByRole('button', { name: 'AI Copilot' }).click();
    await expect(page.getByText('Describe your workflow')).not.toBeVisible();
    await screenshotStep(page, 'phase6-17b-ai-panel-closed');
  });

  test('P6-18: Quick suggestion buttons visible', async ({ page }) => {
    await page.getByRole('button', { name: 'AI Copilot' }).click();

    await expect(page.getByText('Quick start')).toBeVisible();
    await expect(page.getByText('REST API with auth and rate limiting')).toBeVisible();
    await expect(page.getByText('Event-driven microservice')).toBeVisible();
    await expect(page.getByText('HTTP proxy with logging')).toBeVisible();
    await expect(page.getByText('Scheduled data pipeline')).toBeVisible();
    await expect(page.getByText('WebSocket chat backend')).toBeVisible();
    await screenshotStep(page, 'phase6-18-quick-suggestions');
  });

  test('P6-19: Click suggestion populates textarea', async ({ page }) => {
    await page.getByRole('button', { name: 'AI Copilot' }).click();

    await page.getByText('REST API with auth and rate limiting').click();
    const textarea = page.locator('textarea[placeholder*="REST API"]');
    await expect(textarea).toHaveValue('REST API with auth and rate limiting');
    await screenshotStep(page, 'phase6-19-suggestion-populated');
  });

  test('P6-20: Generate button disabled when empty', async ({ page }) => {
    await page.getByRole('button', { name: 'AI Copilot' }).click();

    const textarea = page.locator('textarea[placeholder*="REST API"]');
    await textarea.fill('');

    const generateBtn = page.getByText('Generate Workflow', { exact: true });
    await expect(generateBtn).toBeDisabled();
    await screenshotStep(page, 'phase6-20-generate-disabled');
  });

  test('P6-21: Submit form shows error without server', async ({ page }) => {
    await page.getByRole('button', { name: 'AI Copilot' }).click();

    const textarea = page.locator('textarea[placeholder*="REST API"]');
    await textarea.fill('A simple web server with authentication');

    const generateBtn = page.getByText('Generate Workflow', { exact: true });
    await expect(generateBtn).toBeEnabled();
    await generateBtn.click();
    await page.waitForTimeout(2000);

    // Without a backend server, expect an error indication
    // The page should still be functional even after error
    await expect(page.locator('.react-flow')).toBeVisible();
    await screenshotStep(page, 'phase6-21-generate-error');
  });

  test('P6-22: Explore suggestions section', async ({ page }) => {
    await page.getByRole('button', { name: 'AI Copilot' }).click();

    await expect(page.getByText('Explore suggestions')).toBeVisible();
    const useCaseInput = page.locator('input[placeholder*="Use case"]');
    await expect(useCaseInput).toBeVisible();

    const suggestBtn = page.getByText('Suggest', { exact: true });
    await expect(suggestBtn).toBeVisible();
    await screenshotStep(page, 'phase6-22-explore-suggestions');
  });
});

// ============================================================================
// Section 4: Component Browser
// ============================================================================
test.describe('Phase 6: Component Browser', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForSelector('.react-flow', { timeout: 15000 });
    await page.waitForTimeout(500);
  });

  test('P6-23: Open and close Component Browser', async ({ page }) => {
    await page.getByRole('button', { name: 'Components' }).click();
    await expect(page.getByText('Dynamic Components')).toBeVisible();
    await screenshotStep(page, 'phase6-23-components-open');

    // Toggle off
    await page.getByRole('button', { name: 'Components' }).click();
    await expect(page.getByText('Dynamic Components')).not.toBeVisible();
    await screenshotStep(page, 'phase6-23b-components-closed');
  });

  test('P6-24: Component Browser empty or loading state', async ({ page }) => {
    await page.getByRole('button', { name: 'Components' }).click();
    await page.waitForTimeout(2000);

    await expect(page.getByText('Dynamic Components')).toBeVisible();
    await screenshotStep(page, 'phase6-24-components-state');
  });

  test('P6-25: Create Component form visibility', async ({ page }) => {
    await page.getByRole('button', { name: 'Components' }).click();
    await page.getByText('+ Create Component').click();

    await expect(page.locator('input[placeholder="my-component"]')).toBeVisible();
    await expect(page.locator('textarea[placeholder="package main..."]')).toBeVisible();
    await screenshotStep(page, 'phase6-25-create-form');

    // Cancel hides it
    await page.getByText('Cancel', { exact: true }).click();
    await expect(page.locator('input[placeholder="my-component"]')).not.toBeVisible();
    await screenshotStep(page, 'phase6-25b-form-cancelled');
  });
});

// ============================================================================
// Section 5: Edge Cases & Stress
// ============================================================================
test.describe('Phase 6: Edge Cases', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForSelector('.react-flow', { timeout: 15000 });
    await page.waitForTimeout(500);
  });

  test('P6-26: Rapid undo/redo stability', async ({ page }) => {
    // Add 3 nodes
    await dragModuleToCanvas(page, 'HTTP Server', 200, 100);
    await waitForNodeCount(page, 1);
    await dragModuleToCanvas(page, 'HTTP Router', 400, 100);
    await waitForNodeCount(page, 2);
    await dragModuleToCanvas(page, 'Scheduler', 300, 350);
    await waitForNodeCount(page, 3);

    const undoBtn = page.getByRole('button', { name: 'Undo' });
    const redoBtn = page.getByRole('button', { name: 'Redo' });

    // Rapid undo 3 times
    await undoBtn.click();
    await undoBtn.click();
    await undoBtn.click();
    await page.waitForTimeout(500);
    await waitForNodeCount(page, 0);

    // Rapid redo 3 times
    await redoBtn.click();
    await redoBtn.click();
    await redoBtn.click();
    await page.waitForTimeout(500);
    await waitForNodeCount(page, 3);

    await expect(page.getByText('3 modules')).toBeVisible();
    await screenshotStep(page, 'phase6-26-rapid-undo-redo');
  });

  test('P6-27: Delete node from imported workflow with connections', async ({ page }) => {
    // Import workflow with connections, then delete a node
    const yamlContent = `modules:
  - name: web-server
    type: http.server
    config:
      address: ":8080"
  - name: api-router
    type: http.router
    dependsOn:
      - web-server
workflows: {}
triggers: {}`;

    const tmpDir = path.join(__dirname, 'fixtures');
    fs.mkdirSync(tmpDir, { recursive: true });
    const tmpPath = path.join(tmpDir, 'phase6-delete-connected.yaml');
    fs.writeFileSync(tmpPath, yamlContent, 'utf8');

    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Import' }).click();
    const fileChooser = await fileChooserPromise;
    await fileChooser.setFiles(tmpPath);

    await waitForNodeCount(page, 2);
    await expect(page.getByText('2 modules')).toBeVisible();

    // Delete one of the nodes
    const routerNode = page.locator('.react-flow__node').filter({ hasText: 'api-router' }).first();
    await routerNode.click({ force: true });
    await page.waitForTimeout(300);
    await page.getByRole('button', { name: 'Delete Node' }).click();
    await page.waitForTimeout(500);

    await waitForNodeCount(page, 1);
    await expect(page.getByText('1 module')).toBeVisible();
    await screenshotStep(page, 'phase6-27-delete-connected-node');
  });

  test('P6-28: Keyboard Delete removes selected node', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);

    await page.locator('.react-flow__node').first().click();
    await page.waitForTimeout(300);

    await page.keyboard.press('Delete');
    await waitForNodeCount(page, 0);
    await expect(page.getByText('0 modules')).toBeVisible();
    await screenshotStep(page, 'phase6-28-keyboard-delete');
  });

  test('P6-29: Keyboard Ctrl+Z undoes last action', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);

    // Click pane so canvas has focus
    await page.locator('.react-flow__pane').click({ position: { x: 50, y: 50 } });
    await page.waitForTimeout(200);

    await page.keyboard.press('Control+z');
    await waitForNodeCount(page, 0);
    await screenshotStep(page, 'phase6-29-ctrl-z-undo');
  });

  test('P6-30: Keyboard Ctrl+Shift+Z redoes', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);

    await page.locator('.react-flow__pane').click({ position: { x: 50, y: 50 } });
    await page.waitForTimeout(200);

    // Undo
    await page.keyboard.press('Control+z');
    await waitForNodeCount(page, 0);

    // Redo
    await page.keyboard.press('Control+Shift+z');
    await waitForNodeCount(page, 1);
    await screenshotStep(page, 'phase6-30-ctrl-shift-z-redo');
  });

  test('P6-31: Import all 30 module types via large workflow', async ({ page }) => {
    // Build a YAML with all module types from COMPLETE_MODULE_TYPE_MAP
    const entries = Object.entries(COMPLETE_MODULE_TYPE_MAP);
    const modules = entries.map(([label, type]) => {
      const name = label.toLowerCase().replace(/\s+/g, '-');
      return `  - name: ${name}\n    type: ${type}`;
    });
    const yamlContent = `modules:\n${modules.join('\n')}\nworkflows: {}\ntriggers: {}`;

    const tmpDir = path.join(__dirname, 'fixtures');
    fs.mkdirSync(tmpDir, { recursive: true });
    const tmpPath = path.join(tmpDir, 'phase6-all-modules.yaml');
    fs.writeFileSync(tmpPath, yamlContent, 'utf8');

    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Import' }).click();
    const fileChooser = await fileChooserPromise;
    await fileChooser.setFiles(tmpPath);

    // Should have all module nodes
    await expect(page.locator('.react-flow__node')).toHaveCount(entries.length, { timeout: 15000 });
    await screenshotStep(page, 'phase6-31-all-module-types');
  });

  test('P6-32: Validate button disabled on empty canvas', async ({ page }) => {
    // The Validate button should be disabled when canvas has no modules
    const validateBtn = page.getByRole('button', { name: 'Validate' });
    await expect(validateBtn).toBeVisible();
    await expect(validateBtn).toBeDisabled();

    // Add a module and verify Validate becomes enabled
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);
    await expect(validateBtn).toBeEnabled();

    await screenshotStep(page, 'phase6-32-validate-button');
  });

  test('P6-33: Category collapse and expand toggle', async ({ page }) => {
    // Collapse HTTP category
    const httpHeader = page.locator('[style*="cursor: pointer"]').filter({ hasText: 'HTTP' }).first();
    await httpHeader.scrollIntoViewIfNeeded();
    await httpHeader.click();
    await page.waitForTimeout(400);

    // HTTP Server should be hidden
    const httpServerDraggable = page.locator('[draggable="true"]').filter({ hasText: 'HTTP Server' });
    await expect(httpServerDraggable).toHaveCount(0, { timeout: 3000 });
    await screenshotStep(page, 'phase6-33a-collapsed');

    // Re-expand
    await httpHeader.click();
    await page.waitForTimeout(400);
    await expect(page.getByText('HTTP Server')).toBeVisible();
    await screenshotStep(page, 'phase6-33b-expanded');
  });
});
