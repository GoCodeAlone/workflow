package main

// Package contracts do not prove desktop activation. These tests also work in
// an extracted public package, without importing the Workflow engine module.
import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func packageRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readObject(t *testing.T, path string) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(readFile(t, path), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestFailureVectorsDoNotEnterRunnableExampleDiscovery(t *testing.T) {
	err := filepath.WalkDir(packageRoot(t), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ext := strings.ToLower(filepath.Ext(path)); ext == ".yaml" || ext == ".yml" {
			t.Errorf("fixture enters runnable example discovery: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDirectLocalTransport(t *testing.T) {
	root := packageRoot(t)
	plugin := readObject(t, filepath.Join(root, "plugin.json"))
	if plugin["name"] != "workflow-assistant" {
		t.Fatal("plugin name changed")
	}
	if plugin["extensions"].(map[string]any)["com.openai"].(map[string]any)["requires_local_executor"] != true {
		t.Fatal("local executor requirement absent")
	}
	servers := readObject(t, filepath.Join(root, "mcp.json"))["mcpServers"]
	want := map[string]any{"workflow-config": map[string]any{"type": "stdio", "command": "wfctl", "args": []any{"mcp", "-plugin-dir", "${PLUGIN_DATA}/engine-plugins"}, "cwd": "${PLUGIN_ROOT}", "env": map[string]any{"WFCTL_NO_UPDATE_CHECK": "1"}}}
	if !reflect.DeepEqual(servers, want) {
		t.Fatalf("MCP config differs from direct local transport: %#v", servers)
	}
}

func TestSkillDiscoveryAndSharedContract(t *testing.T) {
	root := packageRoot(t)
	paths, err := filepath.Glob(filepath.Join(root, "skills", "*", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, path := range paths {
		names = append(names, filepath.Base(filepath.Dir(path)))
	}
	slices.Sort(names)
	if want := []string{"workflow-configure", "workflow-diagnose", "workflow-readiness"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("skills: %v", names)
	}
	for _, path := range paths {
		body := string(readFile(t, path))
		parts := strings.SplitN(body, "---", 3)
		if len(parts) != 3 {
			t.Fatalf("missing front matter: %s", path)
		}
		front := parts[1]
		if !strings.Contains(front, "name: "+filepath.Base(filepath.Dir(path))+"\n") || !strings.Contains(front, "description: Use when") || len(front) >= 1024 {
			t.Errorf("invalid skill front matter: %s", path)
		}
		if !strings.Contains(body, "../../references/workflow-contract.md") {
			t.Errorf("shared contract missing: %s", path)
		}
	}
}

func TestPublicPortableFilesAndLinks(t *testing.T) {
	root := packageRoot(t)
	if _, err := os.Stat(filepath.Join(root, "LICENSE")); err != nil {
		t.Fatal(err)
	}
	forbidden := []string{"/Users/", "/home/", "BEGIN PRIVATE KEY", "ghp_", "sk-live-", "app://"}
	links := regexp.MustCompile(`\]\(([^)]+)\)`)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			t.Errorf("symlink in public package: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if !slices.Contains([]string{".json", ".md", ".yaml", ".txt"}, ext) {
			return nil
		}
		body := string(readFile(t, path))
		for _, token := range forbidden {
			if strings.Contains(body, token) {
				t.Errorf("nonportable/sensitive token in %s", path)
			}
		}
		if ext == ".md" {
			for _, match := range links.FindAllStringSubmatch(body, -1) {
				target := match[1]
				if strings.Contains(target, "://") || strings.HasPrefix(target, "#") {
					continue
				}
				dest := filepath.Join(filepath.Dir(path), strings.SplitN(target, "#", 2)[0])
				resolved, err := filepath.EvalSymlinks(dest)
				if err != nil {
					t.Errorf("broken link %s in %s", target, path)
					continue
				}
				relative, err := filepath.Rel(root, resolved)
				if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
					t.Errorf("link escapes package: %s", target)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"hooks", ".app.json", ".mcp.json", ".codex-plugin"} {
		if _, err := os.Lstat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Errorf("unexpected host surface: %s", name)
		}
	}
}

func TestFixtureByteDigests(t *testing.T) {
	want := map[string]string{
		"invalid":              "a7c78deb71c57f46b2948048d8b7c8fe9cf8df57cc371260f923dbc87d85a5bf",
		"minimal":              "5c622dee665432094842b38494b25fc37fd184ae0184167649e71bfb5c49f3f8",
		"missing-plugin":       "e775e4d6eb0b26e05ad46a559fa601335d892e6ef6afaea55312757c93c73905",
		"missing-prerequisite": "5153131368bef320f7b21cfacc9a33b185a87ca020c4b10b580f16bcc212a5f4",
		"unknown-types":        "b6ad2dffbe6a86735220654ce431f1b689e316cb5d9da963776782260920f0f7",
		"unresolved-import":    "bef92b2470cac66675e14f7e5033203c7b1a8858773e7e4048d788ce6b97a0f6",
	}
	paths, err := filepath.Glob(filepath.Join(packageRoot(t), "fixtures", "*.yaml.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != len(want) {
		t.Fatal("fixture inventory changed")
	}
	for name, hash := range want {
		if digest(readFile(t, filepath.Join(packageRoot(t), "fixtures", name+".yaml.txt"))) != hash {
			t.Errorf("fixture bytes changed: %s", name)
		}
	}
}
