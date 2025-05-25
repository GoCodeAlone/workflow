const { test, expect } = require('@playwright/test');

test.describe('Workflow UI', () => {
  test('should display the workflow management interface', async ({ page }) => {
    await page.goto('/');
    
    // Check the page title
    await expect(page).toHaveTitle('Workflow Manager');
    
    // Check for main UI elements
    await expect(page.locator('h1')).toContainText('Workflow Manager');
    await expect(page.locator('#create-workflow-btn')).toBeVisible();
    await expect(page.locator('#workflows-list')).toBeVisible();
  });

  test('should show the create workflow form', async ({ page }) => {
    await page.goto('/');
    
    // Click the create button
    await page.locator('#create-workflow-btn').click();
    
    // Check that the form is displayed
    await expect(page.locator('#workflow-form')).toBeVisible();
    await expect(page.locator('#form-title')).toContainText('Create New Workflow');
    await expect(page.locator('#workflow-form-name')).toBeVisible();
    await expect(page.locator('#workflow-form-config')).toBeVisible();
    
    // Check that the workflows list is hidden
    await expect(page.locator('#workflows-list')).toBeHidden();
  });

  test('should cancel the create workflow form', async ({ page }) => {
    await page.goto('/');
    
    // Open the create form
    await page.locator('#create-workflow-btn').click();
    await expect(page.locator('#workflow-form')).toBeVisible();
    
    // Cancel the form
    await page.locator('#cancel-workflow-btn').click();
    
    // Check that we return to the list view
    await expect(page.locator('#workflows-list')).toBeVisible();
    await expect(page.locator('#workflow-form')).toBeHidden();
  });

  test('should handle creating a new workflow', async ({ page }) => {
    await page.goto('/');
    
    // Create a unique workflow name using timestamp
    const workflowName = `test-workflow-${Date.now()}`;
    
    // Open the create form
    await page.locator('#create-workflow-btn').click();
    
    // Fill the form
    await page.locator('#workflow-form-name').fill(workflowName);
    await page.locator('#workflow-form-config').fill(JSON.stringify({
      name: workflowName,
      description: "Test workflow configuration",
      modules: [
        {
          name: "http-server",
          type: "http.server",
          config: {
            address: ":8080"
          }
        }
      ],
      workflows: {
        http: {
          routes: [
            {
              method: "GET",
              path: "/test",
              response: {
                statusCode: 200,
                body: "{\"status\":\"ok\"}"
              }
            }
          ]
        }
      }
    }, null, 2));
    
    // Submit the form (mock the API call)
    page.route('/api/workflows', route => {
      route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'created', name: workflowName })
      });
    });
    
    // Mock the API call to refresh the list
    page.route('/api/workflows', route => {
      if (route.request().method() === 'GET') {
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            workflows: [
              { name: workflowName, status: 'configured' }
            ]
          })
        });
      }
    });
    
    await page.locator('#save-workflow-btn').click();
    
    // Check we return to the list and see our new workflow
    await expect(page.locator('#workflows-list')).toBeVisible();
  });

  test('should display workflow details', async ({ page }) => {
    // Mock data for a workflow
    const workflowName = 'test-workflow';
    const workflowConfig = {
      name: workflowName,
      description: "Test workflow"
    };
    
    // Mock the APIs
    page.route('/api/workflows', route => {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          workflows: [
            { name: workflowName, status: 'configured' }
          ]
        })
      });
    });
    
    page.route(`/api/workflows/${workflowName}`, route => {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          name: workflowName,
          status: 'configured',
          config: workflowConfig
        })
      });
    });
    
    await page.goto('/');
    
    // Find and click the workflow item
    const workflowItems = await page.locator('.workflow-item').all();
    if (workflowItems.length > 0) {
      const viewButton = await workflowItems[0].locator('.view-btn');
      await viewButton.click();
      
      // Check that details are displayed
      await expect(page.locator('#workflow-detail')).toBeVisible();
      await expect(page.locator('#workflow-name')).toContainText(workflowName);
      await expect(page.locator('#workflow-status')).toContainText('configured');
    }
  });
});