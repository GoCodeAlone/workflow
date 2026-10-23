package acceptance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// RPCClient is supplied by the disposable host controller. This helper creates
// no client, login, process, permissions profile, or provider connection.
type RPCClient interface {
	Call(context.Context, string, map[string]any) (json.RawMessage, error)
}
type ReadOptions struct {
	RunRoot string
	Digests map[string]string
	// Reader is a trusted host-specific argv prefix returning exact UTF-8 bytes.
	// For POSIX hosts the controller can explicitly supply []string{"/bin/cat"}.
	// It is never selected from a tool result or untrusted configuration input.
	Reader []string
}
type FileRead struct {
	File           string `json:"file"`
	Exit           *int   `json:"exit"`
	ExpectedSHA256 string `json:"expected_SHA256"`
	ObservedSHA256 string `json:"observed_SHA256"`
	ExactBytes     bool   `json:"exact_bytes"`
	StderrCategory string `json:"stderr_category"`
}
type ReadReport struct {
	Passed bool       `json:"passed"`
	Files  []FileRead `json:"files"`
	Reason string     `json:"reason,omitempty"`
	Scope  string     `json:"scope"`
}

func inside(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != "." && relative != ".." && !filepath.IsAbs(relative) && !hasParentPrefix(relative)
}
func hasParentPrefix(relative string) bool {
	return len(relative) > 3 && relative[:3] == ".."+string(filepath.Separator)
}
func hasSandboxFailure(stderr string) bool {
	return strings.Contains(stderr, "sandbox_apply") && strings.Contains(stderr, "Operation not permitted")
}
func fileDigest(path string) (string, error) {
	// Nonblocking open prevents a replaced FIFO from hanging before Fstat. On
	// Windows O_NONBLOCK has no effect; regular-file checks still reject devices.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() || info.Size() > 32768 {
		_ = file.Close()
		return "", errors.New("reviewed file must be regular and at most 32768 bytes")
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, 32769))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(raw) > 32768 {
		return "", errors.New("reviewed file read unavailable or oversized")
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// SupportingFilePreflight checks all owned installed main files and the shared
// contract through the real host read-only execution path. Failure categories
// contain no raw host errors, file contents or credential material.
func SupportingFilePreflight(ctx context.Context, client RPCClient, options ReadOptions) ReadReport {
	result := ReadReport{Files: []FileRead{}, Scope: "installed public byte-read capability only; not model skill loading"}
	fail := func(reason string) ReadReport { result.Reason = reason; return result }
	if len(options.Reader) == 0 {
		return fail("trusted_byte_reader_required")
	}
	lexicalRoot, err := filepath.Abs(options.RunRoot)
	if err != nil {
		return fail("owned_run_unavailable")
	}
	runRoot, err := filepath.EvalSymlinks(lexicalRoot)
	if err != nil {
		return fail("owned_run_unavailable")
	}
	runRoot, err = filepath.Abs(runRoot)
	if err != nil {
		return fail("owned_run_unavailable")
	}
	raw, err := client.Call(ctx, "skills/list", map[string]any{"cwds": []string{filepath.Join(runRoot, "work")}, "forceReload": true})
	if err != nil {
		return fail("skill_discovery_unavailable")
	}
	type skill struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	var discovery struct {
		Data []struct {
			Skills []skill `json:"skills"`
			Errors []any   `json:"errors"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &discovery) != nil {
		return fail("skill_discovery_unavailable")
	}
	names := []string{"workflow-configure", "workflow-diagnose", "workflow-readiness"}
	selected := map[string]string{}
	for _, row := range discovery.Data {
		if len(row.Errors) > 0 {
			return fail("skill_discovery_errors")
		}
		for _, entry := range row.Skills {
			for _, name := range names {
				if entry.Name != "workflow-assistant:"+name {
					continue
				}
				if _, duplicate := selected[name]; duplicate {
					return fail("missing_or_duplicate_workflow_skill")
				}
				selected[name] = entry.Path
			}
		}
	}
	if len(selected) != 3 {
		return fail("missing_or_duplicate_workflow_skill")
	}
	installed := ""
	for _, name := range names {
		path, err := filepath.Abs(selected[name])
		if err != nil || (!inside(runRoot, path) && !inside(lexicalRoot, path)) {
			return fail("installed_path_outside_owned_run")
		}
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return fail("supporting_file_missing_or_outside_package")
		}
		if !inside(runRoot, path) {
			return fail("installed_path_outside_owned_run")
		}
		root := filepath.Dir(filepath.Dir(filepath.Dir(path)))
		if installed == "" {
			installed = root
		}
		if root != installed {
			return fail("inconsistent_installed_roots")
		}
		if path != filepath.Join(root, "skills", name, "SKILL.md") {
			return fail("skill_name_path_mismatch")
		}
	}
	if !inside(runRoot, installed) {
		return fail("installed_path_outside_owned_run")
	}
	relatives := []string{"references/workflow-contract.md"}
	sort.Strings(names)
	for _, name := range names {
		relatives = append(relatives, "skills/"+name+"/SKILL.md")
	}
	for _, relative := range relatives {
		path, err := filepath.EvalSymlinks(filepath.Join(installed, filepath.FromSlash(relative)))
		if err != nil || !inside(installed, path) {
			return fail("supporting_file_missing_or_outside_package")
		}
		expected := options.Digests[relative]
		decoded, err := hex.DecodeString(expected)
		if err != nil || len(decoded) != sha256.Size {
			return fail("reviewed_digest_missing_or_invalid")
		}
		expected = hex.EncodeToString(decoded)
		observed, err := fileDigest(path)
		if err != nil || observed != expected {
			return fail("installed_bytes_differ_from_reviewed_payload")
		}
		argv := append(append([]string(nil), options.Reader...), path)
		raw, err := client.Call(ctx, "command/exec", map[string]any{"command": argv,
			"cwd": filepath.Join(runRoot, "work"), "timeoutMs": 5000, "outputBytesCap": 32768,
			"sandboxPolicy": map[string]any{"type": "readOnly", "networkAccess": false}})
		if err != nil {
			return fail("host_readonly_execution_unavailable")
		}
		var response struct {
			Exit   *int   `json:"exitCode"`
			Stdout string `json:"stdout"`
			Stderr string `json:"stderr"`
		}
		if json.Unmarshal(raw, &response) != nil {
			return fail("host_readonly_execution_unavailable")
		}
		hash := sha256.Sum256([]byte(response.Stdout))
		category := "empty"
		if response.Stderr != "" {
			category = "other_nonempty"
		}
		if hasSandboxFailure(response.Stderr) {
			category = "sandbox_apply_EPERM"
		}
		read := FileRead{relative, response.Exit, expected, hex.EncodeToString(hash[:]), false, category}
		read.ExactBytes = response.Exit != nil && *response.Exit == 0 && response.Stderr == "" && read.ObservedSHA256 == expected
		result.Files = append(result.Files, read)
		if !read.ExactBytes {
			return fail("host_readonly_execution_unavailable")
		}
	}
	result.Passed = true
	return result
}
