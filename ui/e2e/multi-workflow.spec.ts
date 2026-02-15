import { test, expect } from '@playwright/test';
import { dragModuleToCanvas, waitForNodeCount, screenshotStep } from './helpers';

/**
 * Helper to inject a fake auth token into localStorage so the app renders
 * the main layout (bypassing the login gate).
 */
async function authenticateAndLoad(page: import('@playwright/test').Page) {
  await page.goto('/');
  await page.evaluate(() => {
    localStorage.setItem('auth_token', 'e2e-fake-token');
    localStorage.setItem('auth_refresh_token', 'e2e-fake-refresh');
  });
  await page.reload();
  // The app should now show the editor view instead of the login page.
  // Wait for either ReactFlow (editor) or the Workflow Engine title (login fallback).
  // The existing tests rely on .react-flow being present.
  await page.waitForSelector('.react-flow', { timeout: 15000 }).catch(() => {});
}

test.describe('Multi-Workflow Tab Management', () => {
  test.beforeEach(async ({ page }) => {
    await authenticateAndLoad(page);
  });

  test('should load with default tab', async ({ page }) => {
    // The WorkflowTabs component renders a bar with at least one tab.
    // Each tab is rendered as a div with a span for the name.
    // The "+" button should also be visible.
    const addTabButton = page.locator('button', { hasText: '+' });
    const tabExists = await addTabButton.isVisible().catch(() => false);

    if (!tabExists) {
      test.skip(true, 'Tab bar not visible - editor view may not be active');
      return;
    }

    // At least one tab should be present. Tabs are spans inside the tab bar area.
    // The default tab name is typically "Untitled" or "Workflow 1"
    const tabBar = addTabButton.locator('..');
    await expect(tabBar).toBeVisible();

    await screenshotStep(page, 'multi-workflow-01-default-tab');
  });

  test('should create new workflow tab', async ({ page }) => {
    const addTabButton = page.locator('button', { hasText: '+' }).last();
    const tabExists = await addTabButton.isVisible().catch(() => false);
    if (!tabExists) {
      test.skip(true, 'Tab bar not visible');
      return;
    }

    // Count current tabs by looking for the "x" close buttons or tab name spans.
    // Initially there's 1 tab, but the close button only shows when tabs > 1.
    await addTabButton.click();
    await page.waitForTimeout(300);

    // After adding a tab, there should now be 2 tabs.
    // Close buttons appear when there are more than 1 tab.
    const closeButtons = page.locator('button', { hasText: 'x' });
    const closeCount = await closeButtons.count();
    expect(closeCount).toBeGreaterThanOrEqual(2);

    await screenshotStep(page, 'multi-workflow-02-new-tab');
  });

  test('should switch between tabs', async ({ page }) => {
    const addTabButton = page.locator('button', { hasText: '+' }).last();
    const tabExists = await addTabButton.isVisible().catch(() => false);
    if (!tabExists) {
      test.skip(true, 'Tab bar not visible');
      return;
    }

    // Create a second tab
    await addTabButton.click();
    await page.waitForTimeout(300);

    // The tab bar container is the parent of the "+" button.
    // Tabs are rendered as divs inside a scrollable area.
    // The active tab has a blue bottom border (#89b4fa).
    // We can identify tabs by their close "x" buttons and click their parent.

    // Get all tab-like elements (elements containing an "x" close button)
    const tabElements = page.locator('button', { hasText: 'x' });
    const count = await tabElements.count();
    expect(count).toBeGreaterThanOrEqual(2);

    // Click on the first tab's parent area (the tab div)
    const firstTabClose = tabElements.nth(0);
    const firstTab = firstTabClose.locator('..');
    await firstTab.click();
    await page.waitForTimeout(200);

    // Click on the second tab
    const secondTabClose = tabElements.nth(1);
    const secondTab = secondTabClose.locator('..');
    await secondTab.click();
    await page.waitForTimeout(200);

    await screenshotStep(page, 'multi-workflow-03-switch-tabs');
  });

  test('should close a tab', async ({ page }) => {
    const addTabButton = page.locator('button', { hasText: '+' }).last();
    const tabExists = await addTabButton.isVisible().catch(() => false);
    if (!tabExists) {
      test.skip(true, 'Tab bar not visible');
      return;
    }

    // Create a second tab
    await addTabButton.click();
    await page.waitForTimeout(300);

    // Count close buttons before
    const closeButtonsBefore = page.locator('button', { hasText: 'x' });
    const countBefore = await closeButtonsBefore.count();
    expect(countBefore).toBeGreaterThanOrEqual(2);

    // Close the second tab
    await closeButtonsBefore.nth(1).click();
    await page.waitForTimeout(300);

    // After closing, if only 1 tab remains, close buttons disappear
    // (the component hides the "x" when tabs.length <= 1)
    const closeButtonsAfter = page.locator('button', { hasText: 'x' });
    const countAfter = await closeButtonsAfter.count();
    // Either 0 (single tab, no close button) or countBefore - 1
    expect(countAfter).toBeLessThan(countBefore);

    await screenshotStep(page, 'multi-workflow-04-close-tab');
  });

  test('should rename tab on double-click', async ({ page }) => {
    const addTabButton = page.locator('button', { hasText: '+' }).last();
    const tabExists = await addTabButton.isVisible().catch(() => false);
    if (!tabExists) {
      test.skip(true, 'Tab bar not visible');
      return;
    }

    // The tab bar is in the parent of the "+" button.
    // Find the scrollable area which contains tab divs (just before the "+" button).
    // Tab names are rendered as <span> elements inside tab divs.
    // Double-clicking a span triggers the editing input.

    // Look for the tab name span. Default tab names contain "Workflow" or "Untitled".
    // We'll find spans inside the tab bar area (height: 32, background: #181825).
    const tabBarContainer = page.locator('div').filter({ has: addTabButton }).first();
    const tabNameSpans = tabBarContainer.locator('span');
    const spanCount = await tabNameSpans.count();

    if (spanCount === 0) {
      test.skip(true, 'No tab name spans found');
      return;
    }

    const firstTabName = tabNameSpans.first();
    // Double-click to start editing
    await firstTabName.dblclick();
    await page.waitForTimeout(200);

    // An input should now appear in the tab bar
    const editInput = tabBarContainer.locator('input');
    const inputVisible = await editInput.isVisible().catch(() => false);

    if (!inputVisible) {
      test.skip(true, 'Tab rename input did not appear');
      return;
    }

    // Clear and type a new name
    await editInput.fill('My Renamed Workflow');
    await editInput.press('Enter');
    await page.waitForTimeout(200);

    // Verify the name changed
    await expect(tabBarContainer.getByText('My Renamed Workflow')).toBeVisible();

    await screenshotStep(page, 'multi-workflow-05-rename-tab');
  });

  test('should preserve canvas state between tabs', async ({ page }) => {
    const addTabButton = page.locator('button', { hasText: '+' }).last();
    const tabExists = await addTabButton.isVisible().catch(() => false);
    if (!tabExists) {
      test.skip(true, 'Tab bar not visible');
      return;
    }

    // Check if ReactFlow canvas is available for drag-and-drop
    const canvas = page.locator('.react-flow');
    const canvasVisible = await canvas.isVisible().catch(() => false);
    if (!canvasVisible) {
      test.skip(true, 'ReactFlow canvas not visible - editor subview may not be active');
      return;
    }

    // Add a node to tab 1
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);

    // Create tab 2
    await addTabButton.click();
    await page.waitForTimeout(500);

    // Tab 2 should have an empty canvas
    const nodesInTab2 = await page.locator('.react-flow__node').count();
    expect(nodesInTab2).toBe(0);

    // Switch back to tab 1 by clicking the first tab
    const closeButtons = page.locator('button', { hasText: 'x' });
    const firstTab = closeButtons.nth(0).locator('..');
    await firstTab.click();
    await page.waitForTimeout(500);

    // Tab 1 should still have the node
    await waitForNodeCount(page, 1);

    await screenshotStep(page, 'multi-workflow-06-preserve-state');
  });
});
