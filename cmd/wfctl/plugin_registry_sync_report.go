package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const registrySyncOperationSchema = "registry-sync-operation.v1"

type registrySyncOperation struct {
	Schema          string                       `json:"schema"`
	Plugin          string                       `json:"plugin"`
	Repository      string                       `json:"repository"`
	Source          string                       `json:"source"`
	OldVersion      string                       `json:"old_version"`
	NewVersion      string                       `json:"new_version"`
	Tag             string                       `json:"tag"`
	TagObjects      []registrySyncGitObject      `json:"tag_objects"`
	SourceSHA       string                       `json:"source_sha"`
	SourceDigest    string                       `json:"source_digest"`
	Release         registrySyncReportRelease    `json:"release"`
	Assets          []registrySyncReportAsset    `json:"assets"`
	Checksums       []registrySyncReportChecksum `json:"checksums"`
	ChecksumsDigest string                       `json:"checksums_digest"`
	ChangedPaths    []string                     `json:"changed_paths"`
	BaseTree        string                       `json:"base_tree"`
	ResultTree      string                       `json:"result_tree"`
	DiffDigest      string                       `json:"diff_digest"`
}

type registrySyncReportRelease struct {
	ID              string `json:"id"`
	TagName         string `json:"tag_name"`
	TargetCommitish string `json:"target_commitish"`
	Immutable       bool   `json:"immutable"`
}

type registrySyncReportAsset struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Size   string `json:"size"`
	URL    string `json:"url"`
	Digest string `json:"digest"`
}

type registrySyncReportChecksum struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

func encodeRegistrySyncOperation(operation registrySyncOperation) ([]byte, error) {
	data, err := json.Marshal(operation)
	if err != nil {
		return nil, err
	}
	var value any
	if err := registrySyncDecodeJSON(data, &value); err != nil {
		return nil, err
	}
	return workflowCanonicalJSONV1(value)
}

