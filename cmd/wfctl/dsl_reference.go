package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DSLReferenceOutput is the top-level JSON output of the dsl-reference command.
type DSLReferenceOutput struct {
	Sections []DSLSection `json:"sections"`
}

// DSLSection represents one documented section of the workflow DSL.
type DSLSection struct {
	ID             string     `json:"id"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	RequiredFields []FieldDoc `json:"requiredFields"`
	OptionalFields []FieldDoc `json:"optionalFields"`
	Example        string     `json:"example"`
	Relationships  []string   `json:"relationships"`
	Parent         string     `json:"parent,omitempty"`
}

// FieldDoc documents a single field within a DSL section.
type FieldDoc struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

var (
	reSectionComment = regexp.MustCompile(`<!--\s*section:\s*(\S+)\s*-->`)
	reFieldLine      = regexp.MustCompile("^-\\s+`([^`]+)`\\s+\\(([^)]+)\\)\\s*(?:—|-)?\\s*(.*)")
	reRelLine        = regexp.MustCompile(`^-\s+(.+)`)
)

// findDSLReferenceMarkdown locates docs/dsl-reference.md by searching from the
// current working directory upward, then from the binary's directory upward.
func findDSLReferenceMarkdown() ([]byte, error) {
	candidates := []string{}

	// Search upward from cwd
	if cwd, err := os.Getwd(); err == nil {
		for dir := cwd; ; dir = filepath.Dir(dir) {
			candidates = append(candidates, filepath.Join(dir, "docs", "dsl-reference.md"))
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}

	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err == nil {
			return data, nil
		}
	}
	return nil, fmt.Errorf("docs/dsl-reference.md not found; run from within the workflow repository or set cwd to the repo root")
}

func runDSLReference(args []string) error {
	fs := flag.NewFlagSet("dsl-reference", flag.ExitOnError)
	output := fs.String("output", "", "Write JSON to file instead of stdout")
	refPath := fs.String("reference", "", "Path to dsl-reference.md (default: auto-detect from repo root)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl dsl-reference [options]\n\nParse docs/dsl-reference.md and output structured JSON.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	var mdBytes []byte
	if *refPath != "" {
		b, err := os.ReadFile(*refPath)
		if err != nil {
			return fmt.Errorf("read reference file: %w", err)
		}
		mdBytes = b
	} else {
		b, err := findDSLReferenceMarkdown()
		if err != nil {
			return err
		}
		mdBytes = b
	}

	out, err := parseDSLReference(string(mdBytes))
	if err != nil {
		return fmt.Errorf("parse dsl-reference.md: %w", err)
	}

	w := os.Stdout
	if *output != "" {
		f, err := os.Create(*output)
		if err != nil {
			return fmt.Errorf("create output file: %w", err)
		}
		defer f.Close()
		w = f
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("encode output: %w", err)
	}

	if *output != "" {
		fmt.Fprintf(os.Stderr, "DSL reference written to %s\n", *output)
	}
	return nil
}

// parseDSLReference parses the dsl-reference markdown and returns structured output.
func parseDSLReference(md string) (*DSLReferenceOutput, error) {
	lines := strings.Split(md, "\n")
	var sections []DSLSection

	type pending struct {
		id    string
		start int
	}
	var cur *pending

	// Map of section comment ID → index in sections slice (for setting parent)
	idToIdx := map[string]int{}

	// Collect section boundaries
	type boundary struct {
		id    string
		start int
	}
	var boundaries []boundary

	for i, line := range lines {
		if m := reSectionComment.FindStringSubmatch(line); m != nil {
			_ = cur
			boundaries = append(boundaries, boundary{id: m[1], start: i})
		}
	}

	for bi, b := range boundaries {
		end := len(lines)
		if bi+1 < len(boundaries) {
			end = boundaries[bi+1].start
		}
		sec := parseSection(b.id, lines[b.start:end])
		idToIdx[b.id] = len(sections)
		sections = append(sections, sec)
	}

	// Set parent relationships: IDs containing "-" derive from the prefix
	for i := range sections {
		id := sections[i].ID
		if idx := strings.Index(id, "-"); idx != -1 {
			parentID := id[:idx]
			if _, ok := idToIdx[parentID]; ok {
				sections[i].Parent = parentID
			}
		}
	}

	return &DSLReferenceOutput{Sections: sections}, nil
}

// parseSection parses one section's block of markdown lines.
func parseSection(id string, lines []string) DSLSection {
	sec := DSLSection{ID: id}

	const (
		stateInit     = iota
		stateDesc     // collecting description paragraphs
		stateRequired // under ### Required Fields
		stateOptional // under ### Optional Fields
		stateExample  // inside a fenced code block
		stateRelation // under ### Relationship
		stateOther    // under some other ### subsection
	)

	state := stateInit
	var descLines []string
	var exampleLines []string
	inFence := false

	for _, raw := range lines {
		line := raw

		// Detect section comment and H2 title
		if reSectionComment.MatchString(line) {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			sec.Title = strings.TrimPrefix(line, "## ")
			state = stateDesc
			continue
		}

		// H3 subsection transitions
		if strings.HasPrefix(line, "### ") {
			sub := strings.ToLower(strings.TrimPrefix(line, "### "))
			switch {
			case strings.Contains(sub, "required field"):
				state = stateRequired
			case strings.Contains(sub, "optional field"):
				state = stateOptional
			case strings.Contains(sub, "example"):
				state = stateExample
			case strings.Contains(sub, "relationship"):
				state = stateRelation
			default:
				state = stateOther
			}
			continue
		}

		// Code fence tracking for example block
		if strings.HasPrefix(line, "```") {
			if state == stateExample {
				if !inFence {
					inFence = true
					continue // skip the opening fence line
				}
				inFence = false
				continue // skip the closing fence line
			}
		}

		switch state {
		case stateDesc:
			// Skip separators and empty leading lines when description is empty
			if line == "---" {
				continue
			}
			if len(descLines) == 0 && strings.TrimSpace(line) == "" {
				continue
			}
			// Stop at first blank line after content
			if strings.TrimSpace(line) == "" && len(descLines) > 0 {
				state = stateOther // done with first paragraph
				continue
			}
			descLines = append(descLines, line)

		case stateRequired:
			if f, ok := parseFieldLine(line); ok {
				sec.RequiredFields = append(sec.RequiredFields, f)
			}

		case stateOptional:
			if f, ok := parseFieldLine(line); ok {
				sec.OptionalFields = append(sec.OptionalFields, f)
			}

		case stateExample:
			if inFence {
				exampleLines = append(exampleLines, line)
			}

		case stateRelation:
			if m := reRelLine.FindStringSubmatch(line); m != nil {
				sec.Relationships = append(sec.Relationships, strings.TrimSpace(m[1]))
			}
		}
	}

	sec.Description = strings.Join(descLines, " ")
	sec.Description = strings.TrimSpace(sec.Description)
	sec.Example = strings.Join(exampleLines, "\n")

	return sec
}

// parseFieldLine parses a markdown bullet like:
//   - `name` (string) — description text
func parseFieldLine(line string) (FieldDoc, bool) {
	m := reFieldLine.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return FieldDoc{}, false
	}
	// m[2] may be "string, required" or "string[]" — use as-is
	typ := strings.TrimSpace(m[2])
	// Remove "required" qualifier from type string
	typ = strings.TrimSuffix(typ, ", required")
	typ = strings.TrimSuffix(typ, ",required")
	return FieldDoc{
		Name:        m[1],
		Type:        typ,
		Description: strings.TrimSpace(m[3]),
	}, true
}
