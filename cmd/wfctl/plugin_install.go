package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// defaultDataDir is the default location for installed plugin binaries.
const defaultDataDir = "data/plugins"

func runPluginSearch(args []string) error {
	fs := flag.NewFlagSet("plugin search", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl plugin search [<query>]\n\nSearch the plugin registry by name, description, or keyword.\n")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	query := ""
	if fs.NArg() > 0 {
		query = strings.Join(fs.Args(), " ")
	}

	fmt.Fprintf(os.Stderr, "Searching registry...\n")
	plugins, err := SearchPlugins(query)
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}
	if len(plugins) == 0 {
		fmt.Println("No plugins found.")
		return nil
	}
	fmt.Printf("%-20s %-10s %-12s %s\n", "NAME", "VERSION", "TIER", "DESCRIPTION")
	fmt.Printf("%-20s %-10s %-12s %s\n", "----", "-------", "----", "-----------")
	for _, p := range plugins {
		desc := p.Description
		if len(desc) > 60 {
			desc = desc[:57] + "..."
		}
		fmt.Printf("%-20s %-10s %-12s %s\n", p.Name, p.Version, p.Tier, desc)
	}
	return nil
}

func runPluginInstall(args []string) error {
	fs := flag.NewFlagSet("plugin install", flag.ContinueOnError)
	dataDir := fs.String("data-dir", defaultDataDir, "Plugin data directory")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl plugin install [options] <name>[@<version>]\n\nDownload and install a plugin from the registry.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("plugin name is required")
	}

	nameArg := fs.Arg(0)
	pluginName, _ := parseNameVersion(nameArg)

	fmt.Fprintf(os.Stderr, "Fetching manifest for %q...\n", pluginName)
	manifest, err := FetchManifest(pluginName)
	if err != nil {
		return err
	}

	dl, err := manifest.FindDownload(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}

	destDir := filepath.Join(*dataDir, pluginName)
	if err := os.MkdirAll(destDir, 0750); err != nil {
		return fmt.Errorf("create plugin dir %s: %w", destDir, err)
	}

	fmt.Fprintf(os.Stderr, "Downloading %s...\n", dl.URL)
	data, err := downloadURL(dl.URL)
	if err != nil {
		return fmt.Errorf("download plugin: %w", err)
	}

	if dl.SHA256 != "" {
		if err := verifyChecksum(data, dl.SHA256); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Checksum verified.\n")
	}

	fmt.Fprintf(os.Stderr, "Extracting to %s...\n", destDir)
	if err := extractTarGz(data, destDir); err != nil {
		return fmt.Errorf("extract plugin: %w", err)
	}

	// Write a minimal plugin.json if not already present (records version).
	pluginJSONPath := filepath.Join(destDir, "plugin.json")
	if _, err := os.Stat(pluginJSONPath); os.IsNotExist(err) {
		if writeErr := writeInstalledManifest(pluginJSONPath, manifest); writeErr != nil {
			fmt.Fprintf(os.Stderr, "warning: could not write plugin.json: %v\n", writeErr)
		}
	}

	fmt.Printf("Installed %s v%s to %s\n", manifest.Name, manifest.Version, destDir)
	return nil
}

func runPluginList(args []string) error {
	fs := flag.NewFlagSet("plugin list", flag.ContinueOnError)
	dataDir := fs.String("data-dir", defaultDataDir, "Plugin data directory")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl plugin list [options]\n\nList installed plugins.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	entries, err := os.ReadDir(*dataDir)
	if os.IsNotExist(err) {
		fmt.Println("No plugins installed.")
		return nil
	}
	if err != nil {
		return fmt.Errorf("read data dir %s: %w", *dataDir, err)
	}

	type installed struct {
		name    string
		version string
	}
	var plugins []installed
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ver := readInstalledVersion(filepath.Join(*dataDir, e.Name()))
		plugins = append(plugins, installed{name: e.Name(), version: ver})
	}

	if len(plugins) == 0 {
		fmt.Println("No plugins installed.")
		return nil
	}

	fmt.Printf("%-20s %s\n", "NAME", "VERSION")
	fmt.Printf("%-20s %s\n", "----", "-------")
	for _, p := range plugins {
		fmt.Printf("%-20s %s\n", p.name, p.version)
	}
	return nil
}

