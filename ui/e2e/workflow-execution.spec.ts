import { test, expect } from '@playwright/test';

const API_BASE = 'http://localhost:8080';

test.describe('Workflow Execution E2E', () => {
  test('GET /api/workflow/status returns running status', async ({ request }) => {
    const response = await request.get(`${API_BASE}/api/workflow/status`);
    expect(response.ok()).toBeTruthy();
    const body = await response.json();
    expect(body.status).toBeDefined();
  });

  test('PUT + GET config round-trip via API', async ({ request }) => {
    const testConfig = {
      modules: [
        {
          name: 'test-server',
          type: 'http.server',
          config: { address: ':9999' },
        },
        {
          name: 'test-router',
          type: 'http.router',
          dependsOn: ['test-server'],
        },
      ],
      workflows: {},
      triggers: {},
    };

    // PUT the config
    const putResponse = await request.put(`${API_BASE}/api/workflow/config`, {
      data: testConfig,
    });
    expect(putResponse.ok()).toBeTruthy();

    // GET it back
    const getResponse = await request.get(`${API_BASE}/api/workflow/config`);
    expect(getResponse.ok()).toBeTruthy();
    const body = await getResponse.json();
    expect(body.modules).toHaveLength(2);
    expect(body.modules[0].name).toBe('test-server');
    expect(body.modules[1].name).toBe('test-router');
  });

  test('Import order-processing-pipeline via UI and save', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Click Import button
    const importBtn = page.getByRole('button', { name: 'Import' });
    await importBtn.click();

    // Upload the order-processing-pipeline.yaml file
    const fileChooserPromise = page.waitForEvent('filechooser');
    const fileChooser = await fileChooserPromise;
    await fileChooser.setInputFiles('../example/order-processing-pipeline.yaml');

    // Wait for nodes to appear
    await expect(page.locator('.react-flow__node')).toHaveCount(10, { timeout: 10000 });

    // Click Save
    const saveBtn = page.getByRole('button', { name: 'Save' });
    await saveBtn.click();

    // Verify toast
    await expect(page.getByText('Workflow saved to server')).toBeVisible({ timeout: 5000 });

    // Verify config was saved via API
    const response = await page.request.get(`${API_BASE}/api/workflow/config`);
    expect(response.ok()).toBeTruthy();
    const body = await response.json();
    expect(body.modules.length).toBeGreaterThanOrEqual(10);
  });

  test('Validate workflow via API', async ({ request }) => {
    // First push a valid config
    const validConfig = {
      modules: [
        { name: 'server', type: 'http.server', config: { address: ':8080' } },
        { name: 'router', type: 'http.router', dependsOn: ['server'] },
      ],
      workflows: {},
      triggers: {},
    };

    await request.put(`${API_BASE}/api/workflow/config`, { data: validConfig });

    // Validate it
    const response = await request.post(`${API_BASE}/api/workflow/validate`, {
      data: validConfig,
    });
    expect(response.ok()).toBeTruthy();
    const body = await response.json();
    expect(body.valid).toBe(true);
  });

  test('Validation catches errors', async ({ request }) => {
    const badConfig = {
      modules: [
        { name: '', type: 'http.server' },
        { name: 'router', type: 'http.router', dependsOn: ['nonexistent'] },
      ],
      workflows: {},
      triggers: {},
    };

    const response = await request.post(`${API_BASE}/api/workflow/validate`, {
      data: badConfig,
    });
    expect(response.ok()).toBeTruthy();
    const body = await response.json();
    expect(body.valid).toBe(false);
    expect(body.errors.length).toBeGreaterThan(0);
  });

  test('GET /api/workflow/modules returns available modules', async ({ request }) => {
    const response = await request.get(`${API_BASE}/api/workflow/modules`);
    expect(response.ok()).toBeTruthy();
    const modules = await response.json();
    expect(Array.isArray(modules)).toBe(true);
    expect(modules.length).toBeGreaterThanOrEqual(20);

    // Check a few known module types
    const types = modules.map((m: { type: string }) => m.type);
    expect(types).toContain('http.server');
    expect(types).toContain('messaging.broker');
    expect(types).toContain('statemachine.engine');
  });

  test('Status endpoint shows module count after config push', async ({ request }) => {
    const config = {
      modules: [
        { name: 'a', type: 'http.server', config: { address: ':8080' } },
        { name: 'b', type: 'http.router' },
        { name: 'c', type: 'http.handler', config: { method: 'GET', path: '/' } },
      ],
      workflows: {},
      triggers: {},
    };

    await request.put(`${API_BASE}/api/workflow/config`, { data: config });

    const response = await request.get(`${API_BASE}/api/workflow/status`);
    expect(response.ok()).toBeTruthy();
    const body = await response.json();
    expect(body.moduleCount).toBe(3);
  });
});
