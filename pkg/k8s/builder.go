package k8s

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"gopkg.in/yaml.v3"

	"github.com/GoCodeAlone/workflow/deploy"
	"github.com/GoCodeAlone/workflow/pkg/k8s/resources"
)

// Build produces a ManifestSet from a DeployRequest.
func Build(req *deploy.DeployRequest) (*ManifestSet, error) {
	if req.AppName == "" {
		return nil, fmt.Errorf("appName is required")
	}
	if req.Image == "" {
		return nil, fmt.Errorf("image is required")
	}

	ns := req.Namespace
	if ns == "" {
		ns = "default"
	}

	ms := &ManifestSet{}

	// 1. Namespace (skip "default")
	if ns != "default" {
		nsObj := resources.NewNamespace(ns, nil)
		if err := ms.AddRuntime(nsObj); err != nil {
			return nil, fmt.Errorf("add namespace: %w", err)
		}
	}

	// 2. ConfigMap from workflow config
	cmName := req.ConfigMapName
	if cmName == "" {
		cmName = req.AppName
	}
	if req.ConfigFileData != nil || req.Config != nil {
		var configYAML string
		if req.ConfigFileData != nil {
			configYAML = string(req.ConfigFileData)
		} else {
			data, err := yaml.Marshal(req.Config)
			if err != nil {
				return nil, fmt.Errorf("marshal workflow config: %w", err)
			}
			configYAML = string(data)
		}
		cm := resources.NewConfigMap(resources.ConfigMapOpts{
			Name:      cmName,
			Namespace: ns,
			Data:      map[string]string{"app.yaml": configYAML},
		})
		if err := ms.AddRuntime(cm); err != nil {
			return nil, fmt.Errorf("add configmap: %w", err)
		}
	}

	// 3. Sidecar ConfigMaps (e.g., tailscale serve config)
	for _, sc := range req.Sidecars {
		if sc == nil || sc.K8s == nil || len(sc.K8s.ConfigMapData) == 0 {
			continue
		}
		// Find the ConfigMap name from the sidecar's volumes
		scCMName := sc.Name + "-config"
		for _, v := range sc.K8s.Volumes {
			if v.ConfigMap != "" {
				scCMName = v.ConfigMap
				break
			}
		}
		scCM := resources.NewConfigMap(resources.ConfigMapOpts{
			Name:      scCMName,
			Namespace: ns,
			Data:      sc.K8s.ConfigMapData,
		})
		if err := ms.AddRuntime(scCM); err != nil {
			return nil, fmt.Errorf("add sidecar configmap %s: %w", sc.Name, err)
		}
	}

	// 4. Secret template (if SecretRef is specified)
	if req.SecretRef != "" {
		secret := resources.NewSecret(resources.SecretOpts{
			Name:      req.SecretRef,
			Namespace: ns,
			StringData: map[string]string{
				"DATABASE_URL": "CHANGE_ME",
				"JWT_SECRET":   "CHANGE_ME",
			},
		})
		if err := ms.AddRuntime(secret); err != nil {
			return nil, fmt.Errorf("add secret: %w", err)
		}
	}

	// 5. PVCs for databases
	if req.Manifest != nil {
		for _, db := range req.Manifest.Databases {
			storageSize := fmt.Sprintf("%dMi", db.EstCapacityMB)
			if db.EstCapacityMB == 0 {
				storageSize = "1Gi"
			}
			pvc := resources.NewPVC(resources.PVCOpts{
				Name:        req.AppName + "-" + db.ModuleName + "-data",
				Namespace:   ns,
				StorageSize: storageSize,
			})
			if err := ms.AddRuntime(pvc); err != nil {
				return nil, fmt.Errorf("add pvc for %s: %w", db.ModuleName, err)
			}
		}
	}

	// 6. Deployment
	replicas := int32(req.Replicas)
	if replicas == 0 {
		replicas = 1
	}

	var ports []int32
	if req.Manifest != nil {
		for _, p := range req.Manifest.Ports {
			ports = append(ports, int32(p.Port))
		}
	}
	if len(ports) == 0 {
		ports = []int32{8080}
	}

	var env []corev1.EnvVar
	for k, v := range req.ExtraEnv {
		env = append(env, corev1.EnvVar{Name: k, Value: v})
	}

	var cpuReq, memReq string
	if req.Manifest != nil {
		cpuReq = fmt.Sprintf("%dm", int(req.Manifest.ResourceEst.CPUCores*1000))
		memReq = fmt.Sprintf("%dMi", req.Manifest.ResourceEst.MemoryMB)
	}

	// Resolve args: explicit > default
	args := req.Args
	if len(args) == 0 {
		args = []string{"-config", "/etc/workflow/app.yaml"}
	}

	// Resolve image pull policy
	var pullPolicy corev1.PullPolicy
	switch req.ImagePullPolicy {
	case "Never":
		pullPolicy = corev1.PullNever
	case "Always":
		pullPolicy = corev1.PullAlways
	case "IfNotPresent":
		pullPolicy = corev1.PullIfNotPresent
	}

	// Resolve strategy
	var strategy appsv1.DeploymentStrategyType
	if req.Strategy == "Recreate" {
		strategy = appsv1.RecreateDeploymentStrategyType
	}

	deployOpts := resources.DeploymentOpts{
		Name:               req.AppName,
		Namespace:          ns,
		Image:              req.Image,
		Replicas:           replicas,
		Ports:              ports,
		Env:                env,
		CPURequest:         cpuReq,
		MemoryRequest:      memReq,
		ConfigMapName:      cmName,
		ConfigMountPath:    "/etc/workflow",
		SecretName:         req.SecretRef,
		Command:            req.Command,
		Args:               args,
		ImagePullPolicy:    pullPolicy,
		Strategy:           strategy,
		ServiceAccountName: req.ServiceAccount,
		HealthPath:         req.HealthPath,
		RunAsUser:          req.RunAsUser,
		RunAsNonRoot:       req.RunAsNonRoot,
		FSGroup:            req.FSGroup,
	}

	dep := resources.NewDeployment(deployOpts)

	// Inject sidecars
	if len(req.Sidecars) > 0 {
		resources.InjectSidecars(&dep.Spec.Template.Spec, req.Sidecars)
	}

	if err := ms.AddRuntime(dep); err != nil {
		return nil, fmt.Errorf("add deployment: %w", err)
	}

	// 7. Service
	var svcPorts []resources.ServicePort
	for i, p := range ports {
		name := "http"
		if i > 0 {
			name = fmt.Sprintf("http-%d", i)
		}
		svcPorts = append(svcPorts, resources.ServicePort{
			Name:       name,
			Port:       p,
			TargetPort: p,
		})
	}

	svc := resources.NewService(resources.ServiceOpts{
		Name:      req.AppName,
		Namespace: ns,
		Ports:     svcPorts,
	})
	if err := ms.AddRuntime(svc); err != nil {
		return nil, fmt.Errorf("add service: %w", err)
	}

	ms.Sort()
	return ms, nil
}