func runPluginUpdate(args []string) error {
	fs := flag.NewFlagSet("plugin update", flag.ContinueOnError)
	dataDir := fs.String("data-dir", defaultDataDir, "Plugin data directory")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl plugin update [options] <name>\n\nUpdate an installed plugin to its latest version.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("plugin name is required")
	}

	pluginName := fs.Arg(0)
	pluginDir := filepath.Join(*dataDir, pluginName)
	if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
		return fmt.Errorf("plugin %q is not installed", pluginName)
	}

	// Re-run install which will overwrite the existing installation.
	return runPluginInstall(append([]string{"--data-dir", *dataDir}, pluginName))
}

func runPluginRemove(args []string) error {
	fs := flag.NewFlagSet("plugin remove", flag.ContinueOnError)
	dataDir := fs.String("data-dir", defaultDataDir, "Plugin data directory")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl plugin remove [options] <name>\n\nUninstall a plugin.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("plugin name is required")
	}

	pluginName := fs.Arg(0)
	pluginDir := filepath.Join(*dataDir, pluginName)
	if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
		return fmt.Errorf("plugin %q is not installed", pluginName)
	}
	if err := os.RemoveAll(pluginDir); err != nil {
		return fmt.Errorf("remove plugin %q: %w", pluginName, err)
	}
	fmt.Printf("Removed plugin %q\n", pluginName)
	return nil
}

// parseNameVersion splits "name@version" into (name, version). Version is empty if absent.
func parseNameVersion(arg string) (name, ver string) {
	if idx := strings.Index(arg, "@"); idx >= 0 {
		return arg[:idx], arg[idx+1:]
	}
	return arg, ""
}

// downloadURL fetches a URL and returns the body bytes.
func downloadURL(url string) ([]byte, error) {
	resp, err := http.Get(url) //nolint:gosec // G107: URL comes from registry manifest
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

// verifyChecksum checks that data matches the expected SHA256 hex string.
func verifyChecksum(data []byte, expected string) error {
	h := sha256.Sum256(data)
	got := hex.EncodeToString(h[:])
	if !strings.EqualFold(got, expected) {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, expected)
	}
	return nil
}

// extractTarGz decompresses and extracts a .tar.gz archive into destDir.
// It guards against path traversal (zip-slip) attacks.
func extractTarGz(data []byte, destDir string) error {
	gzr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("open gzip: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}

		// Strip leading path component (common in tarballs: plugin-name-os-arch/binary).
		name := stripTopDir(hdr.Name)
		if name == "" || name == "." {
			continue
		}

		// Guard against path traversal.
		destPath, err := safeJoin(destDir, name)
		if err != nil {
			return err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, 0750); err != nil {
				return fmt.Errorf("mkdir %s: %w", destPath, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(destPath), 0750); err != nil {
				return fmt.Errorf("mkdir parent %s: %w", filepath.Dir(destPath), err)
			}
			mode := hdr.FileInfo().Mode()
			f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode) //nolint:gosec // G304: path validated by safeJoin
			if err != nil {
				return fmt.Errorf("create file %s: %w", destPath, err)
			}
			if _, copyErr := io.Copy(f, tr); copyErr != nil { //nolint:gosec // G110: tar size is bounded by download
				f.Close()
				return fmt.Errorf("write file %s: %w", destPath, copyErr)
			}
			f.Close()
		}
	}
	return nil
}

// stripTopDir removes the first path component from a tar entry name.
// e.g. "workflow-plugin-admin-darwin-amd64/admin.plugin" -> "admin.plugin"
func stripTopDir(name string) string {
	name = filepath.ToSlash(name)
	if idx := strings.Index(name, "/"); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

// safeJoin joins base and name, returning an error if the result escapes base.
func safeJoin(base, name string) (string, error) {
	dest := filepath.Join(base, filepath.FromSlash(name))
	rel, err := filepath.Rel(base, dest)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path traversal detected: %q", name)
	}
	return dest, nil
}

// installedPluginJSON is the minimal JSON written to plugin.json after install.
type installedPluginJSON struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// writeInstalledManifest writes a minimal plugin.json to record the installed version.
func writeInstalledManifest(path string, m *RegistryManifest) error {
	data, err := json.MarshalIndent(installedPluginJSON{Name: m.Name, Version: m.Version}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0640) //nolint:gosec // G306: plugin.json is user-owned output
}

// readInstalledVersion reads the version from a plugin.json in the given directory.
func readInstalledVersion(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return "unknown"
	}
	var m installedPluginJSON
	if err := json.Unmarshal(data, &m); err != nil {
		return "unknown"
	}
	if m.Version == "" {
		return "unknown"
	}
	return m.Version
}
