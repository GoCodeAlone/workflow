package lsp

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DSLSectionDoc holds abbreviated documentation for one DSL section, used for hover.
type DSLSectionDoc struct {
	ID          string
	Title       string
	Description string
	Example     string
}

// topLevelKeyToSectionID maps top-level YAML keys to DSL section IDs in dsl-reference.md.
var topLevelKeyToSectionID = map[string]string{
	"name":            "application",
	"version":         "application",
	"description":     "application",
	"requires":        "application",
	"modules":         "modules",
	"workflows":       "workflows",
	"pipelines":       "pipelines",
	"triggers":        "triggers",
	"imports":         "imports",
	"configProviders": "config-providers",
	"platform":        "platform",
	"infrastructure":  "platform",
	"sidecars":        "platform",
}

// sectionKindToSectionID maps SectionKind values to DSL section IDs.
var sectionKindToSectionID = map[SectionKind]string{
	SectionModules:  "modules",
	SectionWorkflow: "workflows",
	SectionPipeline: "pipelines",
	SectionTriggers: "triggers",
	SectionImports:  "imports",
	SectionRequires: "application",
}

// loadDSLSections locates docs/dsl-reference.md by searching upward from cwd,
// parses it, and returns a map of section ID → DSLSectionDoc.
// Returns nil (no error) if the file cannot be found — hover will skip DSL docs.
func loadDSLSections() map[string]*DSLSectionDoc {
	data, err := findDSLReferenceFile()
	if err != nil {
		return nil
	}
	return parseDSLSections(string(data))
}

// findDSLReferenceFile searches for docs/dsl-reference.md upward from the cwd.
func findDSLReferenceFile() ([]byte, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getwd: %w", err)
	}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		p := filepath.Join(dir, "docs", "dsl-reference.md")
		if data, err2 := os.ReadFile(p); err2 == nil {
			return data, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return nil, fmt.Errorf("docs/dsl-reference.md not found")
}

var reDSLSectionComment = regexp.MustCompile(`<!--\s*section:\s*(\S+)\s*-->`)

// parseDSLSections parses the dsl-reference.md content and returns a map of
// section ID → DSLSectionDoc.
func parseDSLSections(md string) map[string]*DSLSectionDoc {
	lines := strings.Split(md, "\n")

	// Collect section boundaries.
	type boundary struct {
		id    string
		start int
	}
	var boundaries []boundary
	for i, line := range lines {
		if m := reDSLSectionComment.FindStringSubmatch(line); m != nil {
			boundaries = append(boundaries, boundary{id: m[1], start: i})
		}
	}

	out := make(map[string]*DSLSectionDoc, len(boundaries))
	for bi, b := range boundaries {
		end := len(lines)
		if bi+1 < len(boundaries) {
			end = boundaries[bi+1].start
		}
		doc := parseDSLSectionDoc(b.id, lines[b.start:end])
		out[b.id] = doc
	}
	return out
}

// parseDSLSectionDoc extracts the title, first-paragraph description, and example
// from a section's lines.
func parseDSLSectionDoc(id string, lines []string) *DSLSectionDoc {
	doc := &DSLSectionDoc{ID: id}

	const (
		stateInit    = iota
		stateDesc    // collecting first paragraph
		stateExample // inside fenced code block under ### Example
		stateOther
	)
	state := stateInit
	var descLines []string
	var exampleLines []string
	inFence := false

	for _, raw := range lines {
		if reDSLSectionComment.MatchString(raw) {
			continue
		}
		if strings.HasPrefix(raw, "## ") {
			doc.Title = strings.TrimPrefix(raw, "## ")
			state = stateDesc
			continue
		}
		if strings.HasPrefix(raw, "### ") {
			sub := strings.ToLower(strings.TrimPrefix(raw, "### "))
			if strings.Contains(sub, "example") {
				state = stateExample
			} else {
				state = stateOther
			}
			continue
		}
		if strings.HasPrefix(raw, "```") {
			if state == stateExample {
				if !inFence {
					inFence = true
					continue
				}
				inFence = false
				continue
			}
		}

		switch state {
		case stateDesc:
			if raw == "---" {
				continue
			}
			if len(descLines) == 0 && strings.TrimSpace(raw) == "" {
				continue
			}
			// Stop at first blank line after content.
			if strings.TrimSpace(raw) == "" && len(descLines) > 0 {
				state = stateOther
				continue
			}
			descLines = append(descLines, raw)
		case stateExample:
			if inFence {
				exampleLines = append(exampleLines, raw)
			}
		}
	}

	doc.Description = strings.TrimSpace(strings.Join(descLines, " "))
	doc.Example = strings.Join(exampleLines, "\n")
	return doc
}
