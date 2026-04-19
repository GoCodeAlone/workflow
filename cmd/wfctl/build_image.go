package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
)

// runBuildImage implements `wfctl build image`.
// For each CIContainerTarget:
//   - external:true  — resolve tag from source, skip local build
//   - method:ko      — invoke `ko build`
//   - method:dockerfile (default) — invoke `docker build` via BuildKit
func runBuildImage(args []string) error {
	fs := flag.NewFlagSet("build image", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "Config file")
	dryRun := fs.Bool("dry-run", false, "Print planned actions without executing")
	tagOverride := fs.String("tag", "", "Override image tag for all containers")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if os.Getenv("WFCTL_BUILD_DRY_RUN") == "1" {
		*dryRun = true
	}

	if *cfgPath == "" {
		for _, c := range []string{"workflow.yaml", "app.yaml", "ci.yaml"} {
			if _, err := os.Stat(c); err == nil {
				*cfgPath = c
				break
			}
		}
	}
	if *cfgPath == "" {
		return fmt.Errorf("wfctl build image: no config file found")
	}

	cfg, err := config.LoadFromFile(*cfgPath)
	if err != nil {
		return fmt.Errorf("wfctl build image: load: %w", err)
	}
	if cfg.CI == nil || cfg.CI.Build == nil || len(cfg.CI.Build.Containers) == 0 {
		fmt.Println("wfctl build image: no containers defined")
		return nil
	}

	for _, ctr := range cfg.CI.Build.Containers {
		tag := *tagOverride
		if tag == "" {
			tag = "latest"
		}

		if ctr.External {
			img := ""
			if ctr.Source != nil {
				img = ctr.Source.Ref
			}
			if *dryRun {
				fmt.Printf("[dry-run] image external ref: %s → %s\n", ctr.Name, img)
			} else {
				fmt.Printf("image: %s resolved from external source %s\n", ctr.Name, img)
			}
			continue
		}

		method := ctr.Method
		if method == "" {
			method = "dockerfile"
		}

		switch method {
		case "ko":
			if err := buildWithKo(ctr, tag, *dryRun); err != nil {
				return fmt.Errorf("ko build %q: %w", ctr.Name, err)
			}
		default: // dockerfile
			if err := buildWithDockerfile(ctr, tag, *dryRun); err != nil {
				return fmt.Errorf("dockerfile build %q: %w", ctr.Name, err)
			}
		}
	}
	return nil
}

func buildWithDockerfile(ctr config.CIContainerTarget, tag string, dryRun bool) error {
	dockerfile := ctr.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}

	imageRef := imageRefForContainer(ctr, tag)
	args := []string{"build", "--file", dockerfile, "--tag", imageRef}

	// Platforms (BuildKit multi-arch).
	if len(ctr.Platforms) > 0 {
		args = append(args, "--platform", strings.Join(ctr.Platforms, ","))
	}

	// Build args.
	for k, v := range ctr.BuildArgs {
		args = append(args, "--build-arg", k+"="+v)
	}

	// Secrets.
	for _, s := range ctr.Secrets {
		if s.Env != "" {
			args = append(args, "--secret", fmt.Sprintf("id=%s,env=%s", s.ID, s.Env))
		} else if s.Src != "" {
			args = append(args, "--secret", fmt.Sprintf("id=%s,src=%s", s.ID, s.Src))
		}
	}

	// Cache.
	if ctr.Cache != nil {
		if ctr.Cache.From != nil {
			for _, ref := range ctr.Cache.From {
				if ref.Ref != "" {
					args = append(args, "--cache-from", ref.Ref)
				}
			}
		}
		for _, ref := range ctr.Cache.To {
			if ref.Ref != "" {
				args = append(args, "--cache-to", ref.Ref)
			}
		}
	}

	// Build target (multi-stage).
	if ctr.Target != "" {
		args = append(args, "--target", ctr.Target)
	}

	args = append(args, ".")

	if dryRun {
		fmt.Printf("[dry-run] docker %s\n", strings.Join(args, " "))
		return nil
	}

	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "DOCKER_BUILDKIT=1")
	return cmd.Run()
}

func buildWithKo(ctr config.CIContainerTarget, tag string, dryRun bool) error {
	pkg := ctr.KoPackage
	if pkg == "" {
		pkg = "."
	}

	args := []string{"build"}
	if len(ctr.Platforms) > 0 {
		args = append(args, "--platform", strings.Join(ctr.Platforms, ","))
	}
	if ctr.KoBare {
		args = append(args, "--bare")
	}
	if ctr.KoBaseImage != "" {
		args = append(args, "--base-import-paths")
	}
	args = append(args, pkg)

	if dryRun {
		fmt.Printf("[dry-run] ko %s\n", strings.Join(args, " "))
		return nil
	}

	cmd := exec.Command("ko", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func imageRefForContainer(ctr config.CIContainerTarget, tag string) string {
	if len(ctr.PushTo) > 0 {
		return ctr.PushTo[0] + "/" + ctr.Name + ":" + tag
	}
	return ctr.Name + ":" + tag
}
