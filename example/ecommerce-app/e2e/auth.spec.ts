import { test, expect } from '@playwright/test';
import { clearState, uniqueEmail, registerUser, loginUser } from './helpers';

test.describe('Authentication', () => {
  test.beforeEach(async ({ page }) => {
    await clearState(page);
  });

  test('register a new user', async ({ page }) => {
    const email = uniqueEmail();
    await page.goto('/#/register');

    await page.getByLabel('Full name').fill('New User');
    await page.getByLabel('Email').fill(email);
    await page.getByLabel('Password').fill('password123');
    await page.getByRole('button', { name: 'Create Account' }).click();

    // Should show authenticated nav (Orders, Profile visible)
    await expect(page.getByRole('link', { name: 'Orders' })).toBeVisible({ timeout: 10000 });
    await expect(page.getByRole('link', { name: 'Profile' })).toBeVisible();
    // Should show success toast
    await expect(page.getByText('Account created! Welcome!')).toBeVisible();
  });

  test('login with valid credentials', async ({ page }) => {
    // First register
    const { email, password } = await registerUser(page);

    // Logout
    await page.evaluate(() => {
      localStorage.removeItem('token');
      localStorage.removeItem('user');
    });
    await page.goto('/#/login');

    // Login
    await page.getByLabel('Email').fill(email);
    await page.getByLabel('Password').fill(password);
    await page.getByRole('button', { name: 'Sign In' }).click();

    await expect(page.getByRole('link', { name: 'Profile' })).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('Welcome back!')).toBeVisible();
  });

  test('login with invalid password shows error', async ({ page }) => {
    const { email } = await registerUser(page);

    await page.evaluate(() => {
      localStorage.removeItem('token');
      localStorage.removeItem('user');
    });
    await page.goto('/#/login');

    await page.getByLabel('Email').fill(email);
    await page.getByLabel('Password').fill('wrongpassword');
    await page.getByRole('button', { name: 'Sign In' }).click();

    // Should show error toast and stay on login page
    await expect(page.locator('.toast-error')).toBeVisible({ timeout: 5000 });
  });

  test('view and update profile', async ({ page }) => {
    await registerUser(page, { name: 'Profile Test' });

    await page.getByRole('link', { name: 'Profile' }).click();
    await expect(page.getByRole('heading', { name: 'Your Profile' })).toBeVisible();

    // Wait for profile to load — the form loads asynchronously via API
    await expect(page.locator('#profile-form')).toBeVisible({ timeout: 10000 });
    const nameInput = page.locator('#name');
    await expect(nameInput).toBeVisible();
    await expect(nameInput).toHaveValue('Profile Test');

    // Update name
    await nameInput.fill('Updated Name');
    await page.getByRole('button', { name: 'Save Changes' }).click();
    await expect(page.getByText('Profile updated')).toBeVisible();
  });

  test('logout clears session and redirects', async ({ page }) => {
    await registerUser(page);

    await page.getByRole('link', { name: 'Profile' }).click();
    await expect(page.getByRole('heading', { name: 'Your Profile' })).toBeVisible();

    // Wait for profile form to load (Sign Out button is inside the form)
    await expect(page.locator('#logout-btn')).toBeVisible({ timeout: 10000 });
    await page.locator('#logout-btn').click();

    // Nav should show Login instead of Profile
    await expect(page.getByRole('link', { name: 'Login' })).toBeVisible({ timeout: 5000 });
    // Orders link should not be visible
    await expect(page.getByRole('link', { name: 'Orders' })).not.toBeVisible();
  });
});
