package module

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestJSONResponseStep_LiteralAndReferencedStringFieldsPreserveJSONText(t *testing.T) {
	fixtures := []struct {
		name       string
		literal    string
		structured any
	}{
		{"object", `{"kind":"gift"}`, map[string]any{"kind": "gift"}},
		{"array", `[1,2]`, []any{float64(1), float64(2)}},
	}
	for _, fixture := range fixtures {
		for _, typed := range []bool{true, false} {
			mode := "literal-field"
			var title any = fixture.literal
			var description any = fixture.literal
			if typed {
				mode = "typed-reference"
				title = map[string]any{"_from": "steps.synthetic_query.row.title"}
				description = map[string]any{"_from": "steps.synthetic_query.row.description"}
			}
			t.Run(fixture.name+"/"+mode, func(t *testing.T) {
				step, err := NewJSONResponseStepFactory()("string-contract", map[string]any{
					"status": http.StatusOK,
					"body": map[string]any{
						"title":       title,
						"description": description,
						"details":     map[string]any{"_from": "steps.synthetic_query.row.details"},
					},
				}, nil)
				if err != nil {
					t.Fatalf("factory: %v", err)
				}
				recorder := httptest.NewRecorder() // In-memory writer; no listener.
				pc := NewPipelineContext(nil, map[string]any{"_http_response_writer": recorder})
				pc.MergeStepOutput("synthetic_query", map[string]any{"row": map[string]any{
					"title": fixture.literal, "description": fixture.literal, "details": fixture.structured,
				}})
				result, err := step.Execute(context.Background(), pc)
				if err != nil {
					t.Fatalf("execute: %v", err)
				}
				if result == nil || !result.Stop || recorder.Code != http.StatusOK {
					t.Fatal("expected a stopped successful HTTP response")
				}
				if recorder.Header().Get("Content-Type") != "application/json" {
					t.Fatal("expected JSON response content type")
				}
				var body map[string]json.RawMessage
				if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || len(body) != 3 {
					t.Fatalf("expected exactly the three fixture fields: %v", err)
				}
				for _, field := range []string{"title", "description"} {
					var got string
					if err := json.Unmarshal(body[field], &got); err != nil || got != fixture.literal {
						t.Fatalf("%s must retain its exact literal string", field)
					}
				}
				var details any
				if err := json.Unmarshal(body["details"], &details); err != nil || !reflect.DeepEqual(details, fixture.structured) {
					t.Fatal("explicitly structured details must remain structured")
				}
			})
		}
	}
}

func TestJSONResponseStep_FieldTemplateJSONCompatibility(t *testing.T) {
	for _, style := range []string{"go-template", "expression"} {
		t.Run(style, func(t *testing.T) {
			object := "{{ .steps.synthetic_query.objectJSON }}"
			array := "{{ .steps.synthetic_query.arrayJSON }}"
			if style == "expression" {
				object = "${ steps.synthetic_query.objectJSON }"
				array = "${ steps.synthetic_query.arrayJSON }"
			}
			step, err := NewJSONResponseStepFactory()("structured-fields", map[string]any{
				"body": map[string]any{"object": object, "array": array},
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			pc := NewPipelineContext(nil, map[string]any{"_http_response_writer": recorder})
			pc.MergeStepOutput("synthetic_query", map[string]any{
				"objectJSON": `{"kind":"gift"}`, "arrayJSON": `[1,2]`,
			})
			if _, err := step.Execute(context.Background(), pc); err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{"object": map[string]any{"kind": "gift"}, "array": []any{float64(1), float64(2)}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("rendered field JSON must retain existing structured behavior: got %#v", got)
			}
		})
	}
}

func TestJSONResponseStep_NestedLiteralJSONStrings(t *testing.T) {
	body := map[string]any{
		"nested": map[string]any{"text": "  {\"kind\":\"gift\"}\n"},
		"values": []any{`[1,2]`, map[string]any{"text": `{"kind":"gift"}`}},
	}
	step, err := NewJSONResponseStepFactory()("nested-literals", map[string]any{"body": body}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The no-writer entry point uses the same field contract and preserves whitespace.
	result, err := step.Execute(context.Background(), NewPipelineContext(nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.Stop || !reflect.DeepEqual(result.Output["body"], body) {
		t.Fatalf("nested literal JSON text must remain strings: got %#v", result)
	}
}

func TestJSONResponseStep_LiteralWholeBodyJSONCompatibility(t *testing.T) {
	for _, fixture := range []struct {
		name string
		body string
		want any
	}{
		{"object", `{"kind":"gift"}`, map[string]any{"kind": "gift"}},
		{"array", `[1,2]`, []any{float64(1), float64(2)}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			step, err := NewJSONResponseStepFactory()("whole-body", map[string]any{"body": fixture.body}, nil)
			if err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			pc := NewPipelineContext(nil, map[string]any{"_http_response_writer": recorder})
			if _, err := step.Execute(context.Background(), pc); err != nil {
				t.Fatal(err)
			}
			var got any
			if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, fixture.want) {
				t.Fatalf("whole-body JSON compatibility changed: got %#v", got)
			}
		})
	}
}
