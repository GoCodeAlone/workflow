package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
	"mvdan.cc/sh/v3/syntax"
)

type allowEntry struct {
	Path          string `json:"path"`
	Secret        string `json:"secret"`
	ContextSHA256 string `json:"contextSHA256"`
	State         string `json:"state"`
	Rationale     string `json:"rationale"`
}

type executableEntry struct {
	Path          string `json:"path"`
	WorkflowPath  string `json:"workflowPath"`
	ContextSHA256 string `json:"contextSHA256"`
	SHA256        string `json:"sha256"`
	State         string `json:"state"`
	Rationale     string `json:"rationale"`
}

func trustedPolicyExecutable(entry executableEntry) bool {
	return entry.WorkflowPath == ".github/workflows/public-workflow-policy.yml" &&
		entry.Path == "scripts/check-public-workflow-policy.sh"
}

type commandEntry struct {
	Path            string `json:"path"`
	Command         string `json:"command"`
	StatementSHA256 string `json:"statementSHA256"`
	ContextSHA256   string `json:"contextSHA256"`
	State           string `json:"state"`
	Rationale       string `json:"rationale"`
}

type actionEntry struct {
	Path          string `json:"path"`
	Uses          string `json:"uses"`
	NodeSHA256    string `json:"nodeSHA256"`
	ContextSHA256 string `json:"contextSHA256"`
	State         string `json:"state"`
	Rationale     string `json:"rationale"`
}

type trustGroup struct {
	Path          string `json:"path"`
	ContextSHA256 string `json:"contextSHA256,omitempty"`
	State         string `json:"state"`
	Presence      string `json:"presence"`
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

type trustPolicy struct {
	Secrets     []allowEntry
	Executables []executableEntry
	Commands    []commandEntry
	Actions     []actionEntry
	Presence    []trustGroup
}

var authorityFixedPaths = []string{
	".github/workflows/policytool/go.mod",
	".github/workflows/policytool/go.sum",
	".github/workflows/policytool/main.go",
	".github/workflows/policytool/main_test.go",
	".github/workflows/scripts/verify-public-workflow-branch-protection.sh",
	"scripts/check-public-workflow-policy.sh",
	"scripts/test-check-public-workflow-policy.sh",
}

func authorityPathAllowed(filePath string) bool {
	if filePath == path.Clean(filePath) && !path.IsAbs(filePath) && !strings.Contains(filePath, "\\") &&
		filePath != "." && filePath != ".." && !strings.HasPrefix(filePath, "../") {
		for _, fixed := range authorityFixedPaths {
			if filePath == fixed {
				return true
			}
		}
		return strings.HasPrefix(filePath, ".github/workflows/policytool/") ||
			strings.HasPrefix(filePath, "scripts/fixtures/public-workflow-policy/")
	}
	return false
}

func validateAuthorityManifest(manifest authorityManifest) []string {
	findings := []string{}
	if manifest.Version != 1 {
		findings = append(findings, "authority manifest version must be 1")
	}
	activeCount := 0
	stagedCount := 0
	for bundleIndex, bundle := range manifest.Bundles {
		switch bundle.State {
		case "active":
			activeCount++
		case "staged":
			stagedCount++
		default:
			findings = append(findings, fmt.Sprintf("authority bundle %d has invalid state", bundleIndex))
		}
		seen := make(map[string]bool)
		previous := ""
		for _, file := range bundle.Files {
			if !authorityPathAllowed(file.Path) {
				findings = append(findings, fmt.Sprintf("authority bundle contains invalid path %s", file.Path))
			}
			if seen[file.Path] {
				findings = append(findings, fmt.Sprintf("authority bundle contains duplicate path %s", file.Path))
			}
			if previous != "" && file.Path <= previous {
				findings = append(findings, "authority bundle files must be strictly sorted by path")
			}
			if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(file.SHA256) {
				findings = append(findings, fmt.Sprintf("authority bundle contains invalid hash for %s", file.Path))
			}
			seen[file.Path] = true
			previous = file.Path
		}
		for _, required := range authorityFixedPaths {
			if !seen[required] {
				findings = append(findings, fmt.Sprintf("authority bundle is missing required path %s", required))
			}
		}
		fixtureCount := 0
		for filePath := range seen {
			if strings.HasPrefix(filePath, "scripts/fixtures/public-workflow-policy/") {
				fixtureCount++
			}
		}
		if fixtureCount == 0 {
			findings = append(findings, "authority bundle must contain public workflow policy fixtures")
		}
	}
	if activeCount != 1 {
		findings = append(findings, "authority manifest must contain exactly one active bundle")
	}
	if stagedCount > 1 || len(manifest.Bundles) != activeCount+stagedCount {
		findings = append(findings, "authority manifest may contain at most one staged bundle")
	}
	sort.Strings(findings)
	return findings
}

func hashAuthorityFile(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func authorityInventory(repoRoot string) ([]authorityFile, error) {
	paths := append([]string(nil), authorityFixedPaths...)
	for _, root := range []string{
		".github/workflows/policytool",
		"scripts/fixtures/public-workflow-policy",
	} {
		absoluteRoot := filepath.Join(repoRoot, filepath.FromSlash(root))
		if err := filepath.WalkDir(absoluteRoot, func(filePath string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if filePath == absoluteRoot {
				if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
					return fmt.Errorf("non-directory authority root %s", root)
				}
				return nil
			}
			relative, err := filepath.Rel(repoRoot, filePath)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("non-regular authority path %s", relative)
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("non-regular authority path %s", relative)
			}
			paths = append(paths, relative)
			return nil
		}); err != nil {
			return nil, err
		}
	}
	sort.Strings(paths)
	uniquePaths := paths[:0]
	for _, filePath := range paths {
		if len(uniquePaths) == 0 || uniquePaths[len(uniquePaths)-1] != filePath {
			uniquePaths = append(uniquePaths, filePath)
		}
	}
	inventory := make([]authorityFile, 0, len(uniquePaths))
	for _, filePath := range uniquePaths {
		absolute := filepath.Join(repoRoot, filepath.FromSlash(filePath))
		current := repoRoot
		components := strings.Split(filepath.FromSlash(filePath), string(filepath.Separator))
		for _, component := range components[:len(components)-1] {
			current = filepath.Join(current, component)
			info, err := os.Lstat(current)
			if err != nil {
				return nil, fmt.Errorf("inspect authority ancestor %s: %w", filePath, err)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("authority path has symlink ancestor: %s", filePath)
			}
			if !info.IsDir() {
				return nil, fmt.Errorf("authority path has non-directory ancestor: %s", filePath)
			}
		}
		info, err := os.Lstat(absolute)
		if err != nil {
			return nil, fmt.Errorf("inspect authority path %s: %w", filePath, err)
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("non-regular authority path %s", filePath)
		}
		digest, err := hashAuthorityFile(absolute)
		if err != nil {
			return nil, fmt.Errorf("hash authority path %s: %w", filePath, err)
		}
		inventory = append(inventory, authorityFile{Path: filePath, SHA256: digest})
	}
	return inventory, nil
}

func authorityFilesEqual(left, right []authorityFile) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func authorityBundleForState(manifest authorityManifest, state string) (authorityBundle, bool) {
	for _, bundle := range manifest.Bundles {
		if bundle.State == state {
			return bundle, true
		}
	}
	return authorityBundle{}, false
}

func authorityManifestsEqual(left, right authorityManifest) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func validateAuthorityTransition(base *authorityManifest, candidate authorityManifest, baseInventory, candidateInventory []authorityFile, bootstrap bool) []string {
	findings := validateAuthorityManifest(candidate)
	if bootstrap {
		active, ok := authorityBundleForState(candidate, "active")
		if len(candidate.Bundles) != 1 || !ok || !authorityFilesEqual(candidateInventory, active.Files) {
			findings = append(findings, "bootstrap authority must contain one realized active bundle")
		}
		sort.Strings(findings)
		return findings
	}
	findings = append(findings, validateAuthorityManifest(*base)...)
	if len(findings) > 0 {
		sort.Strings(findings)
		return findings
	}
	baseActive, _ := authorityBundleForState(*base, "active")
	baseStaged, baseHasStaged := authorityBundleForState(*base, "staged")
	candidateActive, _ := authorityBundleForState(candidate, "active")
	candidateStaged, candidateHasStaged := authorityBundleForState(candidate, "staged")

	valid := false
	if authorityManifestsEqual(*base, candidate) && authorityFilesEqual(baseInventory, candidateInventory) {
		valid = authorityFilesEqual(baseInventory, baseActive.Files) ||
			(baseHasStaged && authorityFilesEqual(baseInventory, baseStaged.Files))
	}
	stageShape := !baseHasStaged && len(base.Bundles) == 1 && candidateHasStaged && len(candidate.Bundles) == 2 &&
		authorityFilesEqual(baseActive.Files, candidateActive.Files) &&
		authorityFilesEqual(baseInventory, baseActive.Files) && !authorityFilesEqual(candidateStaged.Files, baseActive.Files)
	if stageShape && authorityFilesEqual(candidateInventory, baseActive.Files) {
		valid = true
	}
	if stageShape && authorityFilesEqual(candidateInventory, candidateStaged.Files) {
		findings = append(findings, "authority bundle cannot be staged and adopted in the same pull request")
	}
	if baseHasStaged && authorityManifestsEqual(*base, candidate) &&
		authorityFilesEqual(baseInventory, baseActive.Files) && authorityFilesEqual(candidateInventory, baseStaged.Files) {
		valid = true
	}
	if baseHasStaged && !candidateHasStaged && len(candidate.Bundles) == 1 &&
		authorityFilesEqual(candidateActive.Files, baseStaged.Files) &&
		authorityFilesEqual(baseInventory, baseStaged.Files) && authorityFilesEqual(candidateInventory, baseStaged.Files) {
		valid = true
	}
	if !valid {
		findings = append(findings, "authority change does not match unchanged, stage, adopt, or promote lifecycle")
	}
	sort.Strings(findings)
	return findings
}

func semanticSet[T any](entries []T) map[string]int {
	result := make(map[string]int)
	for _, entry := range entries {
		encoded, err := json.Marshal(entry)
		if err == nil {
			result[string(encoded)]++
		}
	}
	return result
}

func semanticSetsEqual[T any](left, right []T) bool {
	leftSet := semanticSet(left)
	rightSet := semanticSet(right)
	if len(leftSet) != len(rightSet) {
		return false
	}
	for key, count := range leftSet {
		if rightSet[key] != count {
			return false
		}
	}
	return true
}

func semanticAdditions[T any](base, candidate []T) (additions []T, removed bool) {
	remaining := semanticSet(candidate)
	for _, entry := range base {
		encoded, _ := json.Marshal(entry)
		key := string(encoded)
		if remaining[key] == 0 {
			removed = true
			continue
		}
		remaining[key]--
	}
	for _, entry := range candidate {
		encoded, _ := json.Marshal(entry)
		key := string(encoded)
		if remaining[key] > 0 {
			additions = append(additions, entry)
			remaining[key]--
		}
	}
	return additions, removed
}

func realizesTrustGroup(group trustGroup, contexts map[string]string, present map[string]bool) bool {
	if group.Presence == "absent" {
		return !present[group.Path]
	}
	return present[group.Path] && contexts[group.Path] == group.ContextSHA256
}

func trustPoliciesEqual(left, right trustPolicy) bool {
	return semanticSetsEqual(left.Secrets, right.Secrets) &&
		semanticSetsEqual(left.Executables, right.Executables) &&
		semanticSetsEqual(left.Commands, right.Commands) &&
		semanticSetsEqual(left.Actions, right.Actions) &&
		semanticSetsEqual(left.Presence, right.Presence)
}

