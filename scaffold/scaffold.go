package scaffold

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Options configures scaffold generation.
type Options struct {
	Title    string
	Theme    string // "light" or "dark"
	Auth     bool   // force auth on/off (auto-detected if not set)
	BasePath string // API base path prefix
}

// Spec mirrors the subset of OpenAPI 3.0 we need for scaffolding.
type Spec struct {
	Info       SpecInfo                  `json:"info" yaml:"info"`
	Paths      map[string]*SpecPath      `json:"paths" yaml:"paths"`
	Components *SpecComponents           `json:"components,omitempty" yaml:"components,omitempty"`
}

// SpecInfo holds the info block of an OpenAPI spec.
type SpecInfo struct {
	Title       string `json:"title" yaml:"title"`
	Version     string `json:"version" yaml:"version"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// SpecPath holds the operations for a single path.
type SpecPath struct {
	Get    *SpecOp `json:"get,omitempty" yaml:"get,omitempty"`
	Post   *SpecOp `json:"post,omitempty" yaml:"post,omitempty"`
	Put    *SpecOp `json:"put,omitempty" yaml:"put,omitempty"`
	Delete *SpecOp `json:"delete,omitempty" yaml:"delete,omitempty"`
	Patch  *SpecOp `json:"patch,omitempty" yaml:"patch,omitempty"`
}

// SpecOp describes a single operation in a path.
type SpecOp struct {
	OperationID string          `json:"operationId,omitempty" yaml:"operationId,omitempty"`
	Summary     string          `json:"summary,omitempty" yaml:"summary,omitempty"`
	Tags        []string        `json:"tags,omitempty" yaml:"tags,omitempty"`
	Parameters  []SpecParam     `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	RequestBody *SpecReqBody    `json:"requestBody,omitempty" yaml:"requestBody,omitempty"`
}

// SpecParam describes a parameter in an operation.
type SpecParam struct {
	Name string `json:"name" yaml:"name"`
	In   string `json:"in" yaml:"in"`
}

// SpecReqBody describes the request body of an operation.
type SpecReqBody struct {
	Content map[string]*specMediaType `json:"content,omitempty" yaml:"content,omitempty"`
}

type specMediaType struct {
	Schema *SpecSchema `json:"schema,omitempty" yaml:"schema,omitempty"`
}

// SpecSchema describes a JSON schema.
type SpecSchema struct {
	Ref        string                 `json:"$ref,omitempty" yaml:"$ref,omitempty"`
	Type       string                 `json:"type,omitempty" yaml:"type,omitempty"`
	Properties map[string]*SpecSchema `json:"properties,omitempty" yaml:"properties,omitempty"`
	Required   []string               `json:"required,omitempty" yaml:"required,omitempty"`
	Enum       []string               `json:"enum,omitempty" yaml:"enum,omitempty"`
	Format     string                 `json:"format,omitempty" yaml:"format,omitempty"`
}

// SpecComponents holds the components section of an OpenAPI spec.
type SpecComponents struct {
	Schemas map[string]*SpecSchema `json:"schemas,omitempty" yaml:"schemas,omitempty"`
}

// APIOperation is a parsed API operation for template use.
type APIOperation struct {
	FuncName   string // e.g. "getUsers"
	Method     string // "GET"
	Path       string // "/api/v1/users"
	HasBody    bool
	PathParams []string
}

// ResourceGroup groups related operations under a resource name.
type ResourceGroup struct {
	Name       string // e.g. "Users"
	NameLower  string // e.g. "users"
	NamePlural string // e.g. "users"
	ListOp     *APIOperation
	DetailOp   *APIOperation
	CreateOp   *APIOperation
	UpdateOp   *APIOperation
	DeleteOp   *APIOperation
	FormFields []FormField
}

// FormField describes a field in a generated form.
type FormField struct {
	Name     string
	Label    string
	Type     string // "text", "email", "password", "number", "select"
	Required bool
	Options  []string // for select type
}

