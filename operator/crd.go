package operator

import (
	_ "embed"
)

//go:embed crd_workflow_definition.yaml
var crdYAML string

// GenerateCRD returns the CRD YAML for WorkflowDefinition.
// This is the Kubernetes CustomResourceDefinition that should be applied to a
// cluster before creating WorkflowDefinition resources.
func GenerateCRD() string {
	return crdYAML
}
