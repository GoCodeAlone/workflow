package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	goplugin "github.com/GoCodeAlone/go-plugin"
	engineplugin "github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/plugin/external"
	"github.com/hashicorp/go-hclog"
	"golang.org/x/mod/modfile"
)

func runPluginConformance(args []string) error {
	fs := flag.NewFlagSet("plugin conformance", flag.ContinueOnError)
	mode := fs.String("mode", PluginCompatibilityModeTypedIaC, "Conformance mode (typed-iac)")
	artifact := fs.String("artifact", "", "Release artifact tar.gz to test")
	engineVersion := fs.String("engine-version", "", "Workflow engine version for evidence metadata")
	format := fs.String("format", "text", "Output format: text or json")
	output := fs.String("output", "", "Write JSON evidence to this path")
	timeout := fs.Duration("timeout", 30*time.Second, "Plugin launch/check timeout")
	fs.Usage = func() {
		printPluginConformanceUsage(fs.Output(), fs)
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *mode != PluginCompatibilityModeTypedIaC {
		return fmt.Errorf("unsupported conformance mode %q", *mode)
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("--format must be text or json")
	}
	if *artifact != "" && fs.NArg() > 0 {
		return fmt.Errorf("specify exactly one of <plugin-dir> or --artifact")
	}
	if *artifact == "" && fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("specify exactly one of <plugin-dir> or --artifact")
	}
	if *engineVersion == "" {
		*engineVersion = resolveConformanceEngineVersion()
	} else if strings.EqualFold(*engineVersion, "local") {
		*engineVersion = "v0.0.0"
	}

	source := ""
	if fs.NArg() == 1 {
		source = fs.Arg(0)
	}
	ev, err := runPluginConformanceCheck(pluginConformanceOptions{
		Mode:          *mode,
		SourceDir:     source,
		ArtifactPath:  *artifact,
		EngineVersion: *engineVersion,
		Timeout:       *timeout,
	})
	if err != nil && ev.Plugin == "" {
		return err
	}

	if *output != "" {
		data, err := json.MarshalIndent(ev, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(*output, append(data, '\n'), 0o600); err != nil {
			return fmt.Errorf("write evidence: %w", err)
		}
	}
	switch *format {
	case "json":
		if *output == "" {
			data, err := json.MarshalIndent(ev, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
		}
	case "text":
		fmt.Printf("%s %s %s %s/%s\n", ev.Status, ev.Plugin, ev.Version, ev.OS, ev.Arch)
	}
	if err != nil {
		return err
	}
	return nil
}

func printPluginConformanceUsage(w io.Writer, fs *flag.FlagSet) {
	fmt.Fprintf(w, "Usage: wfctl plugin conformance [options] <plugin-dir>\n       wfctl plugin conformance --artifact <tar.gz> [options]\n\nRun executable plugin/host conformance checks. This executes plugin code; run only on trusted local sources or CI artifacts.\n\nFlags: --artifact --mode --engine-version --format --output --timeout\n\nOptions:\n")
	fs.PrintDefaults()
}

func resolveConformanceEngineVersion() string {
	if env := strings.TrimSpace(os.Getenv("WFCTL_ENGINE_VERSION")); env != "" {
		return env
	}
	if version := buildVersion(); version != "" {
		if _, err := CanonicalEngineVersion(version); err == nil {
			return version
		}
	}
	return "v0.0.0"
}

type pluginConformanceOptions struct {
	Mode          string
	SourceDir     string
	ArtifactPath  string
	EngineVersion string
	Timeout       time.Duration
}

func runPluginConformanceCheck(opts pluginConformanceOptions) (PluginCompatibilityEvidence, error) {
	tmp, err := os.MkdirTemp("", "wfctl-plugin-conformance-*")
	if err != nil {
		return PluginCompatibilityEvidence{}, err
	}
	defer os.RemoveAll(tmp) //nolint:errcheck

	sourceDir := filepath.Join(tmp, "source")
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		return PluginCompatibilityEvidence{}, err
	}
	archiveSHA := ""
	if opts.ArtifactPath != "" {
		sha, err := hashFileSHA256(opts.ArtifactPath)
		if err != nil {
			return PluginCompatibilityEvidence{}, fmt.Errorf("hash artifact: %w", err)
		}
		archiveSHA = sha
		data, err := os.ReadFile(opts.ArtifactPath)
		if err != nil {
			return PluginCompatibilityEvidence{}, fmt.Errorf("read artifact: %w", err)
		}
		if err := extractTarGz(data, sourceDir); err != nil {
			return PluginCompatibilityEvidence{}, fmt.Errorf("extract artifact: %w", err)
		}
	} else {
		if err := copyConformanceSourceDir(opts.SourceDir, sourceDir); err != nil {
			return PluginCompatibilityEvidence{}, fmt.Errorf("stage plugin dir: %w", err)
		}
		if err := absolutizeStagedGoModReplaces(sourceDir, opts.SourceDir); err != nil {
			return PluginCompatibilityEvidence{}, err
		}
	}
	if err := removeConformanceSensitiveFiles(sourceDir); err != nil {
		return PluginCompatibilityEvidence{}, err
	}

	manifest, err := engineplugin.LoadManifest(filepath.Join(sourceDir, "plugin.json"))
	if err != nil {
		return PluginCompatibilityEvidence{}, err
	}
	if err := manifest.Validate(); err != nil {
		return PluginCompatibilityEvidence{}, err
	}
	installName := normalizePluginName(manifest.Name)
	installDir := filepath.Join(tmp, "plugins", installName)
	if err := os.MkdirAll(installDir, 0o750); err != nil {
		return PluginCompatibilityEvidence{}, err
	}
	if err := copyFile(filepath.Join(sourceDir, "plugin.json"), filepath.Join(installDir, "plugin.json"), 0o600); err != nil {
		return PluginCompatibilityEvidence{}, err
	}
	binaryPath := filepath.Join(installDir, installName)
	if info, statErr := os.Stat(filepath.Join(sourceDir, installName)); statErr == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
		if err := copyFile(filepath.Join(sourceDir, installName), binaryPath, info.Mode()); err != nil {
			return PluginCompatibilityEvidence{}, err
		}
	} else {
		cmd := exec.Command("go", "build", "-mod=mod", "-o", binaryPath, ".") //nolint:gosec // command args are fixed; dir is staged source.
		cmd.Dir = sourceDir
		cmd.Env = append(os.Environ(), "GOWORK=off")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return PluginCompatibilityEvidence{}, fmt.Errorf("build plugin: %w: %s", err, string(out))
		}
	}

	binarySHA, err := hashFileSHA256(binaryPath)
	if err != nil {
		return PluginCompatibilityEvidence{}, err
	}
	manifestSHA, err := hashFileSHA256(filepath.Join(installDir, "plugin.json"))
	if err != nil {
		return PluginCompatibilityEvidence{}, err
	}

	stdout, stderr, err := checkTypedIaCPlugin(opts.Timeout, filepath.Join(tmp, "plugins"), installName)
	ev := PluginCompatibilityEvidence{
		Plugin:               manifest.Name,
		Version:              manifest.Version,
		EngineVersion:        opts.EngineVersion,
		WfctlVersion:         opts.EngineVersion,
		Mode:                 opts.Mode,
		Status:               PluginCompatibilityStatusPass,
		OS:                   runtime.GOOS,
		Arch:                 runtime.GOARCH,
		ArchiveSHA256:        archiveSHA,
		BinarySHA256:         binarySHA,
		PluginManifestSHA256: manifestSHA,
		GeneratedBy:          "wfctl plugin conformance",
		StdoutTail:           stdout,
		StderrTail:           stderr,
	}
	if err != nil {
		ev.Status = PluginCompatibilityStatusFail
		if normalized, normErr := ValidateCompatibilityEvidence(ev); normErr == nil {
			ev = normalized
		}
		return ev, err
	}
	ev, err = ValidateCompatibilityEvidence(ev)
	if err != nil {
		return ev, err
	}
	return ev, nil
}