// Data is the top-level data passed to all templates.
type Data struct {
	Title        string
	Version      string
	Theme        string
	HasAuth      bool
	LoginPath    string
	RegisterPath string
	Resources    []ResourceGroup
	Operations   []APIOperation
	// Auth-specific operation paths
	LoginOp    *APIOperation
	RegisterOp *APIOperation
}

// authPathRe matches paths that look like auth endpoints.
var authPathRe = regexp.MustCompile(`(?i)(auth|login|register|signup|signin|token|session)`)

// ParseSpec parses a JSON or YAML OpenAPI spec from bytes.
func ParseSpec(data []byte) (*Spec, error) {
	spec := &Spec{}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, fmt.Errorf("spec is empty")
	}
	// Try JSON first (starts with '{'), then YAML.
	if data[0] == '{' {
		if err := json.Unmarshal(data, spec); err != nil {
			return nil, fmt.Errorf("JSON parse error: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(data, spec); err != nil {
			return nil, fmt.Errorf("YAML parse error: %w", err)
		}
	}
	if spec.Paths == nil {
		spec.Paths = make(map[string]*SpecPath)
	}
	return spec, nil
}

// AnalyzeSpec extracts resources, operations, and auth info from the spec.
func AnalyzeSpec(spec *Spec, opts Options) *Data {
	title := spec.Info.Title
	if opts.Title != "" {
		title = opts.Title
	}
	if title == "" {
		title = "My App"
	}

	theme := opts.Theme
	if theme == "" {
		theme = "auto"
	}

	data := &Data{
		Title:   title,
		Version: spec.Info.Version,
		Theme:   theme,
	}

	// Collect all operations.
	var allOps []APIOperation

	// Walk paths sorted for deterministic output.
	paths := make([]string, 0, len(spec.Paths))
	for p := range spec.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	// Detect auth endpoints.
	for _, p := range paths {
		if authPathRe.MatchString(p) {
			pi := spec.Paths[p]
			if pi.Post != nil {
				op := buildAPIOperation(pi.Post, "POST", p)
				pl := strings.ToLower(p)
				if strings.Contains(pl, "login") || strings.Contains(pl, "signin") || strings.Contains(pl, "token") || strings.Contains(pl, "session") {
					data.HasAuth = true
					data.LoginPath = p
					data.LoginOp = &op
				}
				if strings.Contains(pl, "register") || strings.Contains(pl, "signup") {
					data.HasAuth = true
					data.RegisterPath = p
					data.RegisterOp = &op
				}
			}
		}
	}

	if opts.Auth && !data.HasAuth {
		data.HasAuth = true
		if data.LoginPath == "" {
			data.LoginPath = "/auth/login"
		}
		if data.RegisterPath == "" {
			data.RegisterPath = "/auth/register"
		}
	}

	// Group paths into resources, skipping auth paths.
	resourceMap := map[string]*ResourceGroup{}
	resourceOrder := []string{}

	for _, p := range paths {
		if authPathRe.MatchString(p) {
			// Still add auth ops to allOps.
			pi := spec.Paths[p]
			for _, op := range pathOps(pi, p) {
				allOps = append(allOps, op)
			}
			continue
		}

		pi := spec.Paths[p]
		resName := resourceNameFromPath(p)
		if resName == "" {
			continue
		}

		if _, exists := resourceMap[resName]; !exists {
			resourceMap[resName] = &ResourceGroup{
				Name:       toCamelCase(resName),
				NameLower:  strings.ToLower(resName),
				NamePlural: strings.ToLower(resName),
			}
			resourceOrder = append(resourceOrder, resName)
		}

		rg := resourceMap[resName]
		hasPathParam := strings.Contains(p, "{")

		if pi.Get != nil {
			op := buildAPIOperation(pi.Get, "GET", p)
			allOps = append(allOps, op)
			if hasPathParam {
				rg.DetailOp = &op
			} else {
				rg.ListOp = &op
			}
		}
		if pi.Post != nil {
			op := buildAPIOperation(pi.Post, "POST", p)
			allOps = append(allOps, op)
			if !hasPathParam {
				rg.CreateOp = &op
				// Extract form fields from request body.
				if pi.Post.RequestBody != nil {
					rg.FormFields = extractFormFields(pi.Post.RequestBody, spec.Components)
				}
			}
		}
		if pi.Put != nil {
			op := buildAPIOperation(pi.Put, "PUT", p)
			allOps = append(allOps, op)
			rg.UpdateOp = &op
			if rg.CreateOp == nil && pi.Put.RequestBody != nil {
				rg.FormFields = extractFormFields(pi.Put.RequestBody, spec.Components)
			}
		}
		if pi.Patch != nil {
			op := buildAPIOperation(pi.Patch, "PATCH", p)
			allOps = append(allOps, op)
			if rg.UpdateOp == nil {
				rg.UpdateOp = &op
			}
		}
		if pi.Delete != nil {
			op := buildAPIOperation(pi.Delete, "DELETE", p)
			allOps = append(allOps, op)
			rg.DeleteOp = &op
		}
	}

	// Build ordered resources list.
	for _, name := range resourceOrder {
		data.Resources = append(data.Resources, *resourceMap[name])
	}
	data.Operations = allOps

	return data
}

