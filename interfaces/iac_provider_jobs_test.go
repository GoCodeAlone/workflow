package interfaces_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

func targetedJobSpec(t *testing.T, target *interfaces.ResourceRef, timeout int) interfaces.JobSpec {
	t.Helper()
	spec := interfaces.JobSpec{Name: "migration", Kind: interfaces.JobKindEphemeral, RunCommand: "migrate"}
	value := reflect.ValueOf(&spec).Elem()
	field := value.FieldByName("Target")
	if !field.IsValid() || field.Type() != reflect.TypeFor[*interfaces.ResourceRef]() {
		t.Fatal("JobSpec.Target must be *ResourceRef")
	}
	field.Set(reflect.ValueOf(target))
	field = value.FieldByName("TimeoutSeconds")
	if !field.IsValid() || field.Kind() != reflect.Int {
		t.Fatal("JobSpec.TimeoutSeconds must be int")
	}
	field.SetInt(int64(timeout))
	return spec
}

func TestProviderJobSpecTargetAndTimeoutValidation(t *testing.T) {
	target := &interfaces.ResourceRef{Name: "app", Type: "infra.container_service", ProviderID: "app-id"}
	for _, tc := range []struct {
		name    string
		target  *interfaces.ResourceRef
		timeout int
		valid   bool
	}{
		{"legacy untargeted zero", nil, 0, true},
		{"untargeted positive", nil, 600, true},
		{"targeted minimum", target, 1, true},
		{"targeted maximum", target, 3600, true},
		{"targeted zero", target, 0, false},
		{"targeted negative", target, -1, false},
		{"untargeted negative", nil, -1, false},
		{"targeted over maximum", target, 3601, false},
		{"untargeted over maximum", nil, 3601, false},
		{"integer overflow", target, int(^uint(0) >> 1), false},
		{"empty target", &interfaces.ResourceRef{}, 600, false},
		{"missing name", &interfaces.ResourceRef{Type: target.Type, ProviderID: target.ProviderID}, 600, false},
		{"missing type", &interfaces.ResourceRef{Name: target.Name, ProviderID: target.ProviderID}, 600, false},
		{"missing provider ID", &interfaces.ResourceRef{Name: target.Name, Type: target.Type}, 600, false},
		{"blank identity", &interfaces.ResourceRef{Name: " ", Type: "\t", ProviderID: "\n"}, 600, false},
		{"other provider resource type", &interfaces.ResourceRef{Name: "worker", Type: "other.parent", ProviderID: "resource-id"}, 600, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := targetedJobSpec(t, tc.target, tc.timeout)
			validator, ok := any(spec).(interface{ Validate() error })
			if !ok {
				t.Fatal("JobSpec must expose shared validation")
			}
			err := validator.Validate()
			if tc.valid && err != nil || !tc.valid && !errors.Is(err, interfaces.ErrValidation) {
				t.Fatalf("Validate() = %v, valid = %v", err, tc.valid)
			}
		})
	}
}

func TestProviderJobSpecTargetJSONAndRunnerCompatibility(t *testing.T) {
	target := &interfaces.ResourceRef{Name: "app", Type: "infra.container_service", ProviderID: "app-id"}
	spec := targetedJobSpec(t, target, 600)
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	var decoded interfaces.JobSpec
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(spec, decoded) {
		t.Fatalf("JSON round trip = %+v, want %+v", decoded, spec)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields["target"]) == 0 || string(fields["timeout_seconds"]) != "600" {
		t.Fatalf("target and timeout must use canonical JSON keys: %s", data)
	}
	if _, added := reflect.TypeFor[interfaces.IaCProviderRunner]().MethodByName("CancelJob"); added {
		t.Fatal("cancellation must not change the existing IaCProviderRunner interface")
	}
}
