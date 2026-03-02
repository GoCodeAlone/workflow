package k8s

import (
	"context"
	"fmt"
	"io"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/GoCodeAlone/workflow/deploy"
)

// WaitForRollout polls a Deployment until all replicas are ready or the context is cancelled.
func WaitForRollout(ctx context.Context, client *Client, name, namespace string, timeout time.Duration) error {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for deployment %s/%s: %w", namespace, name, ctx.Err())
		case <-ticker.C:
			dep, err := client.Typed.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				continue
			}

			if dep.Status.UpdatedReplicas == *dep.Spec.Replicas &&
				dep.Status.ReadyReplicas == *dep.Spec.Replicas &&
				dep.Status.AvailableReplicas == *dep.Spec.Replicas &&
				dep.Status.ObservedGeneration >= dep.Generation {
				return nil
			}
		}
	}
}

// GetStatus returns the current deployment status for an app.
func GetStatus(ctx context.Context, client *Client, appName, namespace string) (*StatusResult, error) {
	result := &StatusResult{
		AppName:   appName,
		Namespace: namespace,
		Phase:     "Unknown",
		UpdatedAt: time.Now(),
	}

	// Get deployment
	dep, err := client.Typed.AppsV1().Deployments(namespace).Get(ctx, appName, metav1.GetOptions{})
	if err != nil {
		return result, fmt.Errorf("get deployment %s/%s: %w", namespace, appName, err)
	}

	result.Desired = int(*dep.Spec.Replicas)
	result.Ready = int(dep.Status.ReadyReplicas)

	switch {
	case dep.Status.ReadyReplicas == *dep.Spec.Replicas:
		result.Phase = "Running"
	case dep.Status.UpdatedReplicas > 0:
		result.Phase = "Updating"
	default:
		result.Phase = "Pending"
	}

	// Get pods
	labelSelector := fmt.Sprintf("app=%s", appName)
	pods, err := client.Typed.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return result, nil //nolint:nilerr // intentionally return partial result without pod data when listing pods fails
	}

	for i := range pods.Items {
		pod := &pods.Items[i]
		ps := PodStatus{
			Name:  pod.Name,
			Phase: string(pod.Status.Phase),
			Ready: isPodReady(pod),
			Age:   time.Since(pod.CreationTimestamp.Time),
		}

		var totalRestarts int32
		for j := range pod.Status.ContainerStatuses {
			cs := &pod.Status.ContainerStatuses[j]
			totalRestarts += cs.RestartCount
			state, reason, msg := containerState(*cs)
			ps.Containers = append(ps.Containers, ContainerStatus{
				Name:         cs.Name,
				Ready:        cs.Ready,
				RestartCount: cs.RestartCount,
				State:        state,
				Reason:       reason,
				Message:      msg,
			})
		}
		ps.Restarts = totalRestarts
		result.Pods = append(result.Pods, ps)
	}

	return result, nil
}

// StreamLogs returns a ReadCloser streaming logs from the app's pods.
func StreamLogs(ctx context.Context, client *Client, appName, namespace string, opts deploy.LogOpts) (io.ReadCloser, error) {
	labelSelector := fmt.Sprintf("app=%s", appName)
	pods, err := client.Typed.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no pods found for app %q in namespace %q", appName, namespace)
	}

	// Use the first pod
	podName := pods.Items[0].Name
	container := opts.Container
	if container == "" {
		container = appName
	}

	logOpts := &corev1.PodLogOptions{
		Container: container,
		Follow:    opts.Follow,
		Previous:  opts.Previous,
	}
	if opts.TailLines > 0 {
		logOpts.TailLines = &opts.TailLines
	}
	if opts.Since > 0 {
		sinceSeconds := int64(opts.Since.Seconds())
		logOpts.SinceSeconds = &sinceSeconds
	}

	return client.Typed.CoreV1().Pods(namespace).GetLogs(podName, logOpts).Stream(ctx)
}

func isPodReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func containerState(cs corev1.ContainerStatus) (state, reason, message string) {
	switch {
	case cs.State.Running != nil:
		return "running", "", ""
	case cs.State.Waiting != nil:
		return "waiting", cs.State.Waiting.Reason, cs.State.Waiting.Message
	case cs.State.Terminated != nil:
		return "terminated", cs.State.Terminated.Reason, cs.State.Terminated.Message
	default:
		return "unknown", "", ""
	}
}
