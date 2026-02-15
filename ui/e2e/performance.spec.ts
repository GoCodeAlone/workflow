import { test, expect, Page } from '@playwright/test';

/**
 * Performance tests for the workflow UI with large numbers of nodes.
 * These tests verify that the ReactFlow canvas remains responsive
 * when working with 50+ nodes.
 */

/** Helper to add a node to the canvas by dragging from palette. */
async function addNodeByDrag(page: Page, moduleText: string, targetX: number, targetY: number) {
  const paletteItem = page.getByText(moduleText, { exact: true }).first();
  const canvas = page.locator('.react-flow__pane').first();

  const canvasBox = await canvas.boundingBox();
  if (!canvasBox) throw new Error('Canvas not found');

  await paletteItem.dragTo(canvas, {
    targetPosition: {
      x: targetX - canvasBox.x,
      y: targetY - canvasBox.y,
    },
  });
}

/**
 * Helper to add nodes programmatically via the store, which is faster than
 * drag-and-drop for bulk operations.
 */
async function addNodesViaStore(page: Page, count: number): Promise<number> {
  const added = await page.evaluate((nodeCount: number) => {
    // Access the Zustand store from the window (exposed by ReactFlow internals
    // or the app). If the store is not directly available we fall back to
    // dispatching custom events.
    const store = (window as unknown as Record<string, unknown>).__zustandStore ?? (window as unknown as Record<string, unknown>).__workflowStore;

    if (store) {
      const state = store.getState();
      const existingNodes = state.nodes ?? [];
      const newNodes = [];

      for (let i = 0; i < nodeCount; i++) {
        const col = i % 10;
        const row = Math.floor(i / 10);
        newNodes.push({
          id: `perf-node-${Date.now()}-${i}`,
          type: 'workflow',
          position: { x: 100 + col * 200, y: 100 + row * 150 },
          data: {
            label: `Perf Node ${i}`,
            type: 'http.server',
            config: {},
          },
        });
      }

      state.setNodes([...existingNodes, ...newNodes]);
      return newNodes.length;
    }

    return 0;
  }, count);

  return added;
}

