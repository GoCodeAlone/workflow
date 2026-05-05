package main

import (
	"fmt"
	neturl "net/url"
	"path"
	"strings"
	"time"
)

// parseChecksumsTxt parses a goreleaser-style checksums.txt body into a
// map[filename → sha256hex]. Each non-empty line must be of the form
// "<sha256hex>  <filename>" (exactly two spaces — goreleaser standard).
// Empty lines and Windows \r line endings are handled. Malformed lines
// return an error to prevent silent integrity gaps.
func parseChecksumsTxt(body string) (map[string]string, error) {
	result := make(map[string]string)
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		// goreleaser format: "<sha256hex>  <filename>" (exactly two spaces between hash and name).
		idx := strings.Index(line, "  ")
		if idx < 0 {
			return nil, fmt.Errorf("malformed checksums.txt line: %q (expected \"<sha256hex>  <filename>\")", line)
		}
		hash := line[:idx]
		name := line[idx+2:]
		if hash == "" || name == "" {
			return nil, fmt.Errorf("malformed checksums.txt line: %q (empty hash or filename)", line)
		}
		// Reject 3+ spaces: name would start with a space, causing silent lookup failures.
		if strings.HasPrefix(name, " ") {
			return nil, fmt.Errorf("malformed checksums.txt line: %q (expected exactly two spaces between hash and filename)", line)
		}
		result[name] = hash
	}
	return result, nil
}

// lookupChecksumForURL derives the checksums.txt URL from a release asset download
// URL, downloads it, and returns the expected SHA256 hex string for the asset.
//
// The asset filename is derived by URL-decoding the last path segment of downloadURL
// (equivalent to url.PathUnescape(path.Base(u.Path))). This avoids passing the full
// raw URL string to path.Base and ensures URL-encoded characters in filenames are
// decoded before matching against plain-text checksums.txt entries.
//
// The checksums.txt URL is derived by replacing the last path segment of downloadURL
// with "checksums.txt" and stripping any query/fragment. For example:
//
//	https://github.com/GoCodeAlone/workflow/releases/download/v1.0.0/plugin.tar.gz
//	→ https://github.com/GoCodeAlone/workflow/releases/download/v1.0.0/checksums.txt
//
// No GitHub URL validation is performed — callers must pre-validate that the URL
// is an appropriate GitHub release URL before calling this function.
func lookupChecksumForURL(downloadURL string) (string, error) {
	u, err := neturl.Parse(downloadURL)
	if err != nil {
		return "", fmt.Errorf("parse download URL: %w", err)
	}

	// Derive asset name from URL-decoded last path segment.
	assetName, err := neturl.PathUnescape(path.Base(u.Path))
	if err != nil {
		return "", fmt.Errorf("unescape asset name from %q: %w", u.Path, err)
	}

	// Derive checksums.txt URL: replace last path segment, strip query/fragment.
	u.Path = path.Dir(u.Path) + "/checksums.txt"
	u.RawQuery = ""
	u.Fragment = ""
	checksumsURL := u.String()

	body, err := downloadWithTimeout(checksumsURL, 30*time.Second)
	if err != nil {
		return "", fmt.Errorf("fetch checksums.txt from %s: %w", checksumsURL, err)
	}

	entries, err := parseChecksumsTxt(string(body))
	if err != nil {
		return "", fmt.Errorf("parse checksums.txt from %s: %w", checksumsURL, err)
	}

	sha, ok := entries[assetName]
	if !ok {
		return "", fmt.Errorf("asset %q not listed in checksums.txt at %s", assetName, checksumsURL)
	}
	return sha, nil
}
