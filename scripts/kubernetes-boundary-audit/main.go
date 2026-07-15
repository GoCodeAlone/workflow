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
	workflowModulePath              = "github.com/GoCodeAlone/workflow"
	registryFile                    = "module/platform_kubernetes.go"
	coreFile                        = "module/platform_kubernetes_core.go"
	pluginRegistryFile              = "module/platform_kubernetes_plugin_registry.go"
	gkeFile                         = "module/platform_kubernetes_gke.go"
	registerIdentifier              = "RegisterKubernetesBackend"
	registryIdentifier              = "kubernetesBackendRegistry"
	reservedPredicateIdentifier     = "isReservedKubernetesBackendType"
	removedReservedMapIdentifier    = "reservedKubernetesBackendTypes"
	normalizeRegistrationIdentifier = "normalizeKubernetesBackendRegistration"
)

var requiredBackends = []string{"kind", "k3s", "eks", "aks"}

var expectedBackendFactories = map[string]string{
	"kind": "kindBackend",
	"k3s":  "kindBackend",
	"eks":  "eksErrorBackend",
	"aks":  "aksBackend",
}

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
	info, err := os.Lstat(root)
	if err != nil {
		result.addViolation("invalid Workflow root %s: %v", root, err)
		return result
	}
	if info.Mode()&os.ModeSymlink != 0 {
		result.addViolation("Workflow root must not be a symlink: %s", root)
		return result
	}
	if !info.IsDir() {
		result.addViolation("invalid Workflow root %s: not a directory", root)
		return result
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		result.addViolation("resolve Workflow root %s: %v", root, err)
		return result
	}
	root = filepath.Clean(resolvedRoot)

	if fixtureMode {
		result.repositoryOK = true
	} else {
		result.repositoryOK = validateRepositoryIdentity(root, &result)
	}
	for _, requiredFile := range []string{registryFile, coreFile, pluginRegistryFile} {
		if _, statErr := confinedRegularFile(root, filepath.Join(root, filepath.FromSlash(requiredFile))); statErr != nil {
			result.addViolation("missing canonical Kubernetes registration file %s: %v", requiredFile, statErr)
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
	if _, err := confinedRegularFile(root, goModPath); err != nil {
		result.addViolation("missing go.mod for Workflow module identity: %v", err)
		ok = false
	} else {
		goMod, openErr := os.Open(goModPath)
		if openErr != nil {
			result.addViolation("open go.mod for Workflow module identity: %v", openErr)
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
	}
	return validatePhaseMarkers(root, result, ok)
}

func validatePhaseMarkers(root string, result *auditResult, ok bool) bool {
	for _, marker := range []string{".phase-b-complete", ".phase-c-complete"} {
		if _, statErr := confinedRegularFile(root, filepath.Join(root, marker)); statErr != nil {
			result.addViolation("missing committed phase marker %s: %v", marker, statErr)
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
		if _, statErr := confinedRegularFile(root, path); statErr != nil {
			return fmt.Errorf("production Go file %s: %w", path, statErr)
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
		if !bytes.Contains(source, []byte(registerIdentifier)) &&
			!bytes.Contains(source, []byte(registryIdentifier)) &&
			!bytes.Contains(source, []byte(reservedPredicateIdentifier)) &&
			!bytes.Contains(source, []byte(removedReservedMapIdentifier)) {
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

func confinedRegularFile(root, path string) (fs.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("symlink is not permitted")
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	contained, err := pathIsWithinRoot(root, resolvedPath)
	if err != nil {
		return nil, err
	}
	if !contained {
		return nil, fmt.Errorf("resolved path escapes Workflow root")
	}
	return info, nil
}

func pathIsWithinRoot(root, path string) (bool, error) {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false, err
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative), nil
}

func analyzeFiles(files []parsedGoFile, result *auditResult) {
	analyzeReservedBackendAuthority(files, result)

	handledRegisterRefs := make(map[*ast.Ident]bool)
	allowedRegistryRefs := make(map[*ast.Ident]bool)
	directRegistrationOwners := make(map[*ast.CallExpr]*ast.FuncDecl)
	canonicalRegistrationOwners := make(map[*ast.FuncDecl]bool)
	registrationInitOwners := make(map[*ast.FuncDecl]bool)
	registerDeclCount := 0
	registryDeclCount := 0
	canonicalRegistryWrites := 0
	canonicalRegistryLookups := 0
	registrationCounts := make(map[string]int)

	for _, parsed := range files {
		validateBoundaryLinknames(parsed, result)
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
				if parsed.relPath == coreFile && declaration.Name.Name == "init" && declaration.Recv == nil && declaration.Body != nil {
					directCalls := make([]*ast.CallExpr, 0, len(declaration.Body.List))
					for _, statement := range declaration.Body.List {
						expression, ok := statement.(*ast.ExprStmt)
						if !ok {
							continue
						}
						call, ok := expression.X.(*ast.CallExpr)
						if !ok || !isIdentifier(call.Fun, registerIdentifier) {
							continue
						}
						directRegistrationOwners[call] = declaration
						directCalls = append(directCalls, call)
					}
					if len(declaration.Body.List) == len(requiredBackends) && canonicalRegistrationSequence(directCalls) {
						canonicalRegistrationOwners[declaration] = true
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
				coreLookup, fallbackLookup := validatePlatformKubernetesInit(function, result)
				if coreLookup != nil {
					allowedRegistryRefs[coreLookup] = true
				}
				if fallbackLookup != nil {
					allowedRegistryRefs[fallbackLookup] = true
				}
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
			owner, ownedByInit := directRegistrationOwners[call]
			directInitCall := ownedByInit && canonicalRegistrationOwners[owner]
			name, valid := validateDirectCall(parsed, call, identifier, directInitCall, result)
			if directInitCall {
				registrationInitOwners[owner] = true
			}
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
	if len(registrationInitOwners) != 1 {
		result.addViolation("%s calls must be direct statements of one top-level func init() in %s, found %d registration init functions", registerIdentifier, coreFile, len(registrationInitOwners))
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

func analyzeReservedBackendAuthority(files []parsedGoFile, result *auditResult) {
	allowedPredicateRefs := make(map[*ast.Ident]bool)
	predicateDeclCount := 0
	canonicalRouteCount := 0
	normalizationDeclCount := 0
	canonicalNormalizationGuardCount := 0

	for _, parsed := range files {
		for _, declaration := range parsed.file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if function.Name.Name == reservedPredicateIdentifier {
				allowedPredicateRefs[function.Name] = true
				predicateDeclCount++
				if parsed.relPath != pluginRegistryFile || function.Recv != nil {
					result.addViolation("%s declaration must be in %s", reservedPredicateIdentifier, pluginRegistryFile)
				}
				if !isCanonicalReservedPredicate(function) {
					result.addViolation("%s must be the canonical exact predicate reserving only kind and k3s", reservedPredicateIdentifier)
				}
			}
			if parsed.relPath == registryFile && isPlatformKubernetesInit(function) && function.Body != nil {
				for _, statement := range function.Body.List {
					branch, ok := statement.(*ast.IfStmt)
					if !ok || branch.Init != nil {
						continue
					}
					identifier, ok := reservedPredicateCall(branch.Cond, "clusterType")
					if !ok {
						continue
					}
					allowedPredicateRefs[identifier] = true
					canonicalRouteCount++
				}
			}
			if function.Name.Name == normalizeRegistrationIdentifier {
				normalizationDeclCount++
				if parsed.relPath != pluginRegistryFile || function.Recv != nil {
					result.addViolation("%s declaration must be in %s", normalizeRegistrationIdentifier, pluginRegistryFile)
				}
				identifier, ok := canonicalNormalizationReservedGuard(function)
				if !ok {
					result.addViolation("%s must directly guard normalized name with %s and return the reserved-type rejection", normalizeRegistrationIdentifier, reservedPredicateIdentifier)
					continue
				}
				allowedPredicateRefs[identifier] = true
				canonicalNormalizationGuardCount++
			}
		}
	}

	for _, parsed := range files {
		ast.Inspect(parsed.file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			switch identifier.Name {
			case removedReservedMapIdentifier:
				result.addViolation("%s:%d removed %s map must not exist", parsed.relPath, parsed.fset.Position(identifier.Pos()).Line, removedReservedMapIdentifier)
			case reservedPredicateIdentifier:
				if !allowedPredicateRefs[identifier] {
					result.addViolation("%s:%d %s reference is only permitted in its declaration, core route, and normalization guard", parsed.relPath, parsed.fset.Position(identifier.Pos()).Line, reservedPredicateIdentifier)
				}
			}
			return true
		})
	}

	if predicateDeclCount != 1 {
		result.addViolation("expected exactly one %s declaration in %s, found %d", reservedPredicateIdentifier, pluginRegistryFile, predicateDeclCount)
	}
	if canonicalRouteCount != 1 {
		result.addViolation("expected exactly one canonical %s core route, found %d", reservedPredicateIdentifier, canonicalRouteCount)
	}
	if normalizationDeclCount != 1 {
		result.addViolation("expected exactly one %s declaration in %s, found %d", normalizeRegistrationIdentifier, pluginRegistryFile, normalizationDeclCount)
	}
	if canonicalNormalizationGuardCount != 1 {
		result.addViolation("expected exactly one canonical %s normalization guard, found %d", reservedPredicateIdentifier, canonicalNormalizationGuardCount)
	}
}

func isCanonicalReservedPredicate(function *ast.FuncDecl) bool {
	if function.Recv != nil || function.Type == nil || function.Type.TypeParams != nil ||
		function.Body == nil || len(function.Body.List) != 1 {
		return false
	}
	parameters := function.Type.Params
	if parameters == nil || len(parameters.List) != 1 || len(parameters.List[0].Names) != 1 ||
		parameters.List[0].Names[0].Name != "name" || !isIdentifier(parameters.List[0].Type, "string") {
		return false
	}
	results := function.Type.Results
	if results == nil || len(results.List) != 1 || len(results.List[0].Names) != 0 || !isIdentifier(results.List[0].Type, "bool") {
		return false
	}
	switchStatement, ok := function.Body.List[0].(*ast.SwitchStmt)
	if !ok || switchStatement.Init != nil || !isIdentifier(switchStatement.Tag, "name") ||
		switchStatement.Body == nil || len(switchStatement.Body.List) != 2 {
		return false
	}
	reservedCase, ok := switchStatement.Body.List[0].(*ast.CaseClause)
	if !ok || len(reservedCase.List) != 2 || !isStringLiteral(reservedCase.List[0], "kind") ||
		!isStringLiteral(reservedCase.List[1], "k3s") || len(reservedCase.Body) != 1 ||
		!isBooleanReturn(reservedCase.Body[0], "true") {
		return false
	}
	defaultCase, ok := switchStatement.Body.List[1].(*ast.CaseClause)
	return ok && len(defaultCase.List) == 0 && len(defaultCase.Body) == 1 && isBooleanReturn(defaultCase.Body[0], "false")
}

func isBooleanReturn(statement ast.Stmt, value string) bool {
	returned, ok := statement.(*ast.ReturnStmt)
	return ok && len(returned.Results) == 1 && isIdentifier(returned.Results[0], value)
}

func reservedPredicateCall(expression ast.Expr, argument string) (*ast.Ident, bool) {
	call, ok := expression.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || !isIdentifier(call.Args[0], argument) {
		return nil, false
	}
	identifier, ok := call.Fun.(*ast.Ident)
	return identifier, ok && identifier.Name == reservedPredicateIdentifier
}

func canonicalNormalizationReservedGuard(function *ast.FuncDecl) (*ast.Ident, bool) {
	if function.Body == nil || len(function.Body.List) != 5 ||
		!isCanonicalOwnerNormalization(function.Body.List[0]) ||
		!isCanonicalOwnerEmptyGuard(function.Body.List[1]) ||
		!isCanonicalNormalizedBindingsInitialization(function.Body.List[2]) ||
		!isCanonicalNormalizedBindingsReturn(function.Body.List[4]) {
		return nil, false
	}
	iteration, ok := function.Body.List[3].(*ast.RangeStmt)
	if !ok || iteration.Tok != token.DEFINE || !isIdentifier(iteration.Key, "_") ||
		!isIdentifier(iteration.Value, "binding") || !isIdentifier(iteration.X, "bindings") ||
		iteration.Body == nil || len(iteration.Body.List) < 3 {
		return nil, false
	}
	if !isNormalizedBackendNameAssignment(iteration.Body.List[0]) ||
		!isCanonicalNormalizedNameEmptyGuard(iteration.Body.List[1]) {
		return nil, false
	}
	return isCanonicalReservedNormalizationGuard(iteration.Body.List[2])
}

func isCanonicalOwnerNormalization(statement ast.Stmt) bool {
	assignment, ok := statement.(*ast.AssignStmt)
	if !ok || assignment.Tok != token.ASSIGN || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 ||
		!isIdentifier(assignment.Lhs[0], "owner") {
		return false
	}
	call, ok := assignment.Rhs[0].(*ast.CallExpr)
	return ok && isSelector(call.Fun, "strings", "TrimSpace") && len(call.Args) == 1 &&
		isIdentifier(call.Args[0], "owner")
}

func isCanonicalOwnerEmptyGuard(statement ast.Stmt) bool {
	branch, ok := statement.(*ast.IfStmt)
	if !ok || branch.Init != nil || branch.Else != nil || branch.Body == nil || len(branch.Body.List) != 1 {
		return false
	}
	condition, ok := branch.Cond.(*ast.BinaryExpr)
	if !ok || condition.Op != token.EQL || !isIdentifier(condition.X, "owner") || !isStringLiteral(condition.Y, "") {
		return false
	}
	returned, ok := branch.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(returned.Results) != 3 || !isStringLiteral(returned.Results[0], "") ||
		!isIdentifier(returned.Results[1], "nil") {
		return false
	}
	call, ok := returned.Results[2].(*ast.CallExpr)
	return ok && isSelector(call.Fun, "fmt", "Errorf") && len(call.Args) == 1 &&
		isStringLiteral(call.Args[0], "kubernetes backend registration: owner must not be empty")
}

func isCanonicalNormalizedBindingsInitialization(statement ast.Stmt) bool {
	assignment, ok := statement.(*ast.AssignStmt)
	if !ok || assignment.Tok != token.DEFINE || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 ||
		!isIdentifier(assignment.Lhs[0], "normalized") {
		return false
	}
	call, ok := assignment.Rhs[0].(*ast.CallExpr)
	if !ok || !isIdentifier(call.Fun, "make") || len(call.Args) != 2 {
		return false
	}
	mapType, ok := call.Args[0].(*ast.MapType)
	if !ok || !isIdentifier(mapType.Key, "string") || !isIdentifier(mapType.Value, "KubernetesBackendBinding") {
		return false
	}
	length, ok := call.Args[1].(*ast.CallExpr)
	return ok && isIdentifier(length.Fun, "len") && len(length.Args) == 1 && isIdentifier(length.Args[0], "bindings")
}

func isCanonicalNormalizedBindingsReturn(statement ast.Stmt) bool {
	returned, ok := statement.(*ast.ReturnStmt)
	return ok && len(returned.Results) == 3 && isIdentifier(returned.Results[0], "owner") &&
		isIdentifier(returned.Results[1], "normalized") && isIdentifier(returned.Results[2], "nil")
}

func isCanonicalNormalizedNameEmptyGuard(statement ast.Stmt) bool {
	branch, ok := statement.(*ast.IfStmt)
	if !ok || branch.Init != nil || branch.Else != nil || branch.Body == nil || len(branch.Body.List) != 1 {
		return false
	}
	condition, ok := branch.Cond.(*ast.BinaryExpr)
	if !ok || condition.Op != token.EQL || !isIdentifier(condition.X, "name") || !isStringLiteral(condition.Y, "") {
		return false
	}
	returned, ok := branch.Body.List[0].(*ast.ReturnStmt)
	return ok && len(returned.Results) == 3
}

func isNormalizedBackendNameAssignment(statement ast.Stmt) bool {
	assignment, ok := statement.(*ast.AssignStmt)
	if !ok || assignment.Tok != token.DEFINE || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 ||
		!isIdentifier(assignment.Lhs[0], "name") {
		return false
	}
	call, ok := assignment.Rhs[0].(*ast.CallExpr)
	return ok && isSelector(call.Fun, "strings", "TrimSpace") && len(call.Args) == 1 &&
		isSelector(call.Args[0], "binding", "Name")
}

func isCanonicalReservedNormalizationGuard(statement ast.Stmt) (*ast.Ident, bool) {
	branch, ok := statement.(*ast.IfStmt)
	if !ok || branch.Init != nil || branch.Else != nil || branch.Body == nil || len(branch.Body.List) != 1 {
		return nil, false
	}
	identifier, ok := reservedPredicateCall(branch.Cond, "name")
	if !ok {
		return nil, false
	}
	returned, ok := branch.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(returned.Results) != 3 || !isStringLiteral(returned.Results[0], "") ||
		!isIdentifier(returned.Results[1], "nil") {
		return nil, false
	}
	call, ok := returned.Results[2].(*ast.CallExpr)
	if !ok || !isSelector(call.Fun, "fmt", "Errorf") || len(call.Args) != 2 ||
		!isStringLiteral(call.Args[0], "plugin registered reserved kubernetes backend type %q") ||
		!isIdentifier(call.Args[1], "name") {
		return nil, false
	}
	return identifier, true
}

func validateBoundaryLinknames(parsed parsedGoFile, result *auditResult) {
	for _, group := range parsed.file.Comments {
		for _, comment := range group.List {
			if !strings.HasPrefix(comment.Text, "//go:linkname") {
				continue
			}
			remainder := strings.TrimPrefix(comment.Text, "//go:linkname")
			if remainder != "" && remainder[0] != ' ' && remainder[0] != '\t' {
				continue
			}
			for _, symbol := range strings.Fields(remainder) {
				if !linknameReferencesBoundarySymbol(symbol) {
					continue
				}
				result.addViolation("%s:%d go:linkname must not reference Kubernetes backend boundary symbol %q", parsed.relPath, parsed.fset.Position(comment.Pos()).Line, symbol)
				break
			}
		}
	}
}

func linknameReferencesBoundarySymbol(symbol string) bool {
	for _, boundary := range []string{registerIdentifier, registryIdentifier, removedReservedMapIdentifier} {
		if symbol == boundary || strings.HasSuffix(symbol, "."+boundary) {
			return true
		}
	}
	return false
}

func canonicalRegistrationSequence(calls []*ast.CallExpr) bool {
	if len(calls) != len(requiredBackends) {
		return false
	}
	for index, call := range calls {
		if len(call.Args) != 2 || !isStringLiteral(call.Args[0], requiredBackends[index]) {
			return false
		}
	}
	return true
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

func validatePlatformKubernetesInit(function *ast.FuncDecl, result *auditResult) (*ast.Ident, *ast.Ident) {
	statements := function.Body.List
	routingIndex := -1
	for index := 0; index+2 < len(statements); index++ {
		branch, ok := statements[index+2].(*ast.IfStmt)
		if !isConfigStringExtraction(statements[index], "clusterType", "type") ||
			!isClusterTypeDefault(statements[index+1]) || !ok || !isCoreLocalRoutingCondition(branch) {
			continue
		}
		if routingIndex != -1 {
			routingIndex = -2
			break
		}
		routingIndex = index + 2
	}
	stateIndex := -1
	if routingIndex >= 0 {
		for index := routingIndex + 1; index < len(statements); index++ {
			if !isKubernetesStatePublication(statements[index]) {
				continue
			}
			if stateIndex != -1 {
				stateIndex = -2
				break
			}
			stateIndex = index
		}
	}
	anchored := routingIndex >= 0 && stateIndex > routingIndex &&
		isKubernetesServicePublication(statements[len(statements)-1])
	if anchored && !preRoutingReturnsAreCanonical(statements[:routingIndex-2]) {
		anchored = false
	}
	if anchored {
		for index, statement := range statements {
			if index != routingIndex && containsBackendAssignment(statement) {
				anchored = false
				break
			}
		}
	}
	if !anchored {
		result.addViolation("(*PlatformKubernetes).Init Kubernetes backend branch must remain the anchored routing decision after clusterType selection and before state publication")
		result.addViolation("(*PlatformKubernetes).Init must preserve the core-local Kubernetes backend lookup and initialization branch")
		result.addViolation("(*PlatformKubernetes).Init must preserve the provider-first compatibility fallback lookup and initialization branch")
		return nil, nil
	}
	branch := statements[routingIndex].(*ast.IfStmt)
	coreLookup, coreOK := validateCoreLocalBackendBranch(branch.Body)
	if !coreOK {
		result.addViolation("(*PlatformKubernetes).Init must preserve the core-local Kubernetes backend lookup and initialization branch")
		coreLookup = nil
	}
	fallbackLookup, fallbackOK := validateCompatibilityBackendBranch(branch.Else)
	if !fallbackOK {
		result.addViolation("(*PlatformKubernetes).Init must preserve the provider-first compatibility fallback lookup and initialization branch")
		fallbackLookup = nil
	}
	return coreLookup, fallbackLookup
}

func isConfigStringExtraction(node ast.Stmt, target, key string) bool {
	assignment, ok := node.(*ast.AssignStmt)
	if !ok || assignment.Tok != token.DEFINE || len(assignment.Lhs) != 2 || len(assignment.Rhs) != 1 {
		return false
	}
	if !isIdentifier(assignment.Lhs[0], target) || !isIdentifier(assignment.Lhs[1], "_") {
		return false
	}
	assertion, ok := assignment.Rhs[0].(*ast.TypeAssertExpr)
	if !ok || !isIdentifier(assertion.Type, "string") {
		return false
	}
	index, ok := assertion.X.(*ast.IndexExpr)
	return ok && isSelector(index.X, "m", "config") && isStringLiteral(index.Index, key)
}

func isClusterTypeDefault(node ast.Stmt) bool {
	branch, ok := node.(*ast.IfStmt)
	if !ok || branch.Init != nil || branch.Else != nil || branch.Body == nil || len(branch.Body.List) != 1 {
		return false
	}
	condition, ok := branch.Cond.(*ast.BinaryExpr)
	if !ok || condition.Op != token.EQL || !isIdentifier(condition.X, "clusterType") || !isStringLiteral(condition.Y, "") {
		return false
	}
	assignment, ok := branch.Body.List[0].(*ast.AssignStmt)
	return ok && assignment.Tok == token.ASSIGN && len(assignment.Lhs) == 1 && len(assignment.Rhs) == 1 &&
		isIdentifier(assignment.Lhs[0], "clusterType") && isStringLiteral(assignment.Rhs[0], "kind")
}

func isKubernetesStatePublication(node ast.Stmt) bool {
	assignment, ok := node.(*ast.AssignStmt)
	return ok && assignment.Tok == token.ASSIGN && len(assignment.Lhs) == 1 && len(assignment.Rhs) == 1 && isSelector(assignment.Lhs[0], "m", "state")
}

func containsBackendAssignment(node ast.Node) bool {
	found := false
	ast.Inspect(node, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, target := range assignment.Lhs {
			if isSelector(target, "m", "backend") {
				found = true
				return false
			}
		}
		return !found
	})
	return found
}

func preRoutingReturnsAreCanonical(statements []ast.Stmt) bool {
	canonical := true
	for _, statement := range statements {
		ast.Inspect(statement, func(node ast.Node) bool {
			if _, nested := node.(*ast.FuncLit); nested {
				return false
			}
			returned, ok := node.(*ast.ReturnStmt)
			if !ok {
				return true
			}
			if !isNonNilReturn(returned) {
				canonical = false
				return false
			}
			return true
		})
		if !canonical {
			return false
		}
	}
	return true
}

func isKubernetesServicePublication(node ast.Stmt) bool {
	statement, ok := node.(*ast.ReturnStmt)
	if !ok || len(statement.Results) != 1 {
		return false
	}
	call, ok := statement.Results[0].(*ast.CallExpr)
	return ok && isSelector(call.Fun, "app", "RegisterService") && len(call.Args) == 2 &&
		isSelector(call.Args[0], "m", "name") && isIdentifier(call.Args[1], "m")
}

func isCoreLocalRoutingCondition(branch *ast.IfStmt) bool {
	if branch.Init != nil {
		return false
	}
	_, ok := reservedPredicateCall(branch.Cond, "clusterType")
	return ok
}

func validateCoreLocalBackendBranch(body *ast.BlockStmt) (*ast.Ident, bool) {
	if body == nil || len(body.List) != 5 {
		return nil, false
	}
	lookup, ok := registryLookupAssignment(body.List[0])
	if !ok || !isUnsupportedCoreLookupGuard(body.List[1]) {
		return nil, false
	}
	if !isFactoryInvocationAssignment(body.List[2], "err") || !isErrorGuard(body.List[3], "err") || !isBackendAssignment(body.List[4]) {
		return nil, false
	}
	return lookup, true
}

func validateCompatibilityBackendBranch(node ast.Stmt) (*ast.Ident, bool) {
	block, ok := node.(*ast.BlockStmt)
	if !ok || len(block.List) != 4 || !isApplicationBackendResolution(block.List[0]) ||
		!isErrorGuard(block.List[1], "err") || !isUnscopedCompatibilityResolution(block.List[2]) {
		return nil, false
	}
	providerBranch, ok := block.List[3].(*ast.IfStmt)
	if !ok || providerBranch.Init != nil || !isBindingClientAvailable(providerBranch.Cond) {
		return nil, false
	}
	if providerBranch.Body == nil || len(providerBranch.Body.List) != 1 || !isProviderBackendAssignment(providerBranch.Body.List[0]) {
		return nil, false
	}
	fallback, ok := providerBranch.Else.(*ast.IfStmt)
	if !ok || !isIdentifier(fallback.Cond, "ok") {
		return nil, false
	}
	lookup, ok := registryLookupAssignment(fallback.Init)
	if !ok || fallback.Body == nil || len(fallback.Body.List) != 3 {
		return nil, false
	}
	if !isFactoryInvocationAssignment(fallback.Body.List[0], "createErr") || !isErrorGuard(fallback.Body.List[1], "createErr") || !isBackendAssignment(fallback.Body.List[2]) {
		return nil, false
	}
	if !isUnsupportedCompatibilityFallback(fallback.Else) {
		return nil, false
	}
	return lookup, true
}

func isApplicationBackendResolution(node ast.Stmt) bool {
	assignment, ok := node.(*ast.AssignStmt)
	if !ok || assignment.Tok != token.DEFINE || len(assignment.Lhs) != 3 || len(assignment.Rhs) != 1 {
		return false
	}
	if !isIdentifier(assignment.Lhs[0], "binding") || !isIdentifier(assignment.Lhs[1], "scoped") || !isIdentifier(assignment.Lhs[2], "err") {
		return false
	}
	call, ok := assignment.Rhs[0].(*ast.CallExpr)
	return ok && isIdentifier(call.Fun, "resolveApplicationKubernetesBackend") && len(call.Args) == 2 &&
		isIdentifier(call.Args[0], "app") && isIdentifier(call.Args[1], "clusterType")
}

func isUnscopedCompatibilityResolution(node ast.Stmt) bool {
	branch, ok := node.(*ast.IfStmt)
	if !ok || branch.Init != nil || branch.Else != nil || branch.Body == nil || len(branch.Body.List) != 1 {
		return false
	}
	condition, ok := branch.Cond.(*ast.UnaryExpr)
	if !ok || condition.Op != token.NOT || !isIdentifier(condition.X, "scoped") {
		return false
	}
	assignment, ok := branch.Body.List[0].(*ast.AssignStmt)
	if !ok || assignment.Tok != token.ASSIGN || len(assignment.Lhs) != 2 || len(assignment.Rhs) != 1 ||
		!isIdentifier(assignment.Lhs[0], "binding") || !isIdentifier(assignment.Lhs[1], "_") {
		return false
	}
	call, ok := assignment.Rhs[0].(*ast.CallExpr)
	return ok && isSelector(call.Fun, "kubernetesBackendClientRegistryInstance", "resolve") && len(call.Args) == 1 &&
		isIdentifier(call.Args[0], "clusterType")
}

func registryLookupAssignment(node ast.Node) (*ast.Ident, bool) {
	assignment, ok := node.(*ast.AssignStmt)
	if !ok || assignment.Tok != token.DEFINE || len(assignment.Lhs) != 2 || len(assignment.Rhs) != 1 {
		return nil, false
	}
	if !isIdentifier(assignment.Lhs[0], "factory") || !isIdentifier(assignment.Lhs[1], "ok") {
		return nil, false
	}
	index, ok := assignment.Rhs[0].(*ast.IndexExpr)
	if !ok || !isIdentifier(index.Index, "clusterType") {
		return nil, false
	}
	identifier, ok := index.X.(*ast.Ident)
	return identifier, ok && identifier.Name == registryIdentifier
}

func isUnsupportedCoreLookupGuard(node ast.Stmt) bool {
	branch, ok := node.(*ast.IfStmt)
	if !ok || branch.Init != nil || branch.Else != nil || branch.Body == nil || len(branch.Body.List) != 1 {
		return false
	}
	negation, ok := branch.Cond.(*ast.UnaryExpr)
	if !ok || negation.Op != token.NOT || !isIdentifier(negation.X, "ok") {
		return false
	}
	statement, ok := branch.Body.List[0].(*ast.ReturnStmt)
	return ok && len(statement.Results) == 1 && isUnsupportedCoreTypeError(statement.Results[0])
}

func isUnsupportedCoreTypeError(expression ast.Expr) bool {
	call, ok := expression.(*ast.CallExpr)
	if !ok || !isSelector(call.Fun, "fmt", "Errorf") || len(call.Args) != 3 {
		return false
	}
	return isStringLiteral(call.Args[0], "platform.kubernetes %q: unsupported type %q") &&
		isSelector(call.Args[1], "m", "name") && isIdentifier(call.Args[2], "clusterType")
}

func isUnsupportedCompatibilityFallback(node ast.Stmt) bool {
	block, ok := node.(*ast.BlockStmt)
	if !ok || len(block.List) != 1 {
		return false
	}
	statement, ok := block.List[0].(*ast.ReturnStmt)
	if !ok || len(statement.Results) != 1 {
		return false
	}
	call, ok := statement.Results[0].(*ast.CallExpr)
	if !ok || !isSelector(call.Fun, "fmt", "Errorf") || len(call.Args) != 4 {
		return false
	}
	format, ok := constantStringValue(call.Args[0])
	return ok && format == "platform.kubernetes %q: cluster type %q is not built into workflow core "+
		"(in-core types: 'kind', 'k3s'; compatibility fallbacks: 'eks', 'aks'). If %q is a "+
		"plugin-provided backend, install and load the plugin that declares it" &&
		isSelector(call.Args[1], "m", "name") && isIdentifier(call.Args[2], "clusterType") && isIdentifier(call.Args[3], "clusterType")
}

func isFactoryInvocationAssignment(node ast.Stmt, errorName string) bool {
	assignment, ok := node.(*ast.AssignStmt)
	if !ok || assignment.Tok != token.DEFINE || len(assignment.Lhs) != 2 || len(assignment.Rhs) != 1 {
		return false
	}
	if !isIdentifier(assignment.Lhs[0], "backend") || !isIdentifier(assignment.Lhs[1], errorName) {
		return false
	}
	call, ok := assignment.Rhs[0].(*ast.CallExpr)
	return ok && isIdentifier(call.Fun, "factory") && len(call.Args) == 1 && isSelector(call.Args[0], "m", "config")
}

func isErrorGuard(node ast.Stmt, name string) bool {
	branch, ok := node.(*ast.IfStmt)
	if !ok || branch.Init != nil || branch.Else != nil || branch.Body == nil || len(branch.Body.List) != 1 {
		return false
	}
	condition, ok := branch.Cond.(*ast.BinaryExpr)
	if !ok || condition.Op != token.NEQ || !isIdentifier(condition.X, name) || !isIdentifier(condition.Y, "nil") {
		return false
	}
	statement, ok := branch.Body.List[0].(*ast.ReturnStmt)
	return ok && len(statement.Results) == 1 && isCanonicalErrorReturn(statement.Results[0], name)
}

func isNonNilReturn(node ast.Stmt) bool {
	statement, ok := node.(*ast.ReturnStmt)
	if !ok || len(statement.Results) != 1 {
		return false
	}
	return isFmtErrorfCall(statement.Results[0], "")
}

func isCanonicalErrorReturn(expression ast.Expr, name string) bool {
	if isIdentifier(expression, name) {
		return true
	}
	return isFmtErrorfCall(expression, name)
}

func isFmtErrorfCall(expression ast.Expr, errorName string) bool {
	call, ok := expression.(*ast.CallExpr)
	if !ok || !isSelector(call.Fun, "fmt", "Errorf") || len(call.Args) == 0 {
		return false
	}
	format, ok := stringLiteralValue(call.Args[0])
	if !ok {
		return false
	}
	if errorName == "" {
		return true
	}
	if format != "platform.kubernetes %q: creating backend: %w" && format != "platform.kubernetes %q: %w" {
		return false
	}
	return len(call.Args) == 3 && isSelector(call.Args[1], "m", "name") && isIdentifier(call.Args[2], errorName)
}

func isBackendAssignment(node ast.Stmt) bool {
	assignment, ok := node.(*ast.AssignStmt)
	return ok && assignment.Tok == token.ASSIGN && len(assignment.Lhs) == 1 && len(assignment.Rhs) == 1 &&
		isSelector(assignment.Lhs[0], "m", "backend") && isIdentifier(assignment.Rhs[0], "backend")
}

func isBindingClientAvailable(node ast.Expr) bool {
	condition, ok := node.(*ast.BinaryExpr)
	return ok && condition.Op == token.NEQ && isSelector(condition.X, "binding", "Client") && isIdentifier(condition.Y, "nil")
}

func isProviderBackendAssignment(node ast.Stmt) bool {
	assignment, ok := node.(*ast.AssignStmt)
	if !ok || assignment.Tok != token.ASSIGN || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 || !isSelector(assignment.Lhs[0], "m", "backend") {
		return false
	}
	call, ok := assignment.Rhs[0].(*ast.CallExpr)
	if !ok || !isIdentifier(call.Fun, "newGRPCKubernetesBackend") || len(call.Args) != 3 {
		return false
	}
	return isSelector(call.Args[0], "binding", "Name") && isSelector(call.Args[1], "binding", "ResourceType") && isSelector(call.Args[2], "binding", "Client")
}

func isSelector(node ast.Expr, receiver, field string) bool {
	selector, ok := node.(*ast.SelectorExpr)
	return ok && isIdentifier(selector.X, receiver) && selector.Sel.Name == field
}

func isIdentifier(node ast.Node, name string) bool {
	identifier, ok := node.(*ast.Ident)
	return ok && identifier.Name == name
}

func isStringLiteral(node ast.Expr, value string) bool {
	unquoted, ok := stringLiteralValue(node)
	return ok && unquoted == value
}

func stringLiteralValue(node ast.Expr) (string, bool) {
	literal, ok := node.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	unquoted, err := strconv.Unquote(literal.Value)
	return unquoted, err == nil
}

func constantStringValue(node ast.Expr) (string, bool) {
	if value, ok := stringLiteralValue(node); ok {
		return value, true
	}
	concatenation, ok := node.(*ast.BinaryExpr)
	if !ok || concatenation.Op != token.ADD {
		return "", false
	}
	left, leftOK := constantStringValue(concatenation.X)
	right, rightOK := constantStringValue(concatenation.Y)
	return left + right, leftOK && rightOK
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

func validateDirectCall(parsed parsedGoFile, call *ast.CallExpr, identifier *ast.Ident, directInitCall bool, result *auditResult) (string, bool) {
	line := parsed.fset.Position(identifier.Pos()).Line
	valid := true
	if parsed.relPath != coreFile {
		result.addViolation("%s:%d %s call must be in %s", parsed.relPath, line, registerIdentifier, coreFile)
		valid = false
	}
	if !directInitCall {
		result.addViolation("%s:%d %s calls must be direct statements of one top-level func init() in %s", parsed.relPath, line, registerIdentifier, coreFile)
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
	} else if expectedBackend := expectedBackendFactories[name]; !isCanonicalBackendFactory(call.Args[1], expectedBackend) {
		result.addViolation("%s:%d backend %q factory must be the canonical %s function literal", parsed.relPath, line, name, expectedBackend)
		valid = false
	}
	return name, valid
}

func isCanonicalBackendFactory(expression ast.Expr, expectedBackend string) bool {
	function, ok := expression.(*ast.FuncLit)
	if !ok || function.Type == nil || function.Body == nil || len(function.Body.List) != 1 {
		return false
	}
	if !isCanonicalFactoryParameters(function.Type.Params) || !isCanonicalFactoryResults(function.Type.Results) {
		return false
	}
	returnStatement, ok := function.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(returnStatement.Results) != 2 || !isIdentifier(returnStatement.Results[1], "nil") {
		return false
	}
	address, ok := returnStatement.Results[0].(*ast.UnaryExpr)
	if !ok || address.Op != token.AND {
		return false
	}
	literal, ok := address.X.(*ast.CompositeLit)
	return ok && len(literal.Elts) == 0 && isIdentifier(literal.Type, expectedBackend)
}

func isCanonicalFactoryParameters(parameters *ast.FieldList) bool {
	if parameters == nil || len(parameters.List) != 1 {
		return false
	}
	parameter := parameters.List[0]
	if len(parameter.Names) != 1 || parameter.Names[0].Name != "_" {
		return false
	}
	mapType, ok := parameter.Type.(*ast.MapType)
	return ok && isIdentifier(mapType.Key, "string") && isIdentifier(mapType.Value, "any")
}

func isCanonicalFactoryResults(results *ast.FieldList) bool {
	if results == nil || len(results.List) != 2 {
		return false
	}
	return len(results.List[0].Names) == 0 && isIdentifier(results.List[0].Type, "kubernetesBackend") &&
		len(results.List[1].Names) == 0 && isIdentifier(results.List[1].Type, "error")
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
