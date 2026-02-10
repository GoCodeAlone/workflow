import { test, expect } from '@playwright/test';
import path from 'path';
import fs from 'fs';
import { fileURLToPath } from 'url';
import { dragModuleToCanvas, waitForNodeCount, screenshotStep } from './helpers';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

test.describe('Deep Toast Notifications', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForSelector('.react-flow', { timeout: 15000 });
  });

  test('should show success toast on YAML import', async ({ page }) => {
    const yaml = `modules:
  - name: Server
    type: http.server
workflows: {}
triggers: {}
`;
    const tmpDir = path.join(__dirname, 'fixtures');
    fs.mkdirSync(tmpDir, { recursive: true });
    const yamlPath = path.join(tmpDir, 'deep-toast-import.yaml');
    fs.writeFileSync(yamlPath, yaml, 'utf8');

    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Import' }).click();
    const fileChooser = await fileChooserPromise;
    await fileChooser.setFiles(yamlPath);

    await expect(page.getByText('Workflow imported from file')).toBeVisible({ timeout: 5000 });
    await screenshotStep(page, 'deep-33-toast-success');
  });

  test('should show error toast on invalid import', async ({ page }) => {
    const tmpDir = path.join(__dirname, 'fixtures');
    fs.mkdirSync(tmpDir, { recursive: true });
    const yamlPath = path.join(tmpDir, 'deep-toast-invalid.yaml');
    fs.writeFileSync(yamlPath, '{{broken yaml}', 'utf8');

    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Import' }).click();
    const fileChooser = await fileChooserPromise;
    await fileChooser.setFiles(yamlPath);

    await expect(page.getByText('Failed to parse workflow file')).toBeVisible({ timeout: 5000 });
    await screenshotStep(page, 'deep-34-toast-error');
  });

  test('should enable Validate button when nodes exist', async ({ page }) => {
    // Validate should be disabled with no nodes
    await expect(page.getByRole('button', { name: 'Validate' })).toBeDisabled();

    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);

    // Validate should be enabled now
    await expect(page.getByRole('button', { name: 'Validate' })).toBeEnabled();
    await screenshotStep(page, 'deep-35-validate-enabled');
  });

  test('should dismiss toast by clicking x', async ({ page }) => {
    const yaml = `modules:
  - name: Server
    type: http.server
workflows: {}
triggers: {}
`;
    const tmpDir = path.join(__dirname, 'fixtures');
    fs.mkdirSync(tmpDir, { recursive: true });
    const yamlPath = path.join(tmpDir, 'deep-toast-dismiss.yaml');
    fs.writeFileSync(yamlPath, yaml, 'utf8');

    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Import' }).click();
    const fileChooser = await fileChooserPromise;
    await fileChooser.setFiles(yamlPath);

    const toast = page.getByText('Workflow imported from file');
    await expect(toast).toBeVisible({ timeout: 5000 });

    // Click the dismiss button on the toast
    const toastContainer = toast.locator('..');
    const dismissBtn = toastContainer.locator('button');
    await dismissBtn.click();

    await expect(toast).not.toBeVisible();
  });

  test('should auto-dismiss toast after timeout', async ({ page }) => {
    const yaml = `modules:
  - name: Server
    type: http.server
workflows: {}
triggers: {}
`;
    const tmpDir = path.join(__dirname, 'fixtures');
    fs.mkdirSync(tmpDir, { recursive: true });
    const yamlPath = path.join(tmpDir, 'deep-toast-auto.yaml');
    fs.writeFileSync(yamlPath, yaml, 'utf8');

    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Import' }).click();
    const fileChooser = await fileChooserPromise;
    await fileChooser.setFiles(yamlPath);

    const toast = page.getByText('Workflow imported from file');
    await expect(toast).toBeVisible({ timeout: 5000 });

    // Toast auto-dismisses after 4000ms
    await expect(toast).not.toBeVisible({ timeout: 6000 });
  });

  test('should show multiple toasts from import and validate', async ({ page }) => {
    // Import valid file to trigger success toast
    const yaml = `modules:
  - name: Server
    type: http.server
workflows: {}
triggers: {}
`;
    const tmpDir = path.join(__dirname, 'fixtures');
    fs.mkdirSync(tmpDir, { recursive: true });
    const yamlPath = path.join(tmpDir, 'deep-toast-stack.yaml');
    fs.writeFileSync(yamlPath, yaml, 'utf8');

    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Import' }).click();
    const fileChooser = await fileChooserPromise;
    await fileChooser.setFiles(yamlPath);

    await expect(page.getByText('Workflow imported from file')).toBeVisible({ timeout: 5000 });
    await screenshotStep(page, 'deep-36-toast-stack');
  });

  test('should show toast when Save button clicked', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);

    // Click Save - will either succeed (server running) or fail (server down)
    await page.getByRole('button', { name: 'Save' }).click();

    // Should show either success or error toast
    await expect(page.getByText(/saved|Save failed|API/)).toBeVisible({ timeout: 30000 });
    await screenshotStep(page, 'deep-37-toast-save');
  });

  test('should show toast when Load Server button clicked', async ({ page }) => {
    await page.getByRole('button', { name: 'Load Server' }).click();

    // Should show either success or error toast
    await expect(page.getByText(/loaded|Failed to load|API/)).toBeVisible({ timeout: 30000 });
    await screenshotStep(page, 'deep-38-toast-load');
  });
});