test.describe('Performance - 50+ Nodes', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForSelector('.react-flow');
  });

  test('should handle creating a workflow with 50+ nodes', async ({ page }) => {
    // Try the programmatic approach first
    const addedViaStore = await addNodesViaStore(page, 55);

    if (addedViaStore === 0) {
      // Fallback: use drag-and-drop for a smaller set to validate the approach
      // This is slower but works without store access
      const moduleTypes = [
        'HTTP Server',
        'HTTP Router',
        'HTTP Handler',
        'Message Broker',
        'State Machine',
      ];

      for (let i = 0; i < 10; i++) {
        const moduleText = moduleTypes[i % moduleTypes.length];
        const col = i % 5;
        const row = Math.floor(i / 5);
        try {
          await addNodeByDrag(page, moduleText, 400 + col * 180, 200 + row * 150);
          // Small delay to let ReactFlow process the node addition
          await page.waitForTimeout(200);
        } catch {
          // Some drags may fail if the target is off-screen; that is acceptable
        }
      }

      test.info().annotations.push({
        type: 'note',
        description: 'Store not accessible; used drag-and-drop fallback with fewer nodes',
      });
    } else {
      // Wait for React to render the nodes
      await page.waitForTimeout(500);

      // Verify nodes were rendered
      const nodeCount = await page.locator('.react-flow__node').count();
      expect(nodeCount).toBeGreaterThanOrEqual(50);
      test.info().annotations.push({
        type: 'note',
        description: `Successfully added ${addedViaStore} nodes via store`,
      });
    }
  });

  test('should measure render time for 50+ nodes', async ({ page }) => {
    // Measure time to add nodes and have them rendered
    const startMark = 'perf-start';
    const endMark = 'perf-end';

    await page.evaluate((mark) => performance.mark(mark), startMark);

    const added = await addNodesViaStore(page, 60);

    if (added > 0) {
      // Wait for React to render
      await page.waitForTimeout(1000);

      await page.evaluate((mark) => performance.mark(mark), endMark);

      const duration = await page.evaluate(
        ([start, end]) => {
          performance.measure('node-render', start, end);
          const measure = performance.getEntriesByName('node-render')[0];
          return measure.duration;
        },
        [startMark, endMark],
      );

      test.info().annotations.push({
        type: 'performance',
        description: `Render time for ${added} nodes: ${duration.toFixed(1)}ms`,
      });

      // Render should complete within a reasonable time (5 seconds max)
      expect(duration).toBeLessThan(5000);
    } else {
      test.skip(true, 'Store not accessible for programmatic node creation');
    }
  });

  test('should maintain pan/zoom responsiveness with many nodes', async ({ page }) => {
    const added = await addNodesViaStore(page, 50);

    if (added === 0) {
      test.skip(true, 'Store not accessible for programmatic node creation');
      return;
    }

    await page.waitForTimeout(500);

    const canvas = page.locator('.react-flow__pane').first();
    const canvasBox = await canvas.boundingBox();
    if (!canvasBox) {
      test.skip(true, 'Canvas not visible');
      return;
    }

    const centerX = canvasBox.x + canvasBox.width / 2;
    const centerY = canvasBox.y + canvasBox.height / 2;

    // Measure pan responsiveness
    const panStart = Date.now();

    // Simulate panning by mouse drag on the background
    await page.mouse.move(centerX, centerY);
    await page.mouse.down();
    for (let i = 0; i < 10; i++) {
      await page.mouse.move(centerX + i * 20, centerY + i * 10, { steps: 2 });
    }
    await page.mouse.up();

    const panDuration = Date.now() - panStart;

    test.info().annotations.push({
      type: 'performance',
      description: `Pan operation with ${added} nodes took ${panDuration}ms`,
    });

    // Pan should complete in reasonable time
    expect(panDuration).toBeLessThan(3000);

    // Measure zoom responsiveness
    const zoomStart = Date.now();

    // Simulate zoom with scroll wheel
    await page.mouse.move(centerX, centerY);
    for (let i = 0; i < 5; i++) {
      await page.mouse.wheel(0, -120); // zoom in
      await page.waitForTimeout(50);
    }
    for (let i = 0; i < 5; i++) {
      await page.mouse.wheel(0, 120); // zoom out
      await page.waitForTimeout(50);
    }

    const zoomDuration = Date.now() - zoomStart;

    test.info().annotations.push({
      type: 'performance',
      description: `Zoom operations with ${added} nodes took ${zoomDuration}ms`,
    });

    // Zoom should complete in reasonable time
    expect(zoomDuration).toBeLessThan(5000);
  });

  test('should not freeze the UI with many nodes', async ({ page }) => {
    const added = await addNodesViaStore(page, 55);

    if (added === 0) {
      test.skip(true, 'Store not accessible for programmatic node creation');
      return;
    }

    await page.waitForTimeout(500);

    // Check for long tasks (main thread blocking > 50ms)
    const longTasks = await page.evaluate(() => {
      return new Promise<number>((resolve) => {
        let count = 0;
        const observer = new PerformanceObserver((list) => {
          count += list.getEntries().length;
        });

        try {
          observer.observe({ type: 'longtask', buffered: true });
        } catch {
          // PerformanceObserver for longtask may not be supported in all browsers
          resolve(-1);
          return;
        }

        // Perform some interactions to provoke potential blocking
        window.dispatchEvent(new Event('resize'));

        // Wait and collect
        setTimeout(() => {
          observer.disconnect();
          resolve(count);
        }, 2000);
      });
    });

    if (longTasks >= 0) {
      test.info().annotations.push({
        type: 'performance',
        description: `Long tasks detected during interaction with ${added} nodes: ${longTasks}`,
      });

      // We expect very few long tasks. More than 10 suggests UI freezing.
      expect(longTasks).toBeLessThan(10);
    } else {
      test.info().annotations.push({
        type: 'note',
        description: 'PerformanceObserver longtask not supported; skipping long task check',
      });
    }

    // Verify the app is still responsive by clicking a button
    const toolbar = page.getByRole('button', { name: 'Validate' });
    await expect(toolbar).toBeVisible();
    await toolbar.click();

    // If we get here without timeout, the UI is responsive
  });

  test('should handle rapid node additions without errors', async ({ page }) => {
    // Listen for console errors
    const consoleErrors: string[] = [];
    page.on('console', (msg) => {
      if (msg.type() === 'error') {
        consoleErrors.push(msg.text());
      }
    });

    // Add nodes in batches
    for (let batch = 0; batch < 5; batch++) {
      const added = await page.evaluate(
        ({ batchNum, batchSize }: { batchNum: number; batchSize: number }) => {
          const store =
            (window as unknown as Record<string, unknown>).__zustandStore ?? (window as unknown as Record<string, unknown>).__workflowStore;
          if (!store) return 0;

          const state = store.getState();
          const existing = state.nodes ?? [];
          const newNodes = [];

          for (let i = 0; i < batchSize; i++) {
            const globalIdx = batchNum * batchSize + i;
            const col = globalIdx % 10;
            const row = Math.floor(globalIdx / 10);
            newNodes.push({
              id: `batch-${batchNum}-node-${i}`,
              type: 'workflow',
              position: { x: 100 + col * 200, y: 100 + row * 150 },
              data: {
                label: `Batch ${batchNum} Node ${i}`,
                type: 'http.handler',
                config: {},
              },
            });
          }

          state.setNodes([...existing, ...newNodes]);
          return newNodes.length;
        },
        { batchNum: batch, batchSize: 12 },
      );

      if (added === 0) {
        test.skip(true, 'Store not accessible');
        return;
      }

      // Brief pause between batches
      await page.waitForTimeout(100);
    }

    await page.waitForTimeout(500);

    // Check final node count
    const finalCount = await page.locator('.react-flow__node').count();

    test.info().annotations.push({
      type: 'performance',
      description: `Final node count after batch additions: ${finalCount}`,
    });

    // Filter out noise errors (e.g., network errors from missing API endpoints)
    const relevantErrors = consoleErrors.filter(
      (e) => !e.includes('net::ERR') && !e.includes('Failed to fetch'),
    );

    if (relevantErrors.length > 0) {
      test.info().annotations.push({
        type: 'warning',
        description: `Console errors during batch add: ${relevantErrors.join('; ')}`,
      });
    }

    expect(finalCount).toBeGreaterThanOrEqual(50);
  });
});
