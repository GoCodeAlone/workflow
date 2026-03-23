package bdd

import (
	"fmt"

	"github.com/GoCodeAlone/workflow/wftest"
	"github.com/cucumber/godog"
)

// registerHTTPSteps registers "When" steps for HTTP trigger testing.
func registerHTTPSteps(ctx *godog.ScenarioContext, sc *ScenarioContext) {
	ctx.Step(`^I POST "([^"]*)" with JSON:$`, sc.iPOSTWithJSON)
	ctx.Step(`^I GET "([^"]*)"$`, sc.iGET)
	ctx.Step(`^I GET "([^"]*)" with header "([^"]*)" = "([^"]*)"$`, sc.iGETWithHeader)
	ctx.Step(`^I PUT "([^"]*)" with JSON:$`, sc.iPUTWithJSON)
	ctx.Step(`^I DELETE "([^"]*)"$`, sc.iDELETE)
	ctx.Step(`^I POST "([^"]*)" with:$`, sc.iPOSTWithTable)
}

// iPOSTWithJSON sends a POST request with a JSON docstring body.
func (sc *ScenarioContext) iPOSTWithJSON(path string, doc *godog.DocString) error {
	if err := sc.ensureHarness(); err != nil {
		return err
	}
	sc.result = sc.harness.POST(path, doc.Content)
	return nil
}

// iGET sends a GET request to the given path.
func (sc *ScenarioContext) iGET(path string) error {
	if err := sc.ensureHarness(); err != nil {
		return err
	}
	sc.result = sc.harness.GET(path)
	return nil
}

// iGETWithHeader sends a GET request with a custom request header.
func (sc *ScenarioContext) iGETWithHeader(path, header, value string) error {
	if err := sc.ensureHarness(); err != nil {
		return err
	}
	sc.result = sc.harness.GET(path, wftest.Header(header, value))
	return nil
}

// iPUTWithJSON sends a PUT request with a JSON docstring body.
func (sc *ScenarioContext) iPUTWithJSON(path string, doc *godog.DocString) error {
	if err := sc.ensureHarness(); err != nil {
		return err
	}
	sc.result = sc.harness.PUT(path, doc.Content)
	return nil
}

// iDELETE sends a DELETE request to the given path.
func (sc *ScenarioContext) iDELETE(path string) error {
	if err := sc.ensureHarness(); err != nil {
		return err
	}
	sc.result = sc.harness.DELETE(path)
	return nil
}

// iPOSTWithTable sends a POST request with form-like key/value pairs from a table.
func (sc *ScenarioContext) iPOSTWithTable(path string, table *godog.Table) error {
	if err := sc.ensureHarness(); err != nil {
		return err
	}
	data, err := tableToMap(table)
	if err != nil {
		return fmt.Errorf("POST %q: %w", path, err)
	}
	body, err := mapToJSON(data)
	if err != nil {
		return fmt.Errorf("POST %q: %w", path, err)
	}
	sc.result = sc.harness.POST(path, body)
	return nil
}
