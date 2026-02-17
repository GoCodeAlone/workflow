package dockercompose

import (
	"fmt"
	"sort"
	"strings"
)

// ComposeFile represents the top-level structure of a docker-compose.yml file.
type ComposeFile struct {
	// Services maps service names to their definitions.
	Services map[string]*ComposeService `yaml:"services" json:"services"`

	// Networks maps network names to their definitions.
	Networks map[string]*ComposeNetwork `yaml:"networks,omitempty" json:"networks,omitempty"`

	// Volumes maps volume names to their definitions.
	Volumes map[string]*ComposeVolume `yaml:"volumes,omitempty" json:"volumes,omitempty"`
}

// ComposeService represents a single service in docker-compose.yml.
type ComposeService struct {
	// Image is the Docker image to use.
	Image string `yaml:"image" json:"image"`

	// ContainerName is an explicit container name.
	ContainerName string `yaml:"container_name,omitempty" json:"container_name,omitempty"`

	// Ports maps host ports to container ports.
	Ports []string `yaml:"ports,omitempty" json:"ports,omitempty"`

	// Environment holds environment variable definitions.
	Environment map[string]string `yaml:"environment,omitempty" json:"environment,omitempty"`

	// Volumes are volume mounts for the service.
	Volumes []string `yaml:"volumes,omitempty" json:"volumes,omitempty"`

	// Networks lists the networks this service is attached to.
	Networks []string `yaml:"networks,omitempty" json:"networks,omitempty"`

	// DependsOn lists services that must start before this one.
	DependsOn []string `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`

	// Deploy holds deployment configuration like replicas and resource limits.
	Deploy *DeployConfig `yaml:"deploy,omitempty" json:"deploy,omitempty"`

	// Healthcheck defines a container health check.
	Healthcheck *HealthcheckConfig `yaml:"healthcheck,omitempty" json:"healthcheck,omitempty"`

	// Restart is the restart policy.
	Restart string `yaml:"restart,omitempty" json:"restart,omitempty"`

	// Command overrides the default container command.
	Command string `yaml:"command,omitempty" json:"command,omitempty"`

	// Labels are metadata key-value pairs applied to the container.
	Labels map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

// DeployConfig holds deployment-related configuration.
type DeployConfig struct {
	// Replicas is the number of container instances.
	Replicas int `yaml:"replicas,omitempty" json:"replicas,omitempty"`

	// Resources defines resource limits and reservations.
	Resources *ResourcesConfig `yaml:"resources,omitempty" json:"resources,omitempty"`
}

// ResourcesConfig holds resource limits and reservations.
type ResourcesConfig struct {
	// Limits are the maximum resource constraints.
	Limits *ResourceSpec `yaml:"limits,omitempty" json:"limits,omitempty"`

	// Reservations are the minimum guaranteed resources.
	Reservations *ResourceSpec `yaml:"reservations,omitempty" json:"reservations,omitempty"`
}

// ResourceSpec defines CPU and memory resource values.
type ResourceSpec struct {
	// CPUs is the CPU limit (e.g., "0.5", "2").
	CPUs string `yaml:"cpus,omitempty" json:"cpus,omitempty"`

	// Memory is the memory limit (e.g., "512M", "1G").
	Memory string `yaml:"memory,omitempty" json:"memory,omitempty"`
}

// HealthcheckConfig defines the container health check.
type HealthcheckConfig struct {
	// Test is the health check command.
	Test []string `yaml:"test" json:"test"`

	// Interval is the time between health checks.
	Interval string `yaml:"interval,omitempty" json:"interval,omitempty"`

	// Timeout is the maximum time for a single check.
	Timeout string `yaml:"timeout,omitempty" json:"timeout,omitempty"`

	// Retries is the number of consecutive failures before unhealthy.
	Retries int `yaml:"retries,omitempty" json:"retries,omitempty"`

	// StartPeriod is the time to wait before starting checks.
	StartPeriod string `yaml:"start_period,omitempty" json:"start_period,omitempty"`
}

// ComposeNetwork represents a Docker Compose network definition.
type ComposeNetwork struct {
	// Driver is the network driver (e.g., "bridge", "overlay").
	Driver string `yaml:"driver,omitempty" json:"driver,omitempty"`

	// External indicates whether the network is externally managed.
	External bool `yaml:"external,omitempty" json:"external,omitempty"`

	// Labels are metadata key-value pairs.
	Labels map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`

	// DriverOpts holds driver-specific options.
	DriverOpts map[string]string `yaml:"driver_opts,omitempty" json:"driver_opts,omitempty"`

	// IPAM configures IP address management.
	IPAM *IPAMConfig `yaml:"ipam,omitempty" json:"ipam,omitempty"`
}

// IPAMConfig holds IP address management configuration.
type IPAMConfig struct {
	// Driver is the IPAM driver.
	Driver string `yaml:"driver,omitempty" json:"driver,omitempty"`

	// Config holds per-subnet configuration.
	Config []IPAMPoolConfig `yaml:"config,omitempty" json:"config,omitempty"`
}

// IPAMPoolConfig defines a single IPAM subnet pool.
type IPAMPoolConfig struct {
	// Subnet is the subnet CIDR.
	Subnet string `yaml:"subnet,omitempty" json:"subnet,omitempty"`
}

// ComposeVolume represents a Docker Compose volume definition.
type ComposeVolume struct {
	// Driver is the volume driver.
	Driver string `yaml:"driver,omitempty" json:"driver,omitempty"`

	// External indicates whether the volume is externally managed.
	External bool `yaml:"external,omitempty" json:"external,omitempty"`

	// Labels are metadata key-value pairs.
	Labels map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`

	// DriverOpts holds driver-specific options.
	DriverOpts map[string]string `yaml:"driver_opts,omitempty" json:"driver_opts,omitempty"`
}

// NewComposeFile creates an empty ComposeFile with initialized maps.
func NewComposeFile() *ComposeFile {
	return &ComposeFile{
		Services: make(map[string]*ComposeService),
		Networks: make(map[string]*ComposeNetwork),
		Volumes:  make(map[string]*ComposeVolume),
	}
}

// AddService adds a service to the compose file.
func (cf *ComposeFile) AddService(name string, svc *ComposeService) {
	if cf.Services == nil {
		cf.Services = make(map[string]*ComposeService)
	}
	cf.Services[name] = svc
}

// AddNetwork adds a network to the compose file.
func (cf *ComposeFile) AddNetwork(name string, net *ComposeNetwork) {
	if cf.Networks == nil {
		cf.Networks = make(map[string]*ComposeNetwork)
	}
	cf.Networks[name] = net
}

// AddVolume adds a volume to the compose file.
func (cf *ComposeFile) AddVolume(name string, vol *ComposeVolume) {
	if cf.Volumes == nil {
		cf.Volumes = make(map[string]*ComposeVolume)
	}
	cf.Volumes[name] = vol
}

// MarshalYAML produces a valid docker-compose.yml representation as a string.
// It uses manual marshaling to control output order and avoid external YAML
// dependencies (stdlib only).
func (cf *ComposeFile) MarshalYAML() (string, error) {
	var b strings.Builder

	// Services
	if len(cf.Services) > 0 {
		b.WriteString("services:\n")
		for _, name := range sortedKeys(cf.Services) {
			svc := cf.Services[name]
			writeService(&b, name, svc, "  ")
		}
	}

	// Networks
	if len(cf.Networks) > 0 {
		b.WriteString("\nnetworks:\n")
		for _, name := range sortedNetworkKeys(cf.Networks) {
			net := cf.Networks[name]
			writeNetwork(&b, name, net, "  ")
		}
	}

	// Volumes
	if len(cf.Volumes) > 0 {
		b.WriteString("\nvolumes:\n")
		for _, name := range sortedVolumeKeys(cf.Volumes) {
			vol := cf.Volumes[name]
			writeVolume(&b, name, vol, "  ")
		}
	}

	return b.String(), nil
}

func writeService(b *strings.Builder, name string, svc *ComposeService, indent string) {
	fmt.Fprintf(b,"%s%s:\n", indent, name)
	inner := indent + "  "

	fmt.Fprintf(b,"%simage: %s\n", inner, svc.Image)

	if svc.ContainerName != "" {
		fmt.Fprintf(b,"%scontainer_name: %s\n", inner, svc.ContainerName)
	}

	if svc.Command != "" {
		fmt.Fprintf(b,"%scommand: %s\n", inner, svc.Command)
	}

	if svc.Restart != "" {
		fmt.Fprintf(b,"%srestart: %s\n", inner, svc.Restart)
	}

	if len(svc.Ports) > 0 {
		fmt.Fprintf(b,"%sports:\n", inner)
		for _, p := range svc.Ports {
			fmt.Fprintf(b,"%s  - \"%s\"\n", inner, p)
		}
	}

	if len(svc.Environment) > 0 {
		fmt.Fprintf(b,"%senvironment:\n", inner)
		for _, k := range sortedStringKeys(svc.Environment) {
			fmt.Fprintf(b,"%s  %s: \"%s\"\n", inner, k, svc.Environment[k])
		}
	}

	if len(svc.Volumes) > 0 {
		fmt.Fprintf(b,"%svolumes:\n", inner)
		for _, v := range svc.Volumes {
			fmt.Fprintf(b,"%s  - %s\n", inner, v)
		}
	}

	if len(svc.Networks) > 0 {
		fmt.Fprintf(b,"%snetworks:\n", inner)
		for _, n := range svc.Networks {
			fmt.Fprintf(b,"%s  - %s\n", inner, n)
		}
	}

	if len(svc.DependsOn) > 0 {
		fmt.Fprintf(b,"%sdepends_on:\n", inner)
		for _, d := range svc.DependsOn {
			fmt.Fprintf(b,"%s  - %s\n", inner, d)
		}
	}

	if svc.Deploy != nil {
		fmt.Fprintf(b,"%sdeploy:\n", inner)
		depInner := inner + "  "
		if svc.Deploy.Replicas > 0 {
			fmt.Fprintf(b,"%sreplicas: %d\n", depInner, svc.Deploy.Replicas)
		}
		if svc.Deploy.Resources != nil {
			fmt.Fprintf(b,"%sresources:\n", depInner)
			resInner := depInner + "  "
			if svc.Deploy.Resources.Limits != nil {
				fmt.Fprintf(b,"%slimits:\n", resInner)
				limInner := resInner + "  "
				if svc.Deploy.Resources.Limits.CPUs != "" {
					fmt.Fprintf(b,"%scpus: \"%s\"\n", limInner, svc.Deploy.Resources.Limits.CPUs)
				}
				if svc.Deploy.Resources.Limits.Memory != "" {
					fmt.Fprintf(b,"%smemory: %s\n", limInner, svc.Deploy.Resources.Limits.Memory)
				}
			}
			if svc.Deploy.Resources.Reservations != nil {
				fmt.Fprintf(b,"%sreservations:\n", resInner)
				resrInner := resInner + "  "
				if svc.Deploy.Resources.Reservations.CPUs != "" {
					fmt.Fprintf(b,"%scpus: \"%s\"\n", resrInner, svc.Deploy.Resources.Reservations.CPUs)
				}
				if svc.Deploy.Resources.Reservations.Memory != "" {
					fmt.Fprintf(b,"%smemory: %s\n", resrInner, svc.Deploy.Resources.Reservations.Memory)
				}
			}
		}
	}

	if svc.Healthcheck != nil {
		fmt.Fprintf(b,"%shealthcheck:\n", inner)
		hcInner := inner + "  "
		if len(svc.Healthcheck.Test) > 0 {
			fmt.Fprintf(b,"%stest: [", hcInner)
			for i, t := range svc.Healthcheck.Test {
				if i > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(b,"\"%s\"", t)
			}
			b.WriteString("]\n")
		}
		if svc.Healthcheck.Interval != "" {
			fmt.Fprintf(b,"%sinterval: %s\n", hcInner, svc.Healthcheck.Interval)
		}
		if svc.Healthcheck.Timeout != "" {
			fmt.Fprintf(b,"%stimeout: %s\n", hcInner, svc.Healthcheck.Timeout)
		}
		if svc.Healthcheck.Retries > 0 {
			fmt.Fprintf(b,"%sretries: %d\n", hcInner, svc.Healthcheck.Retries)
		}
		if svc.Healthcheck.StartPeriod != "" {
			fmt.Fprintf(b,"%sstart_period: %s\n", hcInner, svc.Healthcheck.StartPeriod)
		}
	}

	if len(svc.Labels) > 0 {
		fmt.Fprintf(b,"%slabels:\n", inner)
		for _, k := range sortedStringKeys(svc.Labels) {
			fmt.Fprintf(b,"%s  %s: \"%s\"\n", inner, k, svc.Labels[k])
		}
	}
}

func writeNetwork(b *strings.Builder, name string, net *ComposeNetwork, indent string) {
	fmt.Fprintf(b,"%s%s:\n", indent, name)
	inner := indent + "  "

	if net.Driver != "" {
		fmt.Fprintf(b,"%sdriver: %s\n", inner, net.Driver)
	}
	if net.External {
		fmt.Fprintf(b,"%sexternal: true\n", inner)
	}
	if net.IPAM != nil {
		fmt.Fprintf(b,"%sipam:\n", inner)
		ipamInner := inner + "  "
		if net.IPAM.Driver != "" {
			fmt.Fprintf(b,"%sdriver: %s\n", ipamInner, net.IPAM.Driver)
		}
		if len(net.IPAM.Config) > 0 {
			fmt.Fprintf(b,"%sconfig:\n", ipamInner)
			for _, c := range net.IPAM.Config {
				fmt.Fprintf(b,"%s  - subnet: %s\n", ipamInner, c.Subnet)
			}
		}
	}
	if len(net.Labels) > 0 {
		fmt.Fprintf(b,"%slabels:\n", inner)
		for _, k := range sortedStringKeys(net.Labels) {
			fmt.Fprintf(b,"%s  %s: \"%s\"\n", inner, k, net.Labels[k])
		}
	}
	if len(net.DriverOpts) > 0 {
		fmt.Fprintf(b,"%sdriver_opts:\n", inner)
		for _, k := range sortedStringKeys(net.DriverOpts) {
			fmt.Fprintf(b,"%s  %s: \"%s\"\n", inner, k, net.DriverOpts[k])
		}
	}
}

func writeVolume(b *strings.Builder, name string, vol *ComposeVolume, indent string) {
	fmt.Fprintf(b,"%s%s:\n", indent, name)
	inner := indent + "  "

	if vol.Driver != "" {
		fmt.Fprintf(b,"%sdriver: %s\n", inner, vol.Driver)
	}
	if vol.External {
		fmt.Fprintf(b,"%sexternal: true\n", inner)
	}
	if len(vol.Labels) > 0 {
		fmt.Fprintf(b,"%slabels:\n", inner)
		for _, k := range sortedStringKeys(vol.Labels) {
			fmt.Fprintf(b,"%s  %s: \"%s\"\n", inner, k, vol.Labels[k])
		}
	}
	if len(vol.DriverOpts) > 0 {
		fmt.Fprintf(b,"%sdriver_opts:\n", inner)
		for _, k := range sortedStringKeys(vol.DriverOpts) {
			fmt.Fprintf(b,"%s  %s: \"%s\"\n", inner, k, vol.DriverOpts[k])
		}
	}
}

func sortedKeys(m map[string]*ComposeService) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedNetworkKeys(m map[string]*ComposeNetwork) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedVolumeKeys(m map[string]*ComposeVolume) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedStringKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
