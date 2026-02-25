package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func runGitPush(args []string) error {
	fs := flag.NewFlagSet("git push", flag.ContinueOnError)
	message := fs.String("message", "", "Commit message")
	tag := fs.String("tag", "", "Create and push an annotated version tag (e.g. v1.0.0)")
	configOnly := fs.Bool("config-only", false, "Stage only config files (not generated build artifacts)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl git push [options]

Stage, commit, and push workflow project files to the configured GitHub repository.
Reads .wfctl.yaml for repository information.

Options:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := loadWfctlConfig()
	if err != nil {
		return err
	}

	if cfg.GitRepository == "" {
		return fmt.Errorf("no repository configured in .wfctl.yaml (run 'wfctl git connect' first)")
	}

	// Determine commit message
	commitMsg := *message
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("chore: update workflow config [wfctl]")
	}

	// Stage files
	if *configOnly {
		files := configOnlyFiles(cfg)
		fmt.Printf("Staging config files: %s\n", strings.Join(files, ", "))
		for _, f := range files {
			// Only stage if the file actually exists
			if _, err := os.Stat(f); err == nil {
				if err := runCmd("git", "add", f); err != nil {
					return fmt.Errorf("git add %s: %w", f, err)
				}
			}
		}
	} else {
		if err := runCmd("git", "add", "."); err != nil {
			return fmt.Errorf("git add failed: %w", err)
		}
	}

	// Check if there's anything to commit
	statusOut, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		return fmt.Errorf("git status failed: %w", err)
	}

	// Check staged changes specifically
	stagedOut, err := exec.Command("git", "diff", "--cached", "--name-only").Output()
	if err != nil {
		return fmt.Errorf("git diff --cached failed: %w", err)
	}

	_ = statusOut
	if strings.TrimSpace(string(stagedOut)) == "" {
		fmt.Println("Nothing to commit — working tree clean.")
	} else {
		if err := runCmd("git", "commit", "-m", commitMsg); err != nil {
			return fmt.Errorf("git commit failed: %w", err)
		}
		fmt.Printf("Committed: %s\n", commitMsg)
	}

	// Push to remote
	branch := cfg.GitBranch
	if branch == "" {
		branch = "main"
	}
	if err := runCmd("git", "push", "-u", "origin", branch); err != nil {
		return fmt.Errorf("git push failed: %w", err)
	}
	fmt.Printf("Pushed to origin/%s\n", branch)

	// Handle optional tag
	if *tag != "" {
		if err := createAndPushTag(*tag, commitMsg); err != nil {
			return fmt.Errorf("failed to push tag %s: %w", *tag, err)
		}
	}

	return nil
}

// createAndPushTag creates an annotated git tag and pushes it.
func createAndPushTag(tag, message string) error {
	tagMsg := fmt.Sprintf("Release %s\n\n%s", tag, message)
	if err := runCmd("git", "tag", "-a", tag, "-m", tagMsg); err != nil {
		return fmt.Errorf("git tag failed: %w", err)
	}
	fmt.Printf("Created tag %s\n", tag)

	if err := runCmd("git", "push", "origin", tag); err != nil {
		return fmt.Errorf("git push tag failed: %w", err)
	}
	fmt.Printf("Pushed tag %s\n", tag)
	return nil
}
