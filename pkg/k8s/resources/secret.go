package resources

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SecretOpts configures Secret generation.
type SecretOpts struct {
	Name       string
	Namespace  string
	Labels     map[string]string
	Type       corev1.SecretType
	StringData map[string]string
	Data       map[string][]byte
}

// NewSecret builds a Kubernetes Secret.
func NewSecret(opts SecretOpts) *corev1.Secret {
	if opts.Labels == nil {
		opts.Labels = map[string]string{"app.kubernetes.io/managed-by": "wfctl"}
	}
	if opts.Type == "" {
		opts.Type = corev1.SecretTypeOpaque
	}
	return &corev1.Secret{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{Name: opts.Name, Namespace: opts.Namespace, Labels: opts.Labels},
		Type:       opts.Type,
		StringData: opts.StringData,
		Data:       opts.Data,
	}
}
