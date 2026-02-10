import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';

/**
 * Complete mapping of palette label -> module type for all 31 module types.
 */
export const COMPLETE_MODULE_TYPE_MAP: Record<string, string> = {
  'HTTP Server': 'http.server',
  'HTTP Router': 'http.router',
  'HTTP Handler': 'http.handler',
  'HTTP Proxy': 'http.proxy',
  'API Handler': 'api.handler',
  'Auth Middleware': 'http.middleware.auth',
  'Logging Middleware': 'http.middleware.logging',
  'Rate Limiter': 'http.middleware.ratelimit',
  'CORS Middleware': 'http.middleware.cors',
  'Message Broker': 'messaging.broker',
  'Message Handler': 'messaging.handler',
  'EventBus Bridge': 'messaging.broker.eventbus',
  'State Machine': 'statemachine.engine',
  'State Tracker': 'state.tracker',
  'State Connector': 'state.connector',
  'Scheduler': 'scheduler.modular',
  'Auth Service': 'auth.modular',
  'Event Bus': 'eventbus.modular',
  'Cache': 'cache.modular',
  'Chi Mux Router': 'chimux.router',
  'Event Logger': 'eventlogger.modular',
  'HTTP Client': 'httpclient.modular',
  'Database': 'database.modular',
  'JSON Schema Validator': 'jsonschema.modular',
  'Workflow Database': 'database.workflow',
  'Metrics Collector': 'metrics.collector',
  'Health Checker': 'health.checker',
  'Request ID Middleware': 'http.middleware.requestid',
  'Data Transformer': 'data.transformer',
  'Webhook Sender': 'webhook.sender',
};

/**
 * Simulate dragging a palette item to the canvas via DataTransfer API.
 */
export async function dragModuleToCanvas(
  page: Page,
  moduleLabel: string,
  canvasX: number,
  canvasY: number,
) {
  const paletteItem = page.getByText(moduleLabel, { exact: true }).first();
  await paletteItem.scrollIntoViewIfNeeded();
  await expect(paletteItem).toBeVisible({ timeout: 5000 });
  await page.waitForTimeout(200);

  const sourceBounds = await paletteItem.boundingBox();
  if (!sourceBounds) throw new Error(`Could not find palette item: ${moduleLabel}`);

  const canvas = page.locator('.react-flow').first();
  const canvasBounds = await canvas.boundingBox();
  if (!canvasBounds) throw new Error('Could not find canvas');

  const dropX = canvasBounds.x + canvasX;
  const dropY = canvasBounds.y + canvasY;

  const sourceX = sourceBounds.x + sourceBounds.width / 2;
  const sourceY = sourceBounds.y + sourceBounds.height / 2;

  await page.evaluate(
    ({ srcX, srcY, dstX, dstY, label, moduleTypeMap }) => {
      const source = document.elementFromPoint(srcX, srcY) as HTMLElement;
      const target = document.elementFromPoint(dstX, dstY) as HTMLElement;
      if (!source || !target) return;

      let draggable = source;
      while (draggable && draggable.getAttribute('draggable') !== 'true') {
        draggable = draggable.parentElement as HTMLElement;
      }

      const modType = moduleTypeMap[label];
      if (!modType) return;

      const dt = new DataTransfer();
      dt.setData('application/workflow-module-type', modType);

      const dragStartEvent = new DragEvent('dragstart', {
        bubbles: true, cancelable: true, dataTransfer: dt, clientX: srcX, clientY: srcY,
      });
      (draggable || source).dispatchEvent(dragStartEvent);

      const dragOverEvent = new DragEvent('dragover', {
        bubbles: true, cancelable: true, dataTransfer: dt, clientX: dstX, clientY: dstY,
      });
      target.dispatchEvent(dragOverEvent);

      const dropEvent = new DragEvent('drop', {
        bubbles: true, cancelable: true, dataTransfer: dt, clientX: dstX, clientY: dstY,
      });
      target.dispatchEvent(dropEvent);
    },
    { srcX: sourceX, srcY: sourceY, dstX: dropX, dstY: dropY, label: moduleLabel, moduleTypeMap: COMPLETE_MODULE_TYPE_MAP },
  );

  await page.waitForTimeout(500);
}

/**
 * Connect two nodes by mouse-dragging from source bottom handle to target top handle.
 * Uses [data-handlepos] attributes as a fallback for handle detection.
 */
export async function connectNodes(
  page: Page,
  sourceNodeIndex: number,
  targetNodeIndex: number,
) {
  const sourceNode = page.locator('.react-flow__node').nth(sourceNodeIndex);
  const targetNode = page.locator('.react-flow__node').nth(targetNodeIndex);

  // Use data-handlepos attribute for more reliable handle detection
  const sourceHandle = sourceNode.locator('.react-flow__handle[data-handlepos="bottom"]').first();
  const targetHandle = targetNode.locator('.react-flow__handle[data-handlepos="top"]').first();

  // Fall back to class-based selectors if data attributes aren't present
  const srcHandleFallback = sourceNode.locator('.react-flow__handle-bottom').first();
  const tgtHandleFallback = targetNode.locator('.react-flow__handle-top').first();

  // Try primary selector, fall back to class-based
  let srcBox = await sourceHandle.boundingBox().catch(() => null);
  if (!srcBox) {
    await srcHandleFallback.waitFor({ state: 'attached', timeout: 5000 });
    srcBox = await srcHandleFallback.boundingBox();
  }

  let tgtBox = await targetHandle.boundingBox().catch(() => null);
  if (!tgtBox) {
    await tgtHandleFallback.waitFor({ state: 'attached', timeout: 5000 });
    tgtBox = await tgtHandleFallback.boundingBox();
  }

  if (!srcBox || !tgtBox) throw new Error('Handle bounding boxes not found');

  const srcX = srcBox.x + srcBox.width / 2;
  const srcY = srcBox.y + srcBox.height / 2;
  const tgtX = tgtBox.x + tgtBox.width / 2;
  const tgtY = tgtBox.y + tgtBox.height / 2;

  await page.mouse.move(srcX, srcY);
  await page.waitForTimeout(100);
  await page.mouse.down();
  await page.waitForTimeout(100);
  const steps = 20;
  for (let i = 1; i <= steps; i++) {
    await page.mouse.move(
      srcX + (tgtX - srcX) * (i / steps),
      srcY + (tgtY - srcY) * (i / steps),
    );
    await page.waitForTimeout(20);
  }
  await page.waitForTimeout(100);
  await page.mouse.up();
  await page.waitForTimeout(500);
}

/**
 * Wait until the canvas has the expected number of nodes.
 */
export async function waitForNodeCount(page: Page, count: number) {
  await expect(page.locator('.react-flow__node')).toHaveCount(count, { timeout: 5000 });
}

/**
 * Take a screenshot with a standardized path.
 */
export async function screenshotStep(page: Page, name: string) {
  await page.screenshot({ path: `e2e/screenshots/${name}.png`, fullPage: true });
}
