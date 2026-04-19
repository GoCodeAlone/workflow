package buildergo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/plugin/builder"
)

// GoBuilder builds Go binaries via `go build`.
type GoBuilder struct{}

// New returns a new GoBuilder.
func New() builder.Builder { return &GoBuilder{} }

func (g *GoBuilder) Name() string { return "go" }

func (g *GoBuilder) Validate(cfg builder.Config) error {
	if cfg.Path == "" {
		return fmt.Errorf("go builder: path is required")
	}
	return nil
}

func (g *GoBuilder) Build(ctx context.Context, cfg builder.Config, out *builder.Outputs) error {
	if err := g.Validate(cfg); err != nil {
		return err
	}

	name := cfg.TargetName
	if name == "" {
		name = filepath.Base(cfg.Path)
	}
	outputBin := filepath.Join(".", name)

	args := []string{"build", "-o", outputBin}
	if ldflags, ok := cfg.Fields["ldflags"].(string); ok && ldflags != "" {
		args = append(args, "-ldflags", ldflags)
	}
	if tags, ok := cfg.Fields["tags"].(string); ok && tags != "" {
		args = append(args, "-tags", tags)
	}
	for _, flag := range extraFlags(cfg.Fields) {
		args = append(args, flag)
	}
	args = append(args, cfg.Path)

	// Dry-run: skip exec, emit the planned artifact.
	if os.Getenv("WFCTL_BUILD_DRY_RUN") == "1" {
		out.Artifacts = append(out.Artifacts, builder.Artifact{
			Name:  name,
			Kind:  "binary",
			Paths: []string{outputBin},
			Metadata: map[string]any{
				"dry_run": true,
				"args":    args,
			},
		})
		return nil
	}

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Env = os.Environ()
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if goos, ok := cfg.Fields["os"].(string); ok && goos != "" {
		cmd.Env = append(cmd.Env, "GOOS="+goos)
	}
	if goarch, ok := cfg.Fields["arch"].(string); ok && goarch != "" {
		cmd.Env = append(cmd.Env, "GOARCH="+goarch)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build: %w\n%s", err, out)
	}

	out.Artifacts = append(out.Artifacts, builder.Artifact{
		Name:  name,
		Kind:  "binary",
		Paths: []string{outputBin},
	})
	return nil
}

func (g *GoBuilder) SecurityLint(cfg builder.Config) []builder.Finding {
	var findings []builder.Finding

	if ldflags, ok := cfg.Fields["ldflags"].(string); ok {
		// Warn if -X is used to embed secrets (pattern: -X <pkg>.secret=<val>)
		if strings.Contains(ldflags, "-X") && (strings.Contains(strings.ToLower(ldflags), "secret") ||
			strings.Contains(strings.ToLower(ldflags), "token") ||
			strings.Contains(strings.ToLower(ldflags), "password") ||
			strings.Contains(strings.ToLower(ldflags), "key")) {
			findings = append(findings, builder.Finding{
				Severity: "warn",
				Message:  "ldflags may embed a secret via -X; use runtime env vars instead",
			})
		}
	}

	cgo, _ := cfg.Fields["cgo"].(bool)
	if cgo {
		linkMode, _ := cfg.Fields["link_mode"].(string)
		if linkMode == "" {
			findings = append(findings, builder.Finding{
				Severity: "warn",
				Message:  "cgo=true without link_mode; set link_mode to 'external' for predictable linking",
			})
		}
		builderImage, _ := cfg.Fields["builder_image"].(string)
		safeImages := map[string]bool{
			"golang:alpine":   true,
			"golang:bookworm": true,
			"golang:bullseye": true,
		}
		if builderImage != "" {
			imageBase := strings.SplitN(builderImage, ":", 2)[0]
			if !safeImages[imageBase] && !strings.Contains(imageBase, "golang") {
				findings = append(findings, builder.Finding{
					Severity: "warn",
					Message:  fmt.Sprintf("builder_image %q is not in known-safe list; prefer golang:alpine or golang:bookworm", builderImage),
				})
			}
		}
	}

	return findings
}

func extraFlags(fields map[string]any) []string {
	raw, ok := fields["extra_flags"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
