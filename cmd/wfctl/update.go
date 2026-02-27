package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	githubReleasesURL = "https://api.github.com/repos/GoCodeAlone/workflow/releases/latest"
	envNoUpdateCheck  = "WFCTL_NO_UPDATE_CHECK"
)

// githubReleasesURLOverride allows tests to substitute a fake server URL.
var githubReleasesURLOverride string

// githubRelease is the minimal GitHub releases API response we need.
type githubRelease struct {
	TagName string          `json:"tag_name"`
	Assets  []githubAsset   `json:"assets"`
	HTMLURL string          `json:"html_url"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// runUpdate handles the "wfctl update" command.
func runUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	checkOnly := fs.Bool("check", false, "Only check for updates without installing")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl update [options]

Download and install the latest version of wfctl, replacing the current binary.

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if version == "dev" && !*checkOnly {
		fmt.Fprintln(os.Stderr, "warning: running a dev build; update will install the latest release")
	}

	fmt.Fprintln(os.Stderr, "Checking for updates...")
	rel, err := fetchLatestRelease()
	if err != nil {
		return fmt.Errorf("check for updates: %w", err)
	}

	latest := strings.TrimPrefix(rel.TagName, "v")
	current := strings.TrimPrefix(version, "v")

	if *checkOnly {
		if current == "dev" || latest == current {
			fmt.Printf("wfctl is up to date (version %s)\n", version)
		} else {
			fmt.Printf("Update available: %s → %s\n", version, rel.TagName)
			fmt.Printf("Run 'wfctl update' to install the latest version.\n")
			fmt.Printf("Release notes: %s\n", rel.HTMLURL)
		}
		return nil
	}

	if latest == current && current != "dev" {
		fmt.Printf("wfctl %s is already the latest version.\n", version)
		return nil
	}

	asset, err := findReleaseAsset(rel.Assets)
	if err != nil {
		return fmt.Errorf("no binary found for %s/%s in release %s: %w\nVisit %s to download manually",
			runtime.GOOS, runtime.GOARCH, rel.TagName, err, rel.HTMLURL)
	}

	fmt.Fprintf(os.Stderr, "Downloading %s...\n", asset.Name)
	data, err := downloadURL(asset.BrowserDownloadURL)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}

	// If it's an archive, extract it.
	var binaryData []byte
	if strings.HasSuffix(asset.Name, ".tar.gz") {
		binaryData, err = extractBinaryFromTarGz(data, "wfctl")
		if err != nil {
			return fmt.Errorf("extract binary from archive: %w", err)
		}
	} else {
		binaryData = data
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find current executable: %w", err)
	}
	// Resolve symlinks so we replace the real binary.
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	if err := replaceBinary(execPath, binaryData); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}

	fmt.Printf("wfctl updated to %s\n", rel.TagName)
	return nil
}

// checkForUpdateNotice prints an update notice to stderr if a newer version is
// available. The check is performed in a background goroutine and the result is
// printed before the main command returns. It skips gracefully on any error or
// when WFCTL_NO_UPDATE_CHECK is set.
func checkForUpdateNotice() {
	if os.Getenv(envNoUpdateCheck) != "" {
		return
	}
	if version == "dev" {
		return
	}

	type result struct {
		rel *githubRelease
		err error
	}
	ch := make(chan result, 1)
	go func() {
		r, e := fetchLatestRelease()
		ch <- result{r, e}
	}()

	// Wait up to 2 seconds so we never meaningfully delay command execution.
	select {
	case res := <-ch:
		if res.err != nil || res.rel == nil {
			return
		}
		latest := strings.TrimPrefix(res.rel.TagName, "v")
		current := strings.TrimPrefix(version, "v")
		if latest != "" && latest != current {
			fmt.Fprintf(os.Stderr, "\n⚡ wfctl %s is available (you have %s). Run 'wfctl update' to upgrade.\n\n", res.rel.TagName, version)
		}
	case <-time.After(2 * time.Second):
		// Timed out – proceed silently.
	}
}

// fetchLatestRelease queries the GitHub releases API for the latest release.
func fetchLatestRelease() (*githubRelease, error) {
	url := githubReleasesURL
	if githubReleasesURLOverride != "" {
		url = githubReleasesURLOverride
	}
	req, err := http.NewRequest(http.MethodGet, url, nil) //nolint:noctx // no context needed for a quick check
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "wfctl/"+version)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("no releases found")
	}
	return &rel, nil
}

// findReleaseAsset locates the wfctl binary asset for the current OS and arch.
// It tries several naming conventions used by GoReleaser.
func findReleaseAsset(assets []githubAsset) (*githubAsset, error) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	// Candidate names in preference order.
	candidates := []string{
		fmt.Sprintf("wfctl-%s-%s", goos, goarch),
		fmt.Sprintf("wfctl-%s-%s.tar.gz", goos, goarch),
		fmt.Sprintf("wfctl_%s_%s", goos, goarch),
		fmt.Sprintf("wfctl_%s_%s.tar.gz", goos, goarch),
	}
	if goos == "windows" {
		candidates = append(
			[]string{
				fmt.Sprintf("wfctl-%s-%s.exe", goos, goarch),
				fmt.Sprintf("wfctl_%s_%s.exe", goos, goarch),
			},
			candidates...,
		)
	}

	for _, name := range candidates {
		for i := range assets {
			if strings.EqualFold(assets[i].Name, name) {
				return &assets[i], nil
			}
		}
	}
	return nil, fmt.Errorf("no matching asset for %s/%s", goos, goarch)
}

// replaceBinary writes newData to execPath atomically by writing to a temp file
// first and then renaming it over the original.
func replaceBinary(execPath string, newData []byte) error {
	dir := filepath.Dir(execPath)
	tmp, err := os.CreateTemp(dir, ".wfctl-update-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName) // clean up if rename failed
	}()

	if _, err := tmp.Write(newData); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Chmod(0755); err != nil { //nolint:gosec // G302: executable needs 0755
		tmp.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, execPath); err != nil { //nolint:gosec // G703: execPath comes from os.Executable()+EvalSymlinks, tmpName from os.CreateTemp in the same dir
		return fmt.Errorf("replace binary: %w", err)
	}
	return nil
}

// extractBinaryFromTarGz extracts a named binary from a .tar.gz archive.
// It searches for the first entry whose base name matches binaryName (case-insensitive,
// with or without a .exe extension on Windows).
func extractBinaryFromTarGz(data []byte, binaryName string) ([]byte, error) {
	tmpDir, err := os.MkdirTemp("", "wfctl-update-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	// Re-use extractTarGz from plugin_install.go to avoid duplicating decompression logic.
	if err := extractTarGz(data, tmpDir); err != nil {
		return nil, err
	}

	// Walk the extracted directory looking for the binary.
	var found string
	if walkErr := filepath.Walk(tmpDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		base := strings.ToLower(info.Name())
		target := strings.ToLower(binaryName)
		if base == target || base == target+".exe" {
			found = path
		}
		return nil
	}); walkErr != nil {
		return nil, walkErr
	}

	if found == "" {
		return nil, fmt.Errorf("binary %q not found in archive", binaryName)
	}

	return os.ReadFile(found) //nolint:gosec // G304: path is within our own temp dir
}


