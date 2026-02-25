package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// wfctlConfig represents the .wfctl.yaml project config file.
type wfctlConfig struct {
	ProjectName     string
	ProjectVersion  string
	ConfigFile      string
	GitRepository   string
	GitBranch       string
	GitAutoPush     bool
	GenerateActions bool
	DeployTarget    string
	DeployNamespace string
}

func runGit(args []string) error {
	if len(args) < 1 {
		return gitUsage()
	}
	switch args[0] {
	case "connect":
		return runGitConnect(args[1:])
	case "push":
		return runGitPush(args[1:])
	default:
		return gitUsage()
	}
}

func gitUsage() error {
	fmt.Fprintf(os.Stderr, `Usage: wfctl git <subcommand> [options]

Git integration for workflow projects.

Subcommands:
  connect   Connect a workflow project to a GitHub repository
  push      Push workflow project to the configured GitHub repository

Examples:
  wfctl git connect -repo GoCodeAlone/my-api
  wfctl git connect -repo GoCodeAlone/my-api -init
  wfctl git push -message "update config"
  wfctl git push -tag v1.0.0
`)
	return fmt.Errorf("subcommand is required (connect, push)")
}

func runGitConnect(args []string) error {
	fs := flag.NewFlagSet("git connect", flag.ContinueOnError)
	repo := fs.String("repo", "", "GitHub repository (owner/name)")
	token := fs.String("token", "", "GitHub personal access token (or set GITHUB_TOKEN env)")
	initRepo := fs.Bool("init", false, "Initialize git repo and push to GitHub if not already set up")
	configFile := fs.String("config", "workflow.yaml", "Workflow config file for the project")
	deployTarget := fs.String("deploy-target", "kubernetes", "Deployment target (docker, kubernetes, cloud)")
	namespace := fs.String("namespace", "default", "Kubernetes namespace for deployment")
	branch := fs.String("branch", "main", "Default branch name")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl git connect [options]

Connect a workflow project to a GitHub repository.
Writes a .wfctl.yaml project file and optionally initializes the git repo.

Options:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *repo == "" {
		fs.Usage()
		return fmt.Errorf("-repo is required (e.g. -repo owner/my-api)")
	}

	// Validate repo format
	parts := strings.SplitN(*repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid repository format %q: expected owner/name", *repo)
	}

	projectName := parts[1]

	// Determine token for later use
	ghToken := *token
	if ghToken == "" {
		ghToken = os.Getenv("GITHUB_TOKEN")
	}

	cfg := &wfctlConfig{
		ProjectName:     projectName,
		ProjectVersion:  "1.0.0",
		ConfigFile:      *configFile,
		GitRepository:   *repo,
		GitBranch:       *branch,
		GitAutoPush:     false,
		GenerateActions: true,
		DeployTarget:    *deployTarget,
		DeployNamespace: *namespace,
	}

	if err := writeWfctlConfig(cfg); err != nil {
		return fmt.Errorf("failed to write .wfctl.yaml: %w", err)
	}
	fmt.Println("  create  .wfctl.yaml")

	if *initRepo {
		if err := initGitRepo(*repo, *branch, ghToken); err != nil {
			return fmt.Errorf("failed to initialize git repo: %w", err)
		}
	}

	fmt.Printf("\nProject connected to %s\n", *repo)
	fmt.Println("\nNext steps:")
	fmt.Println("  wfctl generate github-actions workflow.yaml")
	fmt.Println("  wfctl git push -message \"initial commit\"")
	return nil
}

// writeWfctlConfig writes the .wfctl.yaml project configuration file.
func writeWfctlConfig(cfg *wfctlConfig) error {
	var b strings.Builder
	b.WriteString("project:\n")
	b.WriteString(fmt.Sprintf("  name: %s\n", cfg.ProjectName))
	b.WriteString(fmt.Sprintf("  version: \"%s\"\n", cfg.ProjectVersion))
	b.WriteString(fmt.Sprintf("  configFile: %s\n", cfg.ConfigFile))
	b.WriteString("git:\n")
	b.WriteString(fmt.Sprintf("  repository: %s\n", cfg.GitRepository))
	b.WriteString(fmt.Sprintf("  branch: %s\n", cfg.GitBranch))
	b.WriteString(fmt.Sprintf("  autoPush: %v\n", cfg.GitAutoPush))
	b.WriteString(fmt.Sprintf("  generateActions: %v\n", cfg.GenerateActions))
	b.WriteString("deploy:\n")
	b.WriteString(fmt.Sprintf("  target: %s\n", cfg.DeployTarget))
	b.WriteString(fmt.Sprintf("  namespace: %s\n", cfg.DeployNamespace))
	return os.WriteFile(".wfctl.yaml", []byte(b.String()), 0640)
}

