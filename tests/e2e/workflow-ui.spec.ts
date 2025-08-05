import { test, expect, Page } from '@playwright/test';

// Helper function to login
async function login(page: Page, username: string = 'admin', password: string = 'admin') {
  await page.goto('/');
  
  // Wait for the login form to be visible
  await page.waitForSelector('input[type="text"]', { timeout: 15000 });
  await page.waitForSelector('input[type="password"]', { timeout: 15000 });
  
  await page.fill('input[type="text"]', username);
  await page.fill('input[type="password"]', password);
  await page.click('button[type="submit"]');
  
  // Wait for successful login and dashboard load
  await page.waitForSelector('.sidebar', { timeout: 15000 });
}

test.describe('Workflow UI Authentication', () => {
  test('should display login page', async ({ page }) => {
    await page.goto('/');
    
    // Wait for login page to load
    await page.waitForSelector('h3:has-text("Workflow Engine")', { timeout: 15000 });
    
    // Take screenshot of login page
    await page.screenshot({ path: 'screenshots/login-page.png', fullPage: true });
    
    await expect(page).toHaveTitle(/Workflow Engine/);
    await expect(page.locator('h3')).toContainText('Workflow Engine');
    await expect(page.locator('input[type="text"]')).toBeVisible();
    await expect(page.locator('input[type="password"]')).toBeVisible();
    await expect(page.locator('button[type="submit"]')).toBeVisible();
  });

  test('should login successfully with valid credentials', async ({ page }) => {
    await page.goto('/');
    
    // Wait for login form to be available
    await page.waitForSelector('input[type="text"]', { timeout: 15000 });
    await page.waitForSelector('input[type="password"]', { timeout: 15000 });
    
    // Fill login form
    await page.fill('input[type="text"]', 'admin');
    await page.fill('input[type="password"]', 'admin');
    
    // Take screenshot before login
    await page.screenshot({ path: 'screenshots/before-login.png', fullPage: true });
    
    await page.click('button[type="submit"]');
    
    // Wait for redirect to dashboard
    await page.waitForSelector('.sidebar', { timeout: 15000 });
    
    // Take screenshot after successful login
    await page.screenshot({ path: 'screenshots/after-login.png', fullPage: true });
    
    // Verify dashboard elements
    await expect(page.locator('.sidebar')).toBeVisible();
    await expect(page.locator('.navbar-brand')).toBeVisible();
    await expect(page.locator('nav.nav')).toBeVisible();
  });

  test('should show error with invalid credentials', async ({ page }) => {
    await page.goto('/');
    
    // Wait for login form
    await page.waitForSelector('input[type="text"]', { timeout: 15000 });
    
    await page.fill('input[type="text"]', 'admin');
    await page.fill('input[type="password"]', 'wrongpassword');
    
    await page.click('button[type="submit"]');
    
    // Wait for error message
    await page.waitForSelector('.alert-danger', { timeout: 10000 });
    
    // Take screenshot of error state
    await page.screenshot({ path: 'screenshots/login-error.png', fullPage: true });
    
    await expect(page.locator('.alert-danger')).toBeVisible();
    await expect(page.locator('.alert-danger')).toContainText(/authentication failed/i);
  });
});

