//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"syscall"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/pipeline"
	"gopkg.in/yaml.v3"
)

func TestFixtureCompositePoolTemplatesSurviveOutputMerge(t *testing.T) {
	for _, name := range []string{"while", "compensation"} {
		for _, size := range []int{2, 3} {
			t.Run(name+"/pool-"+strconv.Itoa(size), func(t *testing.T) {
				type stepConfig struct {
					Name   string         `yaml:"name"`
					Config map[string]any `yaml:"config"`
				}
				var doc struct {
					Pipelines map[string]struct {
						Steps        []stepConfig `yaml:"steps"`
						Compensation []stepConfig `yaml:"compensation"`
					} `yaml:"pipelines"`
				}
				data, err := os.ReadFile(filepath.Join("..", name+".yaml"))
				if err != nil {
					t.Fatal(err)
				}
				if err := yaml.Unmarshal(data, &doc); err != nil {
					t.Fatal(err)
				}
				poolName := name + "-pool-" + strconv.Itoa(size)
				pool := make([]any, size)
				for i := range pool {
					pool[i] = poolName + "-participant-" + strconv.Itoa(i)
				}
				pc := interfaces.NewPipelineContext(map[string]any{
					"pool": pool, "pool_name": poolName, "last_ordinal": strconv.Itoa(size - 1),
				}, nil)
				pc.MergeStepOutput("sdk-work", map[string]any{"pool": poolName})
				pc.Current["iter"] = map[string]any{"index": 1}
				selected := doc.Pipelines["selected"]
				configs := append(selected.Steps, selected.Compensation...)
				checked := 0
				for _, step := range configs {
					var cfg map[string]any
					var want any
					switch step.Name {
					case "loop":
						cfg = step.Config["step"].(map[string]any)["config"].(map[string]any)
						want = pool[1]
					case "fail", "undo-last":
						cfg, want = step.Config, pool[size-1]
					case "forward", "undo-first":
						cfg, want = step.Config, pool[0]
					default:
						continue
					}
					resolved, err := pipeline.NewTemplateEngine().ResolveMap(map[string]any{"participant": cfg["participant"]}, pc)
					if err != nil || resolved["participant"] != want {
						t.Errorf("%s runtime participant was shadowed by promoted SDK output: got %+v, want %v, error=%v", step.Name, resolved, want, err)
					}
					checked++
				}
				wantChecks := 4
				if name == "while" {
					wantChecks = 1
				}
				if checked != wantChecks {
					t.Fatalf("fixture regression did not check every actor lookup: %d != %d", checked, wantChecks)
				}
			})
		}
	}
}

type latePrefixThenBrokenPipe struct {
	buffer            bytes.Buffer
	auditPath         string
	auditedBeforeTail bool
}

func (w *latePrefixThenBrokenPipe) Len() int { return w.buffer.Len() }

func (w *latePrefixThenBrokenPipe) Write(data []byte) (int, error) {
	if w.Len() != 0 {
		_, err := os.Stat(w.auditPath)
		w.auditedBeforeTail = err == nil
		return 0, syscall.EPIPE
	}
	if len(data) > 1024 {
		return 0, syscall.EPIPE
	}
	return w.buffer.Write(data)
}

