package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	workflowModulePath = "github.com/GoCodeAlone/workflow"
	registryFile       = "module/platform_kubernetes.go"
	coreFile           = "module/platform_kubernetes_core.go"
	gkeFile            = "module/platform_kubernetes_gke.go"
	registerIdentifier = "RegisterKubernetesBackend"
	registryIdentifier = "kubernetesBackendRegistry"
)

var requiredBackends = []string{"kind", "k3s", "eks", "aks"}

type auditResult struct {
	fixtureMode   bool
	repositoryOK  bool
	gkeAbsent     bool
	registrations []string
	violations    []string
}

type parsedGoFile struct {
	relPath string
	source  []byte
	file    *ast.File
	fset    *token.FileSet
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("kubernetes-boundary-audit", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "audit a Workflow repository root")
	fixtureRoot := flags.String("fixture-root", "", "audit a fixture root without repository identity checks")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || (*root == "") == (*fixtureRoot == "") {
		fmt.Fprintln(stderr, "exactly one of --root or --fixture-root is required")
		return 2
	}

	fixtureMode := *fixtureRoot != ""
	auditRoot := *root
	if fixtureMode {
		auditRoot = *fixtureRoot
	}
	result := inspectRoot(auditRoot, fixtureMode)
	writeResult(stdout, result)
	if len(result.violations) != 0 {
		return 1
	}
	return 0
}

func inspectRoot(root string, fixtureMode bool) auditResult {
	result := auditResult{fixtureMode: fixtureMode, gkeAbsent: true}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		result.addViolation("resolve root: %v", err)
		return result
	}
	root = filepath.Clean(absRoot)
	info, err := os.Stat(root)
	if err != nil {
		result.addViolation("invalid Workflow root %s: %v", root, err)
		return result
	}
	if !info.IsDir() {
		result.addViolation("invalid Workflow root %s: not a directory", root)
		return result
	}

	if fixtureMode {
		result.repositoryOK = true
	} else {
		result.repositoryOK = validateRepositoryIdentity(root, &result)
	}
	for _, requiredFile := range []string{registryFile, coreFile} {
		if fileInfo, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(requiredFile))); statErr != nil || !fileInfo.Mode().IsRegular() {
			result.addViolation("missing canonical Kubernetes registration file %s", requiredFile)
		}
	}
	if _, statErr := os.Lstat(filepath.Join(root, filepath.FromSlash(gkeFile))); statErr == nil {
		result.gkeAbsent = false
		result.addViolation("deleted %s exists; provider-specific GKE behavior belongs in its plugin", gkeFile)
	} else if !os.IsNotExist(statErr) {
		result.gkeAbsent = false
		result.addViolation("inspect deleted %s invariant: %v", gkeFile, statErr)
	}

	files, err := candidateGoFiles(root)
	if err != nil {
		result.addViolation("scan production Go files: %v", err)
		return result
	}
	analyzeFiles(files, &result)
	return result
}

func validateRepositoryIdentity(root string, result *auditResult) bool {
	ok := true
	goModPath := filepath.Join(root, "go.mod")
	goMod, err := os.Open(goModPath)
	if err != nil {
		result.addViolation("missing go.mod for Workflow module identity: %v", err)
		ok = false
	} else {
		modulePath := ""
		scanner := bufio.NewScanner(goMod)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) >= 2 && fields[0] == "module" {
				modulePath = strings.Trim(fields[1], "\"")
				break
			}
		}
		if scanErr := scanner.Err(); scanErr != nil {
			result.addViolation("read go.mod module identity: %v", scanErr)
			ok = false
		}
		if closeErr := goMod.Close(); closeErr != nil {
			result.addViolation("close go.mod: %v", closeErr)
			ok = false
		}
		if modulePath != workflowModulePath {
			result.addViolation("module identity must be %s, got %q", workflowModulePath, modulePath)
			ok = false
		}
	}
	for _, marker := range []string{".phase-b-complete", ".phase-c-complete"} {
		markerInfo, statErr := os.Stat(filepath.Join(root, marker))
		if statErr != nil || !markerInfo.Mode().IsRegular() {
			result.addViolation("missing committed phase marker %s", marker)
			ok = false
		}
	}
	return ok
}

