package bdd

import (
	"encoding/json"
	"fmt"

	"github.com/GoCodeAlone/workflow/wftest"
	"github.com/cucumber/godog"
)

// registerMockSteps registers step definitions for mocking pipeline steps.
func registerMockSteps(ctx *godog.ScenarioContext, sc *ScenarioContext) {
	ctx.Step(`^step "([^"]*)" is mocked to return:$`, sc.stepIsMockedToReturnTable)
	ctx.Step(`^step "([^"]*)" returns JSON:$`, sc.stepReturnsJSON)
	ctx.Step(`^module "([^"]*)" "([^"]*)" is mocked$`, sc.moduleIsMocked)
}

// stepIsMockedToReturnTable mocks a step to return values from a two-column table.
// The table should have exactly two columns: key and value.
func (sc *ScenarioContext) stepIsMockedToReturnTable(stepType string, table *godog.Table) error {
	output := make(map[string]any)
	for _, row := range table.Rows {
		if len(row.Cells) != 2 {
			return fmt.Errorf("mock table must have exactly 2 columns (key, value), got %d", len(row.Cells))
		}
		output[row.Cells[0].Value] = row.Cells[1].Value
	}
	sc.pendingOpts = append(sc.pendingOpts, wftest.MockStep(stepType, wftest.Returns(output)))
	return nil
}

// stepReturnsJSON mocks a step to return the given JSON docstring as its output.
func (sc *ScenarioContext) stepReturnsJSON(stepType string, doc *godog.DocString) error {
	var output map[string]any
	if err := json.Unmarshal([]byte(doc.Content), &output); err != nil {
		return fmt.Errorf("step %q returns JSON: invalid JSON: %w", stepType, err)
	}
	sc.pendingOpts = append(sc.pendingOpts, wftest.MockStep(stepType, wftest.Returns(output)))
	return nil
}

// moduleIsMocked registers a bare mock module under the given module type and name.
// This is useful when a step looks up a named dependency in the service registry.
func (sc *ScenarioContext) moduleIsMocked(moduleType, name string) error {
	sc.pendingOpts = append(sc.pendingOpts, wftest.WithMockModule(wftest.NewMockModule(name, struct{}{})))
	_ = moduleType // moduleType is informational; name is the registry key
	return nil
}
