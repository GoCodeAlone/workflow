package k8s

import (
	"encoding/json"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

func TestManagedFieldsSchemaCompatibility(t *testing.T) {
	// Exercise the apimachinery/kube-openapi schema boundary without a cluster.
	const schemaJSON = `{
		"io.example.Widget": {
			"type": "object",
			"properties": {
				"apiVersion": {"type": "string"},
				"kind": {"type": "string"},
				"spec": {
					"type": "object",
					"properties": {"message": {"type": "string"}}
				}
			},
			"x-kubernetes-group-version-kind": [
				{"group": "example.com", "version": "v1", "kind": "Widget"}
			]
		}
	}`
	var schemas map[string]*spec.Schema
	if err := json.Unmarshal([]byte(schemaJSON), &schemas); err != nil {
		t.Fatal(err)
	}
	converter, err := managedfields.NewTypeConverter(schemas, false)
	if err != nil {
		t.Fatalf("create type converter: %v", err)
	}
	original := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "example.com/v1",
		"kind":       "Widget",
		"spec":       map[string]interface{}{"message": "hello"},
	}}
	value, err := converter.ObjectToTyped(original)
	if err != nil {
		t.Fatalf("convert object to typed value: %v", err)
	}
	result, err := converter.TypedToObject(value)
	if err != nil {
		t.Fatalf("convert typed value to object: %v", err)
	}
	if !reflect.DeepEqual(original, result) {
		t.Fatalf("schema round trip changed object: got %#v, want %#v", result, original)
	}
	invalid := original.DeepCopy()
	invalid.Object["spec"] = map[string]interface{}{"message": true}
	if _, err := converter.ObjectToTyped(invalid); err == nil {
		t.Fatal("expected the schema to reject a boolean in a string field")
	}
}
