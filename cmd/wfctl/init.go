package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed all:templates
var templateFS embed.FS

// templateData holds values passed to each template during rendering.
type templateData struct {
	Name        string // project name as given (e.g. "my-api")
	NameCamel   string // CamelCase version (e.g. "MyApi")
	Author      string // github username or org
	Description string // short description
}

// templateDef describes a single file to generate.
type templateDef struct {
	// src is the path within templateFS (e.g. "templates/api-service/main.go.tmpl")
	src string
	// dst is the output path relative to the project dir.
	// Empty string means derive from src by stripping the template prefix and ".tmpl".
	dst string
}

// projectTemplate describes a full project scaffold.
type projectTemplate struct {
	name        string
	description string
	files       []templateDef
}

var projectTemplates = map[string]projectTemplate{
	"api-service": {
		name:        "api-service",
		description: "HTTP API service with health check and metrics",
		files: []templateDef{
			{src: "templates/api-service/go.mod.tmpl"},
			{src: "templates/api-service/main.go.tmpl"},
			{src: "templates/api-service/workflow.yaml.tmpl"},
			{src: "templates/api-service/README.md.tmpl"},
			{src: "templates/api-service/Dockerfile.tmpl"},
			{src: "templates/api-service/.gitignore.tmpl"},
			{src: "templates/api-service/.github/workflows/ci.yml.tmpl", dst: ".github/workflows/ci.yml"},
		},
	},
	"event-processor": {
		name:        "event-processor",
		description: "Event-driven processor with state machine and messaging",
		files: []templateDef{
			{src: "templates/event-processor/go.mod.tmpl"},
			{src: "templates/event-processor/main.go.tmpl"},
			{src: "templates/event-processor/workflow.yaml.tmpl"},
			{src: "templates/event-processor/README.md.tmpl"},
			{src: "templates/event-processor/Dockerfile.tmpl"},
			{src: "templates/event-processor/.gitignore.tmpl"},
			{src: "templates/event-processor/.github/workflows/ci.yml.tmpl", dst: ".github/workflows/ci.yml"},
		},
	},
	"full-stack": {
		name:        "full-stack",
		description: "API service + React UI (Vite + TypeScript)",
		files: []templateDef{
			{src: "templates/full-stack/go.mod.tmpl"},
			{src: "templates/full-stack/main.go.tmpl"},
			{src: "templates/full-stack/workflow.yaml.tmpl"},
			{src: "templates/full-stack/README.md.tmpl"},
			{src: "templates/full-stack/Dockerfile.tmpl"},
			{src: "templates/full-stack/.gitignore.tmpl"},
			{src: "templates/full-stack/ui/package.json.tmpl", dst: "ui/package.json"},
			{src: "templates/full-stack/ui/vite.config.ts.tmpl", dst: "ui/vite.config.ts"},
			{src: "templates/full-stack/ui/index.html.tmpl", dst: "ui/index.html"},
			{src: "templates/full-stack/ui/src/main.tsx.tmpl", dst: "ui/src/main.tsx"},
			{src: "templates/full-stack/ui/src/App.tsx.tmpl", dst: "ui/src/App.tsx"},
			{src: "templates/full-stack/.github/workflows/ci.yml.tmpl", dst: ".github/workflows/ci.yml"},
		},
	},
	"plugin": {
		name:        "plugin",
		description: "External workflow engine plugin (gRPC, go-plugin)",
		files: []templateDef{
			{src: "templates/plugin/go.mod.tmpl"},
			{src: "templates/plugin/main.go.tmpl"},
			{src: "templates/plugin/plugin.go.tmpl"},
			{src: "templates/plugin/README.md.tmpl"},
			{src: "templates/plugin/.gitignore.tmpl"},
			{src: "templates/plugin/.github/workflows/release.yml.tmpl", dst: ".github/workflows/release.yml"},
		},
	},
	"ui-plugin": {
		name:        "ui-plugin",
		description: "External plugin with embedded React UI",
		files: []templateDef{
			{src: "templates/ui-plugin/go.mod.tmpl"},
			{src: "templates/ui-plugin/main.go.tmpl"},
			{src: "templates/ui-plugin/plugin.go.tmpl"},
			{src: "templates/ui-plugin/README.md.tmpl"},
			{src: "templates/ui-plugin/.gitignore.tmpl"},
			{src: "templates/ui-plugin/ui/package.json.tmpl", dst: "ui/package.json"},
			{src: "templates/ui-plugin/ui/vite.config.ts.tmpl", dst: "ui/vite.config.ts"},
			{src: "templates/ui-plugin/ui/index.html.tmpl", dst: "ui/index.html"},
			{src: "templates/ui-plugin/ui/src/main.tsx.tmpl", dst: "ui/src/main.tsx"},
			{src: "templates/ui-plugin/ui/src/App.tsx.tmpl", dst: "ui/src/App.tsx"},
			{src: "templates/ui-plugin/.github/workflows/release.yml.tmpl", dst: ".github/workflows/release.yml"},
		},
	},
}

