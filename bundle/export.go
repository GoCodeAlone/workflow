package bundle

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"gopkg.in/yaml.v3"
)

// wellKnownDirs are directories that are always included in a bundle if they exist.
var wellKnownDirs = []string{"components", "spa", "seed", "plugins"}

// excludedDirs are directories that are never included in a bundle (runtime state).
var excludedDirs = []string{"data"}

// isExcluded returns true if the relative path falls under an excluded directory.
func isExcluded(relPath string) bool {
	for _, dir := range excludedDirs {
		if relPath == dir || strings.HasPrefix(relPath, dir+"/") {
			return true
		}
	}
	return false
}

// Export creates a tar.gz bundle from a workflow's YAML content and workspace directory.
func Export(yamlContent string, workspaceDir string, w io.Writer) error {
	gw := gzip.NewWriter(w)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	// Parse config to scan for referenced paths and extract name
	cfg, _ := config.LoadFromString(yamlContent)

	name := extractName(yamlContent)

	// Collect files to include
	files := make(map[string]bool)

	// Add well-known directories
	if workspaceDir != "" {
		for _, dir := range wellKnownDirs {
			dirPath := filepath.Join(workspaceDir, dir)
			if info, err := os.Stat(dirPath); err == nil && info.IsDir() {
				_ = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
					if err != nil {
						return err
					}
					rel, _ := filepath.Rel(workspaceDir, path)
					if !info.IsDir() {
						files[rel] = true
					}
					return nil
				})
			}
		}

		// Add explicitly referenced paths (excluding runtime dirs like data/)
		if cfg != nil {
			for _, p := range ScanReferencedPaths(cfg) {
				if isExcluded(p) {
					continue
				}
				absPath := filepath.Join(workspaceDir, p)
				info, err := os.Stat(absPath)
				if err != nil {
					continue
				}
				if info.IsDir() {
					_ = filepath.Walk(absPath, func(path string, fi os.FileInfo, err error) error {
						if err != nil {
							return err
						}
						if fi.IsDir() {
							return nil
						}
						rel, _ := filepath.Rel(workspaceDir, path)
						if !isExcluded(rel) {
							files[rel] = true
						}
						return nil
					})
				} else {
					files[filepath.Clean(p)] = true
				}
			}
		}
	}

	// Build manifest
	manifest := Manifest{
		Version: BundleFormatVersion,
		Name:    name,
	}
	if cfg != nil {
		manifest.Requires = cfg.Requires
	}
	manifest.Files = []string{"workflow.yaml"}
	for f := range files {
		manifest.Files = append(manifest.Files, f)
	}

	// Write manifest.json
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := writeToTar(tw, "manifest.json", manifestJSON); err != nil {
		return err
	}

	// Write workflow.yaml
	if err := writeToTar(tw, "workflow.yaml", []byte(yamlContent)); err != nil {
		return err
	}

	// Write workspace files
	for relPath := range files {
		if workspaceDir == "" {
			continue
		}
		absPath := filepath.Join(workspaceDir, relPath)
		data, err := os.ReadFile(absPath)
		if err != nil {
			continue // skip files that can't be read
		}
		if err := writeToTar(tw, relPath, data); err != nil {
			return err
		}
	}

	return nil
}

// extractName attempts to pull a workflow name from the YAML content.
// It checks for a top-level "name" field first, then falls back to parsing
// the first comment line (e.g., "# Chat Platform - Monolith Configuration").
func extractName(yamlContent string) string {
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(yamlContent), &raw); err == nil {
		if name, ok := raw["name"].(string); ok && name != "" {
			return name
		}
	}

	// Try to extract from first comment line
	for _, line := range strings.Split(yamlContent, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			comment := strings.TrimSpace(strings.TrimPrefix(line, "#"))
			// Strip common suffixes like " - Configuration", " - Monolith Configuration"
			if idx := strings.Index(comment, " - "); idx > 0 {
				comment = comment[:idx]
			}
			if comment != "" {
				return comment
			}
		}
		if line != "" && !strings.HasPrefix(line, "#") {
			break // stop at first non-comment, non-empty line
		}
	}

	return "workflow"
}

func writeToTar(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{
		Name: name,
		Mode: 0644,
		Size: int64(len(data)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("write tar header for %s: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("write tar data for %s: %w", name, err)
	}
	return nil
}