func candidateGoFiles(root string) ([]parsedGoFile, error) {
	paths := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	files := make([]parsedGoFile, 0)
	for _, path := range paths {
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		if !bytes.Contains(source, []byte(registerIdentifier)) && !bytes.Contains(source, []byte(registryIdentifier)) {
			continue
		}
		fset := token.NewFileSet()
		parsed, parseErr := parser.ParseFile(fset, path, source, parser.ParseComments)
		if parseErr != nil {
			return nil, fmt.Errorf("parse %s: %w", path, parseErr)
		}
		relPath, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil, relErr
		}
		files = append(files, parsedGoFile{
			relPath: filepath.ToSlash(relPath),
			source:  source,
			file:    parsed,
			fset:    fset,
		})
	}
	return files, nil
}

func analyzeFiles(files []parsedGoFile, result *auditResult) {
	handledRegisterRefs := make(map[*ast.Ident]bool)
	allowedRegistryRefs := make(map[*ast.Ident]bool)
	registerDeclCount := 0
	registryDeclCount := 0
	canonicalRegistryWrites := 0
	canonicalRegistryLookups := 0
	registrationCounts := make(map[string]int)

	for _, parsed := range files {
		topLevelRegistryDecls := make(map[*ast.Ident]bool)
		for _, declaration := range parsed.file.Decls {
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				if declaration.Name.Name == registerIdentifier {
					handledRegisterRefs[declaration.Name] = true
					registerDeclCount++
					if parsed.relPath != registryFile || declaration.Recv != nil {
						result.addViolation("%s declaration must be in %s", registerIdentifier, registryFile)
					}
				}
			case *ast.GenDecl:
				if declaration.Tok != token.VAR {
					continue
				}
				for _, spec := range declaration.Specs {
					valueSpec, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, name := range valueSpec.Names {
						if name.Name == registryIdentifier {
							topLevelRegistryDecls[name] = true
						}
					}
				}
			}
		}

		ast.Inspect(parsed.file, func(node ast.Node) bool {
			valueSpec, ok := node.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for _, name := range valueSpec.Names {
				if name.Name != registryIdentifier {
					continue
				}
				allowedRegistryRefs[name] = true
				registryDeclCount++
				if parsed.relPath != registryFile || !topLevelRegistryDecls[name] {
					result.addViolation("%s declaration must be in %s", registryIdentifier, registryFile)
				}
				if !isEmptyRegistryMapInitializer(valueSpec, name) {
					result.addViolation("%s must initialize an empty map literal of type map[string]KubernetesBackendFactory", registryIdentifier)
				}
			}
			return true
		})

		for _, declaration := range parsed.file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			allowedDirectWrites := make(map[*ast.AssignStmt]*ast.Ident)
			if parsed.relPath == registryFile && function.Name.Name == registerIdentifier && function.Recv == nil {
				for _, statement := range function.Body.List {
					assignment, ok := statement.(*ast.AssignStmt)
					if !ok {
						continue
					}
					if identifier, ok := directRegistryAssignmentBase(assignment); ok {
						allowedDirectWrites[assignment] = identifier
					}
				}
				if len(allowedDirectWrites) != 1 {
					result.addViolation("%s must directly assign %s[clusterType] = factory", registerIdentifier, registryIdentifier)
				}
			}
			writeRefs := make(map[*ast.Ident]bool)
			ast.Inspect(function.Body, func(node ast.Node) bool {
				switch node := node.(type) {
				case *ast.AssignStmt:
					invalidWrite := false
					for _, target := range node.Lhs {
						for _, identifier := range identifiersNamed(target, registryIdentifier) {
							writeRefs[identifier] = true
							if allowedDirectWrites[node] == identifier {
								allowedRegistryRefs[identifier] = true
								canonicalRegistryWrites++
								continue
							}
							invalidWrite = true
						}
					}
					if invalidWrite {
						result.addViolation("%s write must remain in %s in %s as the single direct assignment", registryIdentifier, registerIdentifier, registryFile)
					}
				case *ast.IncDecStmt:
					refs := identifiersNamed(node.X, registryIdentifier)
					if len(refs) != 0 {
						for _, identifier := range refs {
							writeRefs[identifier] = true
						}
						result.addViolation("%s write must remain in %s in %s as the single direct assignment", registryIdentifier, registerIdentifier, registryFile)
					}
				case *ast.CallExpr:
					builtin, ok := node.Fun.(*ast.Ident)
					if !ok || (builtin.Name != "delete" && builtin.Name != "clear") || len(node.Args) == 0 {
						return true
					}
					refs := identifiersNamed(node.Args[0], registryIdentifier)
					if len(refs) != 0 {
						for _, identifier := range refs {
							writeRefs[identifier] = true
						}
						result.addViolation("%s write must remain in %s in %s as the single direct assignment", registryIdentifier, registerIdentifier, registryFile)
					}
				}
				return true
			})

			if parsed.relPath == registryFile && isPlatformKubernetesInit(function) {
				ast.Inspect(function.Body, func(node ast.Node) bool {
					if _, nested := node.(*ast.FuncLit); nested {
						return false
					}
					index, ok := node.(*ast.IndexExpr)
					if !ok {
						return true
					}
					identifier, ok := index.X.(*ast.Ident)
					if !ok || identifier.Name != registryIdentifier || writeRefs[identifier] {
						return true
					}
					key, ok := index.Index.(*ast.Ident)
					if !ok || key.Name != "clusterType" {
						result.addViolation("%s lookup in (*PlatformKubernetes).Init must index by clusterType", registryIdentifier)
						return true
					}
					allowedRegistryRefs[identifier] = true
					canonicalRegistryLookups++
					return true
				})
			}
		}

		ast.Inspect(parsed.file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			identifier, ok := call.Fun.(*ast.Ident)
			if !ok || identifier.Name != registerIdentifier {
				return true
			}
			handledRegisterRefs[identifier] = true
			name, valid := validateDirectCall(parsed, call, identifier, result)
			if valid {
				result.registrations = append(result.registrations, name)
				registrationCounts[name]++
			}
			return true
		})
	}

	for _, parsed := range files {
		ast.Inspect(parsed.file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			switch identifier.Name {
			case registerIdentifier:
				if !handledRegisterRefs[identifier] {
					result.addViolation("%s:%d unsupported %s reference; only direct canonical calls are permitted", parsed.relPath, parsed.fset.Position(identifier.Pos()).Line, registerIdentifier)
				}
			case registryIdentifier:
				if !allowedRegistryRefs[identifier] {
					result.addViolation("%s:%d %s reference is only permitted in its declaration and %s write, or two (*PlatformKubernetes).Init lookups", parsed.relPath, parsed.fset.Position(identifier.Pos()).Line, registryIdentifier, registerIdentifier)
				}
			}
			return true
		})
	}

	if registerDeclCount != 1 {
		result.addViolation("expected exactly one %s declaration in %s, found %d", registerIdentifier, registryFile, registerDeclCount)
	}
	if registryDeclCount != 1 {
		result.addViolation("expected exactly one %s declaration in %s, found %d", registryIdentifier, registryFile, registryDeclCount)
	}
	if canonicalRegistryWrites != 1 {
		result.addViolation("expected exactly one %s write in %s in %s, found %d", registryIdentifier, registerIdentifier, registryFile, canonicalRegistryWrites)
	}
	if canonicalRegistryLookups != 2 {
		result.addViolation("expected exactly two %s lookups in (*PlatformKubernetes).Init, found %d", registryIdentifier, canonicalRegistryLookups)
	}
	for _, required := range requiredBackends {
		switch registrationCounts[required] {
		case 0:
			result.addViolation("missing required Kubernetes backend registration %q", required)
		case 1:
		default:
			result.addViolation("duplicate Kubernetes backend registration %q", required)
		}
	}
}