func stagedAuthorityMatches(group trustGroup, policy trustPolicy) bool {
	if len(policy.Secrets) != 0 {
		return false
	}
	seenExecutables := make(map[string]bool)
	for _, entry := range policy.Executables {
		rawPath := strings.TrimSpace(entry.Path)
		slashPath := strings.ReplaceAll(rawPath, "\\", "/")
		cleanPath := path.Clean(slashPath)
		key := entry.WorkflowPath + "\x00" + cleanPath + "\x00" + entry.ContextSHA256
		if entry.State != "staged" || entry.WorkflowPath != group.Path || entry.ContextSHA256 != group.ContextSHA256 ||
			rawPath != cleanPath || filepath.IsAbs(rawPath) || path.IsAbs(slashPath) || filepath.VolumeName(rawPath) != "" ||
			cleanPath == "." || cleanPath == ".." || strings.HasPrefix(cleanPath, "../") ||
			!regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(entry.SHA256) || strings.TrimSpace(entry.Rationale) == "" || seenExecutables[key] {
			return false
		}
		seenExecutables[key] = true
	}
	seenCommands := make(map[string]bool)
	for _, entry := range policy.Commands {
		command := strings.ToLower(strings.TrimSpace(entry.Command))
		key := commandKey(entry.Path, command, entry.StatementSHA256, entry.ContextSHA256)
		if entry.State != "staged" || entry.Path != group.Path || entry.ContextSHA256 != group.ContextSHA256 ||
			command == "" || command != path.Base(command) || categoricallyUnallowlistableCommandName(command) ||
			!regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(entry.StatementSHA256) || strings.TrimSpace(entry.Rationale) == "" || seenCommands[key] {
			return false
		}
		seenCommands[key] = true
	}
	seenActions := make(map[string]bool)
	for _, entry := range policy.Actions {
		uses := strings.TrimSpace(entry.Uses)
		key := actionKey(entry.Path, uses, entry.NodeSHA256, entry.ContextSHA256)
		validReference := immutableActionReference(uses)
		if entry.State != "staged" || entry.Path != group.Path || entry.ContextSHA256 != group.ContextSHA256 ||
			!validReference || strings.Contains(uses, "${{") || providerMarker(uses) || strings.Contains(strings.ToLower(uses), "digitalocean/") ||
			!regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(entry.NodeSHA256) || strings.TrimSpace(entry.Rationale) == "" || seenActions[key] {
			return false
		}
		seenActions[key] = true
	}
	return true
}

func validTrustGroupShape(group trustGroup) bool {
	cleanPath := path.Clean(strings.ReplaceAll(group.Path, "\\", "/"))
	validPath := group.Path == cleanPath && cleanPath != "." && cleanPath != ".." &&
		!strings.HasPrefix(cleanPath, "../") && !path.IsAbs(cleanPath)
	if group.Presence == "present" {
		validPath = validPath && strings.HasPrefix(cleanPath, ".github/workflows/") &&
			(path.Ext(cleanPath) == ".yml" || path.Ext(cleanPath) == ".yaml")
	}
	validContext := group.Presence == "present" && regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(group.ContextSHA256)
	validAbsent := group.Presence == "absent" && group.ContextSHA256 == ""
	return validPath && (group.State == "active" || group.State == "staged") && (validContext || validAbsent)
}

func promotedTrustPolicy(base trustPolicy, staged trustGroup) trustPolicy {
	result := trustPolicy{}
	for _, group := range base.Presence {
		if group.Path != staged.Path {
			result.Presence = append(result.Presence, group)
		}
	}
	staged.State = "active"
	result.Presence = append(result.Presence, staged)
	for _, entry := range base.Executables {
		if entry.WorkflowPath != staged.Path {
			result.Executables = append(result.Executables, entry)
		} else if entry.ContextSHA256 == staged.ContextSHA256 && entry.State == "staged" {
			entry.State = "active"
			result.Executables = append(result.Executables, entry)
		}
	}
	for _, entry := range base.Commands {
		if entry.Path != staged.Path {
			result.Commands = append(result.Commands, entry)
		} else if entry.ContextSHA256 == staged.ContextSHA256 && entry.State == "staged" {
			entry.State = "active"
			result.Commands = append(result.Commands, entry)
		}
	}
	for _, entry := range base.Actions {
		if entry.Path != staged.Path {
			result.Actions = append(result.Actions, entry)
		} else if entry.ContextSHA256 == staged.ContextSHA256 && entry.State == "staged" {
			entry.State = "active"
			result.Actions = append(result.Actions, entry)
		}
	}
	return result
}

func validateTrustTransition(base, candidate trustPolicy, baseContexts, candidateContexts map[string]string, basePresent, candidatePresent map[string]bool) []string {
	findings := []string{}
	if len(base.Secrets) != 0 || len(candidate.Secrets) != 0 {
		findings = append(findings, "public workflow secret manifest must remain exactly empty")
	}
	if trustPoliciesEqual(base, candidate) {
		return findings
	}

	presenceAdditions, presenceRemoved := semanticAdditions(base.Presence, candidate.Presence)
	executableAdditions, executableRemoved := semanticAdditions(base.Executables, candidate.Executables)
	commandAdditions, commandRemoved := semanticAdditions(base.Commands, candidate.Commands)
	actionAdditions, actionRemoved := semanticAdditions(base.Actions, candidate.Actions)
	if !presenceRemoved && !executableRemoved && !commandRemoved && !actionRemoved && len(presenceAdditions) == 1 {
		staged := presenceAdditions[0]
		additions := trustPolicy{Executables: executableAdditions, Commands: commandAdditions, Actions: actionAdditions}
		var active trustGroup
		for _, group := range base.Presence {
			if group.Path == staged.Path && group.State == "active" {
				active = group
			}
		}
		if validTrustGroupShape(staged) && staged.State == "staged" && active.State == "active" && stagedAuthorityMatches(staged, additions) &&
			realizesTrustGroup(active, baseContexts, basePresent) &&
			!(active.Presence == staged.Presence && active.ContextSHA256 == staged.ContextSHA256) {
			if realizesTrustGroup(staged, candidateContexts, candidatePresent) {
				findings = append(findings, "trust context cannot be staged and adopted in the same pull request")
			} else if realizesTrustGroup(active, candidateContexts, candidatePresent) {
				return findings
			}
		}
	}

	for _, staged := range base.Presence {
		if staged.State != "staged" || !realizesTrustGroup(staged, baseContexts, basePresent) || !realizesTrustGroup(staged, candidateContexts, candidatePresent) {
			continue
		}
		expected := promotedTrustPolicy(base, staged)
		if trustPoliciesEqual(expected, candidate) {
			return findings
		}
	}
	findings = append(findings, "trust manifest change does not match unchanged, stage-only, or promotion-only lifecycle")
	sort.Strings(findings)
	return findings
}

func loadTrustPolicy(repoRoot string) (trustPolicy, error) {
	policy := trustPolicy{}
	inputs := []struct {
		name   string
		target any
	}{
		{"secret", &policy.Secrets},
		{"executable", &policy.Executables},
		{"command", &policy.Commands},
		{"action", &policy.Actions},
		{"presence", &policy.Presence},
	}
	for _, input := range inputs {
		filePath := filepath.Join(repoRoot, ".github", "public-workflow-"+input.name+"-allowlist.json")
		if err := decodeJSONFile(filePath, input.target); err != nil {
			return trustPolicy{}, fmt.Errorf("read candidate %s allowlist: %w", input.name, err)
		}
	}
	return policy, nil
}

func repositoryWorkflowContexts(repoRoot string) (map[string]string, error) {
	contexts := make(map[string]string)
	for _, pattern := range []string{"*.yml", "*.yaml"} {
		matches, err := filepath.Glob(filepath.Join(repoRoot, ".github", "workflows", pattern))
		if err != nil {
			return nil, err
		}
		for _, workflowPath := range matches {
			data, err := os.ReadFile(workflowPath)
			if err != nil {
				return nil, err
			}
			var document yaml.Node
			if err := yaml.Unmarshal(data, &document); err != nil || len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
				continue
			}
			relative, err := filepath.Rel(repoRoot, workflowPath)
			if err != nil {
				return nil, err
			}
			contexts[filepath.ToSlash(relative)] = authorizationContextDigest(document.Content[0], nil, nil)
		}
	}
	return contexts, nil
}

func trustPresence(repoRoot string, policies ...trustPolicy) (map[string]bool, error) {
	present := make(map[string]bool)
	for _, policy := range policies {
		for _, group := range policy.Presence {
			if present[group.Path] {
				continue
			}
			candidate := filepath.Join(repoRoot, filepath.FromSlash(group.Path))
			info, err := os.Lstat(candidate)
			if err == nil {
				present[group.Path] = true
				if info.Mode()&os.ModeSymlink != 0 {
					return nil, fmt.Errorf("trust path is a symlink: %s", group.Path)
				}
			} else if !os.IsNotExist(err) {
				return nil, err
			}
		}
	}
	return present, nil
}

func authorityManifestPath(repoRoot string) (string, error) {
	manifestPath := filepath.Join(repoRoot, ".github", "public-workflow-authority.json")
	info, err := os.Lstat(manifestPath)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("authority manifest must not be a symlink")
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("authority manifest must be a regular file")
	}
	return manifestPath, nil
}

func validateRepositoryAuthority(repoRoot, scanRoot string, bootstrap bool) ([]string, error) {
	candidateManifest := authorityManifest{}
	candidateManifestPath, err := authorityManifestPath(scanRoot)
	if err != nil {
		return nil, fmt.Errorf("inspect candidate authority manifest: %w", err)
	}
	if err := decodeJSONFile(candidateManifestPath, &candidateManifest); err != nil {
		return nil, fmt.Errorf("read candidate authority manifest: %w", err)
	}
	candidateInventory, err := authorityInventory(scanRoot)
	if err != nil {
		return nil, fmt.Errorf("inventory candidate authority: %w", err)
	}
	if bootstrap {
		return validateAuthorityTransition(nil, candidateManifest, nil, candidateInventory, true), nil
	}
	baseManifest := authorityManifest{}
	baseManifestPath, err := authorityManifestPath(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("inspect base authority manifest: %w", err)
	}
	if err := decodeJSONFile(baseManifestPath, &baseManifest); err != nil {
		return nil, fmt.Errorf("read base authority manifest: %w", err)
	}
	baseInventory, err := authorityInventory(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("inventory base authority: %w", err)
	}
	findings := validateAuthorityTransition(&baseManifest, candidateManifest, baseInventory, candidateInventory, false)

	basePolicy, err := loadTrustPolicy(repoRoot)
	if err != nil {
		return nil, err
	}
	candidatePolicy, err := loadTrustPolicy(scanRoot)
	if err != nil {
		return nil, err
	}
	baseContexts, err := repositoryWorkflowContexts(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("read base workflow contexts: %w", err)
	}
	candidateContexts, err := repositoryWorkflowContexts(scanRoot)
	if err != nil {
		return nil, fmt.Errorf("read candidate workflow contexts: %w", err)
	}
	basePresent, err := trustPresence(repoRoot, basePolicy, candidatePolicy)
	if err != nil {
		return nil, fmt.Errorf("inspect base trust paths: %w", err)
	}
	candidatePresent, err := trustPresence(scanRoot, basePolicy, candidatePolicy)
	if err != nil {
		return nil, fmt.Errorf("inspect candidate trust paths: %w", err)
	}
	findings = append(findings, validateTrustTransition(basePolicy, candidatePolicy, baseContexts, candidateContexts, basePresent, candidatePresent)...)
	sort.Strings(findings)
	return findings, nil
}

