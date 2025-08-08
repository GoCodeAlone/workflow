package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"path/filepath"

	"github.com/cucumber/godog"
	"github.com/google/uuid"
	"github.com/GoCodeAlone/workflow/ui"
)

// Workflow management step definitions

func (ctx *BDDTestContext) iCreateAWorkflowWith(table *godog.Table) error {
	workflow := &ui.CreateWorkflowRequest{}

	for _, row := range table.Rows {
		switch row.Cells[0].Value {
		case "name":
			workflow.Name = row.Cells[1].Value
		case "description":
			workflow.Description = row.Cells[1].Value
		case "config":
			workflow.Config = row.Cells[1].Value
		case "config_file":
			// Load config from file
			configPath := filepath.Join("config_scenarios", row.Cells[1].Value)
			configBytes, err := ioutil.ReadFile(configPath)
			if err != nil {
				return fmt.Errorf("failed to read config file %s: %v", configPath, err)
			}
			workflow.Config = string(configBytes)
		}
	}

	return ctx.makeRequest("POST", "/api/workflows", ctx.authToken, workflow)
}

func (ctx *BDDTestContext) iCreateAWorkflowNamed(workflowName string, config string) error {
	workflow := &ui.CreateWorkflowRequest{
		Name:        workflowName,
		Description: fmt.Sprintf("Test workflow: %s", workflowName),
		Config:      config,
	}

	return ctx.makeRequest("POST", "/api/workflows", ctx.authToken, workflow)
}

func (ctx *BDDTestContext) theWorkflowShouldBeCreatedSuccessfully() error {
	if ctx.lastResponse == nil {
		return fmt.Errorf("no response received")
	}

	if ctx.lastResponse.StatusCode != http.StatusCreated {
		return fmt.Errorf("expected 201 Created, got %d: %s", ctx.lastResponse.StatusCode, ctx.lastBody)
	}

	var workflow ui.StoredWorkflow
	if err := json.Unmarshal(ctx.lastBody, &workflow); err != nil {
		return err
	}

	ctx.workflows[workflow.Name] = &workflow
	return nil
}

func (ctx *BDDTestContext) iShouldBeAbleToRetrieveTheWorkflow() error {
	for _, workflow := range ctx.workflows {
		err := ctx.makeRequest("GET", fmt.Sprintf("/api/workflows/%s", workflow.ID), ctx.authToken, nil)
		if err != nil {
			return err
		}

		if ctx.lastResponse.StatusCode != http.StatusOK {
			return fmt.Errorf("failed to retrieve workflow: %d", ctx.lastResponse.StatusCode)
		}
		break
	}
	return nil
}

func (ctx *BDDTestContext) thereAreExistingWorkflows() error {
	// Create a test workflow
	workflow := &ui.CreateWorkflowRequest{
		Name:        "Test Workflow " + uuid.New().String()[:8],
		Description: "A test workflow",
		Config:      "modules:\n  - name: test\n    type: http.server",
	}

	return ctx.makeRequest("POST", "/api/workflows", ctx.authToken, workflow)
}

func (ctx *BDDTestContext) iRequestTheListOfWorkflows() error {
	return ctx.makeRequest("GET", "/api/workflows", ctx.authToken, nil)
}

func (ctx *BDDTestContext) iShouldReceiveAllWorkflowsForMyTenant() error {
	if ctx.lastResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("expected 200 OK, got %d", ctx.lastResponse.StatusCode)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(ctx.lastBody, &response); err != nil {
		return err
	}

	workflows, ok := response["workflows"].([]interface{})
	if !ok {
		return fmt.Errorf("workflows not found in response")
	}

	if len(workflows) == 0 {
		return fmt.Errorf("no workflows returned")
	}

	return nil
}

func (ctx *BDDTestContext) eachWorkflowShouldHaveFields() error {
	var response map[string]interface{}
	if err := json.Unmarshal(ctx.lastBody, &response); err != nil {
		return err
	}

	workflows, ok := response["workflows"].([]interface{})
	if !ok {
		return fmt.Errorf("workflows not found in response")
	}

	for _, w := range workflows {
		workflow := w.(map[string]interface{})
		requiredFields := []string{"id", "name", "description", "status"}
		for _, field := range requiredFields {
			if _, exists := workflow[field]; !exists {
				return fmt.Errorf("workflow missing required field: %s", field)
			}
		}
	}

	return nil
}

// Additional workflow step definitions that were placeholders

func (ctx *BDDTestContext) thereIsAWorkflowNamed(name string) error {
	// Simulate existing workflow
	workflow := &ui.StoredWorkflow{
		ID:   uuid.New(),
		Name: name,
	}
	ctx.workflows[name] = workflow
	return nil
}

func (ctx *BDDTestContext) iUpdateTheWorkflowWith(table *godog.Table) error {
	return nil // Placeholder - to be implemented
}

func (ctx *BDDTestContext) theWorkflowShouldBeUpdatedSuccessfully() error {
	return nil // Placeholder - to be implemented
}

func (ctx *BDDTestContext) theChangesShouldBeReflectedInTheWorkflowDetails() error {
	return nil // Placeholder - to be implemented
}

func (ctx *BDDTestContext) iExecuteTheWorkflowWithInputData() error {
	return nil // Placeholder - to be implemented
}

func (ctx *BDDTestContext) aWorkflowExecutionShouldBeCreated() error {
	return nil // Placeholder - to be implemented
}

func (ctx *BDDTestContext) theExecutionShouldHaveStatus(status string) error {
	return nil // Placeholder - to be implemented
}

func (ctx *BDDTestContext) thereIsAWorkflowWithExecutions() error {
	return nil // Placeholder - to be implemented
}

func (ctx *BDDTestContext) iRequestTheExecutionsForTheWorkflow() error {
	return nil // Placeholder - to be implemented
}

func (ctx *BDDTestContext) iShouldReceiveAllExecutionsForThatWorkflow() error {
	return nil // Placeholder - to be implemented
}

func (ctx *BDDTestContext) eachExecutionShouldHaveRequiredFields() error {
	return nil // Placeholder - to be implemented
}

func (ctx *BDDTestContext) iDeleteTheWorkflow() error {
	return nil // Placeholder - to be implemented
}

func (ctx *BDDTestContext) theWorkflowShouldBeMarkedAsInactive() error {
	return nil // Placeholder - to be implemented
}

func (ctx *BDDTestContext) itShouldNotAppearInTheActiveWorkflowsList() error {
	return nil // Placeholder - to be implemented
}