func isEmptyRegistryMapInitializer(spec *ast.ValueSpec, target *ast.Ident) bool {
	if len(spec.Names) != 1 || spec.Names[0] != target || len(spec.Values) != 1 {
		return false
	}
	literal, ok := spec.Values[0].(*ast.CompositeLit)
	if !ok || len(literal.Elts) != 0 {
		return false
	}
	mapType, ok := literal.Type.(*ast.MapType)
	if !ok {
		return false
	}
	key, ok := mapType.Key.(*ast.Ident)
	if !ok || key.Name != "string" {
		return false
	}
	value, ok := mapType.Value.(*ast.Ident)
	return ok && value.Name == "KubernetesBackendFactory"
}

func directRegistryAssignmentBase(assignment *ast.AssignStmt) (*ast.Ident, bool) {
	if assignment.Tok != token.ASSIGN || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
		return nil, false
	}
	index, ok := assignment.Lhs[0].(*ast.IndexExpr)
	if !ok {
		return nil, false
	}
	identifier, ok := index.X.(*ast.Ident)
	if !ok || identifier.Name != registryIdentifier {
		return nil, false
	}
	key, ok := index.Index.(*ast.Ident)
	if !ok || key.Name != "clusterType" {
		return nil, false
	}
	value, ok := assignment.Rhs[0].(*ast.Ident)
	if !ok || value.Name != "factory" {
		return nil, false
	}
	return identifier, true
}

