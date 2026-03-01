package k8s

import (
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestManifestSet_Add(t *testing.T) {
	ms := &ManifestSet{}
	obj := &unstructured.Unstructured{}
	obj.SetKind("ConfigMap")
	obj.SetName("test")
	ms.Add(obj)

	if len(ms.Objects) != 1 {
		t.Fatalf("expected 1 object, got %d", len(ms.Objects))
	}
}

func TestManifestSet_Sort(t *testing.T) {
	ms := &ManifestSet{}

	svc := &unstructured.Unstructured{}
	svc.SetKind("Service")
	svc.SetName("svc")

	ns := &unstructured.Unstructured{}
	ns.SetKind("Namespace")
	ns.SetName("ns")

	deploy := &unstructured.Unstructured{}
	deploy.SetKind("Deployment")
	deploy.SetName("dep")

	cm := &unstructured.Unstructured{}
	cm.SetKind("ConfigMap")
	cm.SetName("cm")

	ms.Add(svc)
	ms.Add(deploy)
	ms.Add(cm)
	ms.Add(ns)

	ms.Sort()

	expected := []string{"Namespace", "ConfigMap", "Deployment", "Service"}
	for i, kind := range expected {
		if ms.Objects[i].GetKind() != kind {
			t.Errorf("position %d: got %s, want %s", i, ms.Objects[i].GetKind(), kind)
		}
	}
}

func TestManifestSet_WriteYAML(t *testing.T) {
	ms := &ManifestSet{}

	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "test",
				"namespace": "default",
			},
			"data": map[string]any{
				"key": "value",
			},
		},
	}
	ms.Add(obj)

	dir := t.TempDir()
	if err := ms.WriteYAML(dir, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
}

func TestManifestSet_WriteMultiDocYAML(t *testing.T) {
	ms := &ManifestSet{}

	cm := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "cm1"},
		},
	}
	cm2 := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "cm2"},
		},
	}
	ms.Add(cm)
	ms.Add(cm2)

	dir := t.TempDir()
	if err := ms.WriteYAML(dir, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "manifests.yaml"))
	if err != nil {
		t.Fatalf("read manifests.yaml: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty manifests.yaml")
	}
}

func TestToLowerKind(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"ConfigMap", "config-map"},
		{"PersistentVolumeClaim", "persistent-volume-claim"},
		{"Service", "service"},
		{"", "unknown"},
	}
	for _, tt := range tests {
		got := toLowerKind(tt.input)
		if got != tt.want {
			t.Errorf("toLowerKind(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
