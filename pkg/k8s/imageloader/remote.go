package imageloader

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RemoteLoader pushes images to a container registry for remote clusters.
type RemoteLoader struct{}

// NewRemote creates a new RemoteLoader.
func NewRemote() *RemoteLoader { return &RemoteLoader{} }

func (r *RemoteLoader) Type() Runtime { return RuntimeRemote }

func (r *RemoteLoader) Validate() error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker not found in PATH")
	}
	return nil
}

func (r *RemoteLoader) Load(cfg *LoadConfig) error {
	if cfg.Registry == "" {
		return fmt.Errorf("--registry is required for remote clusters")
	}

	// Build the registry-qualified image name.
	// If image already starts with the registry, use it as-is.
	remoteImage := cfg.Image
	if !strings.HasPrefix(cfg.Image, cfg.Registry) {
		// Extract just the image:tag part (strip any existing registry prefix)
		imageRef := cfg.Image
		parts := strings.SplitN(cfg.Image, "/", 3)
		if len(parts) > 1 && strings.Contains(parts[0], ".") {
			// Image already has a registry prefix (e.g. "ghcr.io/org/app:v1")
			imageRef = parts[len(parts)-1]
		}
		remoteImage = cfg.Registry + "/" + imageRef
	}

	// Tag the image for the registry
	fmt.Printf("tagging %s → %s\n", cfg.Image, remoteImage)
	tagCmd := exec.Command("docker", "tag", cfg.Image, remoteImage) //nolint:gosec // G204: validated CLI inputs
	tagCmd.Stdout = os.Stdout
	tagCmd.Stderr = os.Stderr
	if err := tagCmd.Run(); err != nil {
		return fmt.Errorf("docker tag: %w", err)
	}

	// Push to registry
	fmt.Printf("pushing %s...\n", remoteImage)
	pushCmd := exec.Command("docker", "push", remoteImage) //nolint:gosec // G204: validated CLI inputs
	pushCmd.Stdout = os.Stdout
	pushCmd.Stderr = os.Stderr
	if err := pushCmd.Run(); err != nil {
		return fmt.Errorf("docker push: %w", err)
	}

	cfg.ResolvedImage = remoteImage
	return nil
}
