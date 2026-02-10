import { test, expect } from '@playwright/test';
import path from 'path';
import fs from 'fs';
import { fileURLToPath } from 'url';
import { dragModuleToCanvas, waitForNodeCount, screenshotStep } from './helpers';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

test.describe('Deep Import/Export', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForSelector('.react-flow', { timeout: 15000 });
  });

  test('should export multi-node workflow with connections', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 200, 50);
    await waitForNodeCount(page, 1);
    await dragModuleToCanvas(page, 'Auth Middleware', 200, 250);
    await waitForNodeCount(page, 2);
    await dragModuleToCanvas(page, 'HTTP Router', 200, 450);
    await waitForNodeCount(page, 3);

    const downloadPromise = page.waitForEvent('download', { timeout: 10000 });
    await page.getByRole('button', { name: 'Export YAML' }).click();
    const download = await downloadPromise;

    const filePath = await download.path();
    expect(filePath).toBeTruthy();
    const content = fs.readFileSync(filePath!, 'utf8');

    expect(content).toContain('http.server');
    expect(content).toContain('http.middleware.auth');
    expect(content).toContain('http.router');
    expect(content).toContain('modules:');
    await screenshotStep(page, 'deep-19-multi-export');
  });

  test('should import YAML with dependsOn edges', async ({ page }) => {
    const yaml = `modules:
  - name: Web Server
    type: http.server
    config:
      address: ":8080"
  - name: Auth
    type: http.middleware.auth
    config:
      type: jwt
    dependsOn:
      - Web Server
  - name: Router
    type: http.router
    dependsOn:
      - Auth
workflows: {}
triggers: {}
`;
    const tmpDir = path.join(__dirname, 'fixtures');
    fs.mkdirSync(tmpDir, { recursive: true });
    const yamlPath = path.join(tmpDir, 'deep-import-deps.yaml');
    fs.writeFileSync(yamlPath, yaml, 'utf8');

    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Import' }).click();
    const fileChooser = await fileChooserPromise;
    await fileChooser.setFiles(yamlPath);

    await waitForNodeCount(page, 3);
    await expect(page.getByText('3 modules')).toBeVisible();

    // Should have edges from dependsOn (at least 1, may be 2)
    const edgeCount = await page.locator('.react-flow__edge').count();
    expect(edgeCount).toBeGreaterThanOrEqual(1);

    // Toast
    await expect(page.getByText('Workflow imported from file')).toBeVisible({ timeout: 5000 });
    await screenshotStep(page, 'deep-20-import-deps');
  });

  test('should round-trip complex workflow with config', async ({ page }) => {
    // Add nodes with config
    await dragModuleToCanvas(page, 'HTTP Server', 300, 100);
    await waitForNodeCount(page, 1);

    // Edit config
    await page.locator('.react-flow__node').first().click();
    const nameInput = page.locator('label').filter({ hasText: 'Name' }).locator('input');
    await nameInput.fill('Production Server');
    const addressInput = page.locator('label').filter({ hasText: 'Address' }).locator('input');
    await addressInput.fill(':9090');
    await page.waitForTimeout(200);

    // Add second node
    await dragModuleToCanvas(page, 'Rate Limiter', 300, 350);
    await waitForNodeCount(page, 2);

    // Export
    const downloadPromise = page.waitForEvent('download', { timeout: 10000 });
    await page.getByRole('button', { name: 'Export YAML' }).click();
    const download = await downloadPromise;
    const filePath = await download.path();
    const exportedContent = fs.readFileSync(filePath!, 'utf8');

    expect(exportedContent).toContain('Production Server');
    expect(exportedContent).toContain(':9090');

    // Clear
    await page.getByRole('button', { name: 'Clear' }).click();
    await waitForNodeCount(page, 0);

    // Re-import
    const tmpDir = path.join(__dirname, 'fixtures');
    fs.mkdirSync(tmpDir, { recursive: true });
    const tmpPath = path.join(tmpDir, 'deep-roundtrip.yaml');
    fs.writeFileSync(tmpPath, exportedContent, 'utf8');

    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Import' }).click();
    const fileChooser = await fileChooserPromise;
    await fileChooser.setFiles(tmpPath);

    await waitForNodeCount(page, 2);
    await expect(page.locator('.react-flow__node').filter({ hasText: 'Production Server' })).toBeVisible();
    await screenshotStep(page, 'deep-21-roundtrip-config');
  });

  test('should import workflow with new module types', async ({ page }) => {
    const yaml = `modules:
  - name: DB
    type: database.workflow
    config:
      driver: sqlite
      dsn: "file:test.db"
  - name: Metrics
    type: metrics.collector
  - name: Transformer
    type: data.transformer
workflows: {}
triggers: {}
`;
    const tmpDir = path.join(__dirname, 'fixtures');
    fs.mkdirSync(tmpDir, { recursive: true });
    const yamlPath = path.join(tmpDir, 'deep-import-new-types.yaml');
    fs.writeFileSync(yamlPath, yaml, 'utf8');

    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Import' }).click();
    const fileChooser = await fileChooserPromise;
    await fileChooser.setFiles(yamlPath);

    await waitForNodeCount(page, 3);
    await expect(page.locator('.react-flow__node').filter({ hasText: 'DB' })).toBeVisible();
    await expect(page.locator('.react-flow__node').filter({ hasText: 'Metrics' })).toBeVisible();
    await expect(page.locator('.react-flow__node').filter({ hasText: 'Transformer' })).toBeVisible();
  });

  test('should handle empty modules import', async ({ page }) => {
    const yaml = `modules: []
workflows: {}
triggers: {}
`;
    const tmpDir = path.join(__dirname, 'fixtures');
    fs.mkdirSync(tmpDir, { recursive: true });
    const yamlPath = path.join(tmpDir, 'deep-import-empty.yaml');
    fs.writeFileSync(yamlPath, yaml, 'utf8');

    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Import' }).click();
    const fileChooser = await fileChooserPromise;
    await fileChooser.setFiles(yamlPath);

    await waitForNodeCount(page, 0);
    await expect(page.getByText('Workflow imported from file')).toBeVisible({ timeout: 5000 });
  });

  test('should show error for malformed YAML', async ({ page }) => {
    const tmpDir = path.join(__dirname, 'fixtures');
    fs.mkdirSync(tmpDir, { recursive: true });
    const yamlPath = path.join(tmpDir, 'deep-malformed.yaml');
    fs.writeFileSync(yamlPath, '{{bad: yaml: [[}', 'utf8');

    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Import' }).click();
    const fileChooser = await fileChooserPromise;
    await fileChooser.setFiles(yamlPath);

    await expect(page.getByText('Failed to parse workflow file')).toBeVisible({ timeout: 5000 });
    await screenshotStep(page, 'deep-22-import-error');
  });
});
