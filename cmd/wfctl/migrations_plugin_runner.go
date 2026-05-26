package main

import (
	"bytes"
	"context"
	"fmt"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type migrationCommandResult struct {
	Stdout string
	Stderr string
}

type migrationPluginRunConfig struct {
	Plugin    string
	PluginDir string
	Driver    string
	SourceDir string
	DSN       string
}

type migrationPluginRunner struct {
	exec func(ctx context.Context, pluginName string, args []string, env map[string]string) (migrationCommandResult, error)
}

func (r migrationPluginRunner) run(ctx context.Context, cfg migrationPluginRunConfig, command string) (migrationCommandResult, error) {
	return r.runArgs(ctx, cfg, strings.Fields(command))
}

func (r migrationPluginRunner) runArgs(ctx context.Context, cfg migrationPluginRunConfig, commandArgs []string) (migrationCommandResult, error) {
	return r.runPluginArgs(ctx, cfg, buildMigrationPluginArgs(cfg, commandArgs), strings.Join(commandArgs, " "))
}

func (r migrationPluginRunner) runLint(ctx context.Context, cfg migrationPluginRunConfig) (migrationCommandResult, error) {
	return r.runPluginArgs(ctx, cfg, buildMigrationPluginLintArgs(cfg), "lint")
}

func (r migrationPluginRunner) runPluginArgs(ctx context.Context, cfg migrationPluginRunConfig, args []string, label string) (migrationCommandResult, error) {
	if err := validateMigrationPluginName(cfg.Plugin); err != nil {
		return migrationCommandResult{}, err
	}

	execFn := r.exec
	if execFn == nil {
		execFn = defaultMigrationPluginExecutor
	}

	result, err := execFn(ctx, cfg.Plugin, args, buildMigrationPluginEnv(cfg))
	result.Stdout = redactMigrationDSN(result.Stdout, cfg.DSN)
	result.Stderr = redactMigrationDSN(result.Stderr, cfg.DSN)
	if err != nil {
		return result, fmt.Errorf("migration plugin %s %s: %s", cfg.Plugin, label, redactMigrationDSN(err.Error(), cfg.DSN))
	}
	return result, nil
}

func buildMigrationPluginArgs(cfg migrationPluginRunConfig, commandArgs []string) []string {
	args := append([]string(nil), commandArgs...)
	args = append(args,
		"--driver", cfg.Driver,
		"--source-dir", cfg.SourceDir,
	)
	return args
}

func buildMigrationPluginLintArgs(cfg migrationPluginRunConfig) []string {
	return []string{"lint", cfg.SourceDir}
}

func buildMigrationPluginEnv(cfg migrationPluginRunConfig) map[string]string {
	env := map[string]string{
		"DATABASE_URL": cfg.DSN,
	}
	if cfg.PluginDir != "" {
		env["WFCTL_PLUGIN_DIR"] = cfg.PluginDir
	}
	return env
}

func defaultMigrationPluginExecutor(ctx context.Context, pluginName string, args []string, env map[string]string) (migrationCommandResult, error) {
	if err := validateMigrationPluginName(pluginName); err != nil {
		return migrationCommandResult{}, err
	}
	pluginDirName := normalizePluginName(pluginName)
	pluginRoot := defaultDataDir
	if env != nil && env["WFCTL_PLUGIN_DIR"] != "" {
		pluginRoot = env["WFCTL_PLUGIN_DIR"]
	}
	binaryPath := filepath.Join(pluginRoot, pluginDirName, pluginDirName)
	cmd := exec.CommandContext(ctx, binaryPath, args...) //nolint:gosec // binary path follows wfctl installed-plugin layout.

	cmd.Env = mapEnv(withMigrationPluginBaseEnv(env))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := migrationCommandResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if err != nil {
		return result, fmt.Errorf("run %s (stderr: %s): %w", binaryPath, stderr.String(), err)
	}
	return result, nil
}

func withMigrationPluginBaseEnv(env map[string]string) map[string]string {
	merged := map[string]string{}
	for _, key := range []string{"HOME", "PATH", "SSL_CERT_FILE", "SSL_CERT_DIR", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY"} {
		if value := os.Getenv(key); value != "" {
			merged[key] = value
		}
	}
	for key, value := range env {
		merged[key] = value
	}
	return merged
}

func mapEnv(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+"="+env[key])
	}
	return pairs
}

func validateMigrationPluginName(pluginName string) error {
	trimmed := strings.TrimSpace(pluginName)
	if trimmed == "" {
		return fmt.Errorf("unsafe plugin name %q", pluginName)
	}
	for _, candidate := range []string{trimmed, normalizePluginName(trimmed)} {
		if candidate == "" || candidate == "." || candidate == ".." || strings.ContainsAny(candidate, `/\`) {
			return fmt.Errorf("unsafe plugin name %q", pluginName)
		}
		for _, r := range candidate {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
				continue
			}
			return fmt.Errorf("unsafe plugin name %q", pluginName)
		}
	}
	return nil
}

func redactMigrationDSN(message, dsn string) string {
	if dsn == "" {
		return message
	}
	candidates := []string{dsn}
	if parsed, err := neturl.Parse(dsn); err == nil {
		if parsed.User != nil {
			if password, ok := parsed.User.Password(); ok && password != "" {
				candidates = append(candidates, password)
			}
			if username := parsed.User.Username(); username != "" {
				candidates = append(candidates, username)
			}
			if userInfo := parsed.User.String(); userInfo != "" {
				candidates = append(candidates, userInfo)
			}
		}
		withoutQuery := *parsed
		withoutQuery.RawQuery = ""
		withoutQuery.Fragment = ""
		if value := withoutQuery.String(); value != "" {
			candidates = append(candidates, value)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return len(candidates[i]) > len(candidates[j])
	})
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		message = strings.ReplaceAll(message, candidate, "[REDACTED_DSN]")
	}
	return message
}
