package bdd

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/GoCodeAlone/workflow/wftest"
	"github.com/cucumber/godog"
)

// registerAssertSteps registers "Then" steps for asserting on results.
func registerAssertSteps(ctx *godog.ScenarioContext, sc *ScenarioContext) {
	// Pipeline assertions
	ctx.Step(`^the pipeline should succeed$`, sc.thePipelineShouldSucceed)
	ctx.Step(`^the pipeline should fail$`, sc.thePipelineShouldFail)
	ctx.Step(`^the pipeline output "([^"]*)" should be "([^"]*)"$`, sc.thePipelineOutputShouldBe)

	// Step execution assertions
	ctx.Step(`^step "([^"]*)" should have been executed$`, sc.stepShouldHaveBeenExecuted)
	ctx.Step(`^step "([^"]*)" should not have been executed$`, sc.stepShouldNotHaveBeenExecuted)
	ctx.Step(`^step "([^"]*)" output "([^"]*)" should be (\d+)$`, sc.stepOutputShouldBeInt)
	ctx.Step(`^step "([^"]*)" output "([^"]*)" should be "([^"]*)"$`, sc.stepOutputShouldBeString)

	// HTTP response assertions
	ctx.Step(`^the response status should be (\d+)$`, sc.theResponseStatusShouldBe)
	ctx.Step(`^the response body should contain "([^"]*)"$`, sc.theResponseBodyShouldContain)
	ctx.Step(`^the response JSON "([^"]*)" should be "([^"]*)"$`, sc.theResponseJSONShouldBe)
	ctx.Step(`^the response JSON "([^"]*)" should not be empty$`, sc.theResponseJSONShouldNotBeEmpty)
	ctx.Step(`^the response header "([^"]*)" should be "([^"]*)"$`, sc.theResponseHeaderShouldBe)
}

// thePipelineShouldSucceed asserts that the last result has no error.
func (sc *ScenarioContext) thePipelineShouldSucceed() error {
	if err := sc.ensureResult(); err != nil {
		return err
	}
	if sc.result.Error != nil {
		return fmt.Errorf("expected pipeline to succeed, got error: %v", sc.result.Error)
	}
	return nil
}

// thePipelineShouldFail asserts that the last result has an error.
func (sc *ScenarioContext) thePipelineShouldFail() error {
	if err := sc.ensureResult(); err != nil {
		return err
	}
	if sc.result.Error == nil {
		return fmt.Errorf("expected pipeline to fail, but it succeeded")
	}
	return nil
}

// thePipelineOutputShouldBe asserts that result.Output[key] == expected.
func (sc *ScenarioContext) thePipelineOutputShouldBe(key, expected string) error {
	if err := sc.ensureResult(); err != nil {
		return err
	}
	actual, ok := sc.result.Output[key]
	if !ok {
		return fmt.Errorf("pipeline output key %q not found in output: %v", key, sc.result.Output)
	}
	actualStr := fmt.Sprintf("%v", actual)
	if actualStr != expected {
		return fmt.Errorf("pipeline output %q: want %q, got %q", key, expected, actualStr)
	}
	return nil
}

// stepShouldHaveBeenExecuted asserts that the named step was executed.
func (sc *ScenarioContext) stepShouldHaveBeenExecuted(name string) error {
	if err := sc.ensureResult(); err != nil {
		return err
	}
	if !sc.result.StepExecuted(name) {
		return fmt.Errorf("expected step %q to have been executed, but it was not", name)
	}
	return nil
}

// stepShouldNotHaveBeenExecuted asserts that the named step was NOT executed.
func (sc *ScenarioContext) stepShouldNotHaveBeenExecuted(name string) error {
	if err := sc.ensureResult(); err != nil {
		return err
	}
	if sc.result.StepExecuted(name) {
		return fmt.Errorf("expected step %q to NOT have been executed, but it was", name)
	}
	return nil
}

