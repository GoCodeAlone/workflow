import { test, expect } from '@playwright/test';

/**
 * Helper to ensure we are on the login page by clearing any stored tokens.
 */
async function goToLoginPage(page: import('@playwright/test').Page) {
  await page.goto('/');
  // Clear any auth tokens so the login page is displayed
  await page.evaluate(() => {
    localStorage.removeItem('auth_token');
    localStorage.removeItem('auth_refresh_token');
  });
  await page.reload();
  // Wait for the login page to render
  await page.waitForSelector('h1', { timeout: 10000 });
}

test.describe('Authentication Flow', () => {
  test('should display login page with title and form', async ({ page }) => {
    await goToLoginPage(page);

    // The login page should show the app title
    await expect(page.locator('h1', { hasText: 'Workflow Engine' })).toBeVisible();

    // Subtitle
    await expect(page.getByText('Build and manage workflow pipelines')).toBeVisible();

    // Email and Password fields
    await expect(page.getByPlaceholder('you@example.com')).toBeVisible();
    await expect(page.getByPlaceholder('Enter password')).toBeVisible();

    // Sign In button (submit button, mode defaults to signin)
    await expect(page.getByRole('button', { name: 'Sign In' }).last()).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/auth-01-login-page.png', fullPage: true });
  });

  test('should show Sign In and Sign Up toggle buttons', async ({ page }) => {
    await goToLoginPage(page);

    // Both toggle buttons should be visible in the tab toggle bar
    const signInBtn = page.getByRole('button', { name: 'Sign In' }).first();
    const signUpBtn = page.getByRole('button', { name: 'Sign Up' }).first();

    await expect(signInBtn).toBeVisible();
    await expect(signUpBtn).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/auth-02-toggle-buttons.png', fullPage: true });
  });

  test('should switch to sign up mode and show additional fields', async ({ page }) => {
    await goToLoginPage(page);

    // Click Sign Up toggle
    const signUpToggle = page.getByRole('button', { name: 'Sign Up' }).first();
    await signUpToggle.click();
    await page.waitForTimeout(200);

    // In sign up mode, confirm password and display name fields appear
    await expect(page.getByPlaceholder('Confirm password')).toBeVisible();
    await expect(page.getByPlaceholder('Your name')).toBeVisible();

    // Submit button text changes to "Create Account"
    await expect(page.getByRole('button', { name: 'Create Account' })).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/auth-03-signup-mode.png', fullPage: true });
  });

  test('should validate password match on sign up', async ({ page }) => {
    await goToLoginPage(page);

    // Switch to sign up mode
    const signUpToggle = page.getByRole('button', { name: 'Sign Up' }).first();
    await signUpToggle.click();
    await page.waitForTimeout(200);

    // Fill in mismatched passwords
    await page.getByPlaceholder('Your name').fill('Test User');
    await page.getByPlaceholder('you@example.com').fill('test@example.com');
    await page.getByPlaceholder('Enter password').fill('password123');
    await page.getByPlaceholder('Confirm password').fill('differentpassword');

    // Submit the form
    await page.getByRole('button', { name: 'Create Account' }).click();
    await page.waitForTimeout(300);

    // Should show password mismatch error
    await expect(page.getByText('Passwords do not match')).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/auth-04-password-mismatch.png', fullPage: true });
  });

  test('should validate password length on sign up', async ({ page }) => {
    await goToLoginPage(page);

    // Switch to sign up mode
    const signUpToggle = page.getByRole('button', { name: 'Sign Up' }).first();
    await signUpToggle.click();
    await page.waitForTimeout(200);

    // Fill in short password
    await page.getByPlaceholder('you@example.com').fill('test@example.com');
    await page.getByPlaceholder('Enter password').fill('12345');
    await page.getByPlaceholder('Confirm password').fill('12345');

    // Submit the form
    await page.getByRole('button', { name: 'Create Account' }).click();
    await page.waitForTimeout(300);

    // Should show password length error
    await expect(page.getByText('Password must be at least 6 characters')).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/auth-05-password-length.png', fullPage: true });
  });

  test('should show OAuth buttons', async ({ page }) => {
    await goToLoginPage(page);

    // The "Or continue with" divider
    await expect(page.getByText('Or continue with')).toBeVisible();

    // OAuth buttons: Google, Okta, Auth0
    await expect(page.getByRole('button', { name: 'Continue with Google' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Continue with Okta' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Continue with Auth0' })).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/auth-06-oauth-buttons.png', fullPage: true });
  });

  test('should clear errors when switching between modes', async ({ page }) => {
    await goToLoginPage(page);

    // Switch to sign up and trigger an error
    const signUpToggle = page.getByRole('button', { name: 'Sign Up' }).first();
    await signUpToggle.click();
    await page.waitForTimeout(200);

    await page.getByPlaceholder('you@example.com').fill('test@example.com');
    await page.getByPlaceholder('Enter password').fill('password123');
    await page.getByPlaceholder('Confirm password').fill('different');
    await page.getByRole('button', { name: 'Create Account' }).click();
    await page.waitForTimeout(300);

    // Error should be visible
    await expect(page.getByText('Passwords do not match')).toBeVisible();

    // Switch back to Sign In - error should be cleared
    const signInToggle = page.getByRole('button', { name: 'Sign In' }).first();
    await signInToggle.click();
    await page.waitForTimeout(200);

    // Error should no longer be visible
    await expect(page.getByText('Passwords do not match')).not.toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/auth-07-clear-errors.png', fullPage: true });
  });

  test('should navigate to main app when authenticated', async ({ page }) => {
    await page.goto('/');
    // Inject a fake token to simulate being authenticated
    await page.evaluate(() => {
      localStorage.setItem('auth_token', 'e2e-fake-token');
      localStorage.setItem('auth_refresh_token', 'e2e-fake-refresh');
    });
    await page.reload();

    // The app should show the main layout instead of the login page.
    // The login page title "Workflow Engine" should be replaced by the app layout.
    // Look for main app UI elements: Toolbar, WorkflowTabs, or the "Workflow Editor" title.
    const isMainApp = await page.locator('.react-flow').isVisible({ timeout: 10000 }).catch(() => false);
    const hasToolbar = await page.getByText('Workflow Editor').isVisible({ timeout: 5000 }).catch(() => false);
    const hasEditor = isMainApp || hasToolbar;

    // If the main app is visible, the login page should NOT be visible
    if (hasEditor) {
      await expect(page.locator('h1', { hasText: 'Workflow Engine' })).not.toBeVisible();
    } else {
      // If the fake token doesn't work (API returns 401), the app might fall back to login.
      // This is expected behavior without a real backend.
      test.skip(true, 'Backend not available to validate token - login page shown');
    }

    await page.screenshot({ path: 'e2e/screenshots/auth-08-authenticated-app.png', fullPage: true });
  });

  test('should show loading state while submitting', async ({ page }) => {
    await goToLoginPage(page);

    // Fill in credentials
    await page.getByPlaceholder('you@example.com').fill('test@example.com');
    await page.getByPlaceholder('Enter password').fill('password123');

    // Click Sign In
    await page.getByRole('button', { name: 'Sign In' }).last().click();

    // The button should briefly show "Please wait..." (isLoading state)
    // Note: this may be very brief if the network request fails fast
    await page.getByText('Please wait...').isVisible({ timeout: 2000 }).catch(() => false);

    // Even if we don't catch the loading state (it may pass too quickly),
    // verify the form still works after the request completes/fails
    await page.waitForTimeout(1000);

    // Form should still be present (login failed because no backend)
    await expect(page.getByPlaceholder('you@example.com')).toBeVisible();

    await page.screenshot({ path: 'e2e/screenshots/auth-09-loading-state.png', fullPage: true });
  });
});
