package store

import (
	"context"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
)

// LocalStorage implements StorageProvider backed by the local filesystem.
type LocalStorage struct {
	root string
}

// NewLocalStorage creates a new LocalStorage rooted at the given directory.
// The directory is created if it does not exist.
func NewLocalStorage(root string) (*LocalStorage, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root path: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("create root directory: %w", err)
	}
	return &LocalStorage{root: abs}, nil
}

// Root returns the absolute root path.
func (l *LocalStorage) Root() string {
	return l.root
}

// resolve converts a relative storage path to an absolute filesystem path,
// ensuring the result stays within the root directory.
func (l *LocalStorage) resolve(path string) (string, error) {
	clean := filepath.Clean(path)
	abs := filepath.Join(l.root, clean)
	// Prevent path traversal
	if !strings.HasPrefix(abs, l.root) {
		return "", fmt.Errorf("path %q escapes storage root", path)
	}
	return abs, nil
}

// detectContentType returns a MIME type based on file extension.
func detectContentType(name string) string {
	ext := filepath.Ext(name)
	if ext == "" {
		return ""
	}
	ct := mime.TypeByExtension(ext)
	return ct
}

func (l *LocalStorage) List(_ context.Context, prefix string) ([]FileInfo, error) {
	dir, err := l.resolve(prefix)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []FileInfo{}, nil
		}
		return nil, fmt.Errorf("read directory: %w", err)
	}

	result := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		relPath := filepath.Join(prefix, entry.Name())
		fi := FileInfo{
			Name:    entry.Name(),
			Path:    relPath,
			Size:    info.Size(),
			ModTime: info.ModTime(),
			IsDir:   entry.IsDir(),
		}
		if !entry.IsDir() {
			fi.ContentType = detectContentType(entry.Name())
		}
		result = append(result, fi)
	}
	return result, nil
}

func (l *LocalStorage) Get(_ context.Context, path string) (io.ReadCloser, error) {
	abs, err := l.resolve(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	return f, nil
}

func (l *LocalStorage) Put(_ context.Context, path string, reader io.Reader) error {
	abs, err := l.resolve(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("create parent directories: %w", err)
	}
	f, err := os.Create(abs)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, reader); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

func (l *LocalStorage) Delete(_ context.Context, path string) error {
	abs, err := l.resolve(path)
	if err != nil {
		return err
	}
	if err := os.Remove(abs); err != nil {
		return fmt.Errorf("delete file: %w", err)
	}
	return nil
}

func (l *LocalStorage) Stat(_ context.Context, path string) (FileInfo, error) {
	abs, err := l.resolve(path)
	if err != nil {
		return FileInfo{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return FileInfo{}, fmt.Errorf("stat file: %w", err)
	}
	fi := FileInfo{
		Name:    info.Name(),
		Path:    path,
		Size:    info.Size(),
		ModTime: info.ModTime(),
		IsDir:   info.IsDir(),
	}
	if !info.IsDir() {
		fi.ContentType = detectContentType(info.Name())
	}
	return fi, nil
}

// MkdirAll creates a directory path and all parents that do not exist.
func (l *LocalStorage) MkdirAll(_ context.Context, path string) error {
	abs, err := l.resolve(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	return nil
}
