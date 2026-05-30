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

	"github.com/GoCodeAlone/workflow/cmd/wfctl/internal/prompt"
	"github.com/GoCodeAlone/workflow/secrets"
	"github.com/mattn/go-isatty"
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
	fromEnv := fs.Bool("from-env", false, "Read each secret value from $NAME (recommended for CI; avoids process-table leaks)")
	var secretFlag multiStringFlag
	fs.Var(&secretFlag, "secret", "NAME=VALUE literal (WARNING: leaks to process table; use --from-env in CI). Repeatable.")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl secrets setup --plugin <name> [options]

Set the secrets declared by a plugin's plugin.json required_secrets[] block.

Interactive (default when stdin is a TTY): each secret is prompted; sensitive
fields are masked.

Non-interactive (when stdin is not a TTY): values come from --from-env ($NAME),
--secret NAME=VALUE, or piped KEY=VALUE lines. A secret with no value source is
skipped (never blocks waiting for input).

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
	ghProvider, scopeLabel, err := buildSecretWriter(scopeStr, *envName, *org, *orgVisibility, *tokenEnv, *configFile)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Setting up secrets for plugin %q → %s\n\n", manifest.Name, scopeLabel)

	// Wrap the GitHub provider in the shared adapter so the engine can use it.
	provider := secretsProviderAdapter{p: ghProvider}

	// Selector: set every declared required secret (no skip-existing for the
	// plugin flow — required secrets are always offered).
	selector := func(ds []PluginRequiredSecret, _ []SecretStatus) ([]PluginRequiredSecret, error) {
		return ds, nil
	}

	// Decide the input mode up-front:
	//   - in != nil                          → reader path (tests / explicit pipe).
	//   - in == nil && stdin is a TTY        → interactive prompt.Input (masked).
	//   - in == nil && stdin is NOT a TTY    → non-interactive value sources only
	//     (--from-env / --secret); a secret with no source is SKIPPED, never read
	//     via Fscanln (which would block forever on an open empty pipe).
	stdinIsTTY := isatty.IsTerminal(os.Stdin.Fd())
	interactive := in == nil && stdinIsTTY

	// Build the non-interactive value source map (--secret literals).
	secretMap := make(map[string]string)
	for _, lit := range secretFlag {
		k, v, found := strings.Cut(lit, "=")
		if !found {
			return fmt.Errorf("--secret %q: expected NAME=VALUE format", lit)
		}
		secretMap[k] = v
	}

	var promptErr error
	valuer := func(rs PluginRequiredSecret) (string, bool, error) {
		switch {
		case in != nil:
			// Reader path (tests / explicit pipe): one line per secret.
			val, verr := promptOne(rs, in)
			if verr != nil {
				return "", false, verr
			}
			if val == "" {
				return "", false, nil
			}
			return val, true, nil

		case interactive:
			label := rs.Prompt
			if label == "" {
				label = rs.Name
			}
			if rs.Description != "" {
				label = label + " — " + rs.Description
			}
			val, verr := prompt.Input(label, rs.Sensitive)
			if verr != nil {
				if errors.Is(verr, prompt.ErrNotInteractive) {
					promptErr = verr
				}
				return "", false, verr
			}
			if val == "" {
				return "", false, nil
			}
			return val, true, nil

		default:
			// Non-interactive (non-TTY): value sources only — NEVER block on stdin.
			if *fromEnv {
				if v := os.Getenv(rs.Name); v != "" {
					return v, true, nil
				}
			}
			if v, ok := secretMap[rs.Name]; ok {
				return v, true, nil
			}
			// No value source for this secret → skip (don't hang, don't fail hard).
			return "", false, nil
		}
	}

	auditFn := func(name, _ string) {
		_ = writeSecretsAuditRecord(name, "github:"+scopeStr) //nolint:errcheck // best-effort audit
	}

	report, err := runSetupEngine(context.Background(), manifest.RequiredSecrets,
		func(rs PluginRequiredSecret) string { return rs.Name },
		provider, selector, valuer, auditFn, true)
	// Surface a mid-flow ErrNotInteractive regardless of the engine's error
	// (stopOnErr=true means it usually returns the wrapped error, but check the
	// captured promptErr too for robustness).
	if promptErr != nil {
		return promptErr
	}
	if err != nil {
		return err
	}

	for _, n := range report.Set {
		fmt.Fprintf(out, "  %s: set\n", n)
	}
	for _, n := range report.Skipped {
		fmt.Fprintf(out, "  %s: skipped (no value provided)\n", n)
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

// promptOne reads a single value for one required secret from the supplied
// reader. It is used only on the reader-backed path (tests / explicit piped
// input); masking is interactive-only and handled by prompt.Input on a TTY, so
// this helper never touches os.Stdin and therefore can never block on an open
// empty pipe via Fscanln.
func promptOne(rs PluginRequiredSecret, in io.Reader) (string, error) {
	label := rs.Prompt
	if label == "" {
		label = rs.Name
	}
	if rs.Description != "" {
		fmt.Fprintf(os.Stderr, "\n# %s\n", rs.Description)
	}
	fmt.Fprintf(os.Stderr, "%s: ", label)

	if in == nil {
		// Defensive: callers must pass a reader. Treat a nil reader as "no
		// value" rather than reading os.Stdin (which could block).
		return "", nil
	}
	buf := make([]byte, 4096)
	n, err := in.Read(buf)
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimRight(string(buf[:n]), "\r\n"), nil
}

// buildSecretWriter mints the GitHub provider for the requested scope.
// scopeLabel is a human-readable string for the setup prelude. The returned
// provider is a full secrets.Provider (the GitHub providers implement Get/Set/
// Delete/List + StatAll + CheckAccess) so it can be wrapped in the shared
// secretsProviderAdapter and driven by the setup engine.
func buildSecretWriter(scope, envName, org, visibility, tokenEnv, configFile string) (secrets.Provider, string, error) {
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
