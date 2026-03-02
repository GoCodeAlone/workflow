package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	"github.com/GoCodeAlone/workflow/deploy"
)

// Deployer handles applying and deleting Kubernetes resources.
type Deployer struct {
	client *Client
}

// NewDeployer creates a new Deployer.
func NewDeployer(client *Client) *Deployer {
	return &Deployer{client: client}
}

// Apply performs server-side apply for all objects in the artifacts.
func (d *Deployer) Apply(ctx context.Context, artifacts *deploy.DeployArtifacts, opts deploy.ApplyOpts) (*deploy.DeployResult, error) {
	startedAt := time.Now()
	fieldManager := opts.FieldManager
	if fieldManager == "" {
		fieldManager = "wfctl"
	}

	result := &deploy.DeployResult{
		Status:    "success",
		StartedAt: startedAt,
	}

	for _, obj := range artifacts.Objects {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			continue
		}

		gvr, err := gvrFromUnstructured(u)
		if err != nil {
			result.Status = "failed"
			result.Message = fmt.Sprintf("resolve GVR for %s/%s: %v", u.GetKind(), u.GetName(), err)
			result.CompletedAt = time.Now()
			return result, err
		}

		data, err := json.Marshal(u.Object)
		if err != nil {
			result.Status = "failed"
			result.Message = fmt.Sprintf("marshal %s/%s: %v", u.GetKind(), u.GetName(), err)
			result.CompletedAt = time.Now()
			return result, err
		}

		var resource dynamic.ResourceInterface
		ns := u.GetNamespace()
		if ns != "" {
			resource = d.client.Dynamic.Resource(gvr).Namespace(ns)
		} else {
			resource = d.client.Dynamic.Resource(gvr)
		}

		applyOpts := metav1.PatchOptions{
			FieldManager: fieldManager,
			Force:        boolPtr(opts.Force),
		}
		if opts.DryRun {
			applyOpts.DryRun = []string{metav1.DryRunAll}
		}

		applied, err := resource.Patch(ctx, u.GetName(), types.ApplyPatchType, data, applyOpts)
		if err != nil {
			result.Status = "failed"
			result.Message = fmt.Sprintf("apply %s/%s: %v", u.GetKind(), u.GetName(), err)
			result.CompletedAt = time.Now()
			return result, fmt.Errorf("apply %s/%s: %w", u.GetKind(), u.GetName(), err)
		}

		status := "applied"
		if applied.GetResourceVersion() != "" {
			status = "configured"
		}

		result.Resources = append(result.Resources, deploy.DeployedResource{
			Kind:      u.GetKind(),
			Name:      u.GetName(),
			Namespace: ns,
			Status:    status,
		})
	}

	result.CompletedAt = time.Now()
	result.Message = fmt.Sprintf("applied %d resources", len(result.Resources))
	return result, nil
}

// Delete removes all resources matching the app label.
func (d *Deployer) Delete(ctx context.Context, appName, namespace string) error {
	labelSelector := fmt.Sprintf("app=%s", appName)

	// Delete in reverse resource order: service, deployment, pvc, secret, configmap, namespace
	gvrs := []schema.GroupVersionResource{
		{Group: "", Version: "v1", Resource: "services"},
		{Group: "apps", Version: "v1", Resource: "deployments"},
		{Group: "", Version: "v1", Resource: "persistentvolumeclaims"},
		{Group: "", Version: "v1", Resource: "secrets"},
		{Group: "", Version: "v1", Resource: "configmaps"},
	}

	for _, gvr := range gvrs {
		err := d.client.Dynamic.Resource(gvr).Namespace(namespace).DeleteCollection(
			ctx,
			metav1.DeleteOptions{},
			metav1.ListOptions{LabelSelector: labelSelector},
		)
		if err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("delete %s: %w", gvr.Resource, err)
		}
	}

	return nil
}

// Diff compares artifacts against live state and returns a human-readable diff.
func (d *Deployer) Diff(ctx context.Context, artifacts *deploy.DeployArtifacts) (string, error) {
	var diff string

	for _, obj := range artifacts.Objects {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			continue
		}

		gvr, err := gvrFromUnstructured(u)
		if err != nil {
			continue
		}

		var resource dynamic.ResourceInterface
		ns := u.GetNamespace()
		if ns != "" {
			resource = d.client.Dynamic.Resource(gvr).Namespace(ns)
		} else {
			resource = d.client.Dynamic.Resource(gvr)
		}

		live, err := resource.Get(ctx, u.GetName(), metav1.GetOptions{})
		if errors.IsNotFound(err) {
			diff += fmt.Sprintf("+ %s/%s (new)\n", u.GetKind(), u.GetName())
			continue
		}
		if err != nil {
			diff += fmt.Sprintf("? %s/%s (error: %v)\n", u.GetKind(), u.GetName(), err)
			continue
		}

		// Compare resource versions
		if live.GetResourceVersion() != "" {
			diff += fmt.Sprintf("~ %s/%s (update)\n", u.GetKind(), u.GetName())
		}
	}

	if diff == "" {
		diff = "no changes detected\n"
	}
	return diff, nil
}

func boolPtr(b bool) *bool { return &b }

// gvrFromUnstructured maps an unstructured object to its GroupVersionResource.
func gvrFromUnstructured(u *unstructured.Unstructured) (schema.GroupVersionResource, error) {
	gvk := u.GroupVersionKind()
	// Map common kinds to their resource names
	resourceMap := map[string]string{
		"Namespace":             "namespaces",
		"ServiceAccount":        "serviceaccounts",
		"ConfigMap":             "configmaps",
		"Secret":                "secrets",
		"PersistentVolumeClaim": "persistentvolumeclaims",
		"Deployment":            "deployments",
		"Service":               "services",
		"Ingress":               "ingresses",
		"Pod":                   "pods",
	}

	resource, ok := resourceMap[gvk.Kind]
	if !ok {
		return schema.GroupVersionResource{}, fmt.Errorf("unknown kind %q", gvk.Kind)
	}

	return schema.GroupVersionResource{
		Group:    gvk.Group,
		Version:  gvk.Version,
		Resource: resource,
	}, nil
}
