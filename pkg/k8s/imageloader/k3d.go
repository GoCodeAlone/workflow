package imageloader

import (
	"fmt"
	"os"
	"os/exec"
)

// K3dLoader loads images into a k3d cluster.
type K3dLoader struct{}

// NewK3d creates a new K3dLoader.
func NewK3d() *K3dLoader { return &K3dLoader{} }

func (k *K3dLoader) Type() Runtime { return RuntimeK3d }

func (k *K3dLoader) Validate() error {
	if _, err := exec.LookPath("k3d"); err != nil {
		return fmt.Errorf("k3d not found in PATH")
	}
	return nil
}

func (k *K3dLoader) Load(cfg *LoadConfig) error {
	cluster := cfg.Cluster
	if cluster == "" {
		cluster = "k3s-default"
	}
	fmt.Printf("loading image %s into k3d cluster %q...\n", cfg.Image, cluster)
	cmd := exec.Command("k3d", "image", "import", cfg.Image, "-c", cluster) //nolint:gosec // G204: validated CLI inputs
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("k3d image import: %w", err)
	}
	return nil
}
