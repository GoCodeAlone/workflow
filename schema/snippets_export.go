package schema

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strings"
)

// ExportSnippetsVSCode returns all snippets in VSCode snippet JSON format.
// The output can be saved to a .code-snippets file or a language-specific
// snippet file in the .vscode directory.
func ExportSnippetsVSCode() ([]byte, error) {
	snippets := GetSnippets()
	out := make(map[string]vscodeSnippet, len(snippets))
	for _, s := range snippets {
		out[s.Name] = vscodeSnippet{
			Prefix:      s.Prefix,
			Body:        s.Body,
			Description: s.Description,
		}
	}
	return json.MarshalIndent(out, "", "  ")
}

type vscodeSnippet struct {
	Prefix      string   `json:"prefix"`
	Body        []string `json:"body"`
	Description string   `json:"description,omitempty"`
}

// jetbrainsTemplateSet is the root XML element for JetBrains live templates.
type jetbrainsTemplateSet struct {
	XMLName   xml.Name           `xml:"templateSet"`
	Group     string             `xml:"group,attr"`
	Templates []jetbrainsTemplate `xml:"template"`
}

type jetbrainsTemplate struct {
	Name        string                `xml:"name,attr"`
	Value       string                `xml:"value,attr"`
	Description string                `xml:"description,attr"`
	ToReformat  bool                  `xml:"toReformat,attr"`
	ToShortenFQ bool                  `xml:"toShortenFQNames,attr"`
	Variables   []jetbrainsVariable   `xml:"variable,omitempty"`
	Contexts    []jetbrainsContext     `xml:"context"`
}

type jetbrainsVariable struct {
	Name         string `xml:"name,attr"`
	Expression   string `xml:"expression,attr"`
	DefaultValue string `xml:"defaultValue,attr"`
	AlwaysStop   bool   `xml:"alwaysStopAt,attr"`
}

type jetbrainsContext struct {
	Options []jetbrainsOption `xml:"option"`
}

type jetbrainsOption struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// ExportSnippetsJetBrains returns all snippets in JetBrains live template XML format.
// The output can be saved to a .xml file in the JetBrains templates directory.
func ExportSnippetsJetBrains() ([]byte, error) {
	snippets := GetSnippets()
	templates := make([]jetbrainsTemplate, 0, len(snippets))

	for _, s := range snippets {
		body := convertToJetBrainsBody(s.Body)
		vars := extractJetBrainsVars(s.Body)

		tmpl := jetbrainsTemplate{
			Name:        s.Prefix,
			Value:       body,
			Description: s.Description,
			ToReformat:  true,
			ToShortenFQ: false,
			Variables:   vars,
			Contexts: []jetbrainsContext{
				{Options: []jetbrainsOption{
					{Name: "YAML", Value: "true"},
				}},
			},
		}
		templates = append(templates, tmpl)
	}

	ts := jetbrainsTemplateSet{
		Group:     "workflow",
		Templates: templates,
	}

	output, err := xml.MarshalIndent(ts, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JetBrains templates: %w", err)
	}
	return append([]byte(xml.Header), output...), nil
}

// convertToJetBrainsBody converts VSCode ${N:placeholder} syntax to
// JetBrains $VAR_N$ syntax and joins lines with &#10; (XML attribute newline).
func convertToJetBrainsBody(lines []string) string {
	joined := strings.Join(lines, "&#10;")
	// Track seen tab-stop indices to assign unique variable names.
	// Replace ${N:placeholder} with $SNIPPET_N$ and ${N} with $SNIPPET_N$.
	result := strings.Builder{}
	rest := joined
	for len(rest) > 0 {
		idx := strings.Index(rest, "${")
		if idx < 0 {
			result.WriteString(rest)
			break
		}
		result.WriteString(rest[:idx])
		rest = rest[idx+2:]
		// Find the matching closing }
		// Handle nested braces (placeholder may contain :)
		end := strings.Index(rest, "}")
		if end < 0 {
			result.WriteString("${")
			continue
		}
		inner := rest[:end]
		rest = rest[end+1:]
		// inner is like "1:placeholder" or "1"
		colon := strings.Index(inner, ":")
		var num string
		if colon >= 0 {
			num = inner[:colon]
		} else {
			num = inner
		}
		result.WriteString("$SNIPPET_")
		result.WriteString(num)
		result.WriteString("$")
	}
	return result.String()
}

// extractJetBrainsVars extracts unique tab-stop variables from VSCode snippet body lines.
func extractJetBrainsVars(lines []string) []jetbrainsVariable {
	joined := strings.Join(lines, "\n")
	seen := make(map[string]string) // num -> default value
	order := []string{}

	rest := joined
	for len(rest) > 0 {
		idx := strings.Index(rest, "${")
		if idx < 0 {
			break
		}
		rest = rest[idx+2:]
		end := strings.Index(rest, "}")
		if end < 0 {
			break
		}
		inner := rest[:end]
		rest = rest[end+1:]

		colon := strings.Index(inner, ":")
		var num, defVal string
		if colon >= 0 {
			num = inner[:colon]
			defVal = inner[colon+1:]
		} else {
			num = inner
			defVal = ""
		}

		if _, exists := seen[num]; !exists {
			seen[num] = defVal
			order = append(order, num)
		}
	}

	vars := make([]jetbrainsVariable, 0, len(order))
	for _, num := range order {
		vars = append(vars, jetbrainsVariable{
			Name:         "SNIPPET_" + num,
			Expression:   "",
			DefaultValue: `"` + seen[num] + `"`,
			AlwaysStop:   true,
		})
	}
	return vars
}
