package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	authorityManifestPath  = ".github/public-workflow-authority.json"
	executableManifestPath = ".github/public-workflow-executable-allowlist.json"
	policyWorkflowPath     = ".github/workflows/public-workflow-policy.yml"
	policyWrapperPath      = "scripts/check-public-workflow-policy.sh"
	policyHarnessPath      = "scripts/test-check-public-workflow-policy.sh"
	policyLauncherPath     = ".github/workflows/policytool/adoptionguard/run.sh"
	authorizationMarker    = "candidate_policy_authorized=true"
)

var immutableGuardPaths = []string{
	".github/workflows/policytool/adoptionguard/main.go",
	".github/workflows/policytool/adoptionguard/main_test.go",
	policyLauncherPath,
}

var unchangedTrustManifestPaths = []string{
	".github/public-workflow-action-allowlist.json",
	".github/public-workflow-command-allowlist.json",
	".github/public-workflow-presence-allowlist.json",
	".github/public-workflow-secret-allowlist.json",
}

type authorityFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type authorityBundle struct {
	State string          `json:"state"`
	Files []authorityFile `json:"files"`
}

type authorityManifest struct {
	Version int               `json:"version"`
	Bundles []authorityBundle `json:"bundles"`
}

type executableEntry struct {
	Path          string `json:"path"`
	WorkflowPath  string `json:"workflowPath"`
	ContextSHA256 string `json:"contextSHA256"`
	SHA256        string `json:"sha256"`
	State         string `json:"state"`
	Rationale     string `json:"rationale"`
}

type presenceEntry struct {
	Path          string `json:"path"`
	ContextSHA256 string `json:"contextSHA256,omitempty"`
	State         string `json:"state"`
	Presence      string `json:"presence"`
}

type actionEntry struct {
	Path          string `json:"path"`
	Uses          string `json:"uses"`
	NodeSHA256    string `json:"nodeSHA256"`
	ContextSHA256 string `json:"contextSHA256"`
	State         string `json:"state"`
	Rationale     string `json:"rationale"`
}

type commandEntry struct {
	Path            string `json:"path"`
	Command         string `json:"command"`
	StatementSHA256 string `json:"statementSHA256"`
	ContextSHA256   string `json:"contextSHA256"`
	State           string `json:"state"`
	Rationale       string `json:"rationale"`
}

type secretEntry struct {
	Path          string `json:"path"`
	Secret        string `json:"secret"`
	ContextSHA256 string `json:"contextSHA256"`
	State         string `json:"state"`
	Rationale     string `json:"rationale"`
}

