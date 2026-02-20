package plugin

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/dynamic"
)

// PluginInstaller handles installing plugins from remote or local sources.
type PluginInstaller struct {
	remoteReg  *RemoteRegistry
	localReg   *LocalRegistry
	loader     *dynamic.Loader
	installDir string
}

// NewPluginInstaller creates a new plugin installer.
func NewPluginInstaller(remoteReg *RemoteRegistry, localReg *LocalRegistry, loader *dynamic.Loader, installDir string) *PluginInstaller {
	return &PluginInstaller{
		remoteReg:  remoteReg,
		localReg:   localReg,
		loader:     loader,
		installDir: installDir,
	}
}

// Install downloads and installs a plugin from the remote registry.
func (i *PluginInstaller) Install(ctx context.Context, name, version string) error {
	if i.IsInstalled(name) {
		return nil // already installed
	}

	if i.remoteReg == nil {
		return fmt.Errorf("no remote registry configured")
	}

	// Get manifest from remote
	manifest, err := i.remoteReg.GetManifest(ctx, name, version)
	if err != nil {
		return fmt.Errorf("get manifest for %s@%s: %w", name, version, err)
	}

	// Validate install directory to prevent directory traversal
	absInstallDir, err := filepath.Abs(i.installDir)
	if err != nil {
		return fmt.Errorf("resolve install directory: %w", err)
	}
	pluginDir := filepath.Join(absInstallDir, name)
	absPluginDir, err := filepath.Abs(pluginDir)
	if err != nil {
		return fmt.Errorf("resolve plugin directory: %w", err)
	}
	if !strings.HasPrefix(absPluginDir, absInstallDir+string(os.PathSeparator)) {
		return fmt.Errorf("invalid plugin name %q", name)
	}

	if err := os.MkdirAll(pluginDir, 0750); err != nil {
		return fmt.Errorf("create plugin dir: %w", err)
	}

	// Download archive from remote registry
	reader, err := i.remoteReg.Download(ctx, name, version)
	if err != nil {
		os.RemoveAll(pluginDir) // cleanup on failure
		return fmt.Errorf("download plugin %s@%s: %w", name, version, err)
	}
	defer reader.Close()

	// Save archive to disk
	archivePath := filepath.Join(pluginDir, fmt.Sprintf("%s-%s.tar.gz", name, version))
	f, err := os.Create(archivePath)
	if err != nil {
		os.RemoveAll(pluginDir)
		return fmt.Errorf("create archive file: %w", err)
	}
	if _, err := io.Copy(f, reader); err != nil {
		f.Close()
		os.RemoveAll(pluginDir)
		return fmt.Errorf("save archive: %w", err)
	}
	f.Close()

	// Extract archive
	if err := extractTarGz(archivePath, pluginDir); err != nil {
		// Archive may not be extractable; that's okay if we have the manifest
	}

	// Save manifest
	manifestPath := filepath.Join(pluginDir, "plugin.json")
	if err := SaveManifest(manifestPath, manifest); err != nil {
		os.RemoveAll(pluginDir)
		return fmt.Errorf("save manifest: %w", err)
	}

	// Register in local registry (without a component -- loaded separately)
	if i.localReg != nil {
		if err := i.localReg.Register(manifest, nil, pluginDir); err != nil {
			return fmt.Errorf("register installed plugin: %w", err)
		}
	}

	return nil
}

// InstallFromBundle installs a plugin from a local bundle directory.
// The bundle directory must contain a plugin.json manifest.
func (i *PluginInstaller) InstallFromBundle(bundlePath string) error {
	// Read plugin manifest
	manifestPath := filepath.Join(bundlePath, "plugin.json")
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("load bundle manifest: %w", err)
	}

	info, err := os.Stat(bundlePath)
	if err != nil {
		return fmt.Errorf("stat bundle: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("bundle path must be a directory")
	}

	destDir := filepath.Join(i.installDir, manifest.Name)

	// Validate destination to prevent directory traversal
	absInstallDir, err := filepath.Abs(i.installDir)
	if err != nil {
		return fmt.Errorf("resolve install directory: %w", err)
	}
	absDestDir, err := filepath.Abs(destDir)
	if err != nil {
		return fmt.Errorf("resolve destination directory: %w", err)
	}
	if !strings.HasPrefix(absDestDir, absInstallDir+string(os.PathSeparator)) {
		return fmt.Errorf("invalid plugin name %q", manifest.Name)
	}

	if err := copyDir(bundlePath, destDir); err != nil {
		return fmt.Errorf("copy plugin bundle: %w", err)
	}

	// Register in local registry
	if i.localReg != nil {
		// Attempt to load component via dynamic loader
		var comp *dynamic.DynamicComponent
		if i.loader != nil {
			sourceFiles, _ := filepath.Glob(filepath.Join(destDir, "*.go"))
			for _, sf := range sourceFiles {
				base := filepath.Base(sf)
				if strings.HasSuffix(base, "_test.go") {
					continue
				}
				c, loadErr := i.loader.LoadFromFile(manifest.Name, sf)
				if loadErr == nil {
					comp = c
					break
				}
			}
		}

		if err := i.localReg.Register(manifest, comp, destDir); err != nil {
			return fmt.Errorf("register plugin %q: %w", manifest.Name, err)
		}
	}

	return nil
}

// IsInstalled checks if a plugin is installed locally.
func (i *PluginInstaller) IsInstalled(name string) bool {
	pluginDir := filepath.Join(i.installDir, name)
	manifestPath := filepath.Join(pluginDir, "plugin.json")
	_, err := os.Stat(manifestPath)
	return err == nil
}

// Uninstall removes an installed plugin.
func (i *PluginInstaller) Uninstall(name string) error {
	pluginDir := filepath.Join(i.installDir, name)
	if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
		return fmt.Errorf("plugin %s not installed", name)
	}

	// Unregister from local registry
	if i.localReg != nil {
		_ = i.localReg.Unregister(name) // best-effort
	}

	return os.RemoveAll(pluginDir)
}

// ScanInstalled loads all previously installed plugins from the install directory.
func (i *PluginInstaller) ScanInstalled() ([]*PluginEntry, error) {
	if i.localReg == nil {
		return nil, nil
	}

	if _, err := os.Stat(i.installDir); os.IsNotExist(err) {
		return nil, nil
	}

	return i.localReg.ScanDirectory(i.installDir, i.loader)
}

// InstallDir returns the configured plugin installation directory.
func (i *PluginInstaller) InstallDir() string {
	return i.installDir
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		destPath := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(destPath, 0750)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destPath, data, info.Mode())
	})
}

// extractTarGz extracts a .tar.gz archive into a destination directory.
func extractTarGz(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(destDir, header.Name)

		// Validate path to prevent directory traversal
		absTarget, err := filepath.Abs(target)
		if err != nil {
			return fmt.Errorf("resolve target path: %w", err)
		}
		absDest, err := filepath.Abs(destDir)
		if err != nil {
			return fmt.Errorf("resolve dest dir: %w", err)
		}
		if !strings.HasPrefix(absTarget, absDest+string(os.PathSeparator)) && absTarget != absDest {
			return fmt.Errorf("archive entry %q escapes destination directory", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0750); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
				return err
			}
			out, err := os.Create(target)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
	return nil
}
