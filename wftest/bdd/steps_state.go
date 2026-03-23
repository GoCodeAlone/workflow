package bdd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/wftest"
	"github.com/cucumber/godog"
	"gopkg.in/yaml.v3"
)

// registerStateSteps registers step definitions for state store setup and assertions.
func registerStateSteps(ctx *godog.ScenarioContext, sc *ScenarioContext) {
	ctx.Step(`^state "([^"]*)" is seeded from "([^"]*)"$`, sc.stateIsSeededFrom)
	ctx.Step(`^state "([^"]*)" has key "([^"]*)" with:$`, sc.stateHasKeyWith)
	ctx.Step(`^state "([^"]*)" key "([^"]*)" field "([^"]*)" should be "([^"]*)"$`, sc.stateFieldShouldBe)
	ctx.Step(`^state "([^"]*)" key "([^"]*)" field "([^"]*)" should be (\d+)$`, sc.stateFieldShouldBeInt)
}

// stateIsSeededFrom loads a fixture file and queues it as a pending state seed.
// WithState() is automatically added to the pending opts the first time this step runs.
func (sc *ScenarioContext) stateIsSeededFrom(store, path string) error {
	data, err := loadFixture(path)
	if err != nil {
		return fmt.Errorf("state %q seed from %q: %w", store, path, err)
	}
	sc.queueState(store, data)
	return nil
}

// stateHasKeyWith seeds a single key in a named store with values from a table.
func (sc *ScenarioContext) stateHasKeyWith(store, key string, table *godog.Table) error {
	row := make(map[string]any)
	for _, r := range table.Rows {
		if len(r.Cells) != 2 {
			return fmt.Errorf("state table must have 2 columns (field, value), got %d", len(r.Cells))
		}
		row[r.Cells[0].Value] = r.Cells[1].Value
	}
	sc.queueState(store, map[string]any{key: row})
	return nil
}

// stateFieldShouldBe asserts that state[store][key][field] == expected string.
func (sc *ScenarioContext) stateFieldShouldBe(store, key, field, expected string) error {
	if err := sc.ensureHarness(); err != nil {
		return err
	}
	s := sc.harness.State()
	if s == nil {
		return fmt.Errorf("state store not initialised; ensure WithState() is queued")
	}
	val, ok := s.Get(store, key)
	if !ok {
		return fmt.Errorf("state[%s][%s]: key not found", store, key)
	}
	m, ok := val.(map[string]any)
	if !ok {
		return fmt.Errorf("state[%s][%s]: expected map, got %T", store, key, val)
	}
	actual, ok := m[field]
	if !ok {
		return fmt.Errorf("state[%s][%s][%s]: field not found", store, key, field)
	}
	actualStr := fmt.Sprintf("%v", actual)
	if actualStr != expected {
		return fmt.Errorf("state[%s][%s][%s]: want %q, got %q", store, key, field, expected, actualStr)
	}
	return nil
}

// stateFieldShouldBeInt asserts that state[store][key][field] == expected int.
func (sc *ScenarioContext) stateFieldShouldBeInt(store, key, field string, expected int) error {
	if err := sc.ensureHarness(); err != nil {
		return err
	}
	s := sc.harness.State()
	if s == nil {
		return fmt.Errorf("state store not initialised")
	}
	val, ok := s.Get(store, key)
	if !ok {
		return fmt.Errorf("state[%s][%s]: key not found", store, key)
	}
	m, ok := val.(map[string]any)
	if !ok {
		return fmt.Errorf("state[%s][%s]: expected map, got %T", store, key, val)
	}
	actual, ok := m[field]
	if !ok {
		return fmt.Errorf("state[%s][%s][%s]: field not found", store, key, field)
	}
	actualJSON, _ := json.Marshal(actual)
	var actualInt int
	if err := json.Unmarshal(actualJSON, &actualInt); err != nil {
		return fmt.Errorf("state[%s][%s][%s]: cannot convert %v to int: %w", store, key, field, actual, err)
	}
	if actualInt != expected {
		return fmt.Errorf("state[%s][%s][%s]: want %d, got %d", store, key, field, expected, actualInt)
	}
	return nil
}

// queueState ensures WithState() is in pendingOpts and adds a pending seed.
func (sc *ScenarioContext) queueState(store string, data map[string]any) {
	if !sc.hasState {
		sc.pendingOpts = append(sc.pendingOpts, wftest.WithState())
		sc.hasState = true
	}
	sc.pendingStateSeeds = append(sc.pendingStateSeeds, pendingStateSeed{store: store, data: data})
}

// loadFixture reads a JSON or YAML fixture file into map[string]any.
func loadFixture(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read fixture: %w", err)
	}
	var m map[string]any
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("parse YAML fixture: %w", err)
		}
	default:
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("parse JSON fixture: %w", err)
		}
	}
	return m, nil
}
