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
)

const (
	// MaxBundleSize is the maximum total size of an extracted bundle (100MB).
	MaxBundleSize = 100 * 1024 * 1024
	// MaxFileSize is the maximum size of a single file in a bundle (10MB).
	MaxFileSize = 10 * 1024 * 1024
)

// Import extracts a tar.gz bundle to the destination directory.
// Returns the manifest, path to the extracted workflow.yaml, and any error.
func Import(r io.Reader, destDir string) (*Manifest, string, error) {
	if err := os.MkdirAll(destDir, 0750); err != nil {
		return nil, "", fmt.Errorf("create dest dir: %w", err)
	}

	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, "", fmt.Errorf("open gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	var totalSize int64
	var manifest *Manifest
	workflowPath := ""

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("read tar: %w", err)
		}

		// Path traversal protection
		clean := filepath.Clean(hdr.Name)
		if strings.HasPrefix(clean, "..") || strings.HasPrefix(clean, "/") {
			return nil, "", fmt.Errorf("invalid path in bundle: %s", hdr.Name)
		}

		// Size checks
		if hdr.Size > MaxFileSize {
			return nil, "", fmt.Errorf("file %s exceeds max size (%d > %d)", hdr.Name, hdr.Size, MaxFileSize)
		}
		totalSize += hdr.Size
		if totalSize > MaxBundleSize {
			return nil, "", fmt.Errorf("bundle exceeds max total size (%d)", MaxBundleSize)
		}

		destPath := filepath.Join(destDir, clean)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, 0750); err != nil {
				return nil, "", fmt.Errorf("create dir %s: %w", clean, err)
			}
		case tar.TypeReg:
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(destPath), 0750); err != nil {
				return nil, "", fmt.Errorf("create parent dir for %s: %w", clean, err)
			}

			data, err := io.ReadAll(io.LimitReader(tr, MaxFileSize+1))
			if err != nil {
				return nil, "", fmt.Errorf("read %s: %w", clean, err)
			}
			if int64(len(data)) > MaxFileSize {
				return nil, "", fmt.Errorf("file %s exceeds max size", clean)
			}

			if err := os.WriteFile(destPath, data, 0600); err != nil {
				return nil, "", fmt.Errorf("write %s: %w", clean, err)
			}

			// Parse manifest
			if clean == "manifest.json" {
				var m Manifest
				if err := json.Unmarshal(data, &m); err != nil {
					return nil, "", fmt.Errorf("parse manifest: %w", err)
				}
				manifest = &m
			}

			if clean == "workflow.yaml" {
				workflowPath = destPath
			}
		}
	}

	if manifest == nil {
		return nil, "", fmt.Errorf("bundle missing manifest.json")
	}
	if workflowPath == "" {
		return nil, "", fmt.Errorf("bundle missing workflow.yaml")
	}

	return manifest, workflowPath, nil
}