test.describe('Workflow Management', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test('should display dashboard with metrics', async ({ page }) => {
    // Navigate to dashboard
    await page.click('a[href="#"]:has-text("Dashboard")');
    
    // Take screenshot of dashboard
    await page.screenshot({ path: 'screenshots/dashboard.png', fullPage: true });
    
    // Verify dashboard cards
    await expect(page.locator('.card')).toHaveCount(4); // Total workflows, executions, completed, failed
    await expect(page.locator('.card:has-text("Total Workflows")')).toBeVisible();
    await expect(page.locator('.card:has-text("Executions")')).toBeVisible();
    await expect(page.locator('.card:has-text("Completed")')).toBeVisible();
    await expect(page.locator('.card:has-text("Failed")')).toBeVisible();
  });

  test('should navigate to workflows page', async ({ page }) => {
    // Navigate to workflows
    await page.click('a[href="#"]:has-text("Workflows")');
    
    // Wait for workflows view with more flexible selector
    await page.waitForFunction(() => {
      return document.querySelector('.navbar-brand')?.textContent?.includes('Workflows') ||
             document.querySelector('[data-testid="workflows-view"]') !== null ||
             document.querySelector('.workflow-card') !== null;
    }, {}, { timeout: 10000 });
    
    // Take screenshot of workflows page
    await page.screenshot({ path: 'screenshots/workflows-page.png', fullPage: true });
    
    // Verify page elements
    await expect(page.locator('.navbar-brand')).toContainText('Workflows');
    await expect(page.locator('button:has-text("New Workflow")')).toBeVisible();
  });

  test('should open create workflow modal', async ({ page }) => {
    // Navigate to workflows
    await page.click('a[href="#"]:has-text("Workflows")');
    
    // Wait for workflows page to load
    await page.waitForSelector('button:has-text("New Workflow")', { timeout: 10000 });
    
    // Click New Workflow button
    await page.click('button:has-text("New Workflow")');
    
    // Wait for modal
    await page.waitForSelector('.modal', { timeout: 10000 });
    
    // Take screenshot of create workflow modal
    await page.screenshot({ path: 'screenshots/create-workflow-modal.png', fullPage: true });
    
    // Verify modal elements
    await expect(page.locator('.modal-title')).toContainText('Create Workflow');
    await expect(page.locator('input[v-model="workflowForm.name"]')).toBeVisible();
    await expect(page.locator('textarea[v-model="workflowForm.description"]')).toBeVisible();
    await expect(page.locator('textarea[v-model="workflowForm.config"]')).toBeVisible();
  });

  test('should validate workflow form', async ({ page }) => {
    // Navigate to workflows and open create modal
    await page.click('a[href="#"]:has-text("Workflows")');
    await page.waitForSelector('button:has-text("New Workflow")', { timeout: 10000 });
    await page.click('button:has-text("New Workflow")');
    await page.waitForSelector('.modal', { timeout: 10000 });
    
    // Fill form with sample data
    await page.fill('input[v-model="workflowForm.name"]', 'Test Workflow');
    await page.fill('textarea[v-model="workflowForm.description"]', 'A test workflow for demonstration');
    
    // The config should have a default value, let's verify it exists
    const configValue = await page.inputValue('textarea[v-model="workflowForm.config"]');
    expect(configValue.length).toBeGreaterThan(0);
    
    // Take screenshot of filled form
    await page.screenshot({ path: 'screenshots/workflow-form-filled.png', fullPage: true });
    
    // Verify form is fillable
    await expect(page.locator('input[v-model="workflowForm.name"]')).toHaveValue('Test Workflow');
    await expect(page.locator('textarea[v-model="workflowForm.description"]')).toHaveValue('A test workflow for demonstration');
  });

  test('should navigate to executions page', async ({ page }) => {
    // Navigate to executions
    await page.click('a[href="#"]:has-text("Executions")');
    
    // Wait for executions view with more flexible approach
    await page.waitForFunction(() => {
      return document.querySelector('.navbar-brand')?.textContent?.includes('Executions') ||
             document.querySelector('.table') !== null ||
             document.querySelector('[data-testid="executions-view"]') !== null;
    }, {}, { timeout: 10000 });
    
    // Take screenshot of executions page
    await page.screenshot({ path: 'screenshots/executions-page.png', fullPage: true });
    
    // Verify page elements
    await expect(page.locator('.navbar-brand')).toContainText('Executions');
    await expect(page.locator('.table')).toBeVisible();
  });

  test('should show user menu in sidebar', async ({ page }) => {
    // Find and click user dropdown
    await page.click('.dropdown button:has-text("admin")');
    
    // Wait for dropdown menu
    await page.waitForSelector('.dropdown-menu', { timeout: 10000 });
    
    // Take screenshot of user menu
    await page.screenshot({ path: 'screenshots/user-menu.png', fullPage: true });
    
    // Verify dropdown elements
    await expect(page.locator('.dropdown-menu')).toBeVisible();
    await expect(page.locator('.dropdown-item:has-text("Sign Out")')).toBeVisible();
  });

  test('should logout successfully', async ({ page }) => {
    // Click user dropdown and logout
    await page.click('.dropdown button:has-text("admin")');
    await page.waitForSelector('.dropdown-menu', { timeout: 10000 });
    
    await page.click('.dropdown-item:has-text("Sign Out")');
    
    // Wait for redirect to login page
    await page.waitForSelector('h3:has-text("Workflow Engine")', { timeout: 10000 });
    
    // Take screenshot after logout
    await page.screenshot({ path: 'screenshots/after-logout.png', fullPage: true });
    
    // Verify we're back on login page
    await expect(page.locator('h3')).toContainText('Workflow Engine');
    await expect(page.locator('input[type="text"]')).toBeVisible();
    await expect(page.locator('input[type="password"]')).toBeVisible();
  });
});

test.describe('Responsive Design', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test('should work on mobile viewport', async ({ page }) => {
    // Set mobile viewport
    await page.setViewportSize({ width: 375, height: 667 });
    
    // Take screenshot of mobile dashboard
    await page.screenshot({ path: 'screenshots/mobile-dashboard.png', fullPage: true });
    
    // Verify basic elements are still accessible
    await expect(page.locator('.sidebar')).toBeVisible();
    await expect(page.locator('.navbar-brand')).toBeVisible();
  });

  test('should work on tablet viewport', async ({ page }) => {
    // Set tablet viewport
    await page.setViewportSize({ width: 768, height: 1024 });
    
    // Take screenshot of tablet dashboard
    await page.screenshot({ path: 'screenshots/tablet-dashboard.png', fullPage: true });
    
    // Verify layout adapts
    await expect(page.locator('.sidebar')).toBeVisible();
    await expect(page.locator('.main-content')).toBeVisible();
  });
});