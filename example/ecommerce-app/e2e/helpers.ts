import { Page, expect } from '@playwright/test';

// Unique suffix per test run to avoid collisions
let counter = 0;
export function uniqueEmail(): string {
  counter++;
  return `test-${Date.now()}-${counter}@example.com`;
}

// Register a new user and return the email used
export async function registerUser(page: Page, opts?: { name?: string; email?: string; password?: string }) {
  const email = opts?.email || uniqueEmail();
  const name = opts?.name || 'Test User';
  const password = opts?.password || 'password123';

  await page.goto('/#/register');
  await expect(page.getByRole('heading', { name: 'Create account' })).toBeVisible();
  await page.getByLabel('Full name').fill(name);
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Create Account' }).click();

  // Wait for redirect to catalog — use a broader matcher
  await expect(page.getByRole('link', { name: 'Profile' })).toBeVisible({ timeout: 10000 });

  return { email, name, password };
}

// Login an existing user
export async function loginUser(page: Page, email: string, password: string) {
  // Clear any existing auth
  await page.evaluate(() => {
    localStorage.removeItem('token');
    localStorage.removeItem('user');
    localStorage.removeItem('cart');
  });

  await page.goto('/#/login');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign In' }).click();

  await expect(page.getByRole('link', { name: 'Profile' })).toBeVisible({ timeout: 10000 });
}

// Clear all localStorage state (auth + cart)
export async function clearState(page: Page) {
  await page.goto('/');
  await page.evaluate(() => {
    localStorage.clear();
    sessionStorage.clear();
  });
}

// Add a product to cart by clicking through the catalog
export async function addProductToCart(page: Page, productName: string) {
  await page.goto('/#/');
  await page.getByText(productName, { exact: true }).click();
  await expect(page.getByRole('heading', { name: productName })).toBeVisible();
  await page.getByRole('button', { name: 'Add to Cart' }).click();
  // Wait for toast to appear and then disappear to avoid text collisions
  await expect(page.getByText(`${productName} added to cart`)).toBeVisible();
  await page.waitForTimeout(500);
}

// Wait for products to load on the catalog page
export async function waitForCatalog(page: Page) {
  await page.goto('/#/');
  await expect(page.getByRole('heading', { name: 'Products' })).toBeVisible();
  // Wait for at least one product card to appear
  await expect(page.locator('.product-card, [cursor=pointer]').first()).toBeVisible({ timeout: 5000 });
}
