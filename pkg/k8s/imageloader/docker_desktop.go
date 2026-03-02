package imageloader

import (
	"fmt"
	"os/exec"
)

// DockerDesktopLoader handles image loading for Docker Desktop's built-in Kubernetes.
// Docker Desktop shares its Docker daemon with Kubernetes, so locally built
// images are already available to the cluster — no loading step required.
type DockerDesktopLoader struct{}

// NewDockerDesktop creates a new DockerDesktopLoader.
func NewDockerDesktop() *DockerDesktopLoader { return &DockerDesktopLoader{} }

func (d *DockerDesktopLoader) Type() Runtime { return RuntimeDockerDesktop }

func (d *DockerDesktopLoader) Validate() error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker not found in PATH")
	}
	return nil
}

func (d *DockerDesktopLoader) Load(cfg *LoadConfig) error {
	fmt.Printf("image %s is available via shared Docker Desktop daemon (no loading needed)\n", cfg.Image)
	return nil
}
