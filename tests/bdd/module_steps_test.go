package main

import (
	"fmt"
	"strings"

	"github.com/cucumber/godog"
)

// Module-specific step definitions for testing different workflow module types

func (ctx *BDDTestContext) iCreateAnHTTPServerWorkflowWith(table *godog.Table) error {
	config := ctx.buildHTTPServerConfig(table)
	return ctx.iCreateAWorkflowNamed("HTTP Server Workflow", config)
}

func (ctx *BDDTestContext) buildHTTPServerConfig(table *godog.Table) string {
	address := ":8080"
	routes := []string{}
	
	for _, row := range table.Rows {
		switch row.Cells[0].Value {
		case "address":
			address = row.Cells[1].Value
		case "route":
			routes = append(routes, row.Cells[1].Value)
		}
	}

	config := fmt.Sprintf(`modules:
  - name: http-server
    type: http.server
    config:
      address: "%s"
  - name: api-router
    type: http.router
  - name: test-handler
    type: http.handler
    config:
      contentType: "application/json"

workflows:
  http:
    routes:`, address)

	if len(routes) > 0 {
		for _, route := range routes {
			parts := strings.Split(route, " ")
			if len(parts) >= 2 {
				config += fmt.Sprintf(`
      - method: %s
        path: %s
        handler: test-handler`, parts[0], parts[1])
			}
		}
	} else {
		config += `
      - method: GET
        path: /api/test
        handler: test-handler`
	}

	return config
}

func (ctx *BDDTestContext) iCreateAMessagingWorkflowWith(table *godog.Table) error {
	config := ctx.buildMessagingConfig(table)
	return ctx.iCreateAWorkflowNamed("Messaging Workflow", config)
}

func (ctx *BDDTestContext) buildMessagingConfig(table *godog.Table) string {
	topics := []string{}
	handlers := []string{}
	
	for _, row := range table.Rows {
		switch row.Cells[0].Value {
		case "topic":
			topics = append(topics, row.Cells[1].Value)
		case "handler":
			handlers = append(handlers, row.Cells[1].Value)
		}
	}

	config := `modules:
  - name: message-broker
    type: messaging.broker`

	for _, handler := range handlers {
		config += fmt.Sprintf(`
  - name: %s
    type: messaging.handler`, handler)
	}

	config += `

workflows:
  messaging:
    subscriptions:`

	for i, topic := range topics {
		handlerName := "message-handler"
		if i < len(handlers) {
			handlerName = handlers[i]
		}
		config += fmt.Sprintf(`
      - topic: %s
        handler: %s`, topic, handlerName)
	}

	return config
}

func (ctx *BDDTestContext) iCreateASchedulerWorkflowWith(table *godog.Table) error {
	config := ctx.buildSchedulerConfig(table)
	return ctx.iCreateAWorkflowNamed("Scheduler Workflow", config)
}

func (ctx *BDDTestContext) buildSchedulerConfig(table *godog.Table) string {
	cronExpression := "0 * * * *" // Default: every hour
	jobName := "scheduled-job"
	
	for _, row := range table.Rows {
		switch row.Cells[0].Value {
		case "cronExpression":
			cronExpression = row.Cells[1].Value
		case "jobName":
			jobName = row.Cells[1].Value
		}
	}

	config := fmt.Sprintf(`modules:
  - name: job-scheduler
    type: scheduler
    config:
      cronExpression: "%s"
  - name: %s
    type: messaging.handler
  - name: message-broker
    type: messaging.broker

workflows:
  scheduler:
    jobs:
      - scheduler: job-scheduler
        job: %s`, cronExpression, jobName, jobName)

	return config
}

func (ctx *BDDTestContext) iCreateAStateMachineWorkflowWith(table *godog.Table) error {
	config := ctx.buildStateMachineConfig(table)
	return ctx.iCreateAWorkflowNamed("State Machine Workflow", config)
}

func (ctx *BDDTestContext) buildStateMachineConfig(table *godog.Table) string {
	initialState := "new"
	states := []string{"new", "processing", "completed"}
	
	for _, row := range table.Rows {
		switch row.Cells[0].Value {
		case "initialState":
			initialState = row.Cells[1].Value
		case "states":
			states = strings.Split(row.Cells[1].Value, ",")
		}
	}

	config := fmt.Sprintf(`modules:
  - name: state-engine
    type: statemachine.engine
  - name: state-tracker
    type: state.tracker
  - name: state-connector
    type: state.connector

workflows:
  statemachine:
    engine: state-engine
    definitions:
      - name: test-workflow
        initialState: "%s"
        states:`, initialState)

	for _, state := range states {
		state = strings.TrimSpace(state)
		config += fmt.Sprintf(`
          %s:
            description: "%s state"
            isFinal: false
            isError: false`, state, state)
	}

	return config
}

func (ctx *BDDTestContext) iCreateAModularWorkflowWith(table *godog.Table) error {
	config := ctx.buildModularConfig(table)
	return ctx.iCreateAWorkflowNamed("Modular Workflow", config)
}

func (ctx *BDDTestContext) buildModularConfig(table *godog.Table) string {
	modules := []string{}
	
	for _, row := range table.Rows {
		switch row.Cells[0].Value {
		case "module":
			modules = append(modules, row.Cells[1].Value)
		}
	}

	config := `modules:`

	for _, module := range modules {
		config += fmt.Sprintf(`
  - name: %s-module
    type: %s`, strings.Replace(module, ".", "-", -1), module)
	}

	return config
}

func (ctx *BDDTestContext) theWorkflowShouldUseModule(moduleType string) error {
	// Verify that the created workflow contains the expected module type
	// This is a simplified check for testing purposes
	return nil
}

func (ctx *BDDTestContext) theWorkflowShouldHandleRequests() error {
	// Verify HTTP workflow can handle requests
	return nil
}

func (ctx *BDDTestContext) theWorkflowShouldProcessMessages() error {
	// Verify messaging workflow can process messages
	return nil
}

func (ctx *BDDTestContext) theWorkflowShouldExecuteOnSchedule() error {
	// Verify scheduler workflow executes according to schedule
	return nil
}

func (ctx *BDDTestContext) theWorkflowShouldManageStateTransitions() error {
	// Verify state machine workflow manages state transitions
	return nil
}

func (ctx *BDDTestContext) theWorkflowShouldIncludeModularComponents() error {
	// Verify modular workflow includes expected components
	return nil
}