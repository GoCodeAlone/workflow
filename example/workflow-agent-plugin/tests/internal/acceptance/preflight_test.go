package acceptance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fakeHost struct {
	skills          []map[string]any
	discoveryErrors []any
	commands        []map[string]any
	failure         map[string]any
	rpcFailure      bool
}

func (f *fakeHost) Call(_ context.Context, method string, params map[string]any) (json.RawMessage, error) {
	if f.rpcFailure {
		return nil, errors.New("synthetic private transport text must not enter reports")
	}
	if method == "skills/list" {
		return json.Marshal(map[string]any{"data": []any{map[string]any{"skills": f.skills, "errors": f.discoveryErrors}}})
	}
	if method != "command/exec" {
		return nil, errors.New("unexpected host method")
	}
	f.commands = append(f.commands, params)
	if f.failure != nil {
		return json.Marshal(f.failure)
	}
	argv := params["command"].([]string)
	raw, err := os.ReadFile(argv[len(argv)-1])
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"exitCode": 0, "stdout": string(raw), "stderr": ""})
}
func readFixture(t *testing.T) (*fakeHost, ReadOptions) {
	t.Helper()
	root := t.TempDir()
	names := []string{"workflow-configure", "workflow-diagnose", "workflow-readiness"}
	paths := []string{"references/workflow-contract.md"}
	for _, name := range names {
		paths = append(paths, "skills/"+name+"/SKILL.md")
	}
	options := ReadOptions{RunRoot: root, Digests: map[string]string{}, Reader: []string{"/bin/cat"}}
	host := &fakeHost{}
	for _, relative := range paths {
		path := filepath.Join(root, "installed", filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		raw := []byte("Public deterministic byte fixture: " + relative + "\n")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(raw)
		options.Digests[relative] = hex.EncodeToString(hash[:])
	}
	for _, name := range names {
		host.skills = append(host.skills, map[string]any{"name": "workflow-assistant:" + name, "path": filepath.Join(root, "installed", "skills", name, "SKILL.md")})
	}
	return host, options
}

func TestReadPrerequisiteUsesExactFourFilesAndReadonlyPolicy(t *testing.T) {
	host, options := readFixture(t)
	result := SupportingFilePreflight(context.Background(), host, options)
	if !result.Passed || len(result.Files) != 4 || len(host.commands) != 4 {
		t.Fatalf("read preflight: %+v", result)
	}
	for _, params := range host.commands {
		if !reflect.DeepEqual(params["sandboxPolicy"], map[string]any{"type": "readOnly", "networkAccess": false}) || params["timeoutMs"] != 5000 || params["outputBytesCap"] != 32768 {
			t.Fatal("missing explicit bounded readonly policy")
		}
	}
}

func TestReadFailureCannotBecomeRegistrationSuccess(t *testing.T) {
	for _, failure := range []map[string]any{
		{"exitCode": 71, "stdout": "", "stderr": "sandbox-exec: sandbox_apply: Operation not permitted"},
		{"exitCode": 0, "stdout": "truncated", "stderr": ""},
		{"stdout": "", "stderr": ""},
	} {
		host, options := readFixture(t)
		host.failure = failure
		result := SupportingFilePreflight(context.Background(), host, options)
		if result.Passed || result.Reason != "host_readonly_execution_unavailable" || len(host.commands) != 1 {
			t.Fatalf("failed read accepted: %+v", result)
		}
		if failure["exitCode"] == 71 && result.Files[0].StderrCategory != "sandbox_apply_EPERM" {
			t.Fatal("missing bounded failure category")
		}
	}
}

func TestInvalidDiscoveryAndBytesStopBeforeCommands(t *testing.T) {
	for _, scenario := range []string{"swap", "duplicate", "missing", "discovery-error", "changed", "outside", "missing-digest", "reader-required", "rpc-error"} {
		t.Run(scenario, func(t *testing.T) {
			host, options := readFixture(t)
			switch scenario {
			case "swap":
				host.skills[0]["path"], host.skills[1]["path"] = host.skills[1]["path"], host.skills[0]["path"]
			case "duplicate":
				host.skills[1] = host.skills[0]
			case "missing":
				host.skills = host.skills[:2]
			case "discovery-error":
				host.discoveryErrors = []any{"synthetic unavailable skill"}
			case "changed":
				if err := os.WriteFile(filepath.Join(options.RunRoot, "installed", "references", "workflow-contract.md"), []byte("changed"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "outside":
				for _, skill := range host.skills {
					skill["path"] = filepath.Join(filepath.Dir(options.RunRoot), "outside", "skills", "unread", "SKILL.md")
				}
			case "missing-digest":
				delete(options.Digests, "references/workflow-contract.md")
			case "reader-required":
				options.Reader = nil
			case "rpc-error":
				host.rpcFailure = true
			}
			result := SupportingFilePreflight(context.Background(), host, options)
			if result.Passed || result.Reason == "" || len(host.commands) != 0 {
				t.Fatalf("invalid prerequisite accepted: %+v", result)
			}
			raw, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "synthetic private transport text must not enter reports") {
				t.Fatal("raw transport error leaked")
			}
		})
	}
}

func TestOwnedRootAliasAndEscapingContract(t *testing.T) {
	host, options := readFixture(t)
	alias := filepath.Join(t.TempDir(), "owned-alias")
	if err := os.Symlink(options.RunRoot, alias); err != nil {
		t.Skip("symlink fixture unavailable on this host")
	}
	for _, skill := range host.skills {
		relative, err := filepath.Rel(options.RunRoot, skill["path"].(string))
		if err != nil {
			t.Fatal(err)
		}
		skill["path"] = filepath.Join(alias, relative)
	}
	options.RunRoot = alias
	if result := SupportingFilePreflight(context.Background(), host, options); !result.Passed {
		t.Fatalf("owned alias rejected: %+v", result)
	}
	contract := filepath.Join(alias, "installed", "references", "workflow-contract.md")
	if err := os.Remove(contract); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "public-outside-canary")
	if err := os.WriteFile(outside, []byte("public canary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, contract); err != nil {
		t.Fatal(err)
	}
	host.commands = nil
	result := SupportingFilePreflight(context.Background(), host, options)
	if result.Passed || result.Reason != "supporting_file_missing_or_outside_package" || len(host.commands) != 0 {
		t.Fatalf("escaping contract accepted: %+v", result)
	}
}

func TestOversizedReviewedFileIsRejectedBeforeHostCall(t *testing.T) {
	host, options := readFixture(t)
	path := filepath.Join(options.RunRoot, "installed", "references", "workflow-contract.md")
	raw := []byte(strings.Repeat("x", 32769))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	options.Digests["references/workflow-contract.md"] = hex.EncodeToString(hash[:])
	result := SupportingFilePreflight(context.Background(), host, options)
	if result.Passed || len(host.commands) != 0 {
		t.Fatal("oversized file accepted before bounded host read")
	}
}
