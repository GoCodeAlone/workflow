package resources

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConfigMapOpts configures ConfigMap generation.
type ConfigMapOpts struct {
	Name       string
	Namespace  string
	Labels     map[string]string
	Data       map[string]string
	BinaryData map[string][]byte
}

// NewConfigMap builds a Kubernetes ConfigMap.
func NewConfigMap(opts ConfigMapOpts) *corev1.ConfigMap {
	if opts.Labels == nil {
		opts.Labels = map[string]string{"app.kubernetes.io/managed-by": "wfctl"}
	}
	return &corev1.ConfigMap{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{Name: opts.Name, Namespace: opts.Namespace, Labels: opts.Labels},
		Data:       opts.Data,
		BinaryData: opts.BinaryData,
	}
}
