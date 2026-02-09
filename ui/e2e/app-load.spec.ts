import { test, expect } from '@playwright/test';

test.describe('App Load', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    // Wait for ReactFlow to render
    await page.waitForSelector('.react-flow');
  });

  test('should load the app and show initial empty state', async ({ page }) => {
    await page.screenshot({ path: 'e2e/screenshots/01-initial-empty-state.png', fullPage: true });

    // App title should be visible
    await expect(page.getByText('Workflow Editor')).toBeVisible();
  });

  test('should display the node palette sidebar with all 10 categories', async ({ page }) => {
    // The sidebar header "Modules" should be visible
    await expect(page.getByText('Modules', { exact: true })).toBeVisible();

    // All 10 categories should be present
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

    for (const category of categories) {
      await expect(page.getByText(category, { exact: true })).toBeVisible();
    }

    await page.screenshot({ path: 'e2e/screenshots/02-node-palette-categories.png', fullPage: true });
  });

  test('should display toolbar with all action buttons', async ({ page }) => {
    // Check for toolbar buttons
    const buttons = ['Import', 'Load Server', 'Export YAML', 'Save', 'Validate', 'Undo', 'Redo', 'Clear'];

    for (const label of buttons) {
      await expect(page.getByRole('button', { name: label })).toBeVisible();
    }

    // AI Copilot and Components buttons
    await expect(page.getByRole('button', { name: 'AI Copilot' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Components' })).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/03-toolbar-buttons.png', fullPage: true });
  });

  test('should render the ReactFlow canvas area', async ({ page }) => {
    // ReactFlow canvas should exist
    const canvas = page.locator('.react-flow');
    await expect(canvas).toBeVisible();

    // Background dots (SVG pattern) should be rendered
    await expect(page.locator('.react-flow__background')).toBeVisible();

    // Controls panel should exist
    await expect(page.locator('.react-flow__controls')).toBeVisible();

    // MiniMap should exist
    await expect(page.locator('.react-flow__minimap')).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/04-canvas-area.png', fullPage: true });
  });

  test('should show module count as 0 initially', async ({ page }) => {
    await expect(page.getByText('0 modules')).toBeVisible();
  });

  test('should show palette module items within categories', async ({ page }) => {
    // HTTP category should show module items like "HTTP Server", "HTTP Router", etc.
    await expect(page.getByText('HTTP Server')).toBeVisible();
    await expect(page.getByText('HTTP Router')).toBeVisible();
    await expect(page.getByText('HTTP Handler')).toBeVisible();

    // Messaging category items
    await expect(page.getByText('Message Broker')).toBeVisible();

    // State Machine items
    await expect(page.getByText('State Machine', { exact: true })).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/05-full-app-layout.png', fullPage: true });
  });

  test('should display the property panel placeholder', async ({ page }) => {
    // When no node is selected, the property panel shows placeholder text
    await expect(page.getByText('Select a node to edit its properties')).toBeVisible();
  });

  test('should display new Database category items', async ({ page }) => {
    // Database category should show module items
    await expect(page.getByText('Workflow Database')).toBeVisible();
  });

  test('should display new Observability category items', async ({ page }) => {
    // Observability category should show module items
    await expect(page.getByText('Metrics Collector')).toBeVisible();
    await expect(page.getByText('Health Checker')).toBeVisible();
  });
});
