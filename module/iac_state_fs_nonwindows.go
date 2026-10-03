//go:build !windows

package module

import "os"

func replaceIaCStateFile(from, to string) error {
	return os.Rename(from, to)
}

func syncIaCStateDirectory(dir string) error {
	parent, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}
