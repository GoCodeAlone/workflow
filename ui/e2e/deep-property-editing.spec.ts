import { test, expect } from '@playwright/test';
import { dragModuleToCanvas, waitForNodeCount, screenshotStep } from './helpers';

test.describe('Deep Property Editing', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForSelector('.react-flow', { timeout: 15000 });
  });

  test('should edit HTTP Server address field', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);
    await page.locator('.react-flow__node').first().click();

    const addressInput = page.locator('label').filter({ hasText: 'Address' }).locator('input');
    await expect(addressInput).toBeVisible();
    await expect(addressInput).toHaveValue(':8080');
    await addressInput.fill(':3000');
    await expect(addressInput).toHaveValue(':3000');
    await screenshotStep(page, 'deep-03-edit-address');
  });

  test('should edit HTTP Server read timeout field', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);
    await page.locator('.react-flow__node').first().click();

    const timeoutInput = page.locator('label').filter({ hasText: 'Read Timeout' }).locator('input');
    await expect(timeoutInput).toBeVisible();
    await timeoutInput.fill('60s');
    await expect(timeoutInput).toHaveValue('60s');
  });

  test('should edit node name and see update on canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);
    await page.locator('.react-flow__node').first().click();

    const nameInput = page.locator('label').filter({ hasText: 'Name' }).locator('input');
    await expect(nameInput).toBeVisible();
    await nameInput.fill('My Custom Server');
    await expect(page.locator('.react-flow__node').first().getByText('My Custom Server')).toBeVisible();
    await screenshotStep(page, 'deep-04-rename-node');
  });

  test('should use select dropdown for HTTP Handler method', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Handler', 300, 200);
    await waitForNodeCount(page, 1);
    await page.locator('.react-flow__node').first().click();

    const methodSelect = page.locator('label').filter({ hasText: 'Method' }).locator('select');
    await expect(methodSelect).toBeVisible();
    await methodSelect.selectOption('POST');
    await expect(methodSelect).toHaveValue('POST');
    await screenshotStep(page, 'deep-05-select-method');
  });

  test('should edit HTTP Handler path field', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Handler', 300, 200);
    await waitForNodeCount(page, 1);
    await page.locator('.react-flow__node').first().click();

    const pathInput = page.locator('label').filter({ hasText: 'Path' }).locator('input');
    await expect(pathInput).toBeVisible();
    await pathInput.fill('/users');
    await expect(pathInput).toHaveValue('/users');
  });

  test('should edit Auth Middleware type dropdown', async ({ page }) => {
    await dragModuleToCanvas(page, 'Auth Middleware', 300, 200);
    await waitForNodeCount(page, 1);
    await page.locator('.react-flow__node').first().click();

    const authSelect = page.locator('label').filter({ hasText: 'Auth Type' }).locator('select');
    await expect(authSelect).toBeVisible();
    await authSelect.selectOption('basic');
    await expect(authSelect).toHaveValue('basic');
  });

  test('should edit Rate Limiter number field', async ({ page }) => {
    await dragModuleToCanvas(page, 'Rate Limiter', 300, 200);
    await waitForNodeCount(page, 1);
    await page.locator('.react-flow__node').first().click();

    const rpsInput = page.locator('label').filter({ hasText: 'Requests/sec' }).locator('input');
    await expect(rpsInput).toBeVisible();
    await rpsInput.fill('500');
    await expect(rpsInput).toHaveValue('500');
    await screenshotStep(page, 'deep-06-number-field');
  });

  test('should edit Logging Middleware log level', async ({ page }) => {
    await dragModuleToCanvas(page, 'Logging Middleware', 300, 200);
    await waitForNodeCount(page, 1);
    await page.locator('.react-flow__node').first().click();

    const levelSelect = page.locator('label').filter({ hasText: 'Log Level' }).locator('select');
    await expect(levelSelect).toBeVisible();
    await levelSelect.selectOption('debug');
    await expect(levelSelect).toHaveValue('debug');
  });

  test('should edit Message Broker provider select', async ({ page }) => {
    await dragModuleToCanvas(page, 'Message Broker', 300, 200);
    await waitForNodeCount(page, 1);
    await page.locator('.react-flow__node').first().click();

    const providerSelect = page.locator('label').filter({ hasText: 'Provider' }).locator('select');
    await expect(providerSelect).toBeVisible();
    await providerSelect.selectOption('kafka');
    await expect(providerSelect).toHaveValue('kafka');
  });

  test('should edit Workflow Database driver and DSN', async ({ page }) => {
    await dragModuleToCanvas(page, 'Workflow Database', 300, 200);
    await waitForNodeCount(page, 1);
    await page.locator('.react-flow__node').first().click();

    const driverSelect = page.locator('label').filter({ hasText: 'Driver' }).locator('select');
    await expect(driverSelect).toBeVisible();
    await driverSelect.selectOption('sqlite');
    await expect(driverSelect).toHaveValue('sqlite');

    const dsnInput = page.locator('label').filter({ hasText: 'DSN' }).locator('input');
    await expect(dsnInput).toBeVisible();
    await dsnInput.fill('file:test.db');
    await expect(dsnInput).toHaveValue('file:test.db');
    await screenshotStep(page, 'deep-07-db-config');
  });

  test('should edit Workflow Database numeric fields', async ({ page }) => {
    await dragModuleToCanvas(page, 'Workflow Database', 300, 200);
    await waitForNodeCount(page, 1);
    await page.locator('.react-flow__node').first().click();

    const maxOpenInput = page.locator('label').filter({ hasText: 'Max Open Connections' }).locator('input');
    await expect(maxOpenInput).toBeVisible();
    await maxOpenInput.fill('50');
    await expect(maxOpenInput).toHaveValue('50');

    const maxIdleInput = page.locator('label').filter({ hasText: 'Max Idle Connections' }).locator('input');
    await expect(maxIdleInput).toBeVisible();
    await maxIdleInput.fill('10');
    await expect(maxIdleInput).toHaveValue('10');
  });

  test('should edit Cache provider and TTL', async ({ page }) => {
    await dragModuleToCanvas(page, 'Cache', 300, 200);
    await waitForNodeCount(page, 1);
    await page.locator('.react-flow__node').first().click();

    const providerSelect = page.locator('label').filter({ hasText: 'Provider' }).locator('select');
    await expect(providerSelect).toBeVisible();
    await providerSelect.selectOption('redis');
    await expect(providerSelect).toHaveValue('redis');

    const ttlInput = page.locator('label').filter({ hasText: 'TTL' }).locator('input');
    await expect(ttlInput).toBeVisible();
    await ttlInput.fill('10m');
    await expect(ttlInput).toHaveValue('10m');
  });

  test('should edit Webhook Sender max retries', async ({ page }) => {
    await dragModuleToCanvas(page, 'Webhook Sender', 300, 200);
    await waitForNodeCount(page, 1);
    await page.locator('.react-flow__node').first().click();

    const retriesInput = page.locator('label').filter({ hasText: 'Max Retries' }).locator('input');
    await expect(retriesInput).toBeVisible();
    await retriesInput.fill('5');
    await expect(retriesInput).toHaveValue('5');
  });

  test('should switch between nodes and property panels update', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 200, 100);
    await waitForNodeCount(page, 1);
    await dragModuleToCanvas(page, 'Message Broker', 200, 350);
    await waitForNodeCount(page, 2);

    // Click first node
    await page.locator('.react-flow__node').first().click();
    await expect(page.getByText('http.server').last()).toBeVisible();

    // Click second node
    await page.locator('.react-flow__node').nth(1).click();
    await expect(page.getByText('messaging.broker').last()).toBeVisible();

    await screenshotStep(page, 'deep-08-switch-nodes');
  });
});
