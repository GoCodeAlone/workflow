package k8s

import (
	"context"
	"fmt"
	"io"

	"github.com/GoCodeAlone/workflow/deploy"
)

// K8sDeployTarget implements deploy.DeployTarget for Kubernetes.
type K8sDeployTarget struct {
	client *Client
}

// NewDeployTarget creates a K8sDeployTarget. If client is nil, one will be
// created lazily from default kubeconfig on first operation.
func NewDeployTarget() *K8sDeployTarget {
	return &K8sDeployTarget{}
}

// NewDeployTargetWithClient creates a K8sDeployTarget with a pre-configured client.
func NewDeployTargetWithClient(client *Client) *K8sDeployTarget {
	return &K8sDeployTarget{client: client}
}

func (t *K8sDeployTarget) Name() string { return "kubernetes" }

func (t *K8sDeployTarget) Generate(ctx context.Context, req *deploy.DeployRequest) (*deploy.DeployArtifacts, error) {
	ms, err := Build(req)
	if err != nil {
		return nil, fmt.Errorf("build manifests: %w", err)
	}

	artifacts := &deploy.DeployArtifacts{
		Target:    "kubernetes",
		AppName:   req.AppName,
		Namespace: req.Namespace,
		Objects:   make([]any, len(ms.Objects)),
		Metadata:  map[string]string{},
	}

	for i, obj := range ms.Objects {
		artifacts.Objects[i] = obj
	}

	// If OutputDir is specified, write files
	if req.OutputDir != "" {
		if err := ms.WriteYAML(req.OutputDir, false); err != nil {
			return nil, fmt.Errorf("write YAML: %w", err)
		}
		artifacts.Files = make(map[string][]byte)
		artifacts.Metadata["outputDir"] = req.OutputDir
	}

	return artifacts, nil
}

func (t *K8sDeployTarget) Apply(ctx context.Context, artifacts *deploy.DeployArtifacts, opts deploy.ApplyOpts) (*deploy.DeployResult, error) {
	client, err := t.ensureClient(artifacts.Namespace)
	if err != nil {
		return nil, err
	}

	deployer := NewDeployer(client)
	return deployer.Apply(ctx, artifacts, opts)
}

func (t *K8sDeployTarget) Destroy(ctx context.Context, appName, namespace string) error {
	client, err := t.ensureClient(namespace)
	if err != nil {
		return err
	}

	deployer := NewDeployer(client)
	return deployer.Delete(ctx, appName, namespace)
}

func (t *K8sDeployTarget) Status(ctx context.Context, appName, namespace string) (*deploy.DeployStatus, error) {
	client, err := t.ensureClient(namespace)
	if err != nil {
		return nil, err
	}

	result, err := GetStatus(ctx, client, appName, namespace)
	if err != nil {
		return nil, err
	}

	status := &deploy.DeployStatus{
		AppName:   result.AppName,
		Namespace: result.Namespace,
		Phase:     result.Phase,
		Ready:     result.Ready,
		Desired:   result.Desired,
		Message:   result.Message,
	}

	for _, pod := range result.Pods {
		status.Resources = append(status.Resources, deploy.ResourceStatus{
			Kind:    "Pod",
			Name:    pod.Name,
			Status:  pod.Phase,
			Message: fmt.Sprintf("ready=%v restarts=%d", pod.Ready, pod.Restarts),
		})
	}

	return status, nil
}

func (t *K8sDeployTarget) Diff(ctx context.Context, artifacts *deploy.DeployArtifacts) (string, error) {
	client, err := t.ensureClient(artifacts.Namespace)
	if err != nil {
		return "", err
	}

	deployer := NewDeployer(client)
	return deployer.Diff(ctx, artifacts)
}

func (t *K8sDeployTarget) Logs(ctx context.Context, appName, namespace string, opts deploy.LogOpts) (io.ReadCloser, error) {
	client, err := t.ensureClient(namespace)
	if err != nil {
		return nil, err
	}

	return StreamLogs(ctx, client, appName, namespace, opts)
}

func (t *K8sDeployTarget) ensureClient(namespace string) (*Client, error) {
	if t.client != nil {
		return t.client, nil
	}
	client, err := NewClient(ClientConfig{Namespace: namespace})
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}
	t.client = client
	return client, nil
}
