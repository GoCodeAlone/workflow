package yamledit

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAppendGeneratedModulesPreservesCommentsAndUnknownKeys(t *testing.T) {
	doc := readYAMLDoc(t, "comments_unknown.input.yaml")
	changed, err := AppendGeneratedModules(doc, []GeneratedModule{{
		Name:      "otel-collector",
		Type:      "infra.container_service",
		Satisfies: []string{"observability.telemetry.default"},
		Config: map[string]any{
			"image": "otel/opentelemetry-collector-contrib:latest",
		},
		DependsOn: []string{"api"},
	}})
	if err != nil {
		t.Fatalf("append generated modules: %v", err)
	}
	if !changed {
		t.Fatalf("changed = false, want true")
	}
	assertYAMLEqual(t, encodeYAML(t, doc), readGolden(t, "comments_unknown.golden.yaml"))
}

func TestAppendGeneratedModulesCreatesModulesWhenAbsent(t *testing.T) {
	doc := readYAMLDoc(t, "no_modules.input.yaml")
	changed, err := AppendGeneratedModules(doc, []GeneratedModule{{
		Name:      "nats",
		Type:      "infra.message_broker",
		Satisfies: []string{"messaging.nats.default"},
		Config:    map[string]any{"plan": "basic"},
	}})
	if err != nil {
		t.Fatalf("append generated modules: %v", err)
	}
	if !changed {
		t.Fatalf("changed = false, want true")
	}
	assertYAMLEqual(t, encodeYAML(t, doc), readGolden(t, "no_modules.golden.yaml"))
}

func TestAppendGeneratedModulesInsertsAfterLastInfraModule(t *testing.T) {
	doc := readYAMLDoc(t, "insert_after_infra.input.yaml")
	changed, err := AppendGeneratedModules(doc, []GeneratedModule{
		{Name: "zeta", Type: "infra.cache", Satisfies: []string{"cache.default"}},
		{Name: "alpha", Type: "infra.database", Satisfies: []string{"database.default"}},
	})
	if err != nil {
		t.Fatalf("append generated modules: %v", err)
	}
	if !changed {
		t.Fatalf("changed = false, want true")
	}
	assertYAMLEqual(t, encodeYAML(t, doc), readGolden(t, "insert_after_infra.golden.yaml"))
}

func TestAppendGeneratedModulesPreservesAnchors(t *testing.T) {
	doc := readYAMLDoc(t, "anchors.input.yaml")
	_, err := AppendGeneratedModules(doc, []GeneratedModule{{
		Name:      "otel-collector",
		Type:      "infra.container_service",
		Satisfies: []string{"observability.telemetry.default"},
	}})
	if err != nil {
		t.Fatalf("append generated modules: %v", err)
	}
	out := encodeYAML(t, doc)
	if !strings.Contains(string(out), "&base") || !strings.Contains(string(out), "*base") {
		t.Fatalf("anchors were not preserved in output:\n%s", out)
	}
}

func TestAppendGeneratedModulesIsIdempotentBySatisfiesKey(t *testing.T) {
	doc := readYAMLDoc(t, "comments_unknown.input.yaml")
	modules := []GeneratedModule{{
		Name:      "otel-collector",
		Type:      "infra.container_service",
		Satisfies: []string{"observability.telemetry.default"},
		Config:    map[string]any{"image": "otel/opentelemetry-collector-contrib:latest"},
	}}
	changed, err := AppendGeneratedModules(doc, modules)
	if err != nil {
		t.Fatalf("first append: %v", err)
	}
	if !changed {
		t.Fatalf("first append changed = false, want true")
	}
	first := encodeYAML(t, doc)
	changed, err = AppendGeneratedModules(doc, modules)
	if err != nil {
		t.Fatalf("second append: %v", err)
	}
	if changed {
		t.Fatalf("second append changed = true, want false")
	}
	if second := encodeYAML(t, doc); !bytes.Equal(second, first) {
		t.Fatalf("second append changed YAML\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func readYAMLDoc(t *testing.T, name string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal(readGolden(t, name), &doc); err != nil {
		t.Fatalf("unmarshal %s: %v", name, err)
	}
	return &doc
}

func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

func encodeYAML(t *testing.T, doc *yaml.Node) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		t.Fatalf("encode YAML: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("close YAML encoder: %v", err)
	}
	return buf.Bytes()
}

func assertYAMLEqual(t *testing.T, got, want []byte) {
	t.Helper()
	if string(got) != string(want) {
		t.Fatalf("YAML mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
