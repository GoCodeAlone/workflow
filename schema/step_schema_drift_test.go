package schema

import (
	"strings"
	"testing"
)

func TestCoreStepTypesHaveSchemas(t *testing.T) {
	reg := GetStepSchemaRegistry()
	for _, mt := range coreModuleTypes {
		if !strings.HasPrefix(mt, "step.") {
			continue
		}
		if reg.Get(mt) == nil {
			t.Errorf("step type %q is in coreModuleTypes but has no StepSchemaRegistry entry — add it to registerBuiltins()", mt)
		}
	}
}

func TestSchemaRegistryTypesInCoreModuleTypes(t *testing.T) {
	coreSet := make(map[string]bool, len(coreModuleTypes))
	for _, mt := range coreModuleTypes {
		coreSet[mt] = true
	}
	for _, st := range GetStepSchemaRegistry().Types() {
		if !coreSet[st] {
			t.Errorf("step type %q is in StepSchemaRegistry but not in coreModuleTypes — add it to schema.go", st)
		}
	}
}