func checkTypedIaCPlugin(timeout time.Duration, pluginsDir, name string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	pluginDir := filepath.Join(pluginsDir, name)
	binaryPath, err := filepath.Abs(filepath.Join(pluginDir, name))
	if err != nil {
		return "", "", err
	}
	var stdout, stderr tailBuffer
	cmd := exec.CommandContext(ctx, binaryPath) //nolint:gosec // staged plugin binary path.
	cmd.Dir = pluginDir
	cmd.Env = conformancePluginEnv()
	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig:  external.Handshake,
		Plugins:          goplugin.PluginSet{"plugin": &external.GRPCPlugin{}},
		Cmd:              cmd,
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		Logger:           hclog.NewNullLogger(),
	})
	defer client.Kill()

	rpcClient, err := client.Client()
	if err != nil {
		if ctx.Err() != nil {
			return stdout.String(), stderr.String(), fmt.Errorf("timeout waiting for plugin handshake")
		}
		return stdout.String(), stderr.String(), err
	}
	raw, err := rpcClient.Dispense("plugin")
	if err != nil {
		return stdout.String(), stderr.String(), err
	}
	pluginClient, ok := raw.(*external.PluginClient)
	if !ok {
		return stdout.String(), stderr.String(), fmt.Errorf("dispensed object is %T, want *external.PluginClient", raw)
	}
	adapter, err := external.NewExternalPluginAdapter(name, pluginClient)
	if err != nil {
		return stdout.String(), stderr.String(), err
	}
	if regErr := adapter.ContractRegistryError(); regErr != nil {
		return stdout.String(), stderr.String(), regErr
	}
	if err := AssertIaCPluginAdvertisesRequiredService(name, "", adapter.ContractRegistry()); err != nil {
		return stdout.String(), stderr.String(), err
	}
	registered := registeredIaCServices(adapter.ContractRegistry())
	typed := newTypedIaCAdapter(adapter.Conn(), registered)
	_ = typed.SupportedCanonicalKeys()
	return stdout.String(), stderr.String(), nil
}