func isPlatformKubernetesInit(function *ast.FuncDecl) bool {
	if function.Name.Name != "Init" || function.Recv == nil || len(function.Recv.List) != 1 {
		return false
	}
	receiver, ok := function.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	identifier, ok := receiver.X.(*ast.Ident)
	return ok && identifier.Name == "PlatformKubernetes"
}

func identifiersNamed(node ast.Node, name string) []*ast.Ident {
	identifiers := make([]*ast.Ident, 0)
	ast.Inspect(node, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if ok && identifier.Name == name {
			identifiers = append(identifiers, identifier)
		}
		return true
	})
	return identifiers
}

func validateDirectCall(parsed parsedGoFile, call *ast.CallExpr, identifier *ast.Ident, result *auditResult) (string, bool) {
	line := parsed.fset.Position(identifier.Pos()).Line
	valid := true
	if parsed.relPath != coreFile {
		result.addViolation("%s:%d %s call must be in %s", parsed.relPath, line, registerIdentifier, coreFile)
		valid = false
	}
	identifierEnd := parsed.fset.Position(identifier.End()).Offset
	leftParen := parsed.fset.Position(call.Lparen).Offset
	if identifierEnd < 0 || leftParen < identifierEnd || leftParen > len(parsed.source) || strings.TrimSpace(string(parsed.source[identifierEnd:leftParen])) != "" {
		result.addViolation("%s:%d %s call must not contain interstitial comments or wrappers", parsed.relPath, line, registerIdentifier)
		valid = false
	}
	if len(call.Args) != 2 {
		result.addViolation("%s:%d %s call must pass exactly two arguments", parsed.relPath, line, registerIdentifier)
		return "", false
	}
	literal, ok := call.Args[0].(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		result.addViolation("%s:%d %s first argument must be an explicit string literal", parsed.relPath, line, registerIdentifier)
		return "", false
	}
	name, err := strconv.Unquote(literal.Value)
	if err != nil {
		result.addViolation("%s:%d invalid %s string literal: %v", parsed.relPath, line, registerIdentifier, err)
		return "", false
	}
	if !isRequiredBackend(name) {
		result.addViolation("%s:%d backend %q is not framework-owned; only kind, k3s, eks, and aks may be registered directly", parsed.relPath, line, name)
		valid = false
	}
	return name, valid
}

func isRequiredBackend(name string) bool {
	for _, required := range requiredBackends {
		if name == required {
			return true
		}
	}
	return false
}

func (result *auditResult) addViolation(format string, args ...any) {
	result.violations = append(result.violations, fmt.Sprintf(format, args...))
}

func writeResult(writer io.Writer, result auditResult) {
	fmt.Fprintln(writer, "== Invariant: Kubernetes backend boundary ==")
	if result.fixtureMode {
		fmt.Fprintln(writer, "  repository identity + phase markers: skipped explicitly for fixture root")
	} else if result.repositoryOK {
		fmt.Fprintln(writer, "  repository identity + phase markers: clean")
	}
	if result.gkeAbsent {
		fmt.Fprintf(writer, "  deleted %s: absent — clean\n", gkeFile)
	}
	for _, violation := range result.violations {
		fmt.Fprintf(writer, "  VIOLATION: %s\n", violation)
	}
	if len(result.registrations) == 0 {
		fmt.Fprintln(writer, "  registrations: (none)")
	} else {
		fmt.Fprintf(writer, "  registrations: %s\n", strings.Join(result.registrations, " "))
	}
	if len(result.violations) == 0 {
		fmt.Fprintln(writer, "  OK — canonical registrations are exactly kind/k3s and temporary eks/aks compatibility fallbacks")
		fmt.Fprintln(writer, "kubernetes-boundary-audit: OK")
		return
	}
	fmt.Fprintln(writer, "kubernetes-boundary-audit: FAIL")
}
