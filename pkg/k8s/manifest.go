package k8s

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// ManifestSet is an ordered collection of Kubernetes objects.
type ManifestSet struct {
	Objects []*unstructured.Unstructured
}

// resourceOrder defines the canonical ordering for k8s resources.
var resourceOrder = map[string]int{
	"Namespace":             0,
	"ServiceAccount":        1,
	"ConfigMap":             2,
	"Secret":                3,
	"PersistentVolumeClaim": 4,
	"Deployment":            5,
	"Service":               6,
	"Ingress":               7,
}

// Add appends an object to the manifest set.
func (m *ManifestSet) Add(obj *unstructured.Unstructured) {
	m.Objects = append(m.Objects, obj)
}

// AddRuntime converts a typed k8s object to unstructured and adds it.
func (m *ManifestSet) AddRuntime(obj runtime.Object) error {
	data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return fmt.Errorf("convert to unstructured: %w", err)
	}
	u := &unstructured.Unstructured{Object: data}
	m.Add(u)
	return nil
}

// Sort orders objects by the canonical resource ordering.
func (m *ManifestSet) Sort() {
	sorted := make([]*unstructured.Unstructured, len(m.Objects))
	copy(sorted, m.Objects)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			oi := resourceOrder[sorted[i].GetKind()]
			oj := resourceOrder[sorted[j].GetKind()]
			if oi > oj {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	m.Objects = sorted
}

// WriteYAML writes all objects as YAML to the given directory.
// If singleFile is true, writes a multi-document YAML to manifests.yaml.
// Otherwise writes each object to a separate file named by kind-name.yaml.
func (m *ManifestSet) WriteYAML(dir string, singleFile bool) error {
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if singleFile {
		return m.writeMultiDocYAML(filepath.Join(dir, "manifests.yaml"))
	}
	for _, obj := range m.Objects {
		filename := fmt.Sprintf("%s-%s.yaml", toLowerKind(obj.GetKind()), obj.GetName())
		path := filepath.Join(dir, filename)
		data, err := toYAML(obj)
		if err != nil {
			return fmt.Errorf("marshal %s/%s: %w", obj.GetKind(), obj.GetName(), err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

func (m *ManifestSet) writeMultiDocYAML(path string) error {
	var buf bytes.Buffer
	for i, obj := range m.Objects {
		if i > 0 {
			buf.WriteString("---\n")
		}
		data, err := toYAML(obj)
		if err != nil {
			return fmt.Errorf("marshal %s/%s: %w", obj.GetKind(), obj.GetName(), err)
		}
		buf.Write(data)
	}
	return os.WriteFile(path, buf.Bytes(), 0600)
}

// WriteJSON writes all objects as a JSON array to the given path.
func (m *ManifestSet) WriteJSON(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	data, err := json.MarshalIndent(m.Objects, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

// toYAML marshals an unstructured object to YAML-compatible JSON with indentation.
// client-go transitively depends on sigs.k8s.io/yaml for proper conversion.
func toYAML(obj *unstructured.Unstructured) ([]byte, error) {
	data, err := json.MarshalIndent(obj.Object, "", "  ")
	if err != nil {
		return nil, err
	}
	// Append newline for clean file endings
	return append(data, '\n'), nil
}

func toLowerKind(kind string) string {
	if kind == "" {
		return "unknown"
	}
	var result []byte
	for i, c := range kind {
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				result = append(result, '-')
			}
			result = append(result, byte(c+'a'-'A')) //nolint:gosec // G115 — rune value is always ASCII letter (A-Z)
		} else {
			result = append(result, byte(c)) //nolint:gosec // G115 — rune value is ASCII printable character
		}
	}
	return string(result)
}