func conformancePluginEnv() []string {
	env := make([]string, 0, 4)
	for _, key := range []string{"PATH", "TMPDIR", "TEMP", "TMP"} {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	return env
}

func copyConformanceSourceDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o750); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if shouldSkipConformancePath(rel, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target, info.Mode())
	})
}

func removeConformanceSensitiveFiles(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if !shouldSkipConformancePath(rel, d.IsDir()) {
			return nil
		}
		if d.IsDir() {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
			return filepath.SkipDir
		}
		return os.Remove(path)
	})
}

func absolutizeStagedGoModReplaces(stagedDir, originalDir string) error {
	goModPath := filepath.Join(stagedDir, "go.mod")
	data, err := os.ReadFile(goModPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read staged go.mod: %w", err)
	}
	mf, err := modfile.Parse(goModPath, data, nil)
	if err != nil {
		return fmt.Errorf("parse staged go.mod: %w", err)
	}
	changed := false
	for _, repl := range mf.Replace {
		if repl.New.Version != "" || filepath.IsAbs(repl.New.Path) || isModulePath(repl.New.Path) {
			continue
		}
		abs, err := filepath.Abs(filepath.Join(originalDir, filepath.FromSlash(repl.New.Path)))
		if err != nil {
			return fmt.Errorf("resolve go.mod replace %q: %w", repl.New.Path, err)
		}
		if err := mf.AddReplace(repl.Old.Path, repl.Old.Version, filepath.ToSlash(abs), ""); err != nil {
			return fmt.Errorf("update go.mod replace %q: %w", repl.Old.Path, err)
		}
		changed = true
	}
	if !changed {
		return nil
	}
	formatted, err := mf.Format()
	if err != nil {
		return fmt.Errorf("format staged go.mod: %w", err)
	}
	if err := os.WriteFile(goModPath, formatted, 0o600); err != nil {
		return fmt.Errorf("write staged go.mod: %w", err)
	}
	return nil
}

func isModulePath(path string) bool {
	return !strings.HasPrefix(path, ".") && !strings.HasPrefix(path, "/")
}

func shouldSkipConformancePath(rel string, isDir bool) bool {
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return false
	}
	base := pathBaseSlash(rel)
	if isDir && (base == ".git" || base == ".wfctl") {
		return true
	}
	return base == ".env" || strings.HasPrefix(base, ".env.")
}

func pathBaseSlash(path string) string {
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

type tailBuffer struct {
	buf []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	const maxTail = 4096
	b.buf = append(b.buf, p...)
	if len(b.buf) > maxTail {
		b.buf = b.buf[len(b.buf)-maxTail:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	return string(b.buf)
}
