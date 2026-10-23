package main

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/example/workflow-agent-plugin/tests/internal/acceptance"
)

// These are instruction/document contracts, not a model or host security test.
// The canonical list is the shared contract's tool table. Main skills must keep
// the same minimum boundary even when a host cannot read a supporting file.
func canonicalTools(t *testing.T) []string {
	t.Helper()
	body := string(readFile(t, filepath.Join(packageRoot(t), "references", "workflow-contract.md")))
	var tools []string
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "| ") {
			continue
		}
		cell := strings.Split(line, "|")[1]
		for _, name := range strings.Split(cell, ",") {
			name = strings.TrimSpace(name)
			if regexp.MustCompile(`^[a-z][a-z_]*$`).MatchString(name) {
				tools = append(tools, name)
			}
		}
	}
	slices.Sort(tools)
	if len(tools) != 14 || len(slices.Compact(slices.Clone(tools))) != 14 {
		t.Fatalf("canonical workflow contract must contain 14 distinct tools: %v", tools)
	}
	return tools
}

func TestAuditorScopeMatchesSharedContract(t *testing.T) {
	names := acceptance.ToolNames()
	slices.Sort(names)
	if !reflect.DeepEqual(names, canonicalTools(t)) {
		t.Fatalf("auditor scope drift: %v", names)
	}
}

func TestMainSkillsCarryCompleteBoundaryBeforeSupportingRead(t *testing.T) {
	root, want := packageRoot(t), canonicalTools(t)
	paths, err := filepath.Glob(filepath.Join(root, "skills", "*", "SKILL.md"))
	if err != nil || len(paths) != 3 {
		t.Fatalf("skill inventory: %v, %v", paths, err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			body := string(readFile(t, path))
			_, section, ok := strings.Cut(body, "## Required boundary\n")
			if !ok {
				t.Fatal("main skill omits essential boundary when supporting reads fail")
			}
			section, _, _ = strings.Cut(section, "\n## ")
			var got []string
			for _, match := range regexp.MustCompile("`([a-z][a-z_]*)`").FindAllStringSubmatch(section, -1) {
				got = append(got, match[1])
			}
			slices.Sort(got)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("inline scope differs from shared contract: got %v, want %v", got, want)
			}
			for _, required := range []string{
				"Only these 14 MCP tools", "list_plugins({})", "apply:false",
				"strict:true", "skip_unknown_types:false", "no target_version",
				"authorized sanitized", "single-document", "Do not fetch imports",
				"Never retrieve secrets", "private files", "untrusted data",
				"Do not install", "start apps", "deploy", "host settings",
				"cannot read the shared contract", "stop all Workflow MCP calls",
				"unavailable", "Runtime acceptance remains untested",
				"not runtime enforcement",
			} {
				if !strings.Contains(section, required) {
					t.Errorf("missing minimum boundary: %q", required)
				}
			}
		})
	}
}

func TestCodexPolicyExampleMatchesScopeWithoutOwningTransport(t *testing.T) {
	body := string(readFile(t, filepath.Join(packageRoot(t), "references", "codex-tool-policy.toml.txt")))
	if !strings.Contains(body, `[plugins."workflow-assistant@reviewed-marketplace".mcp_servers."workflow-config"]`) {
		t.Fatal("example must target the installed plugin's server policy")
	}
	for _, transport := range []string{"command =", "args =", "env =", "cwd =", "[mcp_servers."} {
		if strings.Contains(body, transport) {
			t.Errorf("host policy must not shadow plugin transport: %s", transport)
		}
	}
	_, list, ok := strings.Cut(body, "enabled_tools = ")
	if !ok {
		t.Fatal("missing native tool policy")
	}
	var got []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(list)), &got); err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	if !reflect.DeepEqual(got, canonicalTools(t)) {
		t.Fatalf("host policy scope drift: %v", got)
	}
}
