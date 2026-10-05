package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/plugin"
)

const discoveryBaselineSHA = "64bfdf1a3f99bd634dd2a0c8118a5145cc322149bf34eb1ea30a64836a3adf20"

type discoveryNativeEvent struct {
	PID                                    int
	Owner, Op, Type, Name, Ref, ProviderID string
}
type discoveryNativeHarness struct{ root, plugins, bin string }

func discoveryBuildEnv() []string {
	env := []string{"LC_ALL=C", "GOWORK=off", "GOTOOLCHAIN=go1.26.5", "GOPROXY=off", "GOFLAGS=-mod=readonly"}
	for _, name := range []string{"PATH", "HOME", "TMPDIR", "GOCACHE", "GOPATH", "GOMODCACHE", "CGO_ENABLED", "CC"} {
		if value := os.Getenv(name); value != "" {
			env = append(env, name+"="+value)
		}
	}
	return env
}

func discoveryBuild(t *testing.T, cwd, output, source string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-race", "-o", output, source)
	cmd.Dir, cmd.Env, cmd.WaitDelay = cwd, discoveryBuildEnv(), 5*time.Second
	var out []byte
	var err error
	if source == "./cmd/wfctl" {
		out, err = buildFixtureArtifact(t, ctx, cmd, cwd, output)
	} else {
		out, err = cmd.CombinedOutput()
	}
	if err != nil {
		t.Fatalf("build actual artifact: %v\n%s", err, out)
	}
	t.Logf("go build -race -o %s %s: exit 0", output, source)
}

func newDiscoveryNativeHarness(t *testing.T) *discoveryNativeHarness {
	t.Helper()
	h := &discoveryNativeHarness{root: t.TempDir()}
	h.plugins, h.bin = filepath.Join(h.root, "plugins"), filepath.Join(h.root, "wfctl")
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	discoveryBuild(t, repo, h.bin, "./cmd/wfctl")
	for _, owner := range []string{"alpha", "beta"} {
		name := "fixture-" + owner
		dir := filepath.Join(h.plugins, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		types := []string{owner + ".widget"}
		if owner == "alpha" {
			types = append(types, "infra.fixture_alpha")
		}
		h.manifest(t, name, owner, types)
		source := filepath.Join(h.root, owner+".go")
		discoveryWrite(t, source, strings.ReplaceAll(discoveryNativeSource, "FIXTURE_OWNER", owner))
		discoveryBuild(t, repo, filepath.Join(dir, name), source)
	}
	t.Cleanup(func() { h.reap(t) })
	return h
}

func (h *discoveryNativeHarness) manifest(t *testing.T, name, owner string, types []string) {
	t.Helper()
	type legacy struct {
		IaCProvider plugin.IaCProviderCapability `json:"iacProvider"`
	}
	manifest := struct {
		Name         string                       `json:"name"`
		Version      string                       `json:"version"`
		Author       string                       `json:"author"`
		Description  string                       `json:"description"`
		IaCProvider  plugin.IaCProviderCapability `json:"iacProvider"`
		Capabilities legacy                       `json:"capabilities"`
	}{Name: name, Version: "0.1.0", Author: "workflow-tests", Description: "Independent native SDK resource fixture",
		IaCProvider: plugin.IaCProviderCapability{Name: owner, ResourceTypes: types, ComputePlanVersion: "v2"}}
	manifest.Capabilities.IaCProvider = manifest.IaCProvider
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	discoveryWrite(t, filepath.Join(h.plugins, name, "plugin.json"), string(data))
}

func (h *discoveryNativeHarness) events(t *testing.T) []discoveryNativeEvent {
	t.Helper()
	var events []discoveryNativeEvent
	for _, owner := range []string{"alpha", "beta"} {
		data, err := os.ReadFile(filepath.Join(h.root, owner+".jsonl"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		for {
			var event discoveryNativeEvent
			if err := decoder.Decode(&event); err == io.EOF {
				break
			} else if err != nil {
				t.Fatal(err)
			}
			events = append(events, event)
		}
	}
	return events
}

func (h *discoveryNativeHarness) mutations(t *testing.T) []discoveryNativeEvent {
	t.Helper()
	var result []discoveryNativeEvent
	for _, event := range h.events(t) {
		if event.Op == "Create" || event.Op == "Update" || event.Op == "Delete" {
			result = append(result, event)
		}
	}
	return result
}

func (h *discoveryNativeHarness) reap(t *testing.T) {
	t.Helper()
	pids := map[int]struct{}{}
	for _, event := range h.events(t) {
		pids[event.PID] = struct{}{}
	}
	for pid := range pids {
		process, err := os.FindProcess(pid)
		if err != nil {
			t.Error(err)
			continue
		}
		if err := process.Signal(syscall.Signal(0)); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			cmd := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "command=")
			out, psErr := cmd.Output()
			cancel()
			if psErr == nil && strings.Contains(string(out), h.plugins+string(os.PathSeparator)) {
				_ = process.Kill()
				t.Errorf("CLI left owned SDK child %d alive; killed exact fixture child", pid)
			} else {
				t.Errorf("fixture PID %d still present; refusing unrelated process cleanup", pid)
			}
		}
		_ = process.Release()
	}
	t.Logf("owned SDK PID readback: %d children absent after CLI wait/manager shutdown", len(pids))
}

func discoveryNativeEnv(root, racePolicy string) []string {
	env := []string{"LC_ALL=C", "CI=true", "WFCTL_NO_UPDATE_CHECK=1", "HOME=" + root, "TASK26_FIXTURE_ROOT=" + root}
	// Preserve race reporting; fixture exit sleep must not consume the package budget.
	env = append(env, "GORACE="+racePolicy+" atexit_sleep_ms=0")
	if path := os.Getenv("PATH"); path != "" {
		env = append(env, "PATH="+path)
	}
	return env
}

func TestIaCResourceDiscovery_FixtureRaceOptions(t *testing.T) {
	for _, tc := range []struct{ name, policy string }{
		{"empty", ""},
		{"exit-code", "exitcode=77"},
		{"halt-on-error", "halt_on_error=1"},
		{"existing-exit-sleep", "exitcode=87 halt_on_error=1 strip_path_prefix=/task26/report-policy atexit_sleep_ms=1000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var policies []string
			for _, variable := range discoveryNativeEnv(t.TempDir(), tc.policy) {
				if value, ok := strings.CutPrefix(variable, "GORACE="); ok {
					policies = append(policies, value)
				}
			}
			want := tc.policy + " atexit_sleep_ms=0"
			if len(policies) != 1 || policies[0] != want {
				t.Fatalf("fixture must preserve race reporting policy and disable only exit sleep: got %q, want %q", policies, want)
			}
		})
	}
}

