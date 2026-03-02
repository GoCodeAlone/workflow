package imageloader

import (
	"fmt"
	"os"
	"os/exec"
)

// KindLoader loads images into a kind cluster.
type KindLoader struct{}

// NewKind creates a new KindLoader.
func NewKind() *KindLoader { return &KindLoader{} }

func (k *KindLoader) Type() Runtime { return RuntimeKind }

func (k *KindLoader) Validate() error {
	if _, err := exec.LookPath("kind"); err != nil {
		return fmt.Errorf("kind not found in PATH")
	}
	return nil
}

func (k *KindLoader) Load(cfg *LoadConfig) error {
	cluster := cfg.Cluster
	if cluster == "" {
		cluster = "kind"
	}
	fmt.Printf("loading image %s into kind cluster %q...\n", cfg.Image, cluster)
	cmd := exec.Command("kind", "load", "docker-image", cfg.Image, "--name", cluster) //nolint:gosec // G204: validated CLI inputs
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kind load docker-image: %w", err)
	}
	return nil
}