func runInit(args []string) error {
	fs2 := flag.NewFlagSet("init", flag.ContinueOnError)
	tmplName := fs2.String("template", "api-service", "Project template to use")
	author := fs2.String("author", "", "Author (GitHub username or org, used in go.mod module path)")
	desc := fs2.String("description", "", "Project description")
	output := fs2.String("output", "", "Output directory (defaults to project name)")
	listTemplates := fs2.Bool("list", false, "List available templates and exit")
	fs2.Usage = func() {
		fmt.Fprintf(fs2.Output(), `Usage: wfctl init [options] <project-name>

Scaffold a new workflow application project.

Options:
`)
		fs2.PrintDefaults()
		fmt.Fprintln(fs2.Output())
		printTemplateList(fs2.Output())
	}

	if err := fs2.Parse(args); err != nil {
		return err
	}

	if *listTemplates {
		printTemplateList(os.Stdout)
		return nil
	}

	if fs2.NArg() < 1 {
		fs2.Usage()
		return fmt.Errorf("project name is required")
	}

	projectName := fs2.Arg(0)
	if err := validateProjectName(projectName); err != nil {
		return err
	}

	tmpl, ok := projectTemplates[*tmplName]
	if !ok {
		printTemplateList(os.Stderr)
		return fmt.Errorf("unknown template %q", *tmplName)
	}

	if *author == "" {
		*author = "your-org"
	}
	if *desc == "" {
		*desc = tmpl.description
	}

	outDir := *output
	if outDir == "" {
		outDir = projectName
	}

	data := templateData{
		Name:        projectName,
		NameCamel:   toCamelCaseInit(projectName),
		Author:      *author,
		Description: *desc,
	}

	if err := renderTemplate(tmpl, outDir, data); err != nil {
		return err
	}

	printNextSteps(projectName, *tmplName, outDir)
	return nil
}

func renderTemplate(tmpl projectTemplate, outDir string, data templateData) error {
	for _, f := range tmpl.files {
		dst := f.dst
		if dst == "" {
			// Derive destination from source: strip "templates/<name>/" prefix and ".tmpl" suffix.
			// e.g. "templates/api-service/main.go.tmpl" -> "main.go"
			parts := strings.SplitN(f.src, "/", 3)
			if len(parts) < 3 {
				return fmt.Errorf("unexpected template path: %s", f.src)
			}
			dst = strings.TrimSuffix(parts[2], ".tmpl")
		}

		destPath := filepath.Join(outDir, dst)
		if err := os.MkdirAll(filepath.Dir(destPath), 0750); err != nil {
			return fmt.Errorf("create directory for %s: %w", destPath, err)
		}

		content, err := fs.ReadFile(templateFS, f.src)
		if err != nil {
			return fmt.Errorf("read template %s: %w", f.src, err)
		}

		t, err := template.New(filepath.Base(f.src)).Parse(string(content))
		if err != nil {
			return fmt.Errorf("parse template %s: %w", f.src, err)
		}

		out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640) //nolint:gosec // generated project files
		if err != nil {
			return fmt.Errorf("create %s: %w", destPath, err)
		}

		if err := t.Execute(out, data); err != nil {
			out.Close()
			return fmt.Errorf("render template %s: %w", f.src, err)
		}
		if err := out.Close(); err != nil {
			return fmt.Errorf("close %s: %w", destPath, err)
		}

		fmt.Printf("  create  %s\n", filepath.Join(filepath.Base(outDir), dst))
	}
	return nil
}

func validateProjectName(name string) error {
	if name == "" {
		return fmt.Errorf("project name cannot be empty")
	}
	for _, ch := range name {
		if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') && ch != '-' && ch != '_' {
			return fmt.Errorf("project name %q contains invalid character %q (use letters, digits, - or _)", name, ch)
		}
	}
	return nil
}

func printTemplateList(w interface{ Write([]byte) (int, error) }) {
	fmt.Fprintln(w, "Available templates:")
	for _, name := range []string{"api-service", "event-processor", "full-stack", "plugin", "ui-plugin"} {
		t := projectTemplates[name]
		fmt.Fprintf(w, "  %-16s  %s\n", t.name, t.description)
	}
}

func printNextSteps(name, tmplName, outDir string) {
	fmt.Printf("\nProject %q created in %s/\n\n", name, outDir)
	fmt.Println("Next steps:")
	fmt.Printf("  cd %s\n", outDir)

	switch tmplName {
	case "full-stack":
		fmt.Println("  cd ui && npm install && npm run build && cd ..")
		fmt.Println("  go mod tidy")
		fmt.Println("  go run . -config workflow.yaml")
	case "ui-plugin":
		fmt.Println("  cd ui && npm install && npm run build && cd ..")
		fmt.Println("  go mod tidy")
		fmt.Printf("  go build -o %s.plugin .\n", name)
	case "plugin":
		fmt.Println("  go mod tidy")
		fmt.Printf("  go build -o %s.plugin .\n", name)
	default:
		fmt.Println("  go mod tidy")
		fmt.Println("  go run . -config workflow.yaml")
	}
	fmt.Println()
}

// toCamelCaseInit converts a hyphenated name to CamelCase (e.g. "my-api" -> "MyApi").
func toCamelCaseInit(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '_' })
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		if len(p) > 1 {
			b.WriteString(p[1:])
		}
	}
	return b.String()
}