func decodeRegistrySyncOperation(data []byte) (registrySyncOperation, error) {
	var operation registrySyncOperation
	if len(data) > 1<<20 || !utf8.Valid(data) {
		return operation, fmt.Errorf("invalid report size or UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(&operation); err != nil {
		return operation, fmt.Errorf("invalid registry operation report: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return operation, fmt.Errorf("report contains trailing data")
	}
	if err := validateRegistrySyncOperation(operation); err != nil {
		return operation, err
	}
	canonical, err := encodeRegistrySyncOperation(operation)
	if err != nil || !bytes.Equal(data, canonical) {
		return operation, fmt.Errorf("report must contain exact number-free canonical JSON")
	}
	return operation, nil
}

func validateRegistrySyncOperation(o registrySyncOperation) error {
	invalid := func() error { return fmt.Errorf("invalid %s report bindings", registrySyncOperationSchema) }
	if o.Schema != registrySyncOperationSchema || !validRegistryReportPlugin(o.Plugin) {
		return invalid()
	}
	repo, err := registrySyncRepository(o.Source)
	if err != nil || repo != o.Repository {
		return invalid()
	}
	oldTag, oldErr := registrySyncStableTag(o.OldVersion)
	newTag, newErr := registrySyncStableTag(o.NewVersion)
	if oldErr != nil || newErr != nil || oldTag != "v"+o.OldVersion || newTag != "v"+o.NewVersion || o.Tag != newTag || o.Release.TagName != o.Tag || !o.Release.Immutable || o.Release.TargetCommitish == "" || !registryReportDecimal(o.Release.ID, false) {
		return invalid()
	}
	if !registrySyncValidGitSHA(o.SourceSHA) || !registrySyncValidGitSHA(o.BaseTree) || !registrySyncValidGitSHA(o.ResultTree) || !registryReportDigest(o.SourceDigest) || !registryReportDigest(o.ChecksumsDigest) || !registryReportDigest(o.DiffDigest) || len(o.TagObjects) == 0 || len(o.TagObjects) > 9 {
		return invalid()
	}
	for i, object := range o.TagObjects {
		wantType := "tag"
		if i == len(o.TagObjects)-1 {
			wantType = "commit"
		}
		if object.Type != wantType || !registrySyncValidGitSHA(object.SHA) {
			return invalid()
		}
	}
	if o.TagObjects[len(o.TagObjects)-1].SHA != o.SourceSHA || len(o.Assets) == 0 || len(o.Checksums) == 0 || o.ChangedPaths == nil {
		return invalid()
	}
	if len(o.ChangedPaths) > 1 || (len(o.ChangedPaths) == 1 && o.ChangedPaths[0] != "plugins/"+o.Plugin+"/manifest.json") {
		return invalid()
	}
	if len(o.ChangedPaths) == 0 {
		if o.BaseTree != o.ResultTree || o.DiffDigest != "sha256:"+registrySyncSHA256(nil) {
			return invalid()
		}
	} else if o.BaseTree == o.ResultTree {
		return invalid()
	}
	ids := map[string]bool{}
	checksumAsset := false
	for i, asset := range o.Assets {
		parsedURL, err := url.Parse(asset.URL)
		if !registryReportDecimal(asset.ID, false) || !registryReportDecimal(asset.Size, true) || ids[asset.ID] || !validRegistryReportPlugin(asset.Name) || (i > 0 && o.Assets[i-1].Name >= asset.Name) || err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") || (asset.Digest != "" && !registryReportDigest(asset.Digest)) {
			return invalid()
		}
		ids[asset.ID] = true
		checksumAsset = checksumAsset || asset.Name == "checksums.txt"
	}
	checksums := map[string]string{}
	for i, checksum := range o.Checksums {
		if !validRegistryReportPlugin(checksum.Name) || !registryReportDigest("sha256:"+checksum.SHA256) || (i > 0 && o.Checksums[i-1].Name >= checksum.Name) {
			return invalid()
		}
		checksums[checksum.Name] = checksum.SHA256
	}
	platforms := 0
	for _, asset := range o.Assets {
		if _, _, ok := releaseAssetPlatform(asset.Name); ok {
			if checksums[asset.Name] == "" || (asset.Digest != "" && asset.Digest != "sha256:"+checksums[asset.Name]) {
				return invalid()
			}
			platforms++
		}
	}
	if !checksumAsset || platforms == 0 {
		return invalid()
	}
	return nil
}

func validRegistryReportPlugin(value string) bool {
	return value != "" && value != "." && value != ".." && filepath.IsLocal(value) && !strings.ContainsAny(value, `/\\`)
}

func registryReportDecimal(value string, allowZero bool) bool {
	number, err := strconv.ParseUint(value, 10, 64)
	return err == nil && (number != 0 || allowZero) && strconv.FormatUint(number, 10) == value
}

func registryReportDigest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") {
		return false
	}
	normalized, err := NormalizeSHA256Hex(strings.TrimPrefix(value, "sha256:"))
	return err == nil && value == "sha256:"+normalized
}

type registrySyncReportContext struct {
	root        string
	path        string
	reportRel   string
	manifestRel string
	temp        string
	index       string
	head        string
	baseTree    string
	previous    []byte
	mode        os.FileMode
}

