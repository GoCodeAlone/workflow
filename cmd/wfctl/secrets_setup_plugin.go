package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/secrets"
	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

// PluginRequiredSecret mirrors the plugin.json `required_secrets[]`
// entry. Each entry tells `wfctl secrets setup --plugin <name>` what
// to prompt for + whether to mask input.
type PluginRequiredSecret struct {
	Name        string `json:"name"`
	Sensitive   bool   `json:"sensitive"`
	Description string `json:"description,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
}

// pluginManifest is the slice of plugin.json this command actually
// reads. Other fields are ignored.
type pluginManifest struct {
	Name            string                 `json:"name"`
	RequiredSecrets []PluginRequiredSecret `json:"required_secrets,omitempty"`
}

// runSecretsSetupPlugin is the entry-point for `wfctl secrets setup
// --plugin <name>`. It reads the plugin's plugin.json, prompts for
// each declared required secret, and writes the values to the chosen
// GitHub scope (repo|env|org).
func runSecretsSetupPlugin(args []string) error {
	return runSecretsSetupPluginWithIO(args, nil, os.Stdout)
}

func runSecretsSetupPluginWithIO(args []string, in io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("secrets setup --plugin", flag.ContinueOnError)
	pluginName := fs.String("plugin", "", "Plugin name (must match a directory under --plugin-dir / $WFCTL_PLUGIN_DIR)")
	pluginDir := fs.String("plugin-dir", "", "Plugin install dir (default: $WFCTL_PLUGIN_DIR or ./data/plugins)")
	scope := fs.String("scope", "repo", "GitHub scope: repo | env | org")
	envName := fs.String("env", "", "Environment name (required with --scope=env)")
	org := fs.String("org", "", "Organization slug (required with --scope=org)")
	orgVisibility := fs.String("visibility", "all", "Org-scope visibility: all | selected | private")
	tokenEnv := fs.String("token-env", "GITHUB_TOKEN", "Env var holding the GitHub PAT")
	configFile := fs.String("config", "app.yaml", "app.yaml (used to resolve the github repo when --scope=repo|env)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl secrets setup --plugin <name> [options]

Interactively set the secrets declared by a plugin's plugin.json
required_secrets[] block. Sensitive fields are masked.

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *pluginName == "" {
		return errors.New("--plugin <name> is required")
	}

	manifest, err := loadPluginManifest(*pluginName, *pluginDir)
	if err != nil {
		return err
	}
	if len(manifest.RequiredSecrets) == 0 {
		fmt.Fprintf(out, "plugin %q declares no required_secrets[]; nothing to do\n", manifest.Name)
		return nil
	}

	// Pre-build the destination provider so a malformed scope fails
	// loud BEFORE prompting.
	scopeStr := strings.ToLower(strings.TrimSpace(*scope))
	provider, scopeLabel, err := buildSecretWriter(scopeStr, *envName, *org, *orgVisibility, *tokenEnv, *configFile)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Setting up secrets for plugin %q → %s\n\n", manifest.Name, scopeLabel)

	for _, rs := range manifest.RequiredSecrets {
		val, err := promptOne(rs, in)
		if err != nil {
			return err
		}
		if val == "" {
			fmt.Fprintf(out, "  %s: skipped (no value provided)\n", rs.Name)
			continue
		}
		if err := provider.Set(context.Background(), rs.Name, val); err != nil {
			return fmt.Errorf("set %s: %w", rs.Name, err)
		}
		fmt.Fprintf(out, "  %s: set\n", rs.Name)
	}
	fmt.Fprintf(out, "\nAll done.\n")
	return nil
}

// loadPluginManifest looks for the plugin.json under the resolved
// plugin install dir, parses it, and returns the manifest. Returns
// a clear error when the directory is missing.
func loadPluginManifest(name, dirOverride string) (*pluginManifest, error) {
	dir := dirOverride
	if dir == "" {
		dir = os.Getenv("WFCTL_PLUGIN_DIR")
	}
	if dir == "" {
		dir = "./data/plugins"
	}
	path := filepath.Join(dir, name, "plugin.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plugin manifest %s: %w (run `wfctl plugin install` first; or pass --plugin-dir)", path, err)
	}
	var m pluginManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse plugin manifest %s: %w", path, err)
	}
	return &m, nil
}

// promptOne reads a value for one required secret. Masks if Sensitive.
// When `in` is non-nil (tests / piped input) it reads a line from it
// regardless of Sensitive — masking is interactive-only.
func promptOne(rs PluginRequiredSecret, in io.Reader) (string, error) {
	label := rs.Prompt
	if label == "" {
		label = rs.Name
	}
	if rs.Description != "" {
		fmt.Fprintf(os.Stderr, "\n# %s\n", rs.Description)
	}
	fmt.Fprintf(os.Stderr, "%s: ", label)

	if in != nil {
		// Test/piped path — read one line.
		buf := make([]byte, 4096)
		n, err := in.Read(buf)
		if err != nil && err != io.EOF {
			return "", err
		}
		return strings.TrimRight(string(buf[:n]), "\r\n"), nil
	}

	if rs.Sensitive && isatty.IsTerminal(os.Stdin.Fd()) {
		fd, err := stdinFileDescriptor()
		if err != nil {
			return "", err
		}
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("read masked: %w", err)
		}
		return string(b), nil
	}
	// Non-sensitive interactive — echo.
	var line string
	if _, err := fmt.Fscanln(os.Stdin, &line); err != nil && err.Error() != "unexpected newline" {
		return "", err
	}
	return line, nil
}

// scopedWriter is the narrow interface secrets setup --plugin needs.
// Both secrets.GitHubSecretsProvider satisfies it.
type scopedWriter interface {
	Set(ctx context.Context, key, value string) error
}

// buildSecretWriter mints the GitHub provider for the requested scope.
// scopeLabel is a human-readable string for the setup prelude.
func buildSecretWriter(scope, envName, org, visibility, tokenEnv, configFile string) (scopedWriter, string, error) {
	switch scope {
	case "org":
		if org == "" {
			return nil, "", errors.New("--scope=org requires --org <slug>")
		}
		vis, err := parseGitHubOrgVisibility(visibility)
		if err != nil {
			return nil, "", err
		}
		p, err := secrets.NewGitHubOrgSecretsProvider(org, tokenEnv, vis, nil)
		if err != nil {
			return nil, "", err
		}
		return p, fmt.Sprintf("github org %q (visibility=%s)", org, visibility), nil

	case "env":
		if envName == "" {
			return nil, "", errors.New("--scope=env requires --env <environment-name>")
		}
		repo, err := readGitHubRepoFromAppYAML(configFile)
		if err != nil {
			return nil, "", err
		}
		p, err := secrets.NewGitHubSecretsProvider(repo, tokenEnv)
		if err != nil {
			return nil, "", err
		}
		p.SetEnvironment(envName)
		return p, fmt.Sprintf("github env %q on %s", envName, repo), nil

	case "", "repo":
		repo, err := readGitHubRepoFromAppYAML(configFile)
		if err != nil {
			return nil, "", err
		}
		p, err := secrets.NewGitHubSecretsProvider(repo, tokenEnv)
		if err != nil {
			return nil, "", err
		}
		return p, fmt.Sprintf("github repo %s", repo), nil

	default:
		return nil, "", fmt.Errorf("unknown --scope %q (want repo|env|org)", scope)
	}
}
