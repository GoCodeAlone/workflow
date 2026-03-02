package resources

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NewNamespace builds a Kubernetes Namespace.
func NewNamespace(name string, labels map[string]string) *corev1.Namespace {
	if labels == nil {
		labels = map[string]string{"app.kubernetes.io/managed-by": "wfctl"}
	}
	return &corev1.Namespace{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
	}
}