func prepareRegistrySyncReport(registryDir, plugin, reportPath string) (*registrySyncReportContext, error) {
	if !validRegistryReportPlugin(plugin) || (!filepath.IsAbs(reportPath) && !filepath.IsLocal(reportPath)) {
		return nil, fmt.Errorf("invalid plugin or report path")
	}
	root, err := filepath.Abs(registryDir)
	if err != nil {
		return nil, err
	}
	originalRoot := root
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	path, err := filepath.Abs(reportPath)
	if err != nil {
		return nil, err
	}
	if relative, err := filepath.Rel(originalRoot, path); err == nil && filepath.IsLocal(relative) {
		path = filepath.Join(root, relative)
	} else {
		parent, tail := filepath.Dir(path), filepath.Base(path)
		for {
			if _, err := os.Lstat(parent); err == nil {
				break
			} else if !os.IsNotExist(err) {
				return nil, err
			}
			tail = filepath.Join(filepath.Base(parent), tail)
			parent = filepath.Dir(parent)
		}
		resolved, err := filepath.EvalSymlinks(parent)
		if err != nil {
			return nil, err
		}
		path = filepath.Join(resolved, tail)
	}
	c := &registrySyncReportContext{root: root, path: path, manifestRel: "plugins/" + plugin + "/manifest.json", mode: 0o644}
	if relative, err := filepath.Rel(root, path); err == nil && filepath.IsLocal(relative) {
		c.reportRel = filepath.ToSlash(relative)
		if c.reportRel == "." || c.reportRel == "README.md" || c.reportRel == ".git" || strings.HasPrefix(c.reportRel, ".git/") || strings.HasPrefix(c.reportRel, "plugins/") || strings.HasPrefix(c.reportRel, "v1/") {
			return nil, fmt.Errorf("report path overlaps governed registry paths")
		}
		current := root
		for component := range strings.SplitSeq(relative, string(filepath.Separator)) {
			current = filepath.Join(current, component)
			if info, err := os.Lstat(current); err == nil && info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("report path contains a symlink")
			} else if err != nil && !os.IsNotExist(err) {
				return nil, err
			}
		}
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return nil, fmt.Errorf("report must be a regular file of at most 1 MiB")
		}
		c.mode = info.Mode().Perm()
		c.previous, err = os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if _, err := decodeRegistrySyncOperation(c.previous); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	top, err := registrySyncGit(root, "", nil, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(string(top)) != root {
		return nil, fmt.Errorf("registry-dir must be a committed Git checkout root: %v", err)
	}
	head, err := registrySyncGit(root, "", nil, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	c.head = strings.TrimSpace(string(head))
	if _, err := registrySyncGit(root, "", nil, "cat-file", "-e", "HEAD:"+c.manifestRel); err != nil {
		return nil, fmt.Errorf("plugin manifest must exist in the committed registry: %w", err)
	}
	if err := c.checkPaths(); err != nil {
		return nil, err
	}
	c.temp, err = os.MkdirTemp("", "wfctl-registry-report-*")
	if err != nil {
		return nil, err
	}
	c.index = filepath.Join(c.temp, "index")
	c.baseTree, err = c.snapshotTree()
	if err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

func (c *registrySyncReportContext) close() {
	_ = os.RemoveAll(c.temp)
}

func registrySyncGit(root, index string, input []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root, "--literal-pathspecs"}, args...)...) // #nosec G204 -- internal Git operations use validated registry paths, never shell commands.
	cmd.Env = slices.DeleteFunc(os.Environ(), func(entry string) bool {
		name, _, _ := strings.Cut(entry, "=")
		return name == "GIT_INDEX_FILE" || name == "GIT_DIR" || name == "GIT_WORK_TREE"
	})
	cmd.Env = append(cmd.Env, "GIT_OPTIONAL_LOCKS=0")
	if index != "" {
		cmd.Env = append(cmd.Env, "GIT_INDEX_FILE="+index)
	}
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("registry Git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func (c *registrySyncReportContext) checkPaths() error {
	for _, args := range [][]string{
		{"diff", "--name-only", "-z", "--no-ext-diff", "HEAD", "--"},
		{"ls-files", "--others", "--exclude-standard", "-z"},
	} {
		out, err := registrySyncGit(c.root, "", nil, args...)
		if err != nil {
			return err
		}
		for path := range strings.SplitSeq(string(out), "\x00") {
			if path != "" && path != c.manifestRel && path != c.reportRel {
				return fmt.Errorf("extra generated or modified registry path: %s", path)
			}
		}
	}
	return nil
}

func (c *registrySyncReportContext) snapshotTree() (string, error) {
	if _, err := registrySyncGit(c.root, c.index, nil, "read-tree", "HEAD"); err != nil {
		return "", err
	}
	if c.reportRel != "" {
		if _, err := registrySyncGit(c.root, c.index, nil, "update-index", "--force-remove", "--", c.reportRel); err != nil {
			return "", err
		}
	}
	if _, err := registrySyncGit(c.root, c.index, nil, "add", "-u", "--", "."); err != nil {
		return "", err
	}
	tree, err := registrySyncGit(c.root, c.index, nil, "write-tree")
	return strings.TrimSpace(string(tree)), err
}