// AnalyzeOnly parses an OpenAPI spec and returns analysis data.
// This is used by the UI to show a preview without generating files.
func AnalyzeOnly(specJSON []byte, opts Options) (*Data, error) {
	spec, err := ParseSpec(specJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to parse OpenAPI spec: %w", err)
	}
	return AnalyzeSpec(spec, opts), nil
}

// GenerateScaffold writes all scaffold files to outDir.
func GenerateScaffold(outDir string, data Data) error {
	// Parse all templates once.
	tmplMap, err := parseScaffoldTemplates()
	if err != nil {
		return fmt.Errorf("failed to parse scaffold templates: %w", err)
	}

	// Generate static files.
	for _, sf := range scaffoldFiles {
		if sf.onlyIf != nil && !sf.onlyIf(data) {
			continue
		}
		tmpl, ok := tmplMap[sf.tmplName]
		if !ok {
			return fmt.Errorf("template %q not found", sf.tmplName)
		}
		destPath := filepath.Join(outDir, sf.path)
		if err := writeTemplate(tmpl, destPath, data); err != nil {
			return fmt.Errorf("generate %s: %w", sf.path, err)
		}
		fmt.Printf("  create  %s\n", filepath.Join(filepath.Base(outDir), sf.path))
	}

	// Generate one page per resource.
	for _, rg := range data.Resources {
		tmpl, ok := tmplMap["ResourcePage.tsx"]
		if !ok {
			return fmt.Errorf("template ResourcePage.tsx not found")
		}
		pagePath := filepath.Join(outDir, "src", "pages", rg.Name+"Page.tsx")
		if err := writeTemplate(tmpl, pagePath, rg); err != nil {
			return fmt.Errorf("generate %sPage.tsx: %w", rg.Name, err)
		}
		fmt.Printf("  create  %s\n", filepath.Join(filepath.Base(outDir), "src", "pages", rg.Name+"Page.tsx"))
	}

	return nil
}