func selectTrustGroups(groups []trustGroup, workflowContexts map[string]string, presentPaths map[string]bool) (map[string]bool, []string) {
	selected := make(map[string]bool)
	findings := []string{}
	byPath := make(map[string]map[string]trustGroup)
	for _, group := range groups {
		if !validTrustGroupShape(group) {
			findings = append(findings, fmt.Sprintf("invalid trust group for %s", group.Path))
			continue
		}
		if byPath[group.Path] == nil {
			byPath[group.Path] = make(map[string]trustGroup)
		}
		if _, ok := byPath[group.Path][group.State]; ok {
			findings = append(findings, fmt.Sprintf("multiple %s trust groups for %s", group.State, group.Path))
		}
		byPath[group.Path][group.State] = group
		other := "active"
		if group.State == "active" {
			other = "staged"
		}
		if prior, ok := byPath[group.Path][other]; ok && prior.Presence == group.Presence && prior.ContextSHA256 == group.ContextSHA256 {
			findings = append(findings, fmt.Sprintf("mixed trust group state for %s context %s", group.Path, group.ContextSHA256))
		}
	}
	for workflowPath, context := range workflowContexts {
		var matched trustGroup
		for _, group := range byPath[workflowPath] {
			if group.Presence == "present" && group.ContextSHA256 == context {
				matched = group
			}
		}
		if matched.State == "" {
			findings = append(findings, fmt.Sprintf("no trust group matches workflow %s context %s", workflowPath, context))
			continue
		}
		if matched.State == "staged" && byPath[workflowPath]["active"].State == "" {
			findings = append(findings, fmt.Sprintf("staged trust group for %s requires retained active group", workflowPath))
		}
		selected[workflowPath+"\x00"+context] = true
	}
	for workflowPath, states := range byPath {
		if _, ok := workflowContexts[workflowPath]; ok {
			continue
		}
		selectedGroup := states["active"]
		if states["staged"].Presence == "absent" {
			selectedGroup = states["staged"]
		}
		if selectedGroup.Presence != "absent" {
			findings = append(findings, fmt.Sprintf("no absent trust group matches missing workflow %s", workflowPath))
			continue
		}
		if presentPaths[workflowPath] {
			findings = append(findings, fmt.Sprintf("absent trust group path exists in candidate %s", workflowPath))
			continue
		}
		if selectedGroup.State == "staged" && states["active"].State == "" {
			findings = append(findings, fmt.Sprintf("staged trust group for %s requires retained active group", workflowPath))
		}
		selected[workflowPath+"\x00absent"] = true
	}
	sort.Strings(findings)
	return selected, findings
}

type findingSet struct {
	items []string
}

func (f *findingSet) add(format string, args ...any) {
	f.items = append(f.items, fmt.Sprintf(format, args...))
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func canonicalYAMLNode(out *bytes.Buffer, node *yaml.Node) {
	if node == nil {
		out.WriteString("nil;")
		return
	}
	fmt.Fprintf(out, "%d:%d:%s:%d:%s:", node.Kind, len(node.Tag), node.Tag, len(node.Value), node.Value)
	if node.Kind == yaml.MappingNode {
		type pair struct {
			key   string
			value string
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

func actionNodeDigest(node *yaml.Node) string {
	var canonical bytes.Buffer
	canonicalYAMLNode(&canonical, node)
	digest := sha256.Sum256(canonical.Bytes())
	return fmt.Sprintf("%x", digest)
}

func validateYAMLStructure(prefix string, node *yaml.Node, findings *findingSet) {
	if node == nil {
		return
	}
	if node.Kind == yaml.AliasNode {
		findings.add("%s contains forbidden YAML alias", prefix)
		return
	}
	if node.Kind == yaml.MappingNode {
		seen := make(map[string]bool)
		for index := 0; index+1 < len(node.Content); index += 2 {
			var canonical bytes.Buffer
			canonicalYAMLNode(&canonical, node.Content[index])
			key := canonical.String()
			if seen[key] {
				findings.add("%s contains duplicate mapping key %s", prefix, node.Content[index].Value)
			}
			seen[key] = true
		}
	}
	for _, child := range node.Content {
		validateYAMLStructure(prefix, child, findings)
	}
}

func executionAffectingEnv(name string) bool {
	name = strings.ToUpper(strings.TrimSpace(name))
	exact := map[string]bool{
		"PATH": true, "BASH_ENV": true, "ENV": true, "SHELLOPTS": true,
		"LD_PRELOAD": true, "NODE_OPTIONS": true, "PYTHONPATH": true,
		"PYTHONHOME": true, "RUBYOPT": true, "PERL5OPT": true,
		"PERL5LIB": true, "HOME": true, "IFS": true, "CDPATH": true,
		"CC": true, "CXX": true, "AR": true, "LD": true,
		"GOROOT": true, "GOPATH": true, "GOENV": true, "GOFLAGS": true,
		"GOTOOLCHAIN": true, "LD_LIBRARY_PATH": true, "DYLD_LIBRARY_PATH": true,
		"LIBRARY_PATH": true, "CPATH": true, "C_INCLUDE_PATH": true,
		"CPLUS_INCLUDE_PATH": true, "OBJC_INCLUDE_PATH": true,
		"PKG_CONFIG_PATH": true, "RUSTFLAGS": true, "RUSTC_WRAPPER": true,
		"JAVA_TOOL_OPTIONS": true, "_JAVA_OPTIONS": true, "MAVEN_OPTS": true,
		"GRADLE_OPTS": true, "GEM_HOME": true, "GEM_PATH": true,
		"BUNDLE_GEMFILE": true, "PATHEXT": true, "SHELL": true,
		"BASHOPTS": true, "PROMPT_COMMAND": true, "PS4": true,
		"CFLAGS": true, "CXXFLAGS": true, "CPPFLAGS": true, "LDFLAGS": true,
		"AS": true, "FC": true, "F77": true, "RANLIB": true, "STRIP": true,
		"OBJC": true, "OBJCXX": true, "SDKROOT": true,
		"MACOSX_DEPLOYMENT_TARGET": true, "JAVA_HOME": true,
		"JDK_JAVA_OPTIONS": true, "CARGO_HOME": true, "CARGO_TARGET_DIR": true,
		"CARGO_ENCODED_RUSTFLAGS": true, "ZDOTDIR": true,
	}
	if exact[name] {
		return true
	}
	for _, prefix := range []string{
		"BASH_FUNC_", "DYLD_", "LD_", "GIT_", "NPM_CONFIG_", "NODE_", "PYTHON",
		"RUBY", "PERL", "RUST", "CGO_",
	} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	if strings.HasPrefix(name, "GO") && name != "GOPRIVATE" && name != "GOOS" && name != "GOARCH" {
		return true
	}
	return false
}

func checkWorkingDirectory(prefix string, node *yaml.Node, findings *findingSet) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	if mappingValue(node, "working-directory") != nil {
		findings.add("%s declares forbidden working-directory", prefix)
	}
	if run := mappingValue(mappingValue(node, "defaults"), "run"); run != nil && mappingValue(run, "working-directory") != nil {
		findings.add("%s declares forbidden defaults.run.working-directory", prefix)
	}
}

func checkJobRuntime(prefix string, job *yaml.Node, findings *findingSet) {
	if job == nil || job.Kind != yaml.MappingNode {
		return
	}
	if mappingValue(job, "container") != nil {
		findings.add("%s declares forbidden job container", prefix)
	}
	if mappingValue(job, "services") != nil {
		findings.add("%s declares forbidden job services", prefix)
	}
}

func authorizationContextDigest(workflow, _, _ *yaml.Node) string {
	var canonical bytes.Buffer
	canonicalYAMLNode(&canonical, workflow)
	digest := sha256.Sum256(canonical.Bytes())
	return fmt.Sprintf("%x", digest)
}

func scalars(node *yaml.Node, out *[]string) {
	if node == nil {
		return
	}
	if node.Kind == yaml.ScalarNode {
		*out = append(*out, node.Value)
	}
	for _, child := range node.Content {
		scalars(child, out)
	}
}

func knownCredentialVariables(node *yaml.Node, out map[string]bool) {
	if node == nil {
		return
	}
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i].Value
			value := node.Content[i+1]
			if (key == "env" || key == "secrets") && value.Kind == yaml.MappingNode {
				for j := 0; j+1 < len(value.Content); j += 2 {
					name := strings.ToUpper(value.Content[j].Value)
					if knownCloudSecret(name) {
						out[name] = true
					}
				}
			}
		}
	}
	for _, child := range node.Content {
		knownCredentialVariables(child, out)
	}
}

func triggerPresent(root *yaml.Node, name string) bool {
	on := mappingValue(root, "on")
	if on == nil {
		return false
	}
	switch on.Kind {
	case yaml.ScalarNode:
		return on.Value == name
	case yaml.SequenceNode:
		for _, item := range on.Content {
			if item.Value == name {
				return true
			}
		}
	case yaml.MappingNode:
		return mappingValue(on, name) != nil
	}
	return false
}

