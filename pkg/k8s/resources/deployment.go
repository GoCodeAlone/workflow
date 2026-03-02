package resources

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// DeploymentOpts configures Deployment generation.
type DeploymentOpts struct {
	Name            string
	Namespace       string
	Image           string
	Replicas        int32
	Labels          map[string]string
	Annotations     map[string]string
	Env             []corev1.EnvVar
	Ports           []int32
	CPURequest      string
	MemoryRequest   string
	CPULimit        string
	MemoryLimit     string
	ConfigMapName   string
	ConfigMountPath string
	SecretName      string
	Command         []string
	Args            []string

	ImagePullPolicy    corev1.PullPolicy
	Strategy           appsv1.DeploymentStrategyType
	ServiceAccountName string
	HealthPath         string
	RunAsUser          *int64
	RunAsNonRoot       *bool
	FSGroup            *int64
}

// NewDeployment builds a Kubernetes Deployment from the given options.
func NewDeployment(opts DeploymentOpts) *appsv1.Deployment {
	if opts.Replicas == 0 {
		opts.Replicas = 1
	}
	if opts.Labels == nil {
		opts.Labels = map[string]string{
			"app": opts.Name,
		}
	}

	container := corev1.Container{
		Name:            opts.Name,
		Image:           opts.Image,
		Env:             opts.Env,
		ImagePullPolicy: opts.ImagePullPolicy,
	}
	if len(opts.Command) > 0 {
		container.Command = opts.Command
	}
	if len(opts.Args) > 0 {
		container.Args = opts.Args
	}

	for _, p := range opts.Ports {
		container.Ports = append(container.Ports, corev1.ContainerPort{
			ContainerPort: p,
			Protocol:      corev1.ProtocolTCP,
		})
	}

	container.Resources = buildResources(opts)

	healthPath := opts.HealthPath
	if healthPath == "" {
		healthPath = "/healthz"
	}
	if len(opts.Ports) > 0 {
		container.LivenessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: healthPath,
					Port: intstr.FromInt32(opts.Ports[0]),
				},
			},
			InitialDelaySeconds: 15,
			PeriodSeconds:       20,
			TimeoutSeconds:      5,
		}
		container.ReadinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: healthPath,
					Port: intstr.FromInt32(opts.Ports[0]),
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
			TimeoutSeconds:      3,
		}
	}

	podSpec := corev1.PodSpec{
		Containers:         []corev1.Container{container},
		ServiceAccountName: opts.ServiceAccountName,
	}
	if opts.RunAsUser != nil || opts.RunAsNonRoot != nil || opts.FSGroup != nil {
		podSpec.SecurityContext = &corev1.PodSecurityContext{
			RunAsUser:    opts.RunAsUser,
			RunAsNonRoot: opts.RunAsNonRoot,
			FSGroup:      opts.FSGroup,
		}
	}

	if opts.ConfigMapName != "" {
		mountPath := opts.ConfigMountPath
		if mountPath == "" {
			mountPath = "/etc/workflow"
		}
		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: opts.ConfigMapName},
				},
			},
		})
		podSpec.Containers[0].VolumeMounts = append(podSpec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name:      "config",
			MountPath: mountPath,
			ReadOnly:  true,
		})
	}

	if opts.SecretName != "" {
		podSpec.Containers[0].EnvFrom = append(podSpec.Containers[0].EnvFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: opts.SecretName},
			},
		})
	}

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{
			Name: opts.Name, Namespace: opts.Namespace,
			Labels: opts.Labels, Annotations: opts.Annotations,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &opts.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": opts.Name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: opts.Labels},
				Spec:       podSpec,
			},
			Strategy: buildStrategy(opts.Strategy),
		},
	}
}

func buildResources(opts DeploymentOpts) corev1.ResourceRequirements {
	reqs := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{},
		Limits:   corev1.ResourceList{},
	}
	cpuReq := opts.CPURequest
	if cpuReq == "" {
		cpuReq = "100m"
	}
	memReq := opts.MemoryRequest
	if memReq == "" {
		memReq = "128Mi"
	}
	reqs.Requests[corev1.ResourceCPU] = resource.MustParse(cpuReq)
	reqs.Requests[corev1.ResourceMemory] = resource.MustParse(memReq)
	if opts.CPULimit != "" {
		reqs.Limits[corev1.ResourceCPU] = resource.MustParse(opts.CPULimit)
	}
	memLimit := opts.MemoryLimit
	if memLimit == "" {
		memLimit = "512Mi"
	}
	memLimitParsed := resource.MustParse(memLimit)
	memReqParsed := reqs.Requests[corev1.ResourceMemory]
	if memReqParsed.Cmp(memLimitParsed) > 0 {
		memLimitParsed = memReqParsed.DeepCopy()
	}
	reqs.Limits[corev1.ResourceMemory] = memLimitParsed
	return reqs
}

func buildStrategy(strategyType appsv1.DeploymentStrategyType) appsv1.DeploymentStrategy {
	if strategyType == appsv1.RecreateDeploymentStrategyType {
		return appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
	}
	return appsv1.DeploymentStrategy{
		Type: appsv1.RollingUpdateDeploymentStrategyType,
		RollingUpdate: &appsv1.RollingUpdateDeployment{
			MaxUnavailable: intstrPtr(0),
			MaxSurge:       intstrPtr(1),
		},
	}
}

func intstrPtr(val int) *intstr.IntOrString {
	v := intstr.FromInt32(int32(val))
	return &v
}
