package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

func rewriteManifestConfigRefs(patterns string, mappings map[string]string, out io.Writer) error {
	if len(mappings) == 0 {
		return nil
	}
	files, err := expandConfigPatterns(patterns)
	if err != nil {
		return err
	}
	for _, file := range files {
		changed, err := rewriteEnvRefsInFile(file, mappings)
		if err != nil {
			return err
		}
		if out == nil {
			continue
		}
		if changed {
			fmt.Fprintf(out, "  %s: updated env references\n", file)
		} else {
			fmt.Fprintf(out, "  %s: no mapped env references found\n", file)
		}
	}
	return nil
}

func rewriteEnvRefsInFile(path string, mappings map[string]string) (bool, error) {
	info, statErr := os.Stat(path)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return false, nil
		}
		return false, fmt.Errorf("stat config %s: %w", path, statErr)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read config %s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false, fmt.Errorf("parse config %s: %w", path, err)
	}
	changed := rewriteEnvRefsInNode(&doc, mappings)
	if !changed {
		return false, nil
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		_ = enc.Close()
		return false, fmt.Errorf("encode config %s: %w", path, err)
	}
	if err := enc.Close(); err != nil {
		return false, fmt.Errorf("encode config %s: %w", path, err)
	}
	if err := os.WriteFile(path, buf.Bytes(), info.Mode().Perm()); err != nil {
		return false, fmt.Errorf("write config %s: %w", path, err)
	}
	return true, nil
}

func rewriteEnvRefsInNode(node *yaml.Node, mappings map[string]string) bool {
	if node == nil {
		return false
	}
	changed := false
	if node.Kind == yaml.ScalarNode && strings.Contains(node.Value, "${") {
		next := rewriteEnvRefsInString(node.Value, mappings)
		if next != node.Value {
			node.Value = next
			changed = true
		}
	}
	for _, child := range node.Content {
		if rewriteEnvRefsInNode(child, mappings) {
			changed = true
		}
	}
	return changed
}

func rewriteEnvRefsInString(value string, mappings map[string]string) string {
	if len(mappings) == 0 || !strings.Contains(value, "${") {
		return value
	}
	keys := make([]string, 0, len(mappings))
	for key := range mappings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := value
	for _, logical := range keys {
		stored := strings.TrimSpace(mappings[logical])
		if stored == "" || stored == logical {
			continue
		}
		out = strings.ReplaceAll(out, "${"+logical+"}", "${"+stored+"}")
	}
	return out
}