var (
	secretRefRE          = regexp.MustCompile(`(?i)secrets(?:\.([A-Za-z_][A-Za-z0-9_]*)|\[[[:space:]]*['"]([A-Za-z_][A-Za-z0-9_]*)['"][[:space:]]*\])`)
	providerCLIRE        = regexp.MustCompile(`(^|[^A-Za-z0-9_.-])(doctl|gcloud|az|aws)([^A-Za-z0-9_.-]|$)`)
	integrationRE        = regexp.MustCompile(`(^|[[:space:]])-tags(?:=|[[:space:]])[^[:space:]]*integration`)
	namedLiveRE          = regexp.MustCompile(`(?i)(-run[=[:space:]]+[^[:space:]]*live|test[A-Za-z0-9_]*live|conformance_live_cloud)`)
	providerAPIRE        = regexp.MustCompile(`(?i)(api\.digitalocean\.com|management\.azure\.com|[A-Za-z0-9.-]+\.amazonaws\.com|[A-Za-z0-9.-]+\.googleapis\.com|api\.cloudflare\.com)`)
	githubRunnerRE       = regexp.MustCompile(`^(ubuntu-(latest|[0-9]{2}\.[0-9]{2})(-arm)?|windows-(latest|[0-9]{4})|macos-(latest|[0-9]{2})(-(large|xlarge))?)$`)
	secretIndexRE        = regexp.MustCompile(`(?i)secrets\[[^]\r\n]+\]`)
	secretWildcardRE     = regexp.MustCompile(`(?i)secrets\.\*`)
	wholeSecretsRE       = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_])secrets([^A-Za-z0-9_.\[]|$)`)
	literalSecretIndexRE = regexp.MustCompile(`(?i)^secrets\[[[:space:]]*['"][A-Za-z_][A-Za-z0-9_]*['"][[:space:]]*\]$`)
	varsRefRE            = regexp.MustCompile(`(?i)vars(?:\.([A-Za-z_][A-Za-z0-9_]*)|\[[[:space:]]*['"]([A-Za-z_][A-Za-z0-9_]*)['"][[:space:]]*\])`)
	varsIndexRE          = regexp.MustCompile(`(?i)vars\[[^]\r\n]+\]`)
	literalVarsIndexRE   = regexp.MustCompile(`(?i)^vars\[[[:space:]]*['"][A-Za-z_][A-Za-z0-9_]*['"][[:space:]]*\]$`)
	envAssignmentRE      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	exactGoVersionRE     = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z][0-9A-Za-z.-]*)?(?:\+[0-9A-Za-z][0-9A-Za-z.-]*)?$`)
)

var providerSDKMarkers = [...]string{
	"digitalocean/godo",
	"aws-sdk",
	"azure-sdk",
	"cloud.google.com/go",
	"google-cloud-",
}

func githubExpressions(value string) ([]string, bool) {
	expressions := []string{}
	for {
		start := strings.Index(value, "${{")
		if start < 0 {
			return expressions, true
		}
		value = value[start+3:]
		end := strings.Index(value, "}}")
		if end < 0 {
			return expressions, false
		}
		expressions = append(expressions, strings.TrimSpace(value[:end]))
		value = value[end+2:]
	}
}

func normalizeGithubExpressions(value string) (string, error) {
	var out strings.Builder
	for {
		start := strings.Index(value, "${{")
		if start < 0 {
			out.WriteString(value)
			return out.String(), nil
		}
		out.WriteString(value[:start])
		remainder := value[start+3:]
		end := strings.Index(remainder, "}}")
		if end < 0 {
			return "", fmt.Errorf("unterminated GitHub expression")
		}
		expressionSource := value[start : start+3+end+2]
		digest := sha256.Sum256([]byte(expressionSource))
		fmt.Fprintf(&out, "__GITHUB_EXPRESSION_%x__", digest)
		value = remainder[end+2:]
	}
}

func expressionIdentifiers(value string) string {
	masked := []byte(value)
	for index := 0; index < len(masked); {
		quote := masked[index]
		if quote != '\'' && quote != '"' {
			index++
			continue
		}
		previous := index - 1
		for previous >= 0 && (masked[previous] == ' ' || masked[previous] == '\t') {
			previous--
		}
		// Preserve a quoted bracket selector so exact `secrets['NAME']` and
		// `vars['NAME']` selectors remain statically reviewable. Other string
		// contents are data, not expression identifiers.
		preserve := false
		if previous >= 0 && masked[previous] == '[' {
			selectorPrefix := strings.TrimSpace(string(masked[:previous]))
			preserve = regexp.MustCompile(`(?i)(secrets|vars)$`).MatchString(selectorPrefix)
		}
		index++
		for index < len(masked) {
			if masked[index] == quote {
				if index+1 < len(masked) && masked[index+1] == quote {
					if !preserve {
						masked[index] = ' '
						masked[index+1] = ' '
					}
					index += 2
					continue
				}
				index++
				break
			}
			if !preserve {
				masked[index] = ' '
			}
			index++
		}
	}
	return string(masked)
}

func containsProviderSDKMarker(value string) bool {
	value = strings.ToLower(value)
	for _, marker := range providerSDKMarkers {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func providerMarker(value string) bool {
	return providerCLIRE.MatchString(value) || providerAPIRE.MatchString(value) || containsProviderSDKMarker(value)
}

func envValues(prefix string, env *yaml.Node, allowDenyPattern bool, findings *findingSet) map[string]string {
	patterns := make(map[string]string)
	if env == nil {
		return patterns
	}
	if env.Kind != yaml.MappingNode {
		findings.add("%s uses unsupported environment shape", prefix)
		return patterns
	}
	for i := 0; i+1 < len(env.Content); i += 2 {
		name := env.Content[i].Value
		valueNode := env.Content[i+1]
		literalValue := valueNode.Kind == yaml.ScalarNode && !strings.Contains(valueNode.Value, "${{")
		if executionAffectingEnv(name) && !safeExecutionEnvironmentAssignment(name, valueNode.Value, literalValue) {
			findings.add("%s declares forbidden execution-affecting environment variable %s", prefix, strings.ToUpper(name))
		}
		if valueNode.Kind != yaml.ScalarNode {
			findings.add("%s environment variable %s uses unsupported value shape", prefix, name)
			continue
		}
		value := valueNode.Value
		if strings.HasSuffix(name, "_DENY_PATTERN") && providerMarker(value) {
			if allowDenyPattern {
				patterns[name] = value
			} else {
				findings.add("%s provider deny pattern %s must be step-local", prefix, name)
			}
			continue
		}
		if match := providerCLIRE.FindStringSubmatch(value); match != nil {
			findings.add("%s environment value contains provider CLI %s", prefix, match[2])
		}
		if match := providerAPIRE.FindStringSubmatch(value); match != nil {
			findings.add("%s environment value contains provider API %s", prefix, match[1])
		}
		if containsProviderSDKMarker(value) {
			findings.add("%s environment value contains provider SDK marker", prefix)
		}
	}
	return patterns
}

func secretReferences(node *yaml.Node) map[string]bool {
	values := []string{}
	scalars(node, &values)
	secrets := make(map[string]bool)
	for _, value := range values {
		expressions, _ := githubExpressions(value)
		for _, expression := range expressions {
			expression = expressionIdentifiers(expression)
			for _, match := range secretRefRE.FindAllStringSubmatch(expression, -1) {
				name := match[1]
				if name == "" {
					name = match[2]
				}
				secrets[strings.ToUpper(name)] = true
			}
		}
	}
	return secrets
}

func validateSecretReferences(rel, prefix string, secrets map[string]bool, referenced map[string]bool, findings *findingSet) {
	for secret := range secrets {
		key := rel + "\x00" + secret
		referenced[key] = true
		if knownCloudSecret(secret) {
			findings.add("%s references known cloud secret %s", prefix, secret)
		} else {
			findings.add("%s public workflow references forbidden repository secret %s", prefix, secret)
		}
	}
}

func validateInheritedSecrets(prefix string, secrets *yaml.Node, findings *findingSet) {
	if secrets == nil {
		return
	}
	if secrets.Kind == yaml.ScalarNode {
		if strings.TrimSpace(secrets.Value) == "inherit" {
			findings.add("%s inherits all job secrets", prefix)
		} else {
			findings.add("%s uses unsupported job secrets scalar", prefix)
		}
		return
	}
	if secrets.Kind != yaml.MappingNode {
		findings.add("%s uses unsupported job secrets shape", prefix)
		return
	}
	for index := 0; index+1 < len(secrets.Content); index += 2 {
		if value := secrets.Content[index+1]; value.Kind == yaml.ScalarNode && strings.TrimSpace(value.Value) == "inherit" {
			findings.add("%s maps inherited secret %s", prefix, secrets.Content[index].Value)
		}
	}
}

func validateCredentialSelectors(prefix string, node *yaml.Node, findings *findingSet) {
	values := []string{}
	scalars(node, &values)
	for _, value := range values {
		expressions, complete := githubExpressions(value)
		if !complete {
			findings.add("%s contains an unterminated GitHub expression", prefix)
		}
		for _, expression := range expressions {
			originalExpression := expression
			expression = expressionIdentifiers(originalExpression)
			if wholeSecretsRE.MatchString(expression) {
				findings.add("%s uses forbidden whole secrets context", prefix)
			}
			for _, selector := range secretWildcardRE.FindAllString(expression, -1) {
				findings.add("%s uses forbidden dynamic secret selector %s", prefix, selector)
			}
			for _, bounds := range secretIndexRE.FindAllStringIndex(expression, -1) {
				selector := originalExpression[bounds[0]:bounds[1]]
				if !literalSecretIndexRE.MatchString(selector) {
					findings.add("%s uses forbidden dynamic secret selector %s", prefix, selector)
				}
			}
			for _, match := range varsRefRE.FindAllStringSubmatch(expression, -1) {
				name := match[1]
				if name == "" {
					name = match[2]
				}
				name = strings.ToUpper(name)
				if knownCloudSecret(name) {
					findings.add("%s references known cloud credential variable reference %s", prefix, name)
				}
			}
			for _, bounds := range varsIndexRE.FindAllStringIndex(expression, -1) {
				selector := originalExpression[bounds[0]:bounds[1]]
				if !literalVarsIndexRE.MatchString(selector) {
					findings.add("%s uses forbidden dynamic variable selector %s", prefix, selector)
				}
			}
		}
	}
}

func checkRunnerSelector(prefix string, runsOn *yaml.Node, findings *findingSet) {
	if runsOn == nil {
		findings.add("%s does not declare a GitHub-hosted runner selector", prefix)
		return
	}
	var selectors []string
	switch runsOn.Kind {
	case yaml.ScalarNode:
		selectors = append(selectors, runsOn.Value)
	case yaml.SequenceNode:
		for _, item := range runsOn.Content {
			if item.Kind != yaml.ScalarNode {
				findings.add("%s uses a non-scalar runner selector", prefix)
				continue
			}
			selectors = append(selectors, item.Value)
		}
	default:
		findings.add("%s uses an unsupported runner selector shape", prefix)
		return
	}
	if len(selectors) == 0 {
		findings.add("%s does not declare a GitHub-hosted runner selector", prefix)
	}
	for _, selector := range selectors {
		selector = strings.TrimSpace(selector)
		if strings.Contains(selector, "${{") {
			findings.add("%s uses forbidden dynamic runner selector %s", prefix, selector)
			continue
		}
		if strings.EqualFold(selector, "self-hosted") {
			findings.add("%s uses forbidden self-hosted runner", prefix)
			continue
		}
		if !githubRunnerRE.MatchString(selector) {
			findings.add("%s runner selector %s is not recognized as GitHub-hosted", prefix, selector)
		}
	}
}

func checkJobRunnerSelector(prefix string, job *yaml.Node, findings *findingSet) {
	if mappingValue(job, "uses") != nil {
		findings.add("%s uses forbidden reusable workflow job", prefix)
		return
	}
	checkRunnerSelector(prefix, mappingValue(job, "runs-on"), findings)
}

func checkPermissions(prefix string, permissions *yaml.Node, forbidWrite bool, findings *findingSet) {
	if permissions == nil {
		findings.add("%s does not declare permissions and workflow has no explicit permissions to inherit", prefix)
		return
	}
	if permissions.Kind == yaml.ScalarNode {
		value := strings.TrimSpace(permissions.Value)
		switch {
		case value == "read-all":
			return
		case value == "write-all":
			findings.add("%s uses forbidden permissions: write-all", prefix)
		case strings.Contains(value, "${{"):
			findings.add("%s uses forbidden dynamic permissions selector %s", prefix, value)
		default:
			findings.add("%s uses unsupported permissions scalar %s", prefix, value)
		}
		return
	}
	if permissions.Kind != yaml.MappingNode {
		findings.add("%s uses unsupported permissions shape", prefix)
		return
	}
	known := map[string]bool{
		"actions": true, "attestations": true, "checks": true, "contents": true,
		"deployments": true, "discussions": true, "id-token": true, "issues": true,
		"models": true, "packages": true, "pages": true, "pull-requests": true,
		"security-events": true, "statuses": true,
	}
	for i := 0; i+1 < len(permissions.Content); i += 2 {
		key := permissions.Content[i].Value
		valueNode := permissions.Content[i+1]
		if !known[key] {
			findings.add("%s declares unsupported permission %s", prefix, key)
			continue
		}
		if valueNode.Kind != yaml.ScalarNode {
			findings.add("%s permission %s uses unsupported value shape", prefix, key)
			continue
		}
		value := strings.TrimSpace(valueNode.Value)
		if strings.Contains(value, "${{") {
			findings.add("%s permission %s uses forbidden dynamic value %s", prefix, key, value)
			continue
		}
		if value != "read" && value != "write" && value != "none" {
			findings.add("%s permission %s uses unsupported value %s", prefix, key, value)
			continue
		}
		if forbidWrite && value == "write" {
			findings.add("%s grants write permission %s outside a job", prefix, key)
		}
		if key == "id-token" && value == "write" {
			findings.add("%s grants forbidden id-token: write", prefix)
		}
	}
}

func defaultsRunShell(node *yaml.Node) *yaml.Node {
	defaults := mappingValue(node, "defaults")
	run := mappingValue(defaults, "run")
	return mappingValue(run, "shell")
}

func checkShell(prefix string, shell *yaml.Node, findings *findingSet) {
	if shell == nil {
		return
	}
	if shell.Kind != yaml.ScalarNode || strings.Contains(shell.Value, "${{") {
		findings.add("%s uses forbidden dynamic shell %s", prefix, shell.Value)
		return
	}
	if strings.TrimSpace(shell.Value) != "bash" {
		findings.add("%s uses forbidden custom shell %s", prefix, shell.Value)
	}
}

func knownCloudSecret(name string) bool {
	name = strings.ToUpper(name)
	spacesCredentials := map[string]bool{
		"SPACES_ACCESS_KEY_ID":                  true,
		"SPACES_SECRET_ACCESS_KEY":              true,
		"DIGITALOCEAN_SPACES_ACCESS_KEY_ID":     true,
		"DIGITALOCEAN_SPACES_SECRET_ACCESS_KEY": true,
		"DO_SPACES_ACCESS_KEY_ID":               true,
		"DO_SPACES_SECRET_ACCESS_KEY":           true,
	}
	if spacesCredentials[name] {
		return true
	}
	markers := []string{"DIGITALOCEAN", "AWS", "AZURE", "GCP", "GOOGLE_CLOUD", "CLOUDFLARE"}
	for _, marker := range markers {
		if strings.Contains(name, marker) {
			return true
		}
	}
	if strings.HasPrefix(name, "DO_") && regexp.MustCompile(`(TOKEN|KEY|SECRET|CREDENTIAL)`).MatchString(name) {
		return true
	}
	return strings.Contains(name, "KUBE") && regexp.MustCompile(`(CONFIG|TOKEN|SECRET|CREDENTIAL)`).MatchString(name)
}

func decodeJSONFile(filePath string, target any) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	value := reflect.ValueOf(target)
	if value.Kind() == reflect.Pointer && !value.IsNil() && value.Elem().Kind() == reflect.Slice && value.Elem().IsNil() {
		return fmt.Errorf("top-level JSON array must not be null")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON value")
		}
		return fmt.Errorf("unexpected trailing JSON data: %w", err)
	}
	return nil
}

func statementDigest(stmt *syntax.Stmt) (string, error) {
	var canonical bytes.Buffer
	if err := syntax.NewPrinter().Print(&canonical, stmt); err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical.Bytes())
	return fmt.Sprintf("%x", digest), nil
}

func commandKey(workflowPath, command, statementSHA256, contextSHA256 string) string {
	return workflowPath + "\x00" + command + "\x00" + statementSHA256 + "\x00" + contextSHA256
}

func actionKey(workflowPath, uses, nodeSHA256, contextSHA256 string) string {
	return workflowPath + "\x00" + uses + "\x00" + nodeSHA256 + "\x00" + contextSHA256
}

func immutableActionReference(reference string) bool {
	return regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+@[a-f0-9]{40}$`).MatchString(reference) ||
		regexp.MustCompile(`^docker://[A-Za-z0-9.-]+(/[A-Za-z0-9_.-]+)+@sha256:[a-f0-9]{64}$`).MatchString(reference)
}