// initGitRepo initializes a git repository and sets up the remote.
func initGitRepo(repo, branch, token string) error {
	// Check if already a git repo
	if _, err := os.Stat(".git"); err == nil {
		fmt.Println("  git repo already initialized")
	} else {
		if err := runCmd("git", "init", "-b", branch); err != nil {
			// Older git may not support -b, fall back
			if err2 := runCmd("git", "init"); err2 != nil {
				return fmt.Errorf("git init failed: %w", err2)
			}
		}
		fmt.Println("  git init")
	}

	// Create .gitignore if absent
	if _, err := os.Stat(".gitignore"); os.IsNotExist(err) {
		if err := writeDefaultGitignore(); err != nil {
			return fmt.Errorf("failed to write .gitignore: %w", err)
		}
		fmt.Println("  create  .gitignore")
	}

	// Set remote
	remoteURL := fmt.Sprintf("git@github.com:%s.git", repo)
	if token != "" {
		// Use HTTPS with token when token provided
		remoteURL = fmt.Sprintf("https://%s@github.com/%s.git", token, repo)
	}

	// Check if origin already set
	out, err := exec.Command("git", "remote", "get-url", "origin").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		if err := runCmd("git", "remote", "add", "origin", remoteURL); err != nil {
			return fmt.Errorf("git remote add failed: %w", err)
		}
		fmt.Printf("  git remote add origin %s\n", repoDisplayURL(repo, token != ""))
	} else {
		if err := runCmd("git", "remote", "set-url", "origin", remoteURL); err != nil {
			return fmt.Errorf("git remote set-url failed: %w", err)
		}
		fmt.Printf("  git remote set-url origin %s\n", repoDisplayURL(repo, token != ""))
	}

	return nil
}

// repoDisplayURL returns a display-safe URL (no token in output).
func repoDisplayURL(repo string, useHTTPS bool) string {
	if useHTTPS {
		return fmt.Sprintf("https://github.com/%s.git", repo)
	}
	return fmt.Sprintf("git@github.com:%s.git", repo)
}

// writeDefaultGitignore writes a sensible default .gitignore.
func writeDefaultGitignore() error {
	content := `# Binaries
*.exe
*.exe~
*.dll
*.so
*.dylib
*.out
bin/
dist/

# Test artifacts
*.test
coverage.out

# Environment
.env
.env.local
.env.*.local

# Data
data/
*.db
*.sqlite

# Build artifacts
ui/dist/
ui/node_modules/

# wfctl artifacts
.wfctl.yaml
`
	return os.WriteFile(".gitignore", []byte(content), 0640)
}

// loadWfctlConfig reads .wfctl.yaml from the current directory.
func loadWfctlConfig() (*wfctlConfig, error) {
	data, err := os.ReadFile(".wfctl.yaml")
	if err != nil {
		return nil, fmt.Errorf("failed to read .wfctl.yaml: %w (run 'wfctl git connect' first)", err)
	}

	cfg := &wfctlConfig{
		GitBranch:   "main",
		DeployTarget: "kubernetes",
	}

	// Simple line-by-line YAML parser for .wfctl.yaml
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		kv := strings.SplitN(line, ":", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])

		switch key {
		case "name":
			cfg.ProjectName = val
		case "version":
			cfg.ProjectVersion = strings.Trim(val, "\"")
		case "configFile":
			cfg.ConfigFile = val
		case "repository":
			cfg.GitRepository = val
		case "branch":
			cfg.GitBranch = val
		case "autoPush":
			cfg.GitAutoPush = val == "true"
		case "generateActions":
			cfg.GenerateActions = val == "true"
		case "target":
			cfg.DeployTarget = val
		case "namespace":
			cfg.DeployNamespace = val
		}
	}

	return cfg, nil
}

// runCmd runs a shell command and streams its output.
func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// configOnlyFiles returns the set of files typically staged for config-only pushes.
func configOnlyFiles(cfg *wfctlConfig) []string {
	files := []string{".wfctl.yaml"}
	if cfg.ConfigFile != "" {
		files = append(files, cfg.ConfigFile)
	}
	// plugin.json if exists
	if _, err := os.Stat("plugin.json"); err == nil {
		files = append(files, "plugin.json")
	}
	// .github/workflows/ if exists
	if _, err := os.Stat(filepath.Join(".github", "workflows")); err == nil {
		files = append(files, filepath.Join(".github", "workflows"))
	}
	return files
}