type fileRecord struct {
	SHA256     string
	Executable bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	baseArg, candidateArg, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	baseRoot, err := canonicalRoot(baseArg)
	if err != nil {
		fmt.Fprintf(stderr, "adoption guard: base root: %v\n", err)
		return 1
	}
	candidateRoot, err := canonicalRoot(candidateArg)
	if err != nil {
		fmt.Fprintf(stderr, "adoption guard: candidate root: %v\n", err)
		return 1
	}
	if rootsOverlap(baseRoot, candidateRoot) {
		fmt.Fprintln(stderr, "adoption guard: base and candidate roots must be distinct non-overlapping directories")
		return 1
	}
	if err := validateAdoption(baseRoot, candidateRoot); err != nil {
		fmt.Fprintf(stderr, "adoption guard: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, authorizationMarker)
	return 0
}

func parseArgs(args []string) (string, string, error) {
	var base, candidate string
	for len(args) > 0 {
		if len(args) < 2 {
			return "", "", errors.New("usage: adoptionguard --base TRUSTED_ROOT --candidate CANDIDATE_ROOT")
		}
		switch args[0] {
		case "--base":
			if base != "" {
				return "", "", errors.New("--base may be specified exactly once")
			}
			base = args[1]
		case "--candidate":
			if candidate != "" {
				return "", "", errors.New("--candidate may be specified exactly once")
			}
			candidate = args[1]
		default:
			return "", "", fmt.Errorf("unknown option %q", args[0])
		}
		args = args[2:]
	}
	if base == "" || candidate == "" {
		return "", "", errors.New("usage: adoptionguard --base TRUSTED_ROOT --candidate CANDIDATE_ROOT")
	}
	return base, candidate, nil
}

func canonicalRoot(root string) (string, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return "", errors.New("path must be absolute and clean")
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if resolved != root {
		return "", errors.New("path must not contain symlink aliases")
	}
	info, err := os.Lstat(root)
	if err != nil {
		return "", fmt.Errorf("inspect path: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("path must be a real directory")
	}
	return root, nil
}

func rootsOverlap(left, right string) bool {
	if left == right {
		return true
	}
	for _, pair := range [][2]string{{left, right}, {right, left}} {
		relative, err := filepath.Rel(pair[0], pair[1])
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func validateAdoption(baseRoot, candidateRoot string) error {
	baseAuthorityBytes, err := secureRead(baseRoot, authorityManifestPath)
	if err != nil {
		return fmt.Errorf("read base authority manifest: %w", err)
	}
	candidateAuthorityBytes, err := secureRead(candidateRoot, authorityManifestPath)
	if err != nil {
		return fmt.Errorf("read candidate authority manifest: %w", err)
	}
	if !bytes.Equal(baseAuthorityBytes, candidateAuthorityBytes) {
		return errors.New("base and candidate authority manifests must be byte-identical")
	}
	var manifest authorityManifest
	if err := decodeJSON(baseAuthorityBytes, &manifest); err != nil {
		return fmt.Errorf("decode authority manifest: %w", err)
	}
	active, staged, err := validateAuthorityManifest(manifest)
	if err != nil {
		return err
	}
	baseInventory, err := authorityInventory(baseRoot)
	if err != nil {
		return fmt.Errorf("inventory base authority: %w", err)
	}
	candidateInventory, err := authorityInventory(candidateRoot)
	if err != nil {
		return fmt.Errorf("inventory candidate authority: %w", err)
	}
	if !reflect.DeepEqual(baseInventory, active.Files) {
		return errors.New("base authority inventory does not realize the active bundle")
	}
	if !reflect.DeepEqual(candidateInventory, staged.Files) {
		return errors.New("candidate authority inventory does not realize the staged bundle")
	}
	if err := validateImmutableGuard(active, staged); err != nil {
		return err
	}
	if err := validateUnchangedTrust(baseRoot, candidateRoot); err != nil {
		return err
	}
	if err := validateWorkflowAuthority(baseRoot, candidateRoot); err != nil {
		return err
	}
	if err := validateExecutableTransition(baseRoot, candidateRoot, active, staged); err != nil {
		return err
	}
	if err := validateExactTreeTransition(baseRoot, candidateRoot, active, staged); err != nil {
		return err
	}
	return nil
}

func validateAuthorityManifest(manifest authorityManifest) (authorityBundle, authorityBundle, error) {
	if manifest.Version != 1 {
		return authorityBundle{}, authorityBundle{}, errors.New("authority manifest version must be 1")
	}
	if len(manifest.Bundles) != 2 || manifest.Bundles[0].State != "active" || manifest.Bundles[1].State != "staged" {
		return authorityBundle{}, authorityBundle{}, errors.New("authority manifest must contain exactly one active bundle followed by one staged bundle")
	}
	for _, bundle := range manifest.Bundles {
		if len(bundle.Files) == 0 {
			return authorityBundle{}, authorityBundle{}, fmt.Errorf("%s authority bundle must not be empty", bundle.State)
		}
		previous := ""
		for _, file := range bundle.Files {
			if !authorityPathAllowed(file.Path) {
				return authorityBundle{}, authorityBundle{}, fmt.Errorf("%s authority bundle contains invalid path %q", bundle.State, file.Path)
			}
			if previous != "" && file.Path <= previous {
				return authorityBundle{}, authorityBundle{}, fmt.Errorf("%s authority bundle paths must be unique and strictly sorted", bundle.State)
			}
			if !validSHA256(file.SHA256) {
				return authorityBundle{}, authorityBundle{}, fmt.Errorf("%s authority bundle contains invalid digest for %s", bundle.State, file.Path)
			}
			previous = file.Path
		}
	}
	active, staged := manifest.Bundles[0], manifest.Bundles[1]
	if len(active.Files) != len(staged.Files) {
		return authorityBundle{}, authorityBundle{}, errors.New("active and staged authority bundle membership must have equal cardinality")
	}
	for index := range active.Files {
		if active.Files[index].Path != staged.Files[index].Path {
			return authorityBundle{}, authorityBundle{}, errors.New("active and staged authority bundle membership must be identical")
		}
	}
	for _, required := range append(append([]string(nil), immutableGuardPaths...), policyWrapperPath) {
		if _, ok := authorityDigest(active, required); !ok {
			return authorityBundle{}, authorityBundle{}, fmt.Errorf("active authority bundle is missing %s", required)
		}
	}
	return active, staged, nil
}

func authorityPathAllowed(relative string) bool {
	if relative != path.Clean(relative) || path.IsAbs(relative) || relative == "." || relative == ".." ||
		strings.HasPrefix(relative, "../") || strings.Contains(relative, "\\") {
		return false
	}
	switch relative {
	case ".github/workflows/scripts/verify-public-workflow-branch-protection.sh",
		"scripts/check-public-workflow-policy.sh",
		"scripts/test-check-public-workflow-policy.sh":
		return true
	}
	return strings.HasPrefix(relative, ".github/workflows/policytool/") ||
		strings.HasPrefix(relative, "scripts/fixtures/public-workflow-policy/")
}

func authorityInventory(root string) ([]authorityFile, error) {
	paths := map[string]bool{}
	for _, relativeRoot := range []string{
		".github/workflows/policytool",
		"scripts/fixtures/public-workflow-policy",
	} {
		absoluteRoot := filepath.Join(root, filepath.FromSlash(relativeRoot))
		info, err := os.Lstat(absoluteRoot)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, fmt.Errorf("authority root %s must be a real directory", relativeRoot)
		}
		err = filepath.WalkDir(absoluteRoot, func(filePath string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			relative, err := filepath.Rel(root, filePath)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
				return fmt.Errorf("authority path %s must be a regular non-symlink file", relative)
			}
			paths[relative] = true
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	for _, relative := range []string{
		".github/workflows/scripts/verify-public-workflow-branch-protection.sh",
		"scripts/check-public-workflow-policy.sh",
		"scripts/test-check-public-workflow-policy.sh",
	} {
		filePath := filepath.Join(root, filepath.FromSlash(relative))
		if _, err := os.Lstat(filePath); err == nil {
			paths[relative] = true
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	inventory := make([]authorityFile, 0, len(paths))
	for relative := range paths {
		data, err := secureRead(root, relative)
		if err != nil {
			return nil, err
		}
		inventory = append(inventory, authorityFile{Path: relative, SHA256: sha256Hex(data)})
	}
	sort.Slice(inventory, func(i, j int) bool { return inventory[i].Path < inventory[j].Path })
	return inventory, nil
}

func validateImmutableGuard(active, staged authorityBundle) error {
	for _, relative := range immutableGuardPaths {
		activeDigest, activeOK := authorityDigest(active, relative)
		stagedDigest, stagedOK := authorityDigest(staged, relative)
		if !activeOK || !stagedOK || activeDigest != stagedDigest {
			return fmt.Errorf("trusted adoption guard authority must remain active and unchanged at %s", relative)
		}
	}
	return nil
}

func validateUnchangedTrust(baseRoot, candidateRoot string) error {
	for _, relative := range unchangedTrustManifestPaths {
		baseData, err := secureRead(baseRoot, relative)
		if err != nil {
			return fmt.Errorf("read base trust input %s: %w", relative, err)
		}
		candidateData, err := secureRead(candidateRoot, relative)
		if err != nil {
			return fmt.Errorf("read candidate trust input %s: %w", relative, err)
		}
		if !bytes.Equal(baseData, candidateData) {
			return fmt.Errorf("trust input %s must remain byte-identical", relative)
		}
		switch relative {
		case ".github/public-workflow-action-allowlist.json":
			var entries []actionEntry
			if err := decodeJSON(baseData, &entries); err != nil {
				return fmt.Errorf("decode action trust input: %w", err)
			}
			if err := validateActionEntries(entries); err != nil {
				return err
			}
		case ".github/public-workflow-command-allowlist.json":
			var entries []commandEntry
			if err := decodeJSON(baseData, &entries); err != nil {
				return fmt.Errorf("decode command trust input: %w", err)
			}
			if err := validateCommandEntries(entries); err != nil {
				return err
			}
		case ".github/public-workflow-presence-allowlist.json":
			var entries []presenceEntry
			if err := decodeJSON(baseData, &entries); err != nil {
				return fmt.Errorf("decode presence trust input: %w", err)
			}
			if err := validatePresenceEntries(entries); err != nil {
				return err
			}
		case ".github/public-workflow-secret-allowlist.json":
			var entries []secretEntry
			if err := decodeJSON(baseData, &entries); err != nil {
				return fmt.Errorf("decode secret trust input: %w", err)
			}
			if len(entries) != 0 {
				return errors.New("public workflow secret authority must remain exactly empty")
			}
		}
	}
	return nil
}

func validateWorkflowAuthority(baseRoot, candidateRoot string) error {
	baseWorkflow, err := secureRead(baseRoot, policyWorkflowPath)
	if err != nil {
		return fmt.Errorf("read active public workflow: %w", err)
	}
	candidateWorkflow, err := secureRead(candidateRoot, policyWorkflowPath)
	if err != nil {
		return fmt.Errorf("read candidate public workflow: %w", err)
	}
	if !bytes.Equal(baseWorkflow, candidateWorkflow) {
		return errors.New("active public workflow must remain byte-identical")
	}
	context, err := workflowContext(baseWorkflow)
	if err != nil {
		return fmt.Errorf("validate active public workflow: %w", err)
	}
	if err := rejectWorkflowInfluence(baseWorkflow); err != nil {
		return err
	}
	presenceData, err := secureRead(baseRoot, ".github/public-workflow-presence-allowlist.json")
	if err != nil {
		return err
	}
	var entries []presenceEntry
	if err := decodeJSON(presenceData, &entries); err != nil {
		return fmt.Errorf("decode presence trust input: %w", err)
	}
	matches := 0
	for _, entry := range entries {
		if entry.Path != policyWorkflowPath {
			continue
		}
		if entry.State != "active" || entry.Presence != "present" || entry.ContextSHA256 != context {
			return errors.New("public workflow authority must be active and realized")
		}
		matches++
	}
	if matches != 1 {
		return errors.New("public workflow authority must contain exactly one active realized inventory row")
	}
	return nil
}

func validateExecutableTransition(baseRoot, candidateRoot string, active, staged authorityBundle) error {
	baseData, err := secureRead(baseRoot, executableManifestPath)
	if err != nil {
		return fmt.Errorf("read base executable trust input: %w", err)
	}
	candidateData, err := secureRead(candidateRoot, executableManifestPath)
	if err != nil {
		return fmt.Errorf("read candidate executable trust input: %w", err)
	}
	if bytes.Equal(baseData, candidateData) {
		return errors.New("candidate executable trust input must adopt the staged wrapper digest")
	}
	var baseEntries, candidateEntries []executableEntry
	if err := decodeJSON(baseData, &baseEntries); err != nil {
		return fmt.Errorf("decode base executable trust input: %w", err)
	}
	if err := decodeJSON(candidateData, &candidateEntries); err != nil {
		return fmt.Errorf("decode candidate executable trust input: %w", err)
	}
	if len(baseEntries) != len(candidateEntries) {
		return errors.New("executable trust input cardinality must remain unchanged")
	}
	activeWrapper, activeOK := authorityDigest(active, policyWrapperPath)
	stagedWrapper, stagedOK := authorityDigest(staged, policyWrapperPath)
	if !activeOK || !stagedOK || activeWrapper == stagedWrapper {
		return errors.New("authority bundles must define distinct active and staged wrapper digests")
	}
	activeHarness, activeHarnessOK := authorityDigest(active, policyHarnessPath)
	stagedHarness, stagedHarnessOK := authorityDigest(staged, policyHarnessPath)
	harnessChanged := activeHarnessOK && stagedHarnessOK && activeHarness != stagedHarness
	baseByKey := make(map[string]executableEntry, len(baseEntries))
	for _, entry := range baseEntries {
		if err := validateExecutableEntry(entry); err != nil {
			return err
		}
		key := executableMetadataKey(entry)
		if _, exists := baseByKey[key]; exists {
			return errors.New("base executable trust input contains duplicate metadata")
		}
		baseByKey[key] = entry
	}
	wrapperRows := 0
	harnessRows := 0
	seenCandidate := make(map[string]bool, len(candidateEntries))
	for index, candidateEntry := range candidateEntries {
		if err := validateExecutableEntry(candidateEntry); err != nil {
			return err
		}
		key := executableMetadataKey(candidateEntry)
		if seenCandidate[key] {
			return errors.New("candidate executable trust input contains duplicate metadata")
		}
		seenCandidate[key] = true
		baseEntry, ok := baseByKey[key]
		if !ok {
			return errors.New("executable trust input membership or metadata changed")
		}
		if key != executableMetadataKey(baseEntries[index]) {
			return errors.New("executable trust input row order must remain unchanged")
		}
		if candidateEntry.Path == policyWrapperPath {
			wrapperRows++
			if baseEntry.SHA256 != activeWrapper || candidateEntry.SHA256 != stagedWrapper {
				return fmt.Errorf("every policy wrapper row must change exactly from active digest %s to staged digest %s", activeWrapper, stagedWrapper)
			}
			continue
		}
		if candidateEntry.Path == policyHarnessPath && harnessChanged {
			harnessRows++
			if baseEntry.SHA256 != activeHarness || candidateEntry.SHA256 != stagedHarness {
				return fmt.Errorf("every policy harness row must change exactly from active digest %s to staged digest %s", activeHarness, stagedHarness)
			}
			continue
		}
		if candidateEntry.SHA256 != baseEntry.SHA256 {
			return fmt.Errorf("unrelated executable digest changed for %s", candidateEntry.Path)
		}
	}
	if wrapperRows == 0 {
		return errors.New("executable trust input must contain policy wrapper authority")
	}
	if harnessChanged && harnessRows == 0 {
		return errors.New("changed policy harness must have existing executable authority")
	}
	return nil
}

func executableMetadataKey(entry executableEntry) string {
	entry.SHA256 = ""
	encoded, _ := json.Marshal(entry)
	return string(encoded)
}

func validateExactTreeTransition(baseRoot, candidateRoot string, active, staged authorityBundle) error {
	baseTree, err := repositoryTree(baseRoot)
	if err != nil {
		return fmt.Errorf("inventory base tree: %w", err)
	}
	candidateTree, err := repositoryTree(candidateRoot)
	if err != nil {
		return fmt.Errorf("inventory candidate tree: %w", err)
	}
	for _, executable := range []struct {
		path, label string
	}{
		{path: policyWrapperPath, label: "policy wrapper"},
		{path: policyLauncherPath, label: "adoption guard launcher"},
	} {
		baseRecord, basePresent := baseTree[executable.path]
		candidateRecord, candidatePresent := candidateTree[executable.path]
		if !basePresent || !candidatePresent || !baseRecord.Executable || !candidateRecord.Executable {
			return fmt.Errorf("%s must be executable in both base and candidate trees", executable.label)
		}
	}
	for relative, baseRecord := range baseTree {
		candidateRecord, present := candidateTree[relative]
		if present && baseRecord.Executable != candidateRecord.Executable {
			return fmt.Errorf("candidate executable mode differs from base for %s", relative)
		}
	}
	allowed := map[string]bool{executableManifestPath: true}
	for index := range active.Files {
		if active.Files[index].SHA256 != staged.Files[index].SHA256 {
			allowed[active.Files[index].Path] = true
		}
	}
	actual := make(map[string]bool)
	for relative, baseRecord := range baseTree {
		candidateRecord, ok := candidateTree[relative]
		if !ok || candidateRecord != baseRecord {
			actual[relative] = true
		}
	}
	for relative := range candidateTree {
		if _, ok := baseTree[relative]; !ok {
			actual[relative] = true
		}
	}
	if !reflect.DeepEqual(actual, allowed) {
		return fmt.Errorf("candidate tree diff must equal staged authority delta plus executable trust input: got %v, want %v", sortedKeys(actual), sortedKeys(allowed))
	}
	return nil
}

func repositoryTree(root string) (map[string]fileRecord, error) {
	tree := make(map[string]fileRecord)
	err := filepath.WalkDir(root, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if relative == ".git" && entry.IsDir() {
			return filepath.SkipDir
		}
		relative = filepath.ToSlash(relative)
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("tree contains symlink %s", relative)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("tree contains non-regular path %s", relative)
		}
		data, err := secureRead(root, relative)
		if err != nil {
			return err
		}
		tree[relative] = fileRecord{SHA256: sha256Hex(data), Executable: info.Mode().Perm()&0o111 != 0}
		return nil
	})
	return tree, err
}

func secureRead(root, relative string) ([]byte, error) {
	if relative != path.Clean(relative) || path.IsAbs(relative) || relative == "." || relative == ".." ||
		strings.HasPrefix(relative, "../") || strings.Contains(relative, "\\") {
		return nil, fmt.Errorf("invalid repository-relative path %q", relative)
	}
	current := root
	parts := strings.Split(relative, "/")
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("path has symlink component %s", strings.Join(parts[:index+1], "/"))
		}
		if index < len(parts)-1 && !info.IsDir() {
			return nil, fmt.Errorf("path has non-directory ancestor %s", strings.Join(parts[:index+1], "/"))
		}
		if index == len(parts)-1 && !info.Mode().IsRegular() {
			return nil, fmt.Errorf("path is not a regular file %s", relative)
		}
	}
	return os.ReadFile(current)
}

func decodeJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	value := reflect.ValueOf(target)
	if value.Kind() == reflect.Pointer && !value.IsNil() && value.Elem().Kind() == reflect.Slice && value.Elem().IsNil() {
		return errors.New("top-level JSON array must not be null")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("unexpected trailing JSON value")
		}
		return fmt.Errorf("unexpected trailing JSON data: %w", err)
	}
	return nil
}

func workflowContext(data []byte) (string, error) {
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil {
		return "", err
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return "", errors.New("workflow must contain exactly one YAML document")
		}
		return "", err
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return "", errors.New("workflow root must be a mapping")
	}
	if err := validateYAMLStructure(document.Content[0]); err != nil {
		return "", err
	}
	var canonical bytes.Buffer
	canonicalYAMLNode(&canonical, document.Content[0])
	return sha256Hex(canonical.Bytes()), nil
}

func validateYAMLStructure(node *yaml.Node) error {
	if node.Kind == yaml.AliasNode {
		return errors.New("workflow contains forbidden YAML alias")
	}
	if node.Kind == yaml.MappingNode {
		seen := make(map[string]bool)
		for index := 0; index+1 < len(node.Content); index += 2 {
			var canonical bytes.Buffer
			canonicalYAMLNode(&canonical, node.Content[index])
			key := canonical.String()
			if seen[key] {
				return fmt.Errorf("workflow contains duplicate mapping key %s", node.Content[index].Value)
			}
			seen[key] = true
		}
	}
	for _, child := range node.Content {
		if err := validateYAMLStructure(child); err != nil {
			return err
		}
	}
	return nil
}

func canonicalYAMLNode(out *bytes.Buffer, node *yaml.Node) {
	fmt.Fprintf(out, "%d:%d:%s:%d:%s:", node.Kind, len(node.Tag), node.Tag, len(node.Value), node.Value)
	if node.Kind == yaml.MappingNode {
		type pair struct {
			key, value string
		}
		pairs := make([]pair, 0, len(node.Content)/2)
		for index := 0; index+1 < len(node.Content); index += 2 {
			var key, value bytes.Buffer
			canonicalYAMLNode(&key, node.Content[index])
			canonicalYAMLNode(&value, node.Content[index+1])
			pairs = append(pairs, pair{key: key.String(), value: value.String()})
		}
		sort.Slice(pairs, func(i, j int) bool { return pairs[i].key < pairs[j].key })
		for _, item := range pairs {
			fmt.Fprintf(out, "K%d:%sV%d:%s", len(item.key), item.key, len(item.value), item.value)
		}
		return
	}
	for _, child := range node.Content {
		canonicalYAMLNode(out, child)
	}
}

func rejectWorkflowInfluence(data []byte) error {
	lower := strings.ToLower(string(data))
	for _, marker := range []string{
		"${{ secrets.", "secrets[", "github.actor", "github.triggering_actor",
		"id-token", "self-hosted", "digitalocean_token", "do_token",
		"aws_access_key", "google_application_credentials", "azure_client_secret",
		"kubeconfig", "doctl ", "terraform apply", "kubectl ",
	} {
		if strings.Contains(lower, marker) {
			return fmt.Errorf("active public workflow contains forbidden secret, OIDC, actor, runner, or provider influence marker %q", marker)
		}
	}
	return nil
}

func validatePresenceEntries(entries []presenceEntry) error {
	seen := make(map[string]bool)
	for _, entry := range entries {
		if !validRelativePath(entry.Path) || (entry.State != "active" && entry.State != "staged") {
			return errors.New("presence trust input contains malformed entry")
		}
		if entry.Presence == "present" {
			if !validSHA256(entry.ContextSHA256) {
				return errors.New("present workflow authority requires an exact context digest")
			}
		} else if entry.Presence != "absent" || entry.ContextSHA256 != "" {
			return errors.New("absent workflow authority must not define a context digest")
		}
		encoded, _ := json.Marshal(entry)
		if seen[string(encoded)] {
			return errors.New("presence trust input contains duplicate entry")
		}
		seen[string(encoded)] = true
	}
	return nil
}

func validateActionEntries(entries []actionEntry) error {
	for _, entry := range entries {
		if !validRelativePath(entry.Path) || entry.Uses == "" || !validSHA256(entry.NodeSHA256) ||
			!validSHA256(entry.ContextSHA256) || !validStateAndRationale(entry.State, entry.Rationale) {
			return errors.New("action trust input contains malformed entry")
		}
	}
	return nil
}

func validateCommandEntries(entries []commandEntry) error {
	for _, entry := range entries {
		if !validRelativePath(entry.Path) || strings.TrimSpace(entry.Command) == "" ||
			!validSHA256(entry.StatementSHA256) || !validSHA256(entry.ContextSHA256) ||
			!validStateAndRationale(entry.State, entry.Rationale) {
			return errors.New("command trust input contains malformed entry")
		}
	}
	return nil
}

func validateExecutableEntry(entry executableEntry) error {
	if !validRelativePath(entry.Path) || !validRelativePath(entry.WorkflowPath) ||
		!validSHA256(entry.ContextSHA256) || !validSHA256(entry.SHA256) ||
		!validStateAndRationale(entry.State, entry.Rationale) {
		return errors.New("executable trust input contains malformed entry")
	}
	return nil
}

func validStateAndRationale(state, rationale string) bool {
	return (state == "active" || state == "staged") && strings.TrimSpace(rationale) != ""
}

func validRelativePath(relative string) bool {
	return relative != "" && relative == path.Clean(relative) && !path.IsAbs(relative) &&
		relative != "." && relative != ".." && !strings.HasPrefix(relative, "../") &&
		!strings.Contains(relative, "\\")
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

func authorityDigest(bundle authorityBundle, relative string) (string, bool) {
	index := sort.Search(len(bundle.Files), func(index int) bool { return bundle.Files[index].Path >= relative })
	if index >= len(bundle.Files) || bundle.Files[index].Path != relative {
		return "", false
	}
	return bundle.Files[index].SHA256, true
}

func sha256Hex(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