func matchAction(workflowPath, uses, nodeSHA256, contextSHA256 string, allowed map[string]actionEntry) (string, bool) {
	key := actionKey(workflowPath, uses, nodeSHA256, contextSHA256)
	_, ok := allowed[key]
	return key, ok
}

func assignmentOnlyCall(call *syntax.CallExpr) bool {
	return call != nil && len(call.Args) == 0 && len(call.Assigns) > 0
}

func knownProviderCommand(command string, prefix []string) bool {
	switch normalizedCommandName(command) {
	case "go":
		if len(prefix) == 0 {
			return false
		}
		return prefix[0] == "run" || mutableGoInstall(prefix)
	case "helm":
		return len(prefix) == 0 || (prefix[0] != "lint" && prefix[0] != "template")
	case "npm":
		if len(prefix) == 0 {
			return true
		}
		return prefix[0] != "ci" && prefix[0] != "run" && prefix[0] != "test"
	}
	return categoricallyUnallowlistableCommandName(command)
}

func mutableGoInstall(argv []string) bool {
	if len(argv) < 2 || argv[0] != "install" {
		return false
	}
	for _, argument := range argv[1:] {
		at := strings.LastIndex(argument, "@")
		if at >= 0 && !exactGoVersionRE.MatchString(argument[at+1:]) {
			return true
		}
	}
	return false
}

func categoricallyUnallowlistableCommandName(command string) bool {
	forbidden := map[string]bool{
		"ansible": true, "aws": true, "az": true, "curl": true,
		"builtin": true, "doctl": true, "docker": true, "gcloud": true,
		"eval": true, "http": true, "https": true, "kubectl": true, "mc": true,
		"node": true, "npx": true, "perl": true,
		"php": true, "powershell": true, "pulumi": true, "pwsh": true,
		"python": true, "python3": true, "rclone": true, "ruby": true,
		"s3cmd": true, "terraform": true, "tofu": true, "wget": true,
	}
	if forbidden[normalizedCommandName(command)] {
		return true
	}
	return false
}

