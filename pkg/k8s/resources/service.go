package resources

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// ServiceOpts configures Service generation.
type ServiceOpts struct {
	Name        string
	Namespace   string
	Labels      map[string]string
	Annotations map[string]string
	Ports       []ServicePort
	Type        corev1.ServiceType
}

// ServicePort maps a service port to a target port.
type ServicePort struct {
	Name       string
	Port       int32
	TargetPort int32
	Protocol   corev1.Protocol
}

// NewService builds a Kubernetes Service.
func NewService(opts ServiceOpts) *corev1.Service {
	if opts.Labels == nil {
		opts.Labels = map[string]string{
			"app": opts.Name,
		}
	}
	if opts.Type == "" {
		opts.Type = corev1.ServiceTypeClusterIP
	}

	var ports []corev1.ServicePort
	for _, p := range opts.Ports {
		protocol := p.Protocol
		if protocol == "" {
			protocol = corev1.ProtocolTCP
		}
		targetPort := p.TargetPort
		if targetPort == 0 {
			targetPort = p.Port
		}
		ports = append(ports, corev1.ServicePort{
			Name:       p.Name,
			Port:       p.Port,
			TargetPort: intstr.FromInt32(targetPort),
			Protocol:   protocol,
		})
	}

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{
			Name: opts.Name, Namespace: opts.Namespace,
			Labels: opts.Labels, Annotations: opts.Annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:     opts.Type,
			Selector: map[string]string{"app": opts.Name},
			Ports:    ports,
		},
	}
}