func TestFixtureLateOversizedOutputAuditsWrittenPrefixBeforeTailFailure(t *testing.T) {
	root := t.TempDir()
	emission := filepath.Join(root, "emitted.jsonl")
	writer := &latePrefixThenBrokenPipe{auditPath: emission}
	plugin := &provider{stdout: writer}
	arm, err := plugin.CreateStep("step.record_fixture", "arm", nil)
	if err != nil {
		t.Fatal(err)
	}
	release, err := plugin.CreateStep("step.record_fixture_aux", "result", nil)
	if err != nil {
		t.Fatal(err)
	}
	metadata := map[string]any{"pipeline": "selected", "started_at": "fixture-started"}
	first, err := arm.Execute(t.Context(), nil, nil, nil, metadata, map[string]any{
		"mode": "arm-late-stdout", "late_mode": "late-stdout-overflow", "audit_path": filepath.Join(root, "armed.jsonl"), "emission_path": emission,
	})
	if err != nil || first == nil {
		t.Fatalf("arm did not return its successful RPC result: %+v, %v", first, err)
	}
	result, err := release.Execute(t.Context(), nil, map[string]map[string]any{"arm": first.Output}, nil, metadata, map[string]any{
		"mode": "late-stdout-overflow", "audit_path": filepath.Join(root, "released.jsonl"),
	})
	if result != nil || !errors.Is(err, syscall.EPIPE) || writer.Len() == 0 || !writer.auditedBeforeTail {
		t.Fatalf("oversized tail failure lost proof of the actual bounded late write: result=%+v error=%v bytes=%d checkpoint=%v", result, err, writer.Len(), writer.auditedBeforeTail)
	}
	data, err := os.ReadFile(emission)
	if err != nil {
		t.Fatal(err)
	}
	var audit struct {
		Mode string            `json:"mode"`
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(data, &audit); err != nil || audit.Mode != "late-stdout-overflow-emitted" || audit.Data["bytes"] != strconv.Itoa(writer.Len()) || audit.Data["requested_bytes"] != "1048577" || audit.Data["stage"] != "prefix" {
		t.Fatalf("late write checkpoint did not record actual bytes before cancellation: %s, %v", data, err)
	}
}

func TestFixtureCompositeRetryAuditsResolvedConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	step, err := new(provider).CreateStep("step.record_fixture", "work", nil)
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"marker": "private-parent-marker"}
	prior := map[string]map[string]any{"prepare": {"greeting": "prepared-pool"}}
	metadata := map[string]any{"pipeline": "selected", "started_at": "fixture-started"}
	config := map[string]any{
		"mode": "composite-flaky", "audit_path": path, "participant": "participant-b",
		"pool": "parent-pool", "ordinal": "1", "action": "retry",
	}
	first, err := step.Execute(t.Context(), input, prior, nil, metadata, config)
	if err == nil || first != nil {
		t.Fatalf("first call must fail before the engine retries: %+v, %v", first, err)
	}
	second, err := step.Execute(t.Context(), input, prior, nil, metadata, config)
	if err != nil || second == nil || second.Output["attempt"] != "2" || second.Output["participant"] != "participant-b" {
		t.Fatalf("second call must return the real SDK response after one failure: %+v, %v", second, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	for _, attempt := range []string{"1", "2"} {
		var audit struct {
			Mode string            `json:"mode"`
			Data map[string]string `json:"data"`
		}
		if err := decoder.Decode(&audit); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{
			"participant": "participant-b", "pool": "parent-pool", "ordinal": "1", "action": "retry",
			"attempt": attempt, "marker": "private-parent-marker", "prior_greeting": "prepared-pool", "pipeline": "selected",
		}
		if audit.Mode != "composite-flaky" || !reflect.DeepEqual(audit.Data, want) {
			t.Fatalf("SDK call did not audit resolved config, parent input, and prior output: %+v", audit)
		}
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatalf("unexpected extra SDK audit: %v", err)
	}
}

func TestFixtureCompositeModesReturnSDKProperties(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mode     string
		ordinal  string
		wantMore bool
		wantErr  bool
	}{
		{name: "success", mode: "composite", ordinal: "0"},
		{name: "error", mode: "composite-error", ordinal: "0", wantErr: true},
		{name: "compensate", mode: "composite-compensate", ordinal: "0"},
		{name: "while-next", mode: "composite-while", ordinal: "0", wantMore: true},
		{name: "while-last", mode: "composite-while", ordinal: "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			step, err := new(provider).CreateStep("step.record_fixture", "work", nil)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "audit.jsonl")
			result, err := step.Execute(t.Context(), map[string]any{"marker": tc.name},
				map[string]map[string]any{"prepare": {"greeting": "caller-pool"}}, nil,
				map[string]any{"pipeline": "selected", "started_at": "fixture-started"},
				map[string]any{
					"mode": tc.mode, "audit_path": path, "participant": "caller-participant", "pool": "caller-pool",
					"ordinal": tc.ordinal, "action": tc.name, "last_ordinal": "1",
				})
			if tc.wantErr {
				if err == nil || result != nil {
					t.Fatalf("failed SDK operation was reported as success: %+v, %v", result, err)
				}
			} else {
				if err != nil || result == nil {
					t.Fatalf("SDK operation failed: %+v, %v", result, err)
				}
				want := map[string]any{
					"participant": "caller-participant", "pool": "caller-pool", "ordinal": tc.ordinal,
					"action": tc.name, "attempt": "1", "ready": true, "process_id": result.Output["process_id"],
					"echo": tc.name, "prior_greeting": "caller-pool",
				}
				if tc.mode == "composite-while" {
					want["continue"] = tc.wantMore
				}
				if !reflect.DeepEqual(result.Output, want) {
					t.Fatalf("SDK response did not preserve the supplied properties: got %+v, want %+v", result.Output, want)
				}
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var audit struct {
				Mode string            `json:"mode"`
				Data map[string]string `json:"data"`
			}
			if err := json.Unmarshal(data, &audit); err != nil || audit.Mode != tc.mode || audit.Data["action"] != tc.name || audit.Data["ordinal"] != tc.ordinal {
				t.Fatalf("SDK operation was not independently audited: %s, %v", data, err)
			}
		})
	}
}