// GenerateToZip generates scaffold files and writes them to a zip archive in memory.
// Returns the zip bytes. Each file is stored as "ui/<path>" inside the zip.
func GenerateToZip(data Data) ([]byte, error) {
	tmplMap, err := parseScaffoldTemplates()
	if err != nil {
		return nil, fmt.Errorf("failed to parse scaffold templates: %w", err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	writeZipEntry := func(path string, tmpl *template.Template, tmplData any) error {
		w, err := zw.Create("ui/" + path)
		if err != nil {
			return fmt.Errorf("zip create %s: %w", path, err)
		}
		return tmpl.Execute(w, tmplData)
	}

	// Generate static files.
	for _, sf := range scaffoldFiles {
		if sf.onlyIf != nil && !sf.onlyIf(data) {
			continue
		}
		tmpl, ok := tmplMap[sf.tmplName]
		if !ok {
			return nil, fmt.Errorf("template %q not found", sf.tmplName)
		}
		if err := writeZipEntry(sf.path, tmpl, data); err != nil {
			return nil, fmt.Errorf("generate %s: %w", sf.path, err)
		}
	}

	// Generate one page per resource.
	for _, rg := range data.Resources {
		tmpl, ok := tmplMap["ResourcePage.tsx"]
		if !ok {
			return nil, fmt.Errorf("template ResourcePage.tsx not found")
		}
		path := "src/pages/" + rg.Name + "Page.tsx"
		if err := writeZipEntry(path, tmpl, rg); err != nil {
			return nil, fmt.Errorf("generate %sPage.tsx: %w", rg.Name, err)
		}
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("close zip: %w", err)
	}
	return buf.Bytes(), nil
}

// writeTemplate renders a template to a file, creating parent directories as needed.
func writeTemplate(tmpl *template.Template, destPath string, data any) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0750); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640) //nolint:gosec // generated UI files
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()
	return tmpl.Execute(f, data)
}

// pathOps returns all operations defined in a path item.
func pathOps(pi *SpecPath, p string) []APIOperation {
	var ops []APIOperation
	if pi == nil {
		return ops
	}
	if pi.Get != nil {
		ops = append(ops, buildAPIOperation(pi.Get, "GET", p))
	}
	if pi.Post != nil {
		ops = append(ops, buildAPIOperation(pi.Post, "POST", p))
	}
	if pi.Put != nil {
		ops = append(ops, buildAPIOperation(pi.Put, "PUT", p))
	}
	if pi.Patch != nil {
		ops = append(ops, buildAPIOperation(pi.Patch, "PATCH", p))
	}
	if pi.Delete != nil {
		ops = append(ops, buildAPIOperation(pi.Delete, "DELETE", p))
	}
	return ops
}

// buildAPIOperation creates an APIOperation from a spec op.
func buildAPIOperation(op *SpecOp, method, path string) APIOperation {
	funcName := op.OperationID
	if funcName == "" {
		funcName = generateFuncName(method, path)
	} else {
		// Ensure it starts lower-case for TS convention.
		if len(funcName) > 0 {
			r := []rune(funcName)
			r[0] = unicode.ToLower(r[0])
			funcName = string(r)
		}
	}

	var pathParams []string
	for _, param := range op.Parameters {
		if param.In == "path" {
			pathParams = append(pathParams, param.Name)
		}
	}
	// Also extract from path pattern {name}.
	for _, m := range regexp.MustCompile(`\{([^}]+)\}`).FindAllStringSubmatch(path, -1) {
		name := m[1]
		found := false
		for _, pp := range pathParams {
			if pp == name {
				found = true
				break
			}
		}
		if !found {
			pathParams = append(pathParams, name)
		}
	}

	return APIOperation{
		FuncName:   funcName,
		Method:     strings.ToUpper(method),
		Path:       path,
		HasBody:    op.RequestBody != nil,
		PathParams: pathParams,
	}
}

// generateFuncName produces a camelCase function name from method + path.
func generateFuncName(method, path string) string {
	// Strip leading slash, remove path params, split on / and -.
	clean := strings.TrimPrefix(path, "/")
	// Replace {param} with "By<Param>"
	re := regexp.MustCompile(`\{([^}]+)\}`)
	clean = re.ReplaceAllStringFunc(clean, func(m string) string {
		inner := m[1 : len(m)-1]
		return "By" + toCamelCase(inner)
	})
	clean = strings.NewReplacer("/", "_", "-", "_", ".", "_").Replace(clean)

	parts := strings.Split(clean, "_")
	var sb strings.Builder
	sb.WriteString(strings.ToLower(method))
	for _, p := range parts {
		if p == "" {
			continue
		}
		sb.WriteString(strings.ToUpper(p[:1]))
		if len(p) > 1 {
			sb.WriteString(p[1:])
		}
	}
	return sb.String()
}

