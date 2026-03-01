package resources

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/GoCodeAlone/workflow/deploy"
)

// InjectSidecars adds resolved sidecar containers into a pod template spec.
func InjectSidecars(podSpec *corev1.PodSpec, sidecars []*deploy.SidecarSpec) {
	for _, sc := range sidecars {
		if sc == nil || sc.K8s == nil {
			continue
		}
		k := sc.K8s

		container := corev1.Container{
			Name:  sc.Name,
			Image: k.Image,
		}
		if k.ImagePullPolicy != "" {
			container.ImagePullPolicy = corev1.PullPolicy(k.ImagePullPolicy)
		}
		if len(k.Command) > 0 {
			container.Command = k.Command
		}
		if len(k.Args) > 0 {
			container.Args = k.Args
		}
		for name, value := range k.Env {
			container.Env = append(container.Env, corev1.EnvVar{Name: name, Value: value})
		}
		for _, se := range k.SecretEnv {
			container.Env = append(container.Env, corev1.EnvVar{
				Name: se.EnvName,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: se.SecretName},
						Key:                  se.SecretKey,
					},
				},
			})
		}
		for _, port := range k.Ports {
			container.Ports = append(container.Ports, corev1.ContainerPort{
				ContainerPort: port, Protocol: corev1.ProtocolTCP,
			})
		}
		for _, vm := range k.VolumeMounts {
			container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
				Name: vm.Name, MountPath: vm.MountPath, ReadOnly: vm.ReadOnly,
			})
		}
		if k.SecurityContext != nil {
			sc := &corev1.SecurityContext{
				RunAsUser:  k.SecurityContext.RunAsUser,
				RunAsGroup: k.SecurityContext.RunAsGroup,
				Privileged: k.SecurityContext.Privileged,
			}
			if k.SecurityContext.Capabilities != nil {
				sc.Capabilities = &corev1.Capabilities{}
				for _, cap := range k.SecurityContext.Capabilities.Add {
					sc.Capabilities.Add = append(sc.Capabilities.Add, corev1.Capability(cap))
				}
				for _, cap := range k.SecurityContext.Capabilities.Drop {
					sc.Capabilities.Drop = append(sc.Capabilities.Drop, corev1.Capability(cap))
				}
			}
			container.SecurityContext = sc
		}
		podSpec.Containers = append(podSpec.Containers, container)

		for _, vol := range k.Volumes {
			v := corev1.Volume{Name: vol.Name}
			switch {
			case vol.EmptyDir:
				v.VolumeSource = corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}
			case vol.Secret != "":
				v.VolumeSource = corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{SecretName: vol.Secret},
				}
			case vol.ConfigMap != "":
				v.VolumeSource = corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: vol.ConfigMap},
					},
				}
			}
			podSpec.Volumes = append(podSpec.Volumes, v)
		}

		if k.ServiceAccountName != "" && podSpec.ServiceAccountName == "" {
			podSpec.ServiceAccountName = k.ServiceAccountName
		}
	}
}
