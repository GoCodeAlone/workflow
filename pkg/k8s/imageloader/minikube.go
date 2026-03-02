package imageloader

import (
	"fmt"
	"os"
	"os/exec"
)

// MinikubeLoader loads images into a minikube cluster.
type MinikubeLoader struct{}

// NewMinikube creates a new MinikubeLoader.
func NewMinikube() *MinikubeLoader { return &MinikubeLoader{} }

func (m *MinikubeLoader) Type() Runtime { return RuntimeMinikube }

func (m *MinikubeLoader) Validate() error {
	if _, err := exec.LookPath("minikube"); err != nil {
		return fmt.Errorf("minikube not found in PATH")
	}
	return nil
}

func (m *MinikubeLoader) Load(cfg *LoadConfig) error {
	fmt.Printf("loading image %s into minikube...\n", cfg.Image)
	cmd := exec.Command("minikube", "image", "load", cfg.Image) //nolint:gosec // G204: image is validated CLI input
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("minikube image load: %w", err)
	}
	return nil
}