func normalizedCommandName(command string) string {
	return strings.ToLower(path.Base(strings.ReplaceAll(command, `\`, "")))
}

func pathEscapes(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel)
}

func literalParts(parts []syntax.WordPart, out *strings.Builder) bool {
	for _, part := range parts {
		switch part := part.(type) {
		case *syntax.Lit:
			out.WriteString(part.Value)
		case *syntax.SglQuoted:
			out.WriteString(part.Value)
		case *syntax.DblQuoted:
			if !literalParts(part.Parts, out) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func literalWord(word *syntax.Word) (string, bool) {
	if word == nil {
		return "", false
	}
	var out strings.Builder
	if !literalParts(word.Parts, &out) {
		return "", false
	}
	return out.String(), true
}

func exactParameterParts(parts []syntax.WordPart) (string, bool) {
	if len(parts) != 1 {
		return "", false
	}
	switch part := parts[0].(type) {
	case *syntax.ParamExp:
		if part.Param == nil || part.Excl || part.Length || part.Width || part.IsSet || part.Index != nil || part.Slice != nil || part.Repl != nil || part.Exp != nil || part.Names != 0 || len(part.Modifiers) != 0 {
			return "", false
		}
		return part.Param.Value, true
	case *syntax.DblQuoted:
		return exactParameterParts(part.Parts)
	default:
		return "", false
	}
}

func exactParameterWord(word *syntax.Word) (string, bool) {
	if word == nil {
		return "", false
	}
	return exactParameterParts(word.Parts)
}

func skipWrapperOptions(args []*syntax.Word, index int, wrapper string) (int, bool) {
	for index < len(args) {
		value, literal := literalWord(args[index])
		if !literal {
			return index, false
		}
		if value == "--" {
			return index + 1, true
		}
		if wrapper == "env" && envAssignmentRE.MatchString(value) {
			index++
			continue
		}
		if !strings.HasPrefix(value, "-") || value == "-" {
			return index, true
		}
		// Wrapper options can consume or embed command words. Reject them
		// instead of guessing where execution begins.
		return index, false
	}
	return index, true
}

func resolvedProgram(call *syntax.CallExpr) (*syntax.Word, []*syntax.Word, bool) {
	args := call.Args
	index := 0
	for index < len(args) {
		value, literal := literalWord(args[index])
		if !literal || strings.Contains(value, "__GITHUB_EXPRESSION_") {
			return args[index], nil, false
		}
		base := path.Base(value)
		if base == "sudo" {
			return args[index], nil, false
		}
		if base != "command" && base != "exec" && base != "env" {
			return args[index], args[index+1:], true
		}
		index++
		var ok bool
		index, ok = skipWrapperOptions(args, index, base)
		if !ok {
			if index < len(args) {
				return args[index], nil, false
			}
			return nil, nil, false
		}
	}
	return nil, nil, true
}

func resolvedWorkflowCommand(call *syntax.CallExpr) (*syntax.Word, []*syntax.Word, bool) {
	program, args, resolved := resolvedProgram(call)
	if !resolved || program == nil {
		return program, args, resolved
	}
	value, literal := literalWord(program)
	if !literal {
		return program, nil, false
	}
	base := path.Base(value)
	if base != "bash" && base != "sh" && base != "source" && base != "." {
		return program, args, true
	}
	index := 0
	for index < len(args) {
		argument, ok := literalWord(args[index])
		if !ok {
			return args[index], nil, false
		}
		if argument == "--" {
			index++
			break
		}
		if !strings.HasPrefix(argument, "-") || argument == "-" {
			break
		}
		// Shell options such as -c can execute arbitrary text and are never
		// treated as a local-script invocation.
		return args[index], nil, false
	}
	if index >= len(args) {
		return nil, nil, false
	}
	return args[index], args[index+1:], true
}

func directCall(stmt *syntax.Stmt) (*syntax.CallExpr, bool) {
	if stmt == nil || stmt.Negated || stmt.Background || len(stmt.Redirs) != 0 {
		return nil, false
	}
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	return call, ok && len(call.Assigns) == 0
}

func pureRejectionGuard(file *syntax.File, requiredPatterns map[string]string) bool {
	if file == nil || len(file.Stmts) != 1 {
		return false
	}
	clause, ok := file.Stmts[0].Cmd.(*syntax.IfClause)
	if !ok || clause.Else != nil || len(clause.Cond) != 1 || len(clause.Then) == 0 {
		return false
	}
	condition, ok := directCall(clause.Cond[0])
	if !ok || len(condition.Args) < 3 {
		return false
	}
	tool, literal := literalWord(condition.Args[0])
	if !literal || !map[string]bool{"rg": true, "grep": true, "egrep": true, "fgrep": true}[path.Base(tool)] {
		return false
	}
	patternIndex := 1
	for patternIndex < len(condition.Args) {
		value, ok := literalWord(condition.Args[patternIndex])
		if !ok || !strings.HasPrefix(value, "-") || value == "-" {
			break
		}
		patternIndex++
	}
	if patternIndex >= len(condition.Args)-1 {
		return false
	}
	if len(requiredPatterns) > 0 {
		name, ok := exactParameterWord(condition.Args[patternIndex])
		if !ok {
			return false
		}
		if _, ok := requiredPatterns[name]; !ok || len(requiredPatterns) != 1 {
			return false
		}
	} else if _, ok := literalWord(condition.Args[patternIndex]); !ok {
		return false
	}
	for _, argument := range condition.Args[patternIndex+1:] {
		if _, ok := literalWord(argument); !ok {
			return false
		}
	}
	failed := false
	for _, stmt := range clause.Then {
		call, ok := directCall(stmt)
		if !ok || len(call.Args) == 0 {
			return false
		}
		command, ok := literalWord(call.Args[0])
		if !ok {
			return false
		}
		switch path.Base(command) {
		case "echo", "printf":
			for _, argument := range call.Args[1:] {
				if _, ok := literalWord(argument); !ok {
					return false
				}
			}
		case "false":
			if len(call.Args) != 1 {
				return false
			}
			failed = true
		case "exit":
			if len(call.Args) != 2 {
				return false
			}
			code, ok := literalWord(call.Args[1])
			if !ok || code == "0" {
				return false
			}
			failed = true
		default:
			return false
		}
	}
	return failed
}

type shellAnalysis struct {
	providerAuthority bool
	hasIntegrationTag bool
	hasProviderSDK    bool
	namedLive         bool
}

func inspectStatementGuards(prefix string, stmt *syntax.Stmt, findings *findingSet) {
	dangerousName := func(name string) bool {
		return executionAffectingEnv(name)
	}
	syntax.Walk(stmt, func(node syntax.Node) bool {
		switch node := node.(type) {
		case *syntax.Assign:
			if node.Name == nil {
				return true
			}
			name := node.Name.Value
			value, literal := literalWord(node.Value)
			if dangerousName(name) && (node.Append || node.Index != nil || !safeExecutionEnvironmentAssignment(name, value, literal)) {
				findings.add("%s assigns forbidden execution environment variable %s", prefix, name)
			}
		case *syntax.CallExpr:
			for _, argument := range node.Args {
				if strings.HasPrefix(strings.ToUpper(leadingLiteralWord(argument)), "BASH_FUNC_") {
					findings.add("%s passes forbidden BASH_FUNC_ environment assignment in command arguments", prefix)
					continue
				}
				name, _, _, assignment := environmentWordAssignment(argument)
				if assignment && strings.HasPrefix(strings.ToUpper(name), "BASH_FUNC_") {
					findings.add("%s assigns forbidden execution environment variable %s in command arguments", prefix, name)
				}
			}
			if len(node.Args) == 0 {
				return true
			}
			index := 0
			for index < len(node.Args) {
				wrapper, literal := literalWord(node.Args[index])
				if !literal {
					break
				}
				base := path.Base(wrapper)
				if base == "command" || base == "exec" {
					index++
					if index < len(node.Args) {
						if separator, ok := literalWord(node.Args[index]); ok && separator == "--" {
							index++
						}
					}
					continue
				}
				if base != "env" {
					break
				}
				index++
				for index < len(node.Args) {
					value, literal := literalWord(node.Args[index])
					if literal && value == "--" {
						index++
						continue
					}
					name, assignmentValue, assignmentLiteral, assignment := environmentWordAssignment(node.Args[index])
					if !assignment {
						break
					}
					if dangerousName(name) && !safeExecutionEnvironmentAssignment(name, assignmentValue, assignmentLiteral) {
						findings.add("%s assigns forbidden execution environment variable %s through env", prefix, name)
					}
					index++
				}
				// Continue if env itself wraps another reviewed wrapper.
			}
		case *syntax.Redirect:
			name, parameter := exactParameterWord(node.Word)
			if parameter {
				name = strings.ToUpper(name)
			}
			if literal, ok := literalWord(node.Word); ok {
				upper := strings.ToUpper(literal)
				if strings.Contains(upper, "GITHUB_ENV") {
					name = "GITHUB_ENV"
				} else if strings.Contains(upper, "GITHUB_PATH") {
					name = "GITHUB_PATH"
				}
			}
			syntax.Walk(node.Word, func(part syntax.Node) bool {
				if expansion, ok := part.(*syntax.ParamExp); ok && expansion.Param != nil {
					candidate := strings.ToUpper(expansion.Param.Value)
					if candidate == "GITHUB_ENV" || candidate == "GITHUB_PATH" {
						name = candidate
					}
				}
				return true
			})
			if name == "GITHUB_ENV" || name == "GITHUB_PATH" {
				findings.add("%s redirects to forbidden GitHub command file %s", prefix, name)
			}
		}
		return true
	})
}

func safeExecutionEnvironmentAssignment(name, value string, literal bool) bool {
	if name == "NODE_AUTH_TOKEN" {
		return value == "${{ github.token }}"
	}
	if !literal {
		return false
	}
	switch name {
	case "GOWORK":
		return value == "off"
	case "GIT_ASKPASS":
		return value == "/bin/false"
	case "GIT_CONFIG_GLOBAL":
		return value == "/dev/null"
	case "GIT_CONFIG_NOSYSTEM":
		return value == "1"
	case "GIT_TERMINAL_PROMPT":
		return value == "0"
	default:
		return false
	}
}

func environmentWordAssignment(word *syntax.Word) (name, value string, literal, assignment bool) {
	if word == nil || len(word.Parts) == 0 {
		return "", "", false, false
	}
	whole, literal := literalWord(word)
	encodedBashFunction := regexp.MustCompile(`(?i)^BASH_FUNC_[^=]*%%=`).MatchString(whole)
	if literal && (envAssignmentRE.MatchString(whole) || encodedBashFunction) {
		parts := strings.SplitN(whole, "=", 2)
		return parts[0], parts[1], true, true
	}
	prefix := leadingLiteralWord(word)
	separator := strings.IndexByte(prefix, '=')
	if separator <= 0 || !envAssignmentRE.MatchString(prefix) {
		return "", "", false, false
	}
	return prefix[:separator], "", false, true
}

func leadingLiteralWord(word *syntax.Word) string {
	if word == nil || len(word.Parts) == 0 {
		return ""
	}
	prefix := ""
	switch first := word.Parts[0].(type) {
	case *syntax.Lit:
		prefix = first.Value
	case *syntax.DblQuoted:
		for _, part := range first.Parts {
			literalPart, ok := part.(*syntax.Lit)
			if !ok {
				break
			}
			prefix += literalPart.Value
		}
	}
	return prefix
}

func inspectShell(prefix, workflowPath, contextSHA256, source string, file *syntax.File, pureGuard bool, repoRoot string, executables map[string]executableEntry, executableReferenced map[string]bool, commands map[string]commandEntry, commandReferenced map[string]bool, findings *findingSet) shellAnalysis {
	analysis := shellAnalysis{
		hasIntegrationTag: integrationRE.MatchString(source),
		hasProviderSDK:    containsProviderSDKMarker(source),
		namedLive:         namedLiveRE.MatchString(source),
	}
	if analysis.namedLive {
		analysis.providerAuthority = true
		findings.add("%s invokes forbidden named live test", prefix)
	}
	if !pureGuard {
		if match := providerAPIRE.FindStringSubmatch(source); match != nil {
			analysis.providerAuthority = true
			findings.add("%s executes forbidden fixed provider API %s", prefix, match[1])
		}
	}
	for _, stmt := range file.Stmts {
		inspectStatementGuards(prefix, stmt, findings)
		statementSHA256, err := statementDigest(stmt)
		if err != nil {
			findings.add("%s cannot canonicalize shell statement: %v", prefix, err)
			continue
		}
		hasAuthoritySubject := false
		syntax.Walk(stmt, func(node syntax.Node) bool {
			call, ok := node.(*syntax.CallExpr)
			if !ok {
				return true
			}
			if assignmentOnlyCall(call) {
				hasAuthoritySubject = true
				key := commandKey(workflowPath, "$assignment", statementSHA256, contextSHA256)
				if _, ok := commands[key]; !ok {
					findings.add("%s executes unreviewed standalone assignment (sha256:%s context:%s)", prefix, statementSHA256, contextSHA256)
				} else {
					commandReferenced[key] = true
				}
			}
			if len(call.Args) == 0 {
				return true
			}
			hasAuthoritySubject = true
			commandWord, commandWords, resolved := resolvedWorkflowCommand(call)
			command, literal := literalWord(commandWord)
			if commandWord == nil || !resolved || !literal || strings.Contains(command, "__GITHUB_EXPRESSION_") {
				findings.add("%s uses forbidden dynamic command execution", prefix)
				return true
			}
			base := normalizedCommandName(command)
			if map[string]bool{"doctl": true, "gcloud": true, "az": true, "aws": true}[base] {
				analysis.providerAuthority = true
				findings.add("%s executes forbidden provider authority: executable provider CLI %s", prefix, base)
			}
			argv := make([]string, 0, len(commandWords))
			for _, word := range commandWords {
				value, ok := literalWord(word)
				if !ok || strings.Contains(value, "__GITHUB_EXPRESSION_") {
					argv = append(argv, "__DYNAMIC_ARGUMENT__")
					continue
				}
				argv = append(argv, value)
			}
			if knownProviderCommand(base, argv) {
				findings.add("%s executes categorically forbidden command %s with argv %q", prefix, base, argv)
				return true
			}
			if strings.HasPrefix(command, "./") || strings.HasPrefix(command, "../") || filepath.IsAbs(command) {
				candidate := command
				if !filepath.IsAbs(candidate) {
					candidate = filepath.Join(repoRoot, candidate)
				}
				abs, err := filepath.Abs(candidate)
				if err != nil {
					findings.add("%s cannot resolve workflow executable path %s", prefix, command)
					return true
				}
				lexicalRel, err := filepath.Rel(repoRoot, abs)
				if err != nil || pathEscapes(lexicalRel) {
					findings.add("%s workflow executable path %s is outside repository", prefix, command)
					return true
				}
				scriptRel := filepath.ToSlash(lexicalRel)
				executableKey := workflowPath + "\x00" + scriptRel
				if _, ok := executables[executableKey]; !ok {
					findings.add("%s invokes unallowlisted executable script %s", prefix, command)
					return true
				}
				executableReferenced[executableKey] = true
				key := commandKey(workflowPath, base, statementSHA256, contextSHA256)
				if _, ok := commands[key]; !ok {
					findings.add("%s executes unreviewed exact statement containing local script %s (sha256:%s context:%s)", prefix, scriptRel, statementSHA256, contextSHA256)
				} else {
					commandReferenced[key] = true
				}
				return true
			}
			key := commandKey(workflowPath, base, statementSHA256, contextSHA256)
			if _, ok := commands[key]; !ok {
				findings.add("%s executes unreviewed exact statement containing %s (sha256:%s context:%s)", prefix, base, statementSHA256, contextSHA256)
			} else {
				commandReferenced[key] = true
			}
			return true
		})
		if !hasAuthoritySubject {
			key := commandKey(workflowPath, "$statement", statementSHA256, contextSHA256)
			if _, ok := commands[key]; !ok {
				findings.add("%s executes unreviewed exact shell statement (sha256:%s context:%s)", prefix, statementSHA256, contextSHA256)
			} else {
				commandReferenced[key] = true
			}
		}
	}
	return analysis
}

func absolutePathFromRoot(root, workflowPath string) (string, error) {
	if !filepath.IsAbs(workflowPath) {
		workflowPath = filepath.Join(root, workflowPath)
	}
	return filepath.Abs(workflowPath)
}

func normalizePath(repoRoot, resolvedRepoRoot, workflowPath string) (string, error) {
	abs, err := absolutePathFromRoot(repoRoot, workflowPath)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(repoRoot, abs)
	if err != nil {
		return "", err
	}
	if pathEscapes(rel) {
		return "", fmt.Errorf("workflow path %s is outside repository", workflowPath)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve workflow path %s: %w", workflowPath, err)
	}
	resolvedRel, err := filepath.Rel(resolvedRepoRoot, resolved)
	if err != nil {
		return "", err
	}
	if pathEscapes(resolvedRel) {
		return "", fmt.Errorf("workflow path %s resolves outside repository", workflowPath)
	}
	return filepath.ToSlash(rel), nil
}

func main() {
	var repoArg, scanArg, allowPath, executablePath, commandPath, actionPath, presencePath string
	bootstrapAuthority := false
	customPolicyInputs := false
	workflowArgs := []string{}
	args := os.Args[1:]
	for len(args) > 0 {
		switch args[0] {
		case "--repo":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "--repo requires a path")
				os.Exit(2)
			}
			repoArg = args[1]
			args = args[2:]
		case "--scan-root":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "--scan-root requires a path")
				os.Exit(2)
			}
			scanArg = args[1]
			args = args[2:]
		case "--allowlist":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "--allowlist requires a path")
				os.Exit(2)
			}
			allowPath = args[1]
			customPolicyInputs = true
			args = args[2:]
		case "--executable-allowlist":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "--executable-allowlist requires a path")
				os.Exit(2)
			}
			executablePath = args[1]
			customPolicyInputs = true
			args = args[2:]
		case "--command-allowlist":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "--command-allowlist requires a path")
				os.Exit(2)
			}
			commandPath = args[1]
			customPolicyInputs = true
			args = args[2:]
		case "--action-allowlist":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "--action-allowlist requires a path")
				os.Exit(2)
			}
			actionPath = args[1]
			customPolicyInputs = true
			args = args[2:]
		case "--presence-allowlist":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "--presence-allowlist requires a path")
				os.Exit(2)
			}
			presencePath = args[1]
			customPolicyInputs = true
			args = args[2:]
		case "--bootstrap-authority":
			bootstrapAuthority = true
			args = args[1:]
		case "--":
			workflowArgs = append(workflowArgs, args[1:]...)
			args = nil
		default:
			if strings.HasPrefix(args[0], "-") {
				fmt.Fprintf(os.Stderr, "unknown option: %s\n", args[0])
				os.Exit(2)
			}
			workflowArgs = append(workflowArgs, args[0])
			args = args[1:]
		}
	}
	if repoArg == "" {
		fmt.Fprintln(os.Stderr, "usage: policytool --repo TRUST_ROOT [--scan-root CANDIDATE_ROOT] [--bootstrap-authority] [--allowlist FILE] [--executable-allowlist FILE] [--command-allowlist FILE] [--action-allowlist FILE] [--presence-allowlist FILE] [WORKFLOW...]")
		os.Exit(2)
	}
	repoRoot, err := filepath.Abs(repoArg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve repository root: %v\n", err)
		os.Exit(2)
	}
	if scanArg == "" {
		scanArg = repoRoot
	}
	scanRoot, err := filepath.Abs(scanArg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	resolvedScanRoot, err := filepath.EvalSymlinks(scanRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve scan root: %v\n", err)
		os.Exit(2)
	}
	if bootstrapAuthority && customPolicyInputs {
		fmt.Fprintln(os.Stderr, "--bootstrap-authority cannot be combined with custom policy inputs")
		os.Exit(2)
	}
	if !customPolicyInputs {
		authorityFindings, authorityErr := validateRepositoryAuthority(repoRoot, scanRoot, bootstrapAuthority)
		if authorityErr != nil {
			fmt.Fprintf(os.Stderr, "validate repository authority: %v\n", authorityErr)
			os.Exit(1)
		}
		if len(authorityFindings) > 0 {
			for _, finding := range authorityFindings {
				fmt.Fprintln(os.Stderr, "policy:", finding)
			}
			os.Exit(1)
		}
	}
	if allowPath == "" {
		allowPath = filepath.Join(repoRoot, ".github", "public-workflow-secret-allowlist.json")
	}
	if executablePath == "" {
		executablePath = filepath.Join(repoRoot, ".github", "public-workflow-executable-allowlist.json")
	}
	if commandPath == "" {
		commandPath = filepath.Join(repoRoot, ".github", "public-workflow-command-allowlist.json")
	}
	if actionPath == "" {
		actionPath = filepath.Join(repoRoot, ".github", "public-workflow-action-allowlist.json")
	}
	if presencePath == "" {
		presencePath = filepath.Join(repoRoot, ".github", "public-workflow-presence-allowlist.json")
	}
	if len(workflowArgs) == 0 {
		for _, pattern := range []string{"*.yml", "*.yaml"} {
			matches, globErr := filepath.Glob(filepath.Join(scanRoot, ".github", "workflows", pattern))
			if globErr != nil {
				fmt.Fprintf(os.Stderr, "glob workflows: %v\n", globErr)
				os.Exit(1)
			}
			workflowArgs = append(workflowArgs, matches...)
		}
	}
	workflowContexts := make(map[string]string)
	for _, workflowPath := range workflowArgs {
		rel, pathErr := normalizePath(scanRoot, resolvedScanRoot, workflowPath)
		if pathErr != nil {
			continue
		}
		abs, pathErr := absolutePathFromRoot(scanRoot, workflowPath)
		if pathErr != nil {
			continue
		}
		data, readErr := os.ReadFile(abs)
		if readErr != nil {
			continue
		}
		var doc yaml.Node
		if yaml.Unmarshal(data, &doc) == nil && len(doc.Content) > 0 && doc.Content[0].Kind == yaml.MappingNode {
			workflowContexts[rel] = authorizationContextDigest(doc.Content[0], nil, nil)
		}
	}

	var allowlist []allowEntry
	if err := decodeJSONFile(allowPath, &allowlist); err != nil {
		fmt.Fprintf(os.Stderr, "read allowlist: %v\n", err)
		os.Exit(1)
	}

	findings := &findingSet{}
	var executableList []executableEntry
	if err := decodeJSONFile(executablePath, &executableList); err != nil {
		fmt.Fprintf(os.Stderr, "read executable allowlist: %v\n", err)
		os.Exit(1)
	}
	var commandList []commandEntry
	if err := decodeJSONFile(commandPath, &commandList); err != nil {
		fmt.Fprintf(os.Stderr, "read command allowlist: %v\n", err)
		os.Exit(1)
	}
	var actionList []actionEntry
	if err := decodeJSONFile(actionPath, &actionList); err != nil {
		fmt.Fprintf(os.Stderr, "read action allowlist: %v\n", err)
		os.Exit(1)
	}
	var groups []trustGroup
	if err := decodeJSONFile(presencePath, &groups); err != nil {
		fmt.Fprintf(os.Stderr, "read presence allowlist: %v\n", err)
		os.Exit(1)
	}
	presentTrustPaths := make(map[string]bool)
	for _, group := range groups {
		candidatePath := filepath.Join(scanRoot, filepath.FromSlash(group.Path))
		if _, pathErr := normalizePath(scanRoot, resolvedScanRoot, candidatePath); pathErr != nil {
			continue
		}
		if _, statErr := os.Lstat(candidatePath); statErr == nil {
			presentTrustPaths[group.Path] = true
		} else if !os.IsNotExist(statErr) {
			findings.add("inspect trust group path %s: %v", group.Path, statErr)
		}
	}
	selectedGroups, groupFindings := selectTrustGroups(groups, workflowContexts, presentTrustPaths)
	findings.items = append(findings.items, groupFindings...)
	if len(workflowArgs) == 0 && len(groups) == 0 {
		findings.add("no public workflow files found and no trusted absence is declared")
	}
	declaredGroups := make(map[string]bool)
	for _, group := range groups {
		if group.Presence == "present" {
			declaredGroups[group.Path+"\x00"+group.ContextSHA256+"\x00"+group.State] = true
		}
	}
	executables := make(map[string]executableEntry)
	seenExecutables := make(map[string]bool)
	executableReferenced := make(map[string]bool)
	for _, entry := range executableList {
		rawPath := strings.TrimSpace(entry.Path)
		slashPath := strings.ReplaceAll(rawPath, "\\", "/")
		if filepath.IsAbs(rawPath) || path.IsAbs(slashPath) || filepath.VolumeName(rawPath) != "" || slashPath == ".." || strings.HasPrefix(path.Clean(slashPath), "../") {
			findings.add("executable allowlist path %s escapes the repository", rawPath)
			continue
		}
		entry.Path = path.Clean(slashPath)
		entry.WorkflowPath = path.Clean(strings.ReplaceAll(strings.TrimSpace(entry.WorkflowPath), "\\", "/"))
		if strings.TrimSpace(entry.Rationale) == "" || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(entry.SHA256) ||
			!regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(entry.ContextSHA256) || (entry.State != "active" && entry.State != "staged") {
			findings.add("invalid executable allowlist entry %s", entry.Path)
			continue
		}
		if !declaredGroups[entry.WorkflowPath+"\x00"+entry.ContextSHA256+"\x00"+entry.State] {
			findings.add("executable entry has no matching present trust group for %s", entry.WorkflowPath)
		}
		key := entry.WorkflowPath + "\x00" + entry.Path + "\x00" + entry.ContextSHA256
		if seenExecutables[key] {
			findings.add("duplicate executable allowlist entry %s", entry.Path)
			continue
		}
		seenExecutables[key] = true
		if !selectedGroups[entry.WorkflowPath+"\x00"+entry.ContextSHA256] {
			continue
		}
		executableRoot := scanRoot
		resolvedExecutableRoot := resolvedScanRoot
		if trustedPolicyExecutable(entry) {
			executableRoot = repoRoot
			resolvedExecutableRoot = resolvedRepoRoot
		}
		abs := filepath.Join(executableRoot, filepath.FromSlash(entry.Path))
		rel, pathErr := normalizePath(executableRoot, resolvedExecutableRoot, abs)
		if pathErr != nil {
			findings.add("executable allowlist %v", pathErr)
			continue
		}
		data, readErr := os.ReadFile(abs)
		if readErr != nil {
			findings.add("read executable %s: %v", rel, readErr)
			continue
		}
		actual := fmt.Sprintf("%x", sha256.Sum256(data))
		if actual != entry.SHA256 {
			findings.add("executable hash mismatch for %s", entry.Path)
		}
		executables[entry.WorkflowPath+"\x00"+entry.Path] = entry
	}
	commands := make(map[string]commandEntry)
	seenCommands := make(map[string]bool)
	commandReferenced := make(map[string]bool)
	for _, entry := range commandList {
		rawPath := strings.TrimSpace(entry.Path)
		slashPath := strings.ReplaceAll(rawPath, "\\", "/")
		if filepath.IsAbs(rawPath) || path.IsAbs(slashPath) || filepath.VolumeName(rawPath) != "" {
			findings.add("command allowlist path %s must be repository-relative", rawPath)
			continue
		}
		entry.Path = path.Clean(slashPath)
		if entry.Path == ".." || strings.HasPrefix(entry.Path, "../") {
			findings.add("command allowlist path %s escapes the repository", rawPath)
			continue
		}
		entry.Command = strings.ToLower(strings.TrimSpace(entry.Command))
		if entry.Path == "." || !strings.HasPrefix(entry.Path, ".github/workflows/") ||
			(entry.State != "active" && entry.State != "staged") ||
			entry.Command == "" || entry.Command != path.Base(entry.Command) ||
			!regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(entry.StatementSHA256) ||
			!regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(entry.ContextSHA256) ||
			strings.TrimSpace(entry.Rationale) == "" {
			findings.add("invalid command allowlist entry for %s: exact workflow path, command, statementSHA256, contextSHA256, and rationale are required", entry.Path)
			continue
		}
		if !declaredGroups[entry.Path+"\x00"+entry.ContextSHA256+"\x00"+entry.State] {
			findings.add("command entry has no matching present trust group for %s", entry.Path)
		}
		if categoricallyUnallowlistableCommandName(entry.Command) {
			findings.add("provider-capable command %s is categorically unallowlistable in %s", entry.Command, entry.Path)
			continue
		}
		key := commandKey(entry.Path, entry.Command, entry.StatementSHA256, entry.ContextSHA256)
		if seenCommands[key] {
			findings.add("duplicate command allowlist entry %s sha256:%s in %s", entry.Command, entry.StatementSHA256, entry.Path)
			continue
		}
		seenCommands[key] = true
		if selectedGroups[entry.Path+"\x00"+entry.ContextSHA256] {
			commands[key] = entry
		}
	}
	actions := make(map[string]actionEntry)
	seenActions := make(map[string]bool)
	actionReferenced := make(map[string]bool)
	for _, entry := range actionList {
		rawPath := strings.TrimSpace(entry.Path)
		slashPath := strings.ReplaceAll(rawPath, "\\", "/")
		if filepath.IsAbs(rawPath) || path.IsAbs(slashPath) || filepath.VolumeName(rawPath) != "" {
			findings.add("action allowlist path %s must be repository-relative", rawPath)
			continue
		}
		entry.Path = path.Clean(slashPath)
		if entry.Path == ".." || strings.HasPrefix(entry.Path, "../") {
			findings.add("action allowlist path %s escapes the repository", rawPath)
			continue
		}
		entry.Uses = strings.TrimSpace(entry.Uses)
		validReference := immutableActionReference(entry.Uses)
		if entry.Path == "." || !strings.HasPrefix(entry.Path, ".github/workflows/") ||
			(entry.State != "active" && entry.State != "staged") ||
			!validReference || strings.Contains(entry.Uses, "${{") ||
			!regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(entry.NodeSHA256) ||
			!regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(entry.ContextSHA256) || strings.TrimSpace(entry.Rationale) == "" {
			findings.add("invalid action allowlist entry for %s: exact workflow path, immutable uses reference, nodeSHA256, contextSHA256, and rationale are required", entry.Path)
			continue
		}
		if !declaredGroups[entry.Path+"\x00"+entry.ContextSHA256+"\x00"+entry.State] {
			findings.add("action entry has no matching present trust group for %s", entry.Path)
		}
		if providerMarker(entry.Uses) || strings.Contains(strings.ToLower(entry.Uses), "digitalocean/") {
			findings.add("provider action %s is categorically unallowlistable in %s", entry.Uses, entry.Path)
			continue
		}
		key := actionKey(entry.Path, entry.Uses, entry.NodeSHA256, entry.ContextSHA256)
		if seenActions[key] {
			findings.add("duplicate action allowlist entry %s in %s", entry.Uses, entry.Path)
			continue
		}
		seenActions[key] = true
		if selectedGroups[entry.Path+"\x00"+entry.ContextSHA256] {
			actions[key] = entry
		}
	}
	allowed := make(map[string]allowEntry)
	seenAllowed := make(map[string]bool)
	for _, entry := range allowlist {
		rawPath := strings.TrimSpace(entry.Path)
		slashPath := strings.ReplaceAll(rawPath, "\\", "/")
		if filepath.IsAbs(rawPath) || path.IsAbs(slashPath) || filepath.VolumeName(rawPath) != "" {
			findings.add("allowlist path %s must be repository-relative", rawPath)
			continue
		}
		entry.Path = path.Clean(slashPath)
		if entry.Path == ".." || strings.HasPrefix(entry.Path, "../") {
			findings.add("allowlist path %s escapes the repository", rawPath)
			continue
		}
		entry.Secret = strings.ToUpper(strings.TrimSpace(entry.Secret))
		key := entry.Path + "\x00" + entry.Secret
		seenKey := key + "\x00" + entry.ContextSHA256
		if entry.Path == "." || strings.TrimSpace(entry.Secret) == "" ||
			!regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(entry.ContextSHA256) ||
			(entry.State != "active" && entry.State != "staged") || strings.TrimSpace(entry.Rationale) == "" {
			findings.add("invalid allowlist entry for %s: exact path, secret, and rationale are required", entry.Path)
		}
		if !declaredGroups[entry.Path+"\x00"+entry.ContextSHA256+"\x00"+entry.State] {
			findings.add("secret entry has no matching present trust group for %s", entry.Path)
		}
		if seenAllowed[seenKey] {
			findings.add("duplicate allowlist entry %s in %s", entry.Secret, entry.Path)
		}
		seenAllowed[seenKey] = true
		if knownCloudSecret(entry.Secret) {
			findings.add("known cloud secret %s is categorically unallowlistable in %s", entry.Secret, entry.Path)
		}
		if selectedGroups[entry.Path+"\x00"+entry.ContextSHA256] {
			allowed[key] = entry
		}
	}

	referenced := make(map[string]bool)
	for _, workflowPath := range workflowArgs {
		rel, err := normalizePath(scanRoot, resolvedScanRoot, workflowPath)
		if err != nil {
			findings.add("%v", err)
			continue
		}
		abs, err := absolutePathFromRoot(scanRoot, workflowPath)
		if err != nil {
			findings.add("resolve workflow path %s: %v", workflowPath, err)
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			findings.add("read workflow %s: %v", rel, err)
			continue
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(data, &doc); err != nil {
			findings.add("parse workflow %s: %v", rel, err)
			continue
		}
		structuralFindings := len(findings.items)
		validateYAMLStructure("workflow "+rel, &doc, findings)
		if len(findings.items) != structuralFindings {
			continue
		}
		if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
			findings.add("workflow %s must contain a YAML mapping", rel)
			continue
		}
		root := doc.Content[0]
		checkWorkingDirectory("workflow "+rel, root, findings)
		workflowPermissions := mappingValue(root, "permissions")
		if workflowPermissions != nil {
			checkPermissions("workflow "+rel, workflowPermissions, true, findings)
		}
		workflowShell := defaultsRunShell(root)
		checkShell("workflow "+rel, workflowShell, findings)
		envValues("workflow "+rel, mappingValue(root, "env"), false, findings)
		credentialVariables := make(map[string]bool)
		knownCredentialVariables(root, credentialVariables)
		for name := range credentialVariables {
			findings.add("workflow %s declares known cloud credential variable %s", rel, name)
		}
		globalSecrets := make(map[string]bool)
		for i := 0; i+1 < len(root.Content); i += 2 {
			if root.Content[i].Value == "jobs" {
				continue
			}
			validateCredentialSelectors("workflow "+rel, root.Content[i+1], findings)
			for secret := range secretReferences(root.Content[i+1]) {
				globalSecrets[secret] = true
			}
		}
		validateSecretReferences(rel, "workflow "+rel, globalSecrets, referenced, findings)
		manual := triggerPresent(root, "workflow_dispatch")
		scheduled := triggerPresent(root, "schedule")
		jobs := mappingValue(root, "jobs")
		if jobs == nil || jobs.Kind != yaml.MappingNode {
			findings.add("workflow %s must declare jobs", rel)
			continue
		}
		for i := 0; i+1 < len(jobs.Content); i += 2 {
			jobName := jobs.Content[i].Value
			job := jobs.Content[i+1]
			prefix := rel + " job " + jobName

			checkWorkingDirectory(prefix, job, findings)
			checkJobRuntime(prefix, job, findings)
			checkJobRunnerSelector(prefix, job, findings)

			jobPermissions := mappingValue(job, "permissions")
			if jobPermissions != nil {
				checkPermissions(prefix, jobPermissions, false, findings)
			} else if workflowPermissions == nil {
				checkPermissions(prefix, nil, false, findings)
			}
			jobShell := defaultsRunShell(job)
			checkShell(prefix, jobShell, findings)
			if jobShell == nil {
				jobShell = workflowShell
			}
			envValues(prefix, mappingValue(job, "env"), false, findings)

			jobSecrets := make(map[string]bool)
			for secret := range globalSecrets {
				jobSecrets[secret] = true
			}
			localSecrets := secretReferences(job)
			validateInheritedSecrets(prefix, mappingValue(job, "secrets"), findings)
			validateCredentialSelectors(prefix, job, findings)
			validateSecretReferences(rel, prefix, localSecrets, referenced, findings)
			for secret := range localSecrets {
				jobSecrets[secret] = true
			}

			providerAuthority := false
			hasIntegrationTag := false
			hasProviderSDK := false
			namedLive := false
			if uses := mappingValue(job, "uses"); uses != nil && uses.Kind == yaml.ScalarNode {
				nodeSHA256 := actionNodeDigest(job)
				contextSHA256 := authorizationContextDigest(root, job, nil)
				if strings.Contains(uses.Value, "${{") {
					providerAuthority = true
					findings.add("%s uses forbidden dynamic reusable workflow %s", prefix, uses.Value)
				} else if key, ok := matchAction(rel, uses.Value, nodeSHA256, contextSHA256, actions); !ok {
					providerAuthority = true
					findings.add("%s uses unreviewed exact reusable workflow %s (node sha256:%s context:%s)", prefix, uses.Value, nodeSHA256, contextSHA256)
				} else {
					actionReferenced[key] = true
				}
			}
			steps := mappingValue(job, "steps")
			if steps != nil && steps.Kind == yaml.SequenceNode {
				for _, step := range steps.Content {
					checkWorkingDirectory(prefix, step, findings)
					if uses := mappingValue(step, "uses"); uses != nil && uses.Kind == yaml.ScalarNode {
						nodeSHA256 := actionNodeDigest(step)
						contextSHA256 := authorizationContextDigest(root, job, step)
						if strings.Contains(uses.Value, "${{") {
							providerAuthority = true
							findings.add("%s uses forbidden dynamic action %s", prefix, uses.Value)
						} else if key, ok := matchAction(rel, uses.Value, nodeSHA256, contextSHA256, actions); !ok {
							providerAuthority = true
							findings.add("%s uses unreviewed exact action %s (node sha256:%s context:%s)", prefix, uses.Value, nodeSHA256, contextSHA256)
						} else {
							actionReferenced[key] = true
						}
					}
					denyPatterns := envValues(prefix, mappingValue(step, "env"), true, findings)
					run := mappingValue(step, "run")
					if run == nil || run.Kind != yaml.ScalarNode {
						if len(denyPatterns) > 0 {
							findings.add("%s uses provider deny pattern outside a pure rejection guard", prefix)
						}
						continue
					}
					shell := mappingValue(step, "shell")
					if shell == nil {
						shell = jobShell
					}
					if shell == nil {
						findings.add("%s does not declare an explicit Bash shell", prefix)
					} else {
						checkShell(prefix, shell, findings)
					}
					normalized, err := normalizeGithubExpressions(run.Value)
					if err != nil {
						findings.add("%s shell parse failed: %v", prefix, err)
						continue
					}
					file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(normalized), rel+":"+jobName)
					if err != nil {
						findings.add("%s shell parse failed: %v", prefix, err)
						continue
					}
					pureGuard := pureRejectionGuard(file, denyPatterns)
					if len(denyPatterns) > 0 && !pureGuard {
						findings.add("%s uses provider deny pattern outside a pure rejection guard", prefix)
					}
					contextSHA256 := authorizationContextDigest(root, job, step)
					analysis := inspectShell(prefix, rel, contextSHA256, normalized, file, pureGuard, scanRoot, executables, executableReferenced, commands, commandReferenced, findings)
					providerAuthority = providerAuthority || analysis.providerAuthority
					hasIntegrationTag = hasIntegrationTag || analysis.hasIntegrationTag
					hasProviderSDK = hasProviderSDK || analysis.hasProviderSDK
					namedLive = namedLive || analysis.namedLive
				}
			}
			for secret := range jobSecrets {
				if knownCloudSecret(secret) {
					providerAuthority = true
				}
			}
			if providerAuthority {
				for secret := range jobSecrets {
					findings.add("%s combines provider authority with secret %s", prefix, secret)
				}
				if hasIntegrationTag {
					findings.add("%s combines integration tag with provider authority", prefix)
				}
				if manual {
					findings.add("%s is a forbidden manual provider-authority job", prefix)
				}
				if scheduled {
					findings.add("%s is a forbidden scheduled provider-authority job", prefix)
				}
			}
			if hasProviderSDK && (providerAuthority || namedLive) {
				findings.add("%s combines provider SDK marker with provider authority", prefix)
			}
		}
	}

	for key, entry := range allowed {
		if !referenced[key] {
			findings.add("stale allowlist entry %s in %s", entry.Secret, entry.Path)
		}
	}
	for key, entry := range executables {
		if !executableReferenced[key] {
			findings.add("stale executable allowlist entry %s in %s", entry.Path, entry.WorkflowPath)
		}
	}
	for key, entry := range commands {
		if !commandReferenced[key] {
			findings.add("stale command allowlist entry %s sha256:%s in %s", entry.Command, entry.StatementSHA256, entry.Path)
		}
	}
	for key, entry := range actions {
		if !actionReferenced[key] {
			findings.add("stale action allowlist entry %s in %s", entry.Uses, entry.Path)
		}
	}

	if len(findings.items) > 0 {
		sort.Strings(findings.items)
		for _, finding := range findings.items {
			fmt.Fprintln(os.Stderr, "policy:", finding)
		}
		os.Exit(1)
	}
	fmt.Printf("public workflow policy passed for %d workflow(s)\n", len(workflowArgs))
}
