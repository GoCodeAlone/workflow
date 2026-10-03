package interfaces

import (
	"context"
	"reflect"
	"testing"
)

type sensitiveInputTestDriver struct{ ResourceDriver }

func (*sensitiveInputTestDriver) SensitiveInputPaths(context.Context) ([]string, error) {
	return []string{"/password", "/env/*/value"}, nil
}

func TestResourceSensitiveInputDeclarerIsOptional(t *testing.T) {
	if _, required := reflect.TypeFor[ResourceDriver]().MethodByName("SensitiveInputPaths"); required {
		t.Fatal("sensitive input declarations must not break existing ResourceDriver implementations")
	}
	var driver ResourceDriver = &sensitiveInputTestDriver{}
	declarer, ok := driver.(ResourceSensitiveInputDeclarer)
	if !ok {
		t.Fatal("opt-in driver must satisfy ResourceSensitiveInputDeclarer")
	}
	paths, err := declarer.SensitiveInputPaths(t.Context())
	if err != nil || len(paths) != 2 {
		t.Fatalf("paths=%v err=%v", paths, err)
	}
}
