package bdd

import (
	"fmt"

	"github.com/cucumber/godog"
)

// registerTriggerSteps registers "When" steps for triggering pipeline execution.
func registerTriggerSteps(ctx *godog.ScenarioContext, sc *ScenarioContext) {
	ctx.Step(`^I execute pipeline "([^"]*)"$`, sc.iExecutePipeline)
	ctx.Step(`^I execute pipeline "([^"]*)" with:$`, sc.iExecutePipelineWith)
	ctx.Step(`^I fire event "([^"]*)" with:$`, sc.iFireEventWith)
	ctx.Step(`^I fire schedule "([^"]*)"$`, sc.iFireSchedule)
}

// iExecutePipeline runs the named pipeline with no trigger data.
func (sc *ScenarioContext) iExecutePipeline(name string) error {
	if err := sc.ensureHarness(); err != nil {
		return err
	}
	sc.result = sc.harness.ExecutePipeline(name, nil)
	return nil
}

// iExecutePipelineWith runs the named pipeline with key/value trigger data from a table.
func (sc *ScenarioContext) iExecutePipelineWith(name string, table *godog.Table) error {
	if err := sc.ensureHarness(); err != nil {
		return err
	}
	data, err := tableToMap(table)
	if err != nil {
		return fmt.Errorf("pipeline %q: %w", name, err)
	}
	sc.result = sc.harness.ExecutePipeline(name, data)
	return nil
}

// iFireEventWith fires an eventbus trigger for the given topic with table data.
func (sc *ScenarioContext) iFireEventWith(topic string, table *godog.Table) error {
	if err := sc.ensureHarness(); err != nil {
		return err
	}
	data, err := tableToMap(table)
	if err != nil {
		return fmt.Errorf("event %q: %w", topic, err)
	}
	sc.result = sc.harness.FireEvent(topic, data)
	return nil
}

// iFireSchedule fires a schedule trigger for the named pipeline.
func (sc *ScenarioContext) iFireSchedule(name string) error {
	if err := sc.ensureHarness(); err != nil {
		return err
	}
	sc.result = sc.harness.FireSchedule(name, nil)
	return nil
}

// tableToMap converts a two-column godog table (key | value) into map[string]any.
func tableToMap(table *godog.Table) (map[string]any, error) {
	result := make(map[string]any, len(table.Rows))
	for _, row := range table.Rows {
		if len(row.Cells) != 2 {
			return nil, fmt.Errorf("table must have exactly 2 columns (key, value), got %d", len(row.Cells))
		}
		result[row.Cells[0].Value] = row.Cells[1].Value
	}
	return result, nil
}