func (h *discoveryNativeHarness) run(t *testing.T, binary string, extra []string, args ...string) ([]byte, error) {
	t.Helper()
	entries, err := os.ReadDir(h.plugins)
	if err != nil || len(entries) < 2 {
		t.Fatalf("both independent providers must remain installed: %v %v", entries, err)
	}
	for _, names := range [][]string{{"fixture-alpha", "z-alpha"}, {"fixture-beta", "a-beta"}} {
		found := false
		for _, name := range names {
			if info, err := os.Stat(filepath.Join(h.plugins, name)); err == nil && info.IsDir() {
				found = true
			}
		}
		if !found {
			t.Fatalf("independent fixture directories %v are absent", names)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir, cmd.WaitDelay = h.root, 5*time.Second
	cmd.Env = discoveryNativeEnv(h.root, os.Getenv("GORACE"))
	cmd.Env = append(cmd.Env, extra...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	out := stdout.Bytes()
	if err != nil {
		out = append(out, stderr.Bytes()...)
	}
	t.Logf("native CLI %s %s: exit=%v", binary, strings.Join(args, " "), err)
	h.reap(t)
	return out, err
}

func (h *discoveryNativeHarness) config(t *testing.T, resources, prefix string) string {
	t.Helper()
	path := filepath.Join(h.root, "infra.yaml")
	header := "modules:\n" +
		"  - name: provider-a\n    type: iac.provider\n    config:\n      provider: alpha\n      Ref: provider-a\n      Root: " + strconv.Quote(h.root) + "\n" +
		"  - name: provider-b\n    type: iac.provider\n    config:\n      provider: beta\n      Ref: provider-b\n      Root: " + strconv.Quote(h.root) + "\n" +
		"  - name: state\n    type: iac.state\n    config:\n      backend: filesystem\n      directory: " + strconv.Quote(filepath.Join(h.root, "state")) + "\n"
	discoveryWrite(t, path, prefix+header+resources)
	return path
}

const discoveryNativeCustom = `  - name: custom-a
    type: alpha.widget
    config: {iac_provider: provider-a, value: one, adopt_existing: true}
  - name: custom-b
    type: beta.widget
    config: {provider: provider-b, value: one, adopt_existing: true}
`
const discoveryNativeCanonical = `  - name: canonical
    type: infra.fixture_alpha
    config: {provider: provider-a, value: one, adopt_existing: true}
`

func (h *discoveryNativeHarness) plan(t *testing.T, binary, cfg, path string, extra []string) interfaces.IaCPlan {
	t.Helper()
	out, err := h.run(t, binary, extra, "infra", "plan", "--config", cfg, "--plugin-dir", h.plugins, "--env", "fixture", "--format", "json", "-o", path)
	if err != nil {
		t.Fatalf("actual CLI plan: %v\n%s", err, out)
	}
	plan, err := loadPlanFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
func discoveryPlanNames(plan interfaces.IaCPlan) []string {
	names := make([]string, 0, len(plan.Actions))
	for _, action := range plan.Actions {
		names = append(names, action.Resource.Name)
	}
	sort.Strings(names)
	return names
}

func (h *discoveryNativeHarness) frozenState(t *testing.T, directory string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		result["workflow/"+relative] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"alpha", "beta"} {
		data, err := os.ReadFile(filepath.Join(h.root, owner+".state.json"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		result["provider/"+owner] = hex.EncodeToString(sum[:])
	}
	return result
}

func discoveryVerifyBaseline(t *testing.T) string {
	t.Helper()
	path, selected := os.LookupEnv("TASK26_BASELINE_WFCTL")
	if !selected {
		return ""
	}
	if path == "" {
		t.Fatal("explicit TASK26_BASELINE_WFCTL is empty; acceptance requires the verified artifact")
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Fatal("selected acceptance artifact requires native darwin/arm64")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		t.Fatal(err)
	}
	if stat.Size() != 112815442 || hex.EncodeToString(hash.Sum(nil)) != discoveryBaselineSHA {
		t.Fatal("baseline does not match verified immutable v0.86.1 artifact")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = []string{"LC_ALL=C", "CI=true", "WFCTL_NO_UPDATE_CHECK=1"}
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "v0.86.1" {
		t.Fatalf("baseline version must be v0.86.1: %v %q", err, out)
	}
	t.Logf("verified native baseline size=%d SHA256=%s version=v0.86.1", stat.Size(), discoveryBaselineSHA)
	return path
}

func TestIaCResourceDiscoveryNative_Integration(t *testing.T) {
	baseline := discoveryVerifyBaseline(t)
	h := newDiscoveryNativeHarness(t)
	planPath := filepath.Join(h.root, "plan.json")
	applyArgs := func(cfg string) []string {
		return []string{"infra", "apply", "--config", cfg, "--plugin-dir", h.plugins, "--env", "fixture", "--auto-approve", "--skip-bootstrap", "--skip-refresh"}
	}
	t.Run("custom-only-and-mixed-dry-run", func(t *testing.T) {
		cfg := h.config(t, discoveryNativeCustom, "")
		plan := h.plan(t, h.bin, cfg, planPath, nil)
		if got := discoveryPlanNames(plan); !reflect.DeepEqual(got, []string{"custom-a", "custom-b"}) {
			t.Fatalf("actual CLI custom-only resources=%v; want both providers", got)
		}
		cfg = h.config(t, discoveryNativeCanonical+discoveryNativeCustom, "")
		plan = h.plan(t, h.bin, cfg, planPath, nil)
		if got := discoveryPlanNames(plan); !reflect.DeepEqual(got, []string{"canonical", "custom-a", "custom-b"}) {
			t.Fatalf("actual CLI mixed resources=%v; want exactly three", got)
		}
		out, err := h.run(t, h.bin, nil, append(applyArgs(cfg), "--dry-run", "--format", "json")...)
		if err != nil {
			t.Fatalf("dry-run: %v\n%s", err, out)
		}
		var dry DryRunApplyPlan
		if err := json.Unmarshal(out, &dry); err != nil {
			t.Fatalf("real dry-run JSON: %v\n%s", err, out)
		}
		want := []DryRunProviderGroup{{ModuleRef: "provider-a", ProviderType: "alpha", ResourceCount: 2}, {ModuleRef: "provider-b", ProviderType: "beta", ResourceCount: 1}}
		if len(dry.Actions) != 3 || !reflect.DeepEqual(dry.Providers, want) || len(h.mutations(t)) != 0 {
			t.Fatalf("dry-run exact groups/actions/zero CUD: %+v", dry)
		}
	})
	t.Run("direct-restart-repeat-saved-update", func(t *testing.T) {
		cfg := h.config(t, discoveryNativeCanonical+discoveryNativeCustom, "")
		out, err := h.run(t, h.bin, nil, applyArgs(cfg)...)
		if err != nil {
			t.Fatalf("direct native apply: %v\n%s", err, out)
		}
		mutations := h.mutations(t)
		if len(mutations) != 3 {
			t.Fatalf("direct CUD count=%d; want exactly 3", len(mutations))
		}
		for _, e := range mutations {
			if e.Op != "Create" || (e.Name == "custom-b" && (e.Owner != "beta" || e.Ref != "provider-b")) || (e.Name != "custom-b" && (e.Owner != "alpha" || e.Ref != "provider-a")) {
				t.Fatalf("provider identity redirected: %+v", e)
			}
		}
		states, err := loadCurrentState(cfg, "fixture")
		if err != nil || len(states) != 3 {
			t.Fatalf("real state reload: count=%d err=%v", len(states), err)
		}
		for _, s := range states {
			owner, ref := "alpha", "provider-a"
			if s.Name == "custom-b" {
				owner, ref = "beta", "provider-b"
			}
			if s.Provider != owner || s.ProviderRef != ref || s.ProviderID != owner+"/"+s.Name {
				t.Fatalf("persisted exact identity lost: %+v", s)
			}
		}
		out, err = h.run(t, h.bin, nil, applyArgs(cfg)...)
		if err != nil || len(h.mutations(t)) != 3 {
			t.Fatalf("restart/repeat must not mutate: %v\n%s", err, out)
		}
		updated := strings.Replace(discoveryNativeCanonical+discoveryNativeCustom, "iac_provider: provider-a, value: one", "iac_provider: provider-a, value: two", 1)
		cfg = h.config(t, updated, "")
		plan := h.plan(t, h.bin, cfg, planPath, nil)
		if len(plan.Actions) != 1 || plan.Actions[0].Action != "update" || plan.Actions[0].Resource.Name != "custom-a" {
			t.Fatalf("actual dependency Diff must find only custom-a update: %+v", plan.Actions)
		}
		out, err = h.run(t, h.bin, nil, append(applyArgs(cfg), "--plan", planPath)...)
		if err != nil || len(h.mutations(t)) != 4 {
			t.Fatalf("actual saved update: %v\n%s", err, out)
		}
	})
	t.Run("binding-order-and-runtime-authority", func(t *testing.T) {
		before := len(h.mutations(t))
		cfg := h.config(t, strings.Replace(discoveryNativeCustom, "iac_provider: provider-a", "iac_provider: provider-b", 1), "")
		out, err := h.run(t, h.bin, nil, "infra", "plan", "--config", cfg, "--plugin-dir", h.plugins)
		if err == nil || !bytes.Contains(out, []byte("not declared by selected provider")) {
			t.Fatalf("wrong selection must be binding denial: %v\n%s", err, out)
		}
		for _, mode := range []string{"no-required", "no-driver", "v1", "wrong-type"} {
			cfg = h.config(t, discoveryNativeCustom, "")
			out, err = h.run(t, h.bin, []string{"TASK26_ALPHA_MODE=" + mode}, applyArgs(cfg)...)
			if err == nil {
				t.Fatalf("runtime authority %s permitted apply", mode)
			}
			marker := map[string]string{"no-required": "does not register the required", "no-driver": "unimplemented", "v1": "requires \"v2\"", "wrong-type": "fixture type unavailable"}[mode]
			if !bytes.Contains(out, []byte(marker)) {
				t.Fatalf("runtime %s denial must not be a transport false positive: %s", mode, out)
			}
		}
		if len(h.mutations(t)) != before {
			t.Fatal("binding/runtime denials must have zero CUD")
		}
		func() {
			binary := filepath.Join(h.plugins, "fixture-alpha", "fixture-alpha")
			held := filepath.Join(h.root, "held-alpha")
			if err := os.Rename(binary, held); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.Rename(held, binary); err != nil {
					t.Error(err)
				}
			}()
			out, err := h.run(t, h.bin, nil, applyArgs(cfg)...)
			if err == nil || !bytes.Contains(out, []byte("binary is missing")) || len(h.mutations(t)) != before {
				t.Fatalf("missing executable must fail before CUD: %v\n%s", err, out)
			}
		}()
		for _, pair := range [][2]string{{"fixture-alpha", "z-alpha"}, {"fixture-beta", "a-beta"}} {
			old, name := pair[0], pair[1]
			dir := filepath.Join(h.plugins, old)
			if err := os.Rename(filepath.Join(dir, old), filepath.Join(dir, name)); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(dir, filepath.Join(h.plugins, name)); err != nil {
				t.Fatal(err)
			}
			owner := strings.TrimPrefix(old, "fixture-")
			types := []string{owner + ".widget"}
			if owner == "alpha" {
				types = append(types, "infra.fixture_alpha")
			}
			h.manifest(t, name, owner, types)
		}
		cfg = h.config(t, discoveryNativeCanonical+discoveryNativeCustom, "")
		data, err := os.ReadFile(cfg)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		start, middle, end := strings.Index(text, "  - name: provider-a"), strings.Index(text, "  - name: provider-b"), strings.Index(text, "  - name: state")
		text = text[:start] + text[middle:end] + text[start:middle] + text[end:]
		customStart, customMiddle := strings.Index(text, "  - name: custom-a"), strings.Index(text, "  - name: custom-b")
		text = text[:customStart] + text[customMiddle:] + text[customStart:customMiddle]
		discoveryWrite(t, cfg, text)
		plan := h.plan(t, h.bin, cfg, planPath, nil)
		for _, a := range plan.Actions {
			if a.Resource.Name == "custom-a" && resolveIaCProviderRef(a.Resource.Config) != "provider-a" {
				t.Fatal("reversed order redirected alpha")
			}
			if a.Action == "delete" {
				t.Fatal("order-only changes must not delete")
			}
		}
		out, err = h.run(t, h.bin, nil, append(applyArgs(cfg), "--plan", planPath)...)
		if err != nil {
			t.Fatalf("reversed order saved apply: %v\n%s", err, out)
		}
	})
	t.Run("imported-env-fingerprint-before-mutation", func(t *testing.T) {
		cfg := h.config(t, discoveryNativeCanonical, "imports: [custom.yaml]\n")
		discoveryWrite(t, filepath.Join(h.root, "custom.yaml"), discoveryNativeImported)
		extra := []string{"TASK26_VALUE=task26-private-value-a"}
		plan := h.plan(t, h.bin, cfg, planPath, extra)
		if len(plan.InputSnapshot) != 1 || plan.InputSnapshot["TASK26_VALUE"] == "" {
			t.Fatalf("custom fingerprint missing: %v", plan.InputSnapshot)
		}
		data, err := os.ReadFile(planPath)
		if err != nil || !bytes.Contains(data, []byte("${TASK26_VALUE}")) || bytes.Contains(data, []byte("task26-private-value-a")) {
			t.Fatal("plan must preserve custom ref and omit value")
		}
		out, err := h.run(t, h.bin, extra, append(applyArgs(cfg), "--plan", planPath)...)
		if err != nil {
			t.Fatalf("unchanged saved input: %v\n%s", err, out)
		}
		directory := filepath.Join(h.root, "state")
		frozen, before := h.frozenState(t, directory), len(h.mutations(t))
		out, err = h.run(t, h.bin, []string{"TASK26_VALUE=task26-private-value-b"}, append(applyArgs(cfg), "--plan", planPath)...)
		if err == nil || !bytes.Contains(out, []byte("TASK26_VALUE")) {
			t.Fatalf("changed fingerprint must reject by key: %v\n%s", err, out)
		}
		if len(h.mutations(t)) != before || !reflect.DeepEqual(h.frozenState(t, directory), frozen) {
			t.Fatal("fingerprint denial mutated provider or Workflow state")
		}
		for _, value := range []string{"task26-private-value-a", "task26-private-value-b"} {
			if bytes.Contains(out, []byte(value)) {
				t.Fatal("drift diagnostic disclosed input bytes")
			}
			err := filepath.WalkDir(h.root, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() || !strings.HasSuffix(path, ".json") && !strings.HasSuffix(path, ".jsonl") {
					return nil
				}
				data, err := os.ReadFile(path)
				if err == nil && bytes.Contains(data, []byte(value)) {
					return fmt.Errorf("fixture input persisted at %s", path)
				}
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	})
	t.Run("actual-old-plan-denial", func(t *testing.T) {
		if baseline == "" {
			t.Skip("baseline unconfigured: ordinary local coverage only, NOT downgrade acceptance")
		}
		cfg := h.config(t, discoveryNativeCanonical+discoveryNativeCustom, "")
		out, err := h.run(t, h.bin, nil, applyArgs(cfg)...)
		if err != nil {
			t.Fatalf("candidate baseline state: %v\n%s", err, out)
		}
		source, clone := filepath.Join(h.root, "state"), filepath.Join(h.root, "state-clone")
		err = filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}
			dest := filepath.Join(clone, relative)
			if entry.IsDir() {
				return os.MkdirAll(dest, 0o700)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(dest, data, 0o600)
		})
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(cfg)
		if err != nil {
			t.Fatal(err)
		}
		clonedCfg := filepath.Join(h.root, "clone.yaml")
		discoveryWrite(t, clonedCfg, strings.Replace(string(data), strconv.Quote(source), strconv.Quote(clone), 1))
		oldPath := filepath.Join(h.root, "old-plan.json")
		before, frozen := len(h.mutations(t)), h.frozenState(t, clone)
		old := h.plan(t, baseline, clonedCfg, oldPath, nil)
		found := false
		for _, a := range old.Actions {
			if a.Action == "delete" && a.Resource.Name == "custom-a" && a.Resource.Type == "alpha.widget" {
				found = true
			}
		}
		if !found {
			t.Fatalf("actual old CLI must emit omission-derived custom Delete: %+v", old.Actions)
		}
		out, err = h.run(t, h.bin, nil, append(applyArgs(clonedCfg), "--plan", oldPath)...)
		if err == nil || !bytes.Contains(out, []byte("plan stale: config hash mismatch")) {
			t.Fatalf("candidate must reject actual old plan: %v\n%s", err, out)
		}
		if len(h.mutations(t)) != before || !reflect.DeepEqual(frozen, h.frozenState(t, clone)) {
			t.Fatal("old-plan denial changed provider or cloned state")
		}
		current := h.plan(t, h.bin, clonedCfg, planPath, nil)
		if len(current.Actions) != 0 {
			t.Fatalf("candidate replan must have no omission deletes: %+v", current.Actions)
		}
		states, err := loadCurrentState(clonedCfg, "fixture")
		if err != nil || len(states) != 3 {
			t.Fatalf("all state identities must remain: %d %v", len(states), err)
		}
	})
}

const discoveryNativeImported = `modules:
  - name: custom-a
    type: alpha.widget
    config: {iac_provider: provider-a, value: three, adopt_existing: true}
    environments:
      fixture:
        config:
          name: driver-facing-name
          env_vars: {VALUE: "${TASK26_VALUE}"}
  - name: custom-b
    type: beta.widget
    config: {provider: provider-b, value: one, adopt_existing: true}
`

func TestIaCResourceDiscoveryNative_ReviewCorrections(t *testing.T) {
	_ = discoveryVerifyBaseline(t)
	h := newDiscoveryNativeHarness(t)
	cfg := h.config(t, "", "imports: [routed.yaml]\nenvironments:\n  fixture: {provider: \"${TASK26_R1_REF_A}\", region: local}\n")
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(data), "provider: alpha", `provider: "${TASK26_R1_TYPE_A}"`, 1)
	text = strings.Replace(text, "provider: beta", `provider: "$TASK26_R1_TYPE_B"`, 1)
	discoveryWrite(t, cfg, text)
	discoveryWrite(t, filepath.Join(h.root, "routed.yaml"), `modules:
  - name: routed-a
    type: alpha.widget
    config: {iac_provider: provider-b, value: one, adopt_existing: true}
    environments:
      fixture:
        config:
          iac_provider: "${TASK26_R1_REF_A}"
          env_vars: {VALUE: "${TASK26_R1_VALUE}"}
  - name: routed-b
    type: beta.widget
    config: {provider: "$TASK26_R1_REF_B", value: one, adopt_existing: true}
  - name: routed-default
    type: alpha.widget
    config: {value: one, adopt_existing: true}
`)
	extra := []string{"TASK26_R1_TYPE_A=alpha", "TASK26_R1_TYPE_B=beta", "TASK26_R1_REF_A=provider-a", "TASK26_R1_REF_B=provider-b", "TASK26_R1_VALUE=task26-routing-private-a"}
	args := []string{"infra", "apply", "--config", cfg, "--plugin-dir", h.plugins, "--env", "fixture", "--auto-approve", "--skip-bootstrap", "--skip-refresh"}
	planPath := filepath.Join(h.root, "routed-plan.json")
	t.Run("R1-interpolated-routing", func(t *testing.T) {
		plan := h.plan(t, h.bin, cfg, planPath, extra)
		if !reflect.DeepEqual(discoveryPlanNames(plan), []string{"routed-a", "routed-b", "routed-default"}) || len(plan.InputSnapshot) != 3 {
			t.Fatal("actual CLI must discover both owners/default and fingerprint raw routing/payload refs")
		}
		for _, name := range []string{"TASK26_R1_REF_A", "TASK26_R1_REF_B", "TASK26_R1_VALUE"} {
			if plan.InputSnapshot[name] == "" {
				t.Fatalf("missing raw reference fingerprint %s", name)
			}
		}
		data, err := os.ReadFile(planPath)
		if err != nil || !bytes.Contains(data, []byte("${TASK26_R1_VALUE}")) || bytes.Contains(data, []byte("task26-routing-private-a")) {
			t.Fatal("routing classification expanded preserved payload bytes")
		}
		out, err := h.run(t, h.bin, extra, append(args, "--plan", planPath)...)
		if err != nil || len(h.mutations(t)) != 3 {
			t.Fatalf("unchanged interpolated saved apply: %v\n%s", err, out)
		}
		for _, event := range h.mutations(t) {
			owner, ref := "alpha", "provider-a"
			if event.Name == "routed-b" {
				owner, ref = "beta", "provider-b"
			}
			if event.Op != "Create" || event.Owner != owner || event.Ref != ref || event.ProviderID != owner+"/"+event.Name {
				t.Fatal("effective routing redirected actual driver ownership")
			}
		}
		states, err := loadCurrentState(cfg, "fixture")
		if err != nil || len(states) != 3 {
			t.Fatalf("actual state reload after interpolated routing: %d %v", len(states), err)
		}
		frozen := h.frozenState(t, filepath.Join(h.root, "state"))
		for _, change := range []struct{ before, after, marker string }{
			{"TASK26_R1_VALUE=task26-routing-private-a", "TASK26_R1_VALUE=task26-routing-private-b", "TASK26_R1_VALUE"},
			{"TASK26_R1_REF_A=provider-a", "TASK26_R1_REF_A=provider-b", "not declared by selected provider"},
		} {
			changed := append([]string(nil), extra...)
			for i := range changed {
				if changed[i] == change.before {
					changed[i] = change.after
				}
			}
			out, err := h.run(t, h.bin, changed, append(args, "--plan", planPath)...)
			if err == nil || !bytes.Contains(out, []byte(change.marker)) || bytes.Contains(out, []byte("task26-routing-private-")) {
				t.Fatalf("interpolated drift must fail without payload bytes: %v\n%s", err, out)
			}
			if len(h.mutations(t)) != 3 || !reflect.DeepEqual(frozen, h.frozenState(t, filepath.Join(h.root, "state"))) {
				t.Fatal("interpolated drift changed state or performed CUD")
			}
		}
		if plan := h.plan(t, h.bin, cfg, planPath, extra); len(plan.Actions) != 0 {
			t.Fatal("interpolated replan after reload must not omit or redirect resources")
		}
	})
	t.Run("R2-unneeded-metadata", func(t *testing.T) {
		before := len(h.mutations(t))
		dir := filepath.Join(h.plugins, "unrelated")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		manifest := filepath.Join(dir, "plugin.json")
		for _, unreadable := range []bool{false, true} {
			if unreadable {
				if err := os.Remove(manifest); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(manifest, 0o700); err != nil {
					t.Fatal(err)
				}
			} else {
				discoveryWrite(t, manifest, "{broken")
			}
			h.plan(t, h.bin, cfg, planPath, extra)
			out, err := h.run(t, h.bin, extra, append(args, "--dry-run", "--format", "json")...)
			var dry DryRunApplyPlan
			if err != nil || json.Unmarshal(out, &dry) != nil || !reflect.DeepEqual(dry.Providers, []DryRunProviderGroup{{ModuleRef: "provider-a", ProviderType: "${TASK26_R1_TYPE_A}", ResourceCount: 2}, {ModuleRef: "provider-b", ProviderType: "$TASK26_R1_TYPE_B", ResourceCount: 1}}) || len(h.mutations(t)) != before {
				t.Fatalf("unneeded metadata vetoed actual SDK plan/dry-run or changed CUD: %v\n%s", err, out)
			}
		}
		discoveryWrite(t, filepath.Join(h.plugins, "fixture-alpha", "plugin.json"), `{"iacProvider":{"name":"alpha","resourceTypes":[42]}}`)
		out, err := h.run(t, h.bin, extra, args...)
		if err == nil || !bytes.Contains(out, []byte("invalid IaC manifest metadata")) || len(h.mutations(t)) != before {
			t.Fatalf("needed metadata must deny before CUD: %v\n%s", err, out)
		}
	})
}

// The only simulated component is the external dependency. Workflow owns
// planning, persistence and execution; SDK owns registration and transport.
const discoveryNativeSource = `package main

import (
 "context"
 "encoding/json"
 "os"
 "path/filepath"
 "sync"
 pluginpkg "github.com/GoCodeAlone/workflow/plugin"
 pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
 "github.com/GoCodeAlone/workflow/plugin/external/sdk"
 "google.golang.org/grpc/codes"
 "google.golang.org/grpc/status"
)

const owner="FIXTURE_OWNER"
type event struct { PID int; Owner,Op,Type,Name,Ref,ProviderID string }
type resource struct { Name,Type,ID,Value string }
type provider struct {
 pb.UnimplementedIaCProviderRequiredServer
 pb.UnimplementedResourceDriverServer
 pb.UnimplementedResourceSensitiveInputDeclarerServer
 mu sync.Mutex
 root,ref,mode string
}
func (p *provider) record(op,typ,name,id string)error{
 file,err:=os.OpenFile(filepath.Join(p.root,owner+".jsonl"),os.O_CREATE|os.O_WRONLY|os.O_APPEND,0600)
 if err!=nil{return err};defer file.Close()
 return json.NewEncoder(file).Encode(event{os.Getpid(),owner,op,typ,name,p.ref,id})
}
func (p *provider) Initialize(_ context.Context,req *pb.InitializeRequest)(*pb.InitializeResponse,error){
 p.mu.Lock();defer p.mu.Unlock()
 var cfg struct{Root,Ref string}
 if err:=json.Unmarshal(req.ConfigJson,&cfg);err!=nil{return nil,err}
 if cfg.Root!=p.root{return nil,status.Error(codes.InvalidArgument,"fixture root mismatch")}
 p.ref=cfg.Ref
 if err:=p.record("Initialize","","","");err!=nil{return nil,err}
 return &pb.InitializeResponse{},nil
}
func (*provider) Name(context.Context,*pb.NameRequest)(*pb.NameResponse,error){return &pb.NameResponse{Name:owner},nil}
func (*provider) Version(context.Context,*pb.VersionRequest)(*pb.VersionResponse,error){return &pb.VersionResponse{Version:"0.1.0"},nil}
func (p *provider) Capabilities(context.Context,*pb.CapabilitiesRequest)(*pb.CapabilitiesResponse,error){
 version:="v2";if p.mode=="v1"{version="v1"}
 return &pb.CapabilitiesResponse{ComputePlanVersion:version,Capabilities:[]*pb.IaCCapabilityDeclaration{{ResourceType:owner+".widget",Operations:[]string{"create","read","update","delete"}}}},nil
}
func (p *provider) check(typ string)error{
 if p.mode=="wrong-type"||(typ!=owner+".widget"&&!(owner=="alpha"&&typ=="infra.fixture_alpha")){return status.Error(codes.InvalidArgument,"fixture type unavailable")}
 return nil
}
func (p *provider) SensitiveInputPaths(_ context.Context,req *pb.ResourceSensitiveInputPathsRequest)(*pb.ResourceSensitiveInputPathsResponse,error){
 if err:=p.check(req.ResourceType);err!=nil{return nil,err};return &pb.ResourceSensitiveInputPathsResponse{},nil
}
func (p *provider) load()(map[string]resource,error){
 records:=map[string]resource{}
 data,err:=os.ReadFile(filepath.Join(p.root,owner+".state.json"));if os.IsNotExist(err){return records,nil};if err!=nil{return nil,err}
 err=json.Unmarshal(data,&records);return records,err
}
func (p *provider) save(records map[string]resource)error{
 data,err:=json.Marshal(records);if err!=nil{return err};return os.WriteFile(filepath.Join(p.root,owner+".state.json"),data,0600)
}
func output(r resource)*pb.ResourceOutput{
 data,_:=json.Marshal(struct{Value string}{r.Value})
 return &pb.ResourceOutput{Name:r.Name,Type:r.Type,ProviderId:r.ID,OutputsJson:data,Status:"running"}
}
func (p *provider) put(op,typ string,spec *pb.ResourceSpec,ref *pb.ResourceRef)(*pb.ResourceOutput,error){
 if err:=p.check(typ);err!=nil{return nil,err}
 if spec==nil||spec.Type!=typ||spec.Name==""{return nil,status.Error(codes.InvalidArgument,"fixture identity mismatch")}
 var cfg struct{Value string};if err:=json.Unmarshal(spec.ConfigJson,&cfg);err!=nil{return nil,err}
 records,err:=p.load();if err!=nil{return nil,err}
 if op=="Update"{
  current,ok:=records[spec.Name]
  if !ok||ref==nil||ref.Name!=spec.Name||ref.Type!=typ||ref.ProviderId!=current.ID{return nil,status.Error(codes.InvalidArgument,"fixture update identity mismatch")}
 }else if _,ok:=records[spec.Name];ok{return nil,status.Error(codes.AlreadyExists,"fixture already exists")}
 r:=resource{Name:spec.Name,Type:typ,ID:owner+"/"+spec.Name,Value:cfg.Value};records[r.Name]=r
 if err:=p.save(records);err!=nil{return nil,err};if err:=p.record(op,typ,r.Name,r.ID);err!=nil{return nil,err}
 return output(r),nil
}
func (p *provider) Create(_ context.Context,req *pb.ResourceCreateRequest)(*pb.ResourceCreateResponse,error){
 p.mu.Lock();defer p.mu.Unlock();out,err:=p.put("Create",req.ResourceType,req.Spec,nil);return &pb.ResourceCreateResponse{Output:out},err
}
func (p *provider) Update(_ context.Context,req *pb.ResourceUpdateRequest)(*pb.ResourceUpdateResponse,error){
 p.mu.Lock();defer p.mu.Unlock();out,err:=p.put("Update",req.ResourceType,req.Spec,req.Ref);return &pb.ResourceUpdateResponse{Output:out},err
}
func (p *provider) Read(_ context.Context,req *pb.ResourceReadRequest)(*pb.ResourceReadResponse,error){
 p.mu.Lock();defer p.mu.Unlock()
 if err:=p.check(req.ResourceType);err!=nil{return nil,err}
 if req.Ref==nil||req.Ref.Type!=req.ResourceType{return nil,status.Error(codes.InvalidArgument,"fixture read identity mismatch")}
 if err:=p.record("Read",req.ResourceType,req.Ref.Name,req.Ref.ProviderId);err!=nil{return nil,err}
 records,err:=p.load();if err!=nil{return nil,err}
 r,ok:=records[req.Ref.Name];if !ok{return nil,status.Error(codes.NotFound,"iac: resource not found")}
 if r.Type!=req.ResourceType||(req.Ref.ProviderId!=""&&req.Ref.ProviderId!=r.ID){return nil,status.Error(codes.InvalidArgument,"fixture read identity mismatch")}
 return &pb.ResourceReadResponse{Output:output(r)},nil
}
func (p *provider) Diff(_ context.Context,req *pb.ResourceDiffRequest)(*pb.ResourceDiffResponse,error){
 p.mu.Lock();defer p.mu.Unlock()
 if err:=p.check(req.ResourceType);err!=nil{return nil,err}
 if req.Desired==nil||req.Desired.Type!=req.ResourceType{return nil,status.Error(codes.InvalidArgument,"fixture diff identity mismatch")}
 if err:=p.record("Diff",req.ResourceType,req.Desired.Name,"");err!=nil{return nil,err}
 var desired,current struct{Value string}
 if err:=json.Unmarshal(req.Desired.ConfigJson,&desired);err!=nil{return nil,err}
 if req.Current!=nil{
  if req.Current.Name!=req.Desired.Name||req.Current.Type!=req.ResourceType{return nil,status.Error(codes.InvalidArgument,"fixture diff output identity mismatch")}
  if err:=json.Unmarshal(req.Current.OutputsJson,&current);err!=nil{return nil,err}
 }
 return &pb.ResourceDiffResponse{Result:&pb.DiffResult{NeedsUpdate:desired.Value!=current.Value}},nil
}
func (p *provider) Delete(_ context.Context,req *pb.ResourceDeleteRequest)(*pb.ResourceDeleteResponse,error){
 p.mu.Lock();defer p.mu.Unlock()
 if err:=p.check(req.ResourceType);err!=nil{return nil,err}
 if req.Ref==nil||req.Ref.Type!=req.ResourceType{return nil,status.Error(codes.InvalidArgument,"fixture delete identity mismatch")}
 records,err:=p.load();if err!=nil{return nil,err}
 current,ok:=records[req.Ref.Name];if !ok||current.ID!=req.Ref.ProviderId{return nil,status.Error(codes.NotFound,"iac: resource not found")}
 delete(records,req.Ref.Name)
 if err:=p.save(records);err!=nil{return nil,err}
 if err:=p.record("Delete",req.ResourceType,req.Ref.Name,req.Ref.ProviderId);err!=nil{return nil,err}
 return &pb.ResourceDeleteResponse{},nil
}
func (*provider) SensitiveKeys(context.Context,*pb.SensitiveKeysRequest)(*pb.SensitiveKeysResponse,error){return &pb.SensitiveKeysResponse{},nil}
type generic struct{manifest *pluginpkg.PluginManifest}
func (g generic) Manifest()sdk.PluginManifest{return sdk.PluginManifest{Name:g.manifest.Name,Version:g.manifest.Version,Author:g.manifest.Author,Description:g.manifest.Description}}
type requiredOnly struct{pb.IaCProviderRequiredServer}
func main(){
 manifest,err:=pluginpkg.LoadManifest(filepath.Join(filepath.Dir(os.Args[0]),"plugin.json"));if err!=nil{panic(err)}
 p:=&provider{root:os.Getenv("TASK26_FIXTURE_ROOT")}
 if owner=="alpha"{p.mode=os.Getenv("TASK26_ALPHA_MODE")}
 if err:=p.record("Start","","","");err!=nil{panic(err)}
 opts:=sdk.IaCServeOptions{ManifestProvider:manifest}
 if p.mode=="no-required"{sdk.Serve(generic{manifest});return}
 if p.mode=="no-driver"{sdk.ServeIaCPlugin(requiredOnly{p},opts);return}
 sdk.ServeIaCPlugin(p,opts)
}
`