func (c *registrySyncReportContext) write(plugin, source, repo, oldVersion, newVersion string, snapshot *registrySyncReleaseSnapshot, manifest []byte) error {
	if err := c.checkPaths(); err != nil {
		return err
	}
	head, err := registrySyncGit(c.root, "", nil, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(string(head)) != c.head {
		return fmt.Errorf("registry HEAD changed during sync")
	}
	currentTree, err := c.snapshotTree()
	if err != nil {
		return err
	}
	if currentTree != c.baseTree {
		return fmt.Errorf("registry tree changed during sync")
	}
	mode, err := registrySyncGit(c.root, c.index, nil, "ls-files", "--stage", "--", c.manifestRel)
	if err != nil {
		return err
	}
	gitMode, _, ok := strings.Cut(string(mode), " ")
	if !ok || (gitMode != "100644" && gitMode != "100755") {
		return fmt.Errorf("registry manifest must be a tracked regular file")
	}
	blob, err := registrySyncGit(c.root, c.index, manifest, "hash-object", "-w", "--path="+c.manifestRel, "--stdin")
	if err != nil {
		return err
	}
	if _, err := registrySyncGit(c.root, c.index, nil, "update-index", "--cacheinfo", gitMode+","+strings.TrimSpace(string(blob))+","+c.manifestRel); err != nil {
		return err
	}
	result, err := registrySyncGit(c.root, c.index, nil, "write-tree")
	if err != nil {
		return err
	}
	resultTree := strings.TrimSpace(string(result))
	pathsBytes, err := registrySyncGit(c.root, c.index, nil, "diff-tree", "--no-commit-id", "-r", "--name-only", "-z", "--no-renames", c.baseTree, resultTree, "--")
	if err != nil {
		return err
	}
	paths := []string{}
	for path := range strings.SplitSeq(string(pathsBytes), "\x00") {
		if path != "" {
			paths = append(paths, path)
		}
	}
	diff, err := registrySyncGit(c.root, c.index, nil, "diff-tree", "--no-commit-id", "-r", "-p", "--binary", "--full-index", "--no-renames", "--no-ext-diff", "--no-textconv", "--no-color", "--src-prefix=a/", "--dst-prefix=b/", c.baseTree, resultTree, "--")
	if err != nil {
		return err
	}
	operation := registrySyncOperation{
		Schema: registrySyncOperationSchema, Plugin: plugin, Repository: repo, Source: source,
		OldVersion: oldVersion, NewVersion: newVersion, Tag: snapshot.Release.TagName,
		TagObjects: snapshot.TagObjects, SourceSHA: snapshot.SourceSHA, SourceDigest: "sha256:" + registrySyncSHA256(snapshot.PluginJSON),
		Release: registrySyncReportRelease{ID: strconv.FormatInt(snapshot.Release.ID, 10), TagName: snapshot.Release.TagName, TargetCommitish: snapshot.Release.TargetCommitish, Immutable: snapshot.Release.Immutable},
		Assets:  []registrySyncReportAsset{}, Checksums: []registrySyncReportChecksum{}, ChecksumsDigest: "sha256:" + snapshot.ChecksumsSHA256,
		ChangedPaths: paths, BaseTree: c.baseTree, ResultTree: resultTree, DiffDigest: "sha256:" + registrySyncSHA256(diff),
	}
	for _, asset := range snapshot.Release.Assets {
		operation.Assets = append(operation.Assets, registrySyncReportAsset{ID: strconv.FormatInt(asset.ID, 10), Name: asset.Name, Size: strconv.FormatInt(asset.Size, 10), URL: asset.BrowserDownloadURL, Digest: asset.Digest})
	}
	names := make([]string, 0, len(snapshot.Checksums))
	for name := range snapshot.Checksums {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		operation.Checksums = append(operation.Checksums, registrySyncReportChecksum{Name: name, SHA256: snapshot.Checksums[name]})
	}
	if err := validateRegistrySyncOperation(operation); err != nil {
		return err
	}
	data, err := encodeRegistrySyncOperation(operation)
	if err != nil {
		return err
	}
	current, err := os.ReadFile(c.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if !bytes.Equal(current, c.previous) {
		return fmt.Errorf("operation report changed during sync")
	}
	if bytes.Equal(current, data) {
		return nil
	}
	return atomicWriteFile(c.path, data, c.mode)
}
