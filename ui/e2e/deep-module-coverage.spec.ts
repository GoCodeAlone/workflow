import { test, expect } from '@playwright/test';
import { dragModuleToCanvas, waitForNodeCount, screenshotStep } from './helpers';

test.describe('Deep Module Coverage', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForSelector('.react-flow', { timeout: 15000 });
  });

  // HTTP Category
  test('should drag HTTP Server to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/HTTP Server/)).toBeVisible();
    await expect(node.getByText('http.server')).toBeVisible();
    await screenshotStep(page, 'deep-01-http-server');
  });

  test('should drag HTTP Router to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Router', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/HTTP Router/)).toBeVisible();
    await expect(node.getByText('http.router')).toBeVisible();
  });

  test('should drag HTTP Handler to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Handler', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/HTTP Handler/)).toBeVisible();
    await expect(node.getByText('http.handler')).toBeVisible();
  });

  test('should drag HTTP Proxy to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Proxy', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/HTTP Proxy/)).toBeVisible();
    await expect(node.getByText('http.proxy')).toBeVisible();
  });

  test('should drag API Handler to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'API Handler', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/API Handler/)).toBeVisible();
    await expect(node.getByText('api.handler')).toBeVisible();
  });

  test('should drag Chi Mux Router to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'Chi Mux Router', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/Chi Mux Router/)).toBeVisible();
    await expect(node.getByText('chimux.router')).toBeVisible();
  });

  // Middleware Category
  test('should drag Auth Middleware to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'Auth Middleware', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/Auth Middleware/)).toBeVisible();
    await expect(node.getByText('http.middleware.auth')).toBeVisible();
  });

  test('should drag Logging Middleware to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'Logging Middleware', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/Logging Middleware/)).toBeVisible();
    await expect(node.getByText('http.middleware.logging')).toBeVisible();
  });

  test('should drag Rate Limiter to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'Rate Limiter', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/Rate Limiter/)).toBeVisible();
    await expect(node.getByText('http.middleware.ratelimit')).toBeVisible();
  });

  test('should drag CORS Middleware to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'CORS Middleware', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/CORS Middleware/)).toBeVisible();
    await expect(node.getByText('http.middleware.cors')).toBeVisible();
  });

  test('should drag Request ID Middleware to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'Request ID Middleware', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/Request ID Middleware/)).toBeVisible();
    await expect(node.getByText('http.middleware.requestid')).toBeVisible();
  });

  // Messaging Category
  test('should drag Message Broker to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'Message Broker', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/Message Broker/)).toBeVisible();
    await expect(node.getByText('messaging.broker')).toBeVisible();
  });

  test('should drag Message Handler to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'Message Handler', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/Message Handler/)).toBeVisible();
    await expect(node.getByText('messaging.handler')).toBeVisible();
  });

  // State Machine Category
  test('should drag State Machine to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'State Machine', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText('statemachine.engine')).toBeVisible();
  });

  test('should drag State Tracker to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'State Tracker', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/State Tracker/)).toBeVisible();
    await expect(node.getByText('state.tracker')).toBeVisible();
  });

  test('should drag State Connector to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'State Connector', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/State Connector/)).toBeVisible();
    await expect(node.getByText('state.connector')).toBeVisible();
  });

  // Events Category
  test('should drag Event Logger to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'Event Logger', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/Event Logger/)).toBeVisible();
    await expect(node.getByText('eventlogger.modular')).toBeVisible();
  });

  // Integration Category
  test('should drag HTTP Client to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Client', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/HTTP Client/)).toBeVisible();
    await expect(node.getByText('httpclient.modular')).toBeVisible();
  });

  test('should drag Data Transformer to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'Data Transformer', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/Data Transformer/)).toBeVisible();
    await expect(node.getByText('data.transformer')).toBeVisible();
  });

  test('should drag Webhook Sender to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'Webhook Sender', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/Webhook Sender/)).toBeVisible();
    await expect(node.getByText('webhook.sender')).toBeVisible();
  });

  // Scheduling Category
  test('should drag Scheduler to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'Scheduler', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/Scheduler/)).toBeVisible();
    await expect(node.getByText('scheduler.modular')).toBeVisible();
  });

  // Infrastructure Category
  test('should drag Auth Service to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'Auth Service', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/Auth Service/)).toBeVisible();
    await expect(node.getByText('auth.modular')).toBeVisible();
  });

  test('should drag Event Bus to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'Event Bus', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/Event Bus/)).toBeVisible();
    await expect(node.getByText('eventbus.modular')).toBeVisible();
  });

  test('should drag Cache to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'Cache', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/Cache/)).toBeVisible();
    await expect(node.getByText('cache.modular')).toBeVisible();
  });

  test('should drag Database to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'Database', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText('database.modular')).toBeVisible();
  });

  test('should drag JSON Schema Validator to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'JSON Schema Validator', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/JSON Schema Validator/)).toBeVisible();
    await expect(node.getByText('jsonschema.modular')).toBeVisible();
  });

  // Database Category
  test('should drag Workflow Database to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'Workflow Database', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/Workflow Database/)).toBeVisible();
    await expect(node.getByText('database.workflow')).toBeVisible();
  });

  // Observability Category
  test('should drag Metrics Collector to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'Metrics Collector', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/Metrics Collector/)).toBeVisible();
    await expect(node.getByText('metrics.collector')).toBeVisible();
  });

  test('should drag Health Checker to canvas', async ({ page }) => {
    await dragModuleToCanvas(page, 'Health Checker', 300, 200);
    await waitForNodeCount(page, 1);
    const node = page.locator('.react-flow__node').first();
    await expect(node.getByText(/Health Checker/)).toBeVisible();
    await expect(node.getByText('health.checker')).toBeVisible();
  });

  // Multi-module test
  test('should add modules from every category simultaneously', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 150, 50);
    await waitForNodeCount(page, 1);
    await dragModuleToCanvas(page, 'Auth Middleware', 400, 50);
    await waitForNodeCount(page, 2);
    await dragModuleToCanvas(page, 'Message Broker', 150, 250);
    await waitForNodeCount(page, 3);
    await dragModuleToCanvas(page, 'Event Logger', 400, 250);
    await waitForNodeCount(page, 4);
    await dragModuleToCanvas(page, 'Scheduler', 150, 450);
    await waitForNodeCount(page, 5);

    await expect(page.getByText('5 modules')).toBeVisible();
    await screenshotStep(page, 'deep-02-multi-category');
  });
});
