package main

import (
	"bytes"
	"context"
	"fmt"
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
	Driver    string
	SourceDir string
	DSN       string
}

type migrationPluginRunner struct {
	exec func(ctx context.Context, pluginName string, args []string, env map[string]string) (migrationCommandResult, error)
}

func (r migrationPluginRunner) run(ctx context.Context, cfg migrationPluginRunConfig, command string) (migrationCommandResult, error) {
	execFn := r.exec
	if execFn == nil {
		execFn = defaultMigrationPluginExecutor
	}

	result, err := execFn(ctx, cfg.Plugin, buildMigrationPluginArgs(cfg, command), nil)
	if err != nil {
		return result, fmt.Errorf("migration plugin %s migrate %s: %s", cfg.Plugin, command, redactMigrationDSN(err.Error(), cfg.DSN))
	}
	return result, nil
}

func buildMigrationPluginArgs(cfg migrationPluginRunConfig, command string) []string {
	args := []string{"--wfctl-cli", "migrate"}
	args = append(args, strings.Fields(command)...)
	args = append(args,
		"--driver", cfg.Driver,
		"--source-dir", cfg.SourceDir,
		"--dsn", cfg.DSN,
	)
	return args
}

func defaultMigrationPluginExecutor(ctx context.Context, pluginName string, args []string, env map[string]string) (migrationCommandResult, error) {
	pluginDirName := normalizePluginName(pluginName)
	binaryPath := filepath.Join(defaultDataDir, pluginDirName, pluginDirName)
	cmd := exec.CommandContext(ctx, binaryPath, args...) //nolint:gosec // binary path follows wfctl installed-plugin layout.

	if len(env) > 0 {
		cmd.Env = append(os.Environ(), mapEnv(env)...)
	}

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

func redactMigrationDSN(message, dsn string) string {
	if dsn == "" {
		return message
	}
	return strings.ReplaceAll(message, dsn, "[REDACTED_DSN]")
}
