//go:build linux || darwin

package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type pipelineProcessIdentity struct {
	PID   int    `json:"pid"`
	PGID  int    `json:"pgid"`
	Start string `json:"start"`
}

type pipelineDockerIdentity struct {
	Endpoint string `json:"endpoint"`
	TLSHash  string `json:"tls_hash"`
	ServerID string `json:"server_id"`
}

type pipelineCleanupEntry struct {
	Version  int                     `json:"version"`
	Label    string                  `json:"label"`
	Parent   pipelineProcessIdentity `json:"parent"`
	Child    pipelineProcessIdentity `json:"child"`
	Docker   pipelineDockerIdentity  `json:"docker"`
	Deadline string                  `json:"deadline"`
	CIDFiles []string                `json:"cidfiles"`
	IDs      []string                `json:"ids"`
}

type pipelineCleanupState struct {
	root string
	lock *os.File
}

func openPipelineCleanupState(root string) (*pipelineCleanupState, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	// Validate every path component, not just the final directory. The journal
	// cannot live through a symlink that could redirect restart reconciliation.
	for path := root; ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("unsafe pipeline cleanup state path")
		}
		if path == root {
			if err := validatePipelinePrivateFile(info, 0700, true); err != nil {
				return nil, err
			}
		}
		if filepath.Dir(path) == path {
			break
		}
	}
	fd, err := unix.Open(filepath.Join(root, "lock"), unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, fmt.Errorf("unsafe pipeline cleanup lock: %w", err)
	}
	lock := os.NewFile(uintptr(fd), "pipeline cleanup lock") // #nosec G115 -- successful unix.Open returns a nonnegative descriptor.
	info, err := lock.Stat()
	if err == nil {
		err = validatePipelinePrivateFile(info, 0600, false)
	}
	if err == nil {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
	}
	if err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("pipeline cleanup lock unavailable: %w", err)
	}
	return &pipelineCleanupState{root: root, lock: lock}, nil
}

func validatePipelinePrivateFile(info os.FileInfo, mode os.FileMode, directory bool) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int64(stat.Uid) != int64(os.Geteuid()) || info.Mode().Perm() != mode ||
		info.IsDir() != directory || (!directory && !info.Mode().IsRegular()) {
		return fmt.Errorf("unsafe pipeline cleanup owner or mode")
	}
	return nil
}

func (s *pipelineCleanupState) Close() error { return s.lock.Close() }

func validPipelineCleanupLabel(label string) bool {
	if !strings.HasPrefix(label, "wfctl-") || len(label) != len("wfctl-")+32 {
		return false
	}
	data, err := hex.DecodeString(strings.TrimPrefix(label, "wfctl-"))
	return err == nil && len(data) == 16 && strings.ToLower(label) == label
}

func validatePipelineCleanupEntry(entry pipelineCleanupEntry) error {
	if entry.Version != 1 || !validPipelineCleanupLabel(entry.Label) || entry.Parent.PID <= 0 || entry.Parent.Start == "" ||
		entry.Docker.Endpoint == "" || entry.Docker.TLSHash == "" || entry.Docker.ServerID == "" {
		return fmt.Errorf("invalid pipeline cleanup identity")
	}
	if _, err := time.Parse(time.RFC3339Nano, entry.Deadline); err != nil {
		return fmt.Errorf("invalid cleanup deadline")
	}
	if entry.Child.PID != 0 && (entry.Child.PID <= 0 || entry.Child.PGID != entry.Child.PID || entry.Child.Start == "") {
		return fmt.Errorf("invalid child process identity")
	}
	if entry.Child.PID == 0 && (entry.Child.PGID != 0 || entry.Child.Start != "") {
		return fmt.Errorf("partial child identity")
	}
	seen := make(map[string]bool)
	for _, id := range entry.IDs {
		if len(id) != 64 || strings.ToLower(id) != id {
			return fmt.Errorf("invalid journaled container ID")
		}
		if _, err := hex.DecodeString(id); err != nil || seen[id] {
			return fmt.Errorf("invalid journaled container ID")
		}
		seen[id] = true
	}
	for _, name := range entry.CIDFiles {
		if !strings.HasPrefix(name, entry.Label+"-") || filepath.Base(name) != name || !strings.HasSuffix(name, ".cid") {
			return fmt.Errorf("invalid journaled cidfile")
		}
	}
	return nil
}

func (s *pipelineCleanupState) Save(entry pipelineCleanupEntry) error {
	if err := validatePipelineCleanupEntry(entry); err != nil {
		return err
	}
	data, err := json.Marshal(entry)
	if err != nil || len(data) > maxPipelineRecordBytes {
		return fmt.Errorf("invalid cleanup record")
	}
	path := filepath.Join(s.root, entry.Label+".json")
	if info, err := os.Lstat(path); err == nil {
		if err := validatePipelinePrivateFile(info, 0600, false); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temp, err := os.CreateTemp(s.root, ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return err
	}
	return s.sync()
}

func (s *pipelineCleanupState) Entries() ([]pipelineCleanupEntry, error) {
	names, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	var records []pipelineCleanupEntry
	var cidFiles []string
	allowedCID := make(map[string]bool)
	for _, name := range names {
		if name.Name() == "lock" {
			continue
		}
		if strings.HasSuffix(name.Name(), ".cid") {
			cidFiles = append(cidFiles, name.Name())
			continue
		}
		if !strings.HasSuffix(name.Name(), ".json") {
			return nil, fmt.Errorf("unrecognized cleanup state file")
		}
		file, err := openPipelinePrivateFile(filepath.Join(s.root, name.Name()))
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(file, maxPipelineRecordBytes+1))
		closeErr := file.Close()
		if err != nil || closeErr != nil || len(data) > maxPipelineRecordBytes {
			return nil, fmt.Errorf("unreadable cleanup record")
		}
		if err := validatePipelinePrivateJSON(data); err != nil {
			return nil, fmt.Errorf("ambiguous cleanup record")
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		var entry pipelineCleanupEntry
		if err := decoder.Decode(&entry); err != nil {
			return nil, fmt.Errorf("malformed cleanup record")
		}
		if decoder.Decode(new(any)) != io.EOF {
			return nil, fmt.Errorf("trailing cleanup record data")
		}
		if err := validatePipelineCleanupEntry(entry); err != nil {
			return nil, err
		}
		if name.Name() != entry.Label+".json" {
			return nil, fmt.Errorf("cleanup label/filename mismatch")
		}
		for _, cid := range entry.CIDFiles {
			allowedCID[cid] = true
		}
		records = append(records, entry)
	}
	for _, name := range cidFiles {
		if !allowedCID[name] {
			return nil, fmt.Errorf("unowned cleanup cidfile")
		}
		file, err := openPipelinePrivateFile(filepath.Join(s.root, name))
		if err != nil {
			return nil, err
		}
		if err := file.Close(); err != nil {
			return nil, err
		}
	}
	return records, nil
}

func openPipelinePrivateFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("unsafe cleanup record file: %w", err)
	}
	file := os.NewFile(uintptr(fd), "pipeline cleanup record") // #nosec G115 -- successful unix.Open returns a nonnegative descriptor.
	info, err := file.Stat()
	if err == nil {
		err = validatePipelinePrivateFile(info, 0600, false)
	}
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func (s *pipelineCleanupState) Delete(label string) error {
	if !validPipelineCleanupLabel(label) {
		return fmt.Errorf("invalid cleanup label")
	}
	if err := os.Remove(filepath.Join(s.root, label+".json")); err != nil {
		return err
	}
	return s.sync()
}

func (s *pipelineCleanupState) sync() error {
	directory, err := os.Open(s.root)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
