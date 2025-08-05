package main

import (
	"context"
	"testing"

	"github.com/cucumber/godog"
)

// InitializeScenario sets up all BDD step definitions
func InitializeScenario(ctx *godog.ScenarioContext) {
	testCtx := NewBDDTestContext()

	// Authentication scenarios
	ctx.Given(`^the workflow UI is running$`, testCtx.theWorkflowUIIsRunning)
	ctx.Given(`^there is a default tenant "([^"]*)"$`, testCtx.thereIsADefaultTenant)
	ctx.Given(`^there is an admin user "([^"]*)" with password "([^"]*)"$`, testCtx.thereIsAnAdminUser)
	ctx.Given(`^there is a tenant named "([^"]*)"$`, testCtx.thereIsATenantNamed)
	ctx.Given(`^there is a user "([^"]*)" in tenant "([^"]*)"$`, testCtx.thereIsAUserInTenant)
	ctx.When(`^I login with username "([^"]*)" and password "([^"]*)"$`, testCtx.iLoginWithUsernameAndPassword)
	ctx.When(`^I login as "([^"]*)" in tenant "([^"]*)"$`, testCtx.iLoginAsTenant)
	ctx.Then(`^I should receive a valid JWT token$`, testCtx.iShouldReceiveAValidJWTToken)
	ctx.Then(`^I should see my user information$`, testCtx.iShouldSeeMyUserInformation)
	ctx.Then(`^I should see my tenant information$`, testCtx.iShouldSeeMyTenantInformation)
	ctx.Then(`^I should receive an authentication error$`, testCtx.iShouldReceiveAnAuthenticationError)
	ctx.Then(`^I should not receive a token$`, testCtx.iShouldNotReceiveAToken)
	ctx.When(`^I try to access "([^"]*)" without a token$`, testCtx.iTryToAccessWithoutAToken)
	ctx.Then(`^I should receive an unauthorized error$`, testCtx.iShouldReceiveAnUnauthorizedError)
	ctx.Given(`^I am logged in as "([^"]*)"$`, testCtx.iAmLoggedInAs)
	ctx.When(`^I access "([^"]*)" with my token$`, testCtx.iAccessWithMyToken)
	ctx.Then(`^I should receive a successful response$`, testCtx.iShouldReceiveASuccessfulResponse)

	// Basic workflow management scenarios
	ctx.Given(`^I am logged in as an admin user$`, func() error { return testCtx.iAmLoggedInAs("admin") })
	ctx.When(`^I create a workflow with:$`, testCtx.iCreateAWorkflowWith)
	ctx.Then(`^the workflow should be created successfully$`, testCtx.theWorkflowShouldBeCreatedSuccessfully)
	ctx.Then(`^I should be able to retrieve the workflow$`, testCtx.iShouldBeAbleToRetrieveTheWorkflow)
	ctx.Given(`^there are existing workflows$`, testCtx.thereAreExistingWorkflows)
	ctx.When(`^I request the list of workflows$`, testCtx.iRequestTheListOfWorkflows)
	ctx.Then(`^I should receive all workflows for my tenant$`, testCtx.iShouldReceiveAllWorkflowsForMyTenant)
	ctx.Then(`^each workflow should have id, name, description, and status$`, testCtx.eachWorkflowShouldHaveFields)

	// Module-specific workflow scenarios
	ctx.When(`^I create an HTTP server workflow with:$`, testCtx.iCreateAnHTTPServerWorkflowWith)
	ctx.When(`^I create a messaging workflow with:$`, testCtx.iCreateAMessagingWorkflowWith)
	ctx.When(`^I create a scheduler workflow with:$`, testCtx.iCreateASchedulerWorkflowWith)
	ctx.When(`^I create a state machine workflow with:$`, testCtx.iCreateAStateMachineWorkflowWith)
	ctx.When(`^I create a modular workflow with:$`, testCtx.iCreateAModularWorkflowWith)
	ctx.Then(`^the workflow should use "([^"]*)" module$`, testCtx.theWorkflowShouldUseModule)
	ctx.Then(`^the workflow should handle HTTP requests$`, testCtx.theWorkflowShouldHandleRequests)
	ctx.Then(`^the workflow should process messages$`, testCtx.theWorkflowShouldProcessMessages)
	ctx.Then(`^the workflow should execute on schedule$`, testCtx.theWorkflowShouldExecuteOnSchedule)
	ctx.Then(`^the workflow should manage state transitions$`, testCtx.theWorkflowShouldManageStateTransitions)
	ctx.Then(`^the workflow should include modular components$`, testCtx.theWorkflowShouldIncludeModularComponents)

	// Multi-tenancy scenarios
	ctx.Given(`^there are tenants: "([^"]*)"$`, testCtx.thereAreTenants)
	ctx.When(`^I switch to tenant "([^"]*)"$`, testCtx.iSwitchToTenant)
	ctx.When(`^I create (\d+) "([^"]*)" workflows in tenant "([^"]*)"$`, testCtx.iCreateWorkflowsInTenant)
	ctx.Then(`^I should only see workflows for my tenant$`, testCtx.iShouldOnlySeeWorkflowsForMyTenant)
	ctx.Then(`^I should not see workflows from other tenants$`, testCtx.iShouldNotSeeWorkflowsFromOtherTenants)
	ctx.Then(`^each tenant should have isolated workflows$`, testCtx.eachTenantShouldHaveIsolatedWorkflows)
	ctx.Then(`^I can access my tenant's workflows$`, testCtx.iCanAccessMyTenantsWorkflows)
	ctx.Then(`^I cannot access other tenants' workflows$`, testCtx.iCannotAccessOtherTenantsWorkflows)
	ctx.Then(`^tenant "([^"]*)" should have (\d+) workflows$`, testCtx.tenantShouldHaveWorkflows)

	// Legacy workflow step definitions
	ctx.Given(`^there is a workflow named "([^"]*)"$`, testCtx.thereIsAWorkflowNamed)
	ctx.When(`^I update the workflow with:$`, testCtx.iUpdateTheWorkflowWith)
	ctx.Then(`^the workflow should be updated successfully$`, testCtx.theWorkflowShouldBeUpdatedSuccessfully)
	ctx.Then(`^the changes should be reflected in the workflow details$`, testCtx.theChangesShouldBeReflectedInTheWorkflowDetails)
	ctx.When(`^I execute the workflow with input data$`, testCtx.iExecuteTheWorkflowWithInputData)
	ctx.Then(`^a workflow execution should be created$`, testCtx.aWorkflowExecutionShouldBeCreated)
	ctx.Then(`^the execution should have status "([^"]*)" initially$`, testCtx.theExecutionShouldHaveStatus)
	ctx.Given(`^there is a workflow with executions$`, testCtx.thereIsAWorkflowWithExecutions)
	ctx.When(`^I request the executions for the workflow$`, testCtx.iRequestTheExecutionsForTheWorkflow)
	ctx.Then(`^I should receive all executions for that workflow$`, testCtx.iShouldReceiveAllExecutionsForThatWorkflow)
	ctx.Then(`^each execution should have id, status, start time, and logs$`, testCtx.eachExecutionShouldHaveRequiredFields)
	ctx.When(`^I delete the workflow$`, testCtx.iDeleteTheWorkflow)
	ctx.Then(`^the workflow should be marked as inactive$`, testCtx.theWorkflowShouldBeMarkedAsInactive)
	ctx.Then(`^it should not appear in the active workflows list$`, testCtx.itShouldNotAppearInTheActiveWorkflowsList)

	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		testCtx.cleanup()
		return ctx, nil
	})
}

func TestFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: InitializeScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features"},
			TestingT: t,
		},
	}

	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run feature tests")
	}
}