func TestFixtureManifestMatchesSDKCatalog(t *testing.T) {
	var disk struct {
		Name      string   `json:"name"`
		Version   string   `json:"version"`
		StepTypes []string `json:"stepTypes"`
	}
	if err := json.Unmarshal(manifestJSON, &disk); err != nil {
		t.Fatal(err)
	}
	plugin := new(provider)
	runtime := plugin.Manifest()
	if runtime.Name != disk.Name || runtime.Version != disk.Version || !slices.Equal(plugin.StepTypes(), disk.StepTypes) {
		t.Fatalf("baseline disk/SDK declarations differ: %+v, %+v, %v", disk, runtime, plugin.StepTypes())
	}
	for _, stepType := range disk.StepTypes {
		if step, err := plugin.CreateStep(stepType, "fixture", nil); err != nil || step == nil {
			t.Fatalf("declared SDK factory is missing: %s: %v", stepType, err)
		}
	}
	if _, err := plugin.CreateStep("step.record_fixture_absent", "fixture", nil); err == nil {
		t.Fatal("fixture unexpectedly exports the runtime-mismatch test type")
	}
}

func TestFixtureWaitAuditAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	step := new(recordStep)
	result, err := step.Execute(ctx, nil, nil, nil, nil, map[string]any{"mode": "wait", "audit_path": path})
	if result != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("wait mode did not return cancellation: %+v, %v", result, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var audit struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(data, &audit); err != nil || audit.Mode != "wait" {
		t.Fatalf("wait mode did not establish its Execute audit: %s, %v", data, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("audit permissions are not private: %v, %v", info, err)
	}
}

func TestFixtureLateOutputRequiresPriorRPCResult(t *testing.T) {
	root := t.TempDir()
	plugin := new(provider)
	var output bytes.Buffer
	plugin.stdout = &output
	arm, err := plugin.CreateStep("step.record_fixture", "arm", nil)
	if err != nil {
		t.Fatal(err)
	}
	release, err := plugin.CreateStep("step.record_fixture_aux", "result", nil)
	if err != nil {
		t.Fatal(err)
	}
	metadata := map[string]any{"pipeline": "selected", "started_at": "fixture-started"}
	emission := filepath.Join(root, "emitted.jsonl")
	first, err := arm.Execute(t.Context(), nil, nil, nil, metadata, map[string]any{
		"mode": "arm-late-stdout", "late_mode": "late-stdout", "audit_path": filepath.Join(root, "armed.jsonl"), "emission_path": emission,
	})
	if err != nil || first == nil || first.Output["late_stdout"] != "late-stdout" {
		t.Fatalf("arming must return a successful prior-RPC-shaped result: %+v, %v", first, err)
	}
	if _, err := os.Stat(emission); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("late output was emitted before its release barrier: %v", err)
	}
	if output.Len() != 0 {
		t.Fatal("late emitter wrote before the successful-response barrier")
	}
	second, err := release.Execute(t.Context(), nil, map[string]map[string]any{"arm": first.Output}, nil, metadata, map[string]any{
		"mode": "late-stdout", "audit_path": filepath.Join(root, "released.jsonl"),
	})
	if err != nil || second == nil {
		t.Fatalf("release after prior result did not finish: %+v, %v", second, err)
	}
	if !bytes.Contains(output.Bytes(), []byte("fixture-late-stdout-canary")) {
		t.Fatal("late emitter did not write its actual output after release")
	}
	data, err := os.ReadFile(emission)
	if err != nil {
		t.Fatal(err)
	}
	var audit struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(data, &audit); err != nil || audit.Mode != "late-stdout-emitted" {
		t.Fatalf("stdout write was not independently audited: %s, %v", data, err)
	}
}
