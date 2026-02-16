package sandbox

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
)

// createTarFromFile creates a tar archive containing a single file.
func createTarFromFile(f *os.File, stat os.FileInfo) (io.Reader, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	header := &tar.Header{
		Name: filepath.Base(stat.Name()),
		Size: stat.Size(),
		Mode: int64(stat.Mode()),
	}

	if err := tw.WriteHeader(header); err != nil {
		return nil, err
	}

	if _, err := io.Copy(tw, f); err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}

	return &buf, nil
}