// resourceNameFromPath derives a resource name from a URL path.
// e.g. "/api/v1/users/{id}" -> "users", "/users" -> "users"
func resourceNameFromPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// Find the last non-param, non-version segment.
	for i := len(parts) - 1; i >= 0; i-- {
		seg := parts[i]
		if seg == "" || strings.HasPrefix(seg, "{") {
			continue
		}
		// Skip version segments like "v1", "v2", "api".
		if seg == "api" || regexp.MustCompile(`^v\d+$`).MatchString(seg) {
			continue
		}
		return seg
	}
	return ""
}

// extractFormFields infers form fields from a request body schema.
func extractFormFields(rb *SpecReqBody, components *SpecComponents) []FormField {
	if rb == nil {
		return nil
	}
	mt, ok := rb.Content["application/json"]
	if !ok {
		// Try the first content type.
		for _, v := range rb.Content {
			mt = v
			break
		}
	}
	if mt == nil || mt.Schema == nil {
		return nil
	}

	schema := resolveSchemaRef(mt.Schema, components)
	if schema == nil {
		return nil
	}

	required := map[string]bool{}
	for _, r := range schema.Required {
		required[r] = true
	}

	// Sort property names for deterministic output.
	propNames := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		propNames = append(propNames, name)
	}
	sort.Strings(propNames)

	var fields []FormField
	for _, name := range propNames {
		prop := schema.Properties[name]
		prop = resolveSchemaRef(prop, components)
		if prop == nil {
			prop = &SpecSchema{Type: "string"}
		}
		ft := inferFieldType(name, prop)
		f := FormField{
			Name:     name,
			Label:    toLabel(name),
			Type:     ft,
			Required: required[name],
		}
		if ft == "select" && len(prop.Enum) > 0 {
			f.Options = prop.Enum
		}
		fields = append(fields, f)
	}
	return fields
}

// resolveSchemaRef dereferences a $ref if present.
func resolveSchemaRef(s *SpecSchema, components *SpecComponents) *SpecSchema {
	if s == nil {
		return nil
	}
	if s.Ref == "" || components == nil {
		return s
	}
	// Refs look like "#/components/schemas/Foo"
	parts := strings.Split(s.Ref, "/")
	if len(parts) >= 4 && parts[1] == "components" && parts[2] == "schemas" {
		name := parts[3]
		if components.Schemas != nil {
			if resolved, ok := components.Schemas[name]; ok {
				return resolved
			}
		}
	}
	return s
}

// inferFieldType guesses the HTML input type from name and schema.
func inferFieldType(name string, schema *SpecSchema) string {
	if len(schema.Enum) > 0 {
		return "select"
	}
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "email"):
		return "email"
	case strings.Contains(lower, "password") || strings.Contains(lower, "secret"):
		return "password"
	case schema.Type == "integer" || schema.Type == "number" || schema.Format == "int32" || schema.Format == "int64":
		return "number"
	default:
		return "text"
	}
}

// toLabel converts a camelCase or snake_case field name to a human label.
func toLabel(name string) string {
	// snake_case to words.
	s := strings.ReplaceAll(name, "_", " ")
	// camelCase to words.
	var out []rune
	for i, r := range s {
		if i > 0 && unicode.IsUpper(r) && !unicode.IsUpper(rune(s[i-1])) {
			out = append(out, ' ')
		}
		out = append(out, r)
	}
	s = string(out)
	// Capitalize first letter.
	if len(s) > 0 {
		r := []rune(s)
		r[0] = unicode.ToUpper(r[0])
		s = string(r)
	}
	return s
}

