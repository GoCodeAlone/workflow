const { test, expect } = require('@playwright/test');

// Basic server accessibility test
test('UI Server is running and accessible', async ({ request }) => {
  const response = await request.get('/');
  expect(response.status()).toBe(200);
  expect(response.headers()['content-type']).toContain('text/html');
});

// API test - workflow listing
test('API returns workflow list', async ({ request }) => {
  const response = await request.get('/api/workflows');
  expect(response.status()).toBe(200);
  expect(response.headers()['content-type']).toContain('application/json');
  
  const data = await response.json();
  expect(data).toHaveProperty('workflows');
  expect(Array.isArray(data.workflows)).toBe(true);
});

// API test - nonexistent workflow
test('API returns 404 for nonexistent workflow', async ({ request }) => {
  const response = await request.get('/api/workflows/nonexistent-workflow');
  expect(response.status()).toBe(404);
});