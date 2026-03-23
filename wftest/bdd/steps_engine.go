package bdd

import (
	"github.com/GoCodeAlone/workflow/wftest"
	"github.com/cucumber/godog"
)

// registerEngineSteps registers "Given" steps for loading the workflow engine.
func registerEngineSteps(ctx *godog.ScenarioContext, sc *ScenarioContext) {
	ctx.Step(`^the workflow engine is loaded with config:$`, sc.theEngineIsLoadedWithConfig)
	ctx.Step(`^the workflow engine is loaded with "([^"]*)"$`, sc.theEngineIsLoadedWithFile)
}

// theEngineIsLoadedWithConfig stores inline YAML as a pending harness option.
// The harness is created lazily on the first action ("When") step.
func (sc *ScenarioContext) theEngineIsLoadedWithConfig(doc *godog.DocString) error {
	sc.pendingOpts = append(sc.pendingOpts, wftest.WithYAML(doc.Content))
	return nil
}

// theEngineIsLoadedWithFile stores a config file path as a pending harness option.
func (sc *ScenarioContext) theEngineIsLoadedWithFile(path string) error {
	sc.pendingOpts = append(sc.pendingOpts, wftest.WithConfig(path))
	return nil
}