// stepOutputShouldBeInt asserts that result.StepResults[step][key] == expected int.
func (sc *ScenarioContext) stepOutputShouldBeInt(step, key string, expected int) error {
	if err := sc.ensureResult(); err != nil {
		return err
	}
	out := sc.result.StepOutput(step)
	if out == nil {
		return fmt.Errorf("step %q was not executed or has no output", step)
	}
	actual, ok := out[key]
	if !ok {
		return fmt.Errorf("step %q output key %q not found", step, key)
	}
	actualStr := fmt.Sprintf("%v", actual)
	actualInt, err := strconv.Atoi(actualStr)
	if err != nil {
		return fmt.Errorf("step %q output %q: cannot convert %q to int: %w", step, key, actualStr, err)
	}
	if actualInt != expected {
		return fmt.Errorf("step %q output %q: want %d, got %d", step, key, expected, actualInt)
	}
	return nil
}

// stepOutputShouldBeString asserts that result.StepResults[step][key] == expected string.
func (sc *ScenarioContext) stepOutputShouldBeString(step, key, expected string) error {
	if err := sc.ensureResult(); err != nil {
		return err
	}
	out := sc.result.StepOutput(step)
	if out == nil {
		return fmt.Errorf("step %q was not executed or has no output", step)
	}
	actual, ok := out[key]
	if !ok {
		return fmt.Errorf("step %q output key %q not found", step, key)
	}
	actualStr := fmt.Sprintf("%v", actual)
	if actualStr != expected {
		return fmt.Errorf("step %q output %q: want %q, got %q", step, key, expected, actualStr)
	}
	return nil
}

// theResponseStatusShouldBe asserts the HTTP response status code.
func (sc *ScenarioContext) theResponseStatusShouldBe(code int) error {
	if err := sc.ensureResult(); err != nil {
		return err
	}
	if sc.result.StatusCode != code {
		return fmt.Errorf("expected HTTP status %d, got %d", code, sc.result.StatusCode)
	}
	return nil
}

// theResponseBodyShouldContain asserts the HTTP response body contains a substring.
func (sc *ScenarioContext) theResponseBodyShouldContain(text string) error {
	if err := sc.ensureResult(); err != nil {
		return err
	}
	if !bytes.Contains(sc.result.RawBody, []byte(text)) {
		return fmt.Errorf("response body does not contain %q\nbody: %s", text, sc.result.RawBody)
	}
	return nil
}

// theResponseJSONShouldBe asserts that response JSON at dot-path equals expected.
func (sc *ScenarioContext) theResponseJSONShouldBe(path, expected string) error {
	if err := sc.ensureResult(); err != nil {
		return err
	}
	val, err := wftest.JSONPath(sc.result.RawBody, path)
	if err != nil {
		return err
	}
	actualStr := fmt.Sprintf("%v", val)
	if actualStr != expected {
		return fmt.Errorf("response JSON %q: want %q, got %q", path, expected, actualStr)
	}
	return nil
}

// theResponseJSONShouldNotBeEmpty asserts that response JSON at dot-path is non-empty.
func (sc *ScenarioContext) theResponseJSONShouldNotBeEmpty(path string) error {
	if err := sc.ensureResult(); err != nil {
		return err
	}
	val, err := wftest.JSONPath(sc.result.RawBody, path)
	if err != nil {
		return err
	}
	if wftest.IsJSONEmpty(val) {
		return fmt.Errorf("response JSON %q: expected non-empty, got %v", path, val)
	}
	return nil
}

// theResponseHeaderShouldBe asserts that a response header equals expected.
func (sc *ScenarioContext) theResponseHeaderShouldBe(header, expected string) error {
	if err := sc.ensureResult(); err != nil {
		return err
	}
	actual := sc.result.Header(header)
	if actual != expected {
		return fmt.Errorf("response header %q: want %q, got %q", header, expected, actual)
	}
	return nil
}