// toCamelCase converts snake_case or kebab-case to CamelCase.
func toCamelCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '_' })
	var sb strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		sb.WriteString(strings.ToUpper(p[:1]))
		if len(p) > 1 {
			sb.WriteString(p[1:])
		}
	}
	return sb.String()
}

// jsPathExpr converts an OpenAPI path like "/users/{id}" to a JS template
// literal expression like "`/users/${id}`".
func jsPathExpr(path string) string {
	result := regexp.MustCompile(`\{([^}]+)\}`).ReplaceAllString(path, `${$1}`)
	if strings.Contains(result, "${") {
		return "`" + result + "`"
	}
	return "'" + result + "'"
}

// tsTupleArgs returns comma-separated TypeScript parameter list for path params + optional body.
// e.g. ("GET", ["id"], false) -> "id: string"
// e.g. ("POST", [], true) -> "data: any"
// e.g. ("PUT", ["id"], true) -> "id: string, data: any"
func tsTupleArgs(method string, pathParams []string, hasBody bool) string {
	var parts []string
	for _, p := range pathParams {
		parts = append(parts, p+": string")
	}
	if hasBody || method == "POST" || method == "PUT" || method == "PATCH" {
		parts = append(parts, "data: any")
	}
	return strings.Join(parts, ", ")
}

// scaffoldFile describes one file to generate.
type scaffoldFile struct {
	// path relative to the output directory.
	path string
	// tmplName keys into scaffoldTemplates map.
	tmplName string
	// onlyIf: if non-nil, the file is only generated when this returns true.
	onlyIf func(Data) bool
}

var scaffoldFiles = []scaffoldFile{
	{path: "package.json", tmplName: "package.json"},
	{path: "tsconfig.json", tmplName: "tsconfig.json"},
	{path: "vite.config.ts", tmplName: "vite.config.ts"},
	{path: "index.html", tmplName: "index.html"},
	{path: "src/main.tsx", tmplName: "main.tsx"},
	{path: "src/App.tsx", tmplName: "App.tsx"},
	{path: "src/api.ts", tmplName: "api.ts"},
	{path: "src/auth.tsx", tmplName: "auth.tsx", onlyIf: func(d Data) bool { return d.HasAuth }},
	{path: "src/components/Layout.tsx", tmplName: "Layout.tsx"},
	{path: "src/components/DataTable.tsx", tmplName: "DataTable.tsx"},
	{path: "src/components/FormField.tsx", tmplName: "FormField.tsx"},
	{path: "src/pages/DashboardPage.tsx", tmplName: "DashboardPage.tsx"},
	{path: "src/pages/LoginPage.tsx", tmplName: "LoginPage.tsx", onlyIf: func(d Data) bool { return d.HasAuth }},
	{path: "src/pages/RegisterPage.tsx", tmplName: "RegisterPage.tsx", onlyIf: func(d Data) bool { return d.HasAuth }},
}

// parseScaffoldTemplates parses all scaffold template strings into a map.
func parseScaffoldTemplates() (map[string]*template.Template, error) {
	funcs := template.FuncMap{
		"lower":      strings.ToLower,
		"upper":      strings.ToUpper,
		"title":      toCamelCase,
		"join":       strings.Join,
		"hasPrefix":  strings.HasPrefix,
		"trimPrefix": strings.TrimPrefix,
		"replace":    strings.ReplaceAll,
		"jsPath":     jsPathExpr,
		"tsTupleArgs": tsTupleArgs,
	}

	result := make(map[string]*template.Template, len(scaffoldTemplates))
	for name, src := range scaffoldTemplates {
		t, err := template.New(name).Funcs(funcs).Parse(src)
		if err != nil {
			return nil, fmt.Errorf("parse template %q: %w", name, err)
		}
		result[name] = t
	}
	return result, nil
}
