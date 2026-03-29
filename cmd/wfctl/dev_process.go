package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"

	"github.com/GoCodeAlone/workflow/config"
)

// ANSI color codes for service name prefixes.
var serviceColors = []string{
	"\033[36m", // cyan
	"\033[32m", // green
	"\033[33m", // yellow
	"\033[35m", // magenta
	"\033[34m", // blue
	"\033[31m", // red
}

const colorReset = "\033[0m"

// managedProcess holds a running service subprocess.
type managedProcess struct {
	name   string
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

// runDevProcess starts infrastructure as Docker and app services as local Go
// processes with hot-reload via fsnotify.
func runDevProcess(cfg *config.WorkflowConfig, verbose bool) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start infrastructure containers (postgres, redis, nats, etc.) via
	// docker compose using a generated infra-only compose file.
	infra := &config.WorkflowConfig{Modules: cfg.Modules}
	composeYAML, err := generateDevCompose(infra)
	if err != nil {
		return fmt.Errorf("generate infra compose: %w", err)
	}
	const infraComposeFile = "docker-compose.dev-infra.yml"
	if err := os.WriteFile(infraComposeFile, []byte(composeYAML), 0o644); err != nil {
		return fmt.Errorf("write infra compose: %w", err)
	}

	fmt.Println("[wfctl] Starting infrastructure containers...")
	infraCmd := exec.CommandContext(ctx, "docker", "compose", "-f", infraComposeFile, "up", "-d") //nolint:gosec
	infraCmd.Stdout = os.Stdout
	infraCmd.Stderr = os.Stderr
	if err := infraCmd.Run(); err != nil {
		return fmt.Errorf("start infra containers: %w", err)
	}

	// Collect service definitions.
	services := collectProcessServices(cfg)
	if len(services) == 0 {
		fmt.Println("[wfctl] No local services to run (no services: section or binary targets).")
		fmt.Println("[wfctl] Infrastructure is up. Press Ctrl+C to stop.")
		<-ctx.Done()
		return nil
	}

	var (
		mu        sync.Mutex
		procs     = make(map[string]*managedProcess, len(services))
		wg        sync.WaitGroup
		rebuildCh = make(chan string, len(services))
	)

	// Start all services.
	for i, svc := range services {
		color := serviceColors[i%len(serviceColors)]
		if err := startServiceProcess(ctx, svc, color, procs, &mu); err != nil {
			fmt.Printf("[wfctl] Warning: failed to start %s: %v\n", svc.name, err)
		}
	}

	// Set up file watcher for hot-reload.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create file watcher: %w", err)
	}
	defer watcher.Close() //nolint:errcheck

	// Watch all Go source directories.
	watchDirs := collectWatchDirs(".")
	for _, dir := range watchDirs {
		if err := watcher.Add(dir); err != nil && verbose {
			fmt.Printf("[wfctl] Warning: cannot watch %s: %v\n", dir, err)
		}
	}
	if verbose {
		fmt.Printf("[wfctl] Watching %d directories for changes\n", len(watchDirs))
	}

	// Rebuild dispatcher.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case svcName := <-rebuildCh:
				mu.Lock()
				proc, ok := procs[svcName]
				mu.Unlock()
				if !ok {
					continue
				}
				fmt.Printf("[wfctl] Rebuilding %s...\n", svcName)
				proc.cancel()

				// Find the service spec and restart.
				for _, svc := range services {
					if svc.name != svcName {
						continue
					}
					color := serviceColors[0]
					for i, s := range services {
						if s.name == svcName {
							color = serviceColors[i%len(serviceColors)]
						}
					}
					if err := startServiceProcess(ctx, svc, color, procs, &mu); err != nil {
						fmt.Printf("[wfctl] Restart failed for %s: %v\n", svcName, err)
					}
				}
			}
		}
	}()

	// File change dispatcher.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if !isGoFile(event.Name) {
					continue
				}
				if verbose {
					fmt.Printf("[wfctl] Change detected: %s\n", event.Name)
				}
				// Notify all services (simple strategy: rebuild all on any change).
				for _, svc := range services {
					select {
					case rebuildCh <- svc.name:
					default:
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				fmt.Printf("[wfctl] Watcher error: %v\n", err)
			}
		}
	}()

	fmt.Println("[wfctl] Local development mode active. Press Ctrl+C to stop.")
	<-ctx.Done()

	// Stop all processes.
	mu.Lock()
	for _, proc := range procs {
		proc.cancel()
	}
	mu.Unlock()
	wg.Wait()
	return nil
}

// processServiceSpec describes a local service to run.
type processServiceSpec struct {
	name    string
	binary  string   // path to Go main package or pre-built binary
	args    []string // extra args
	env     map[string]string
	workDir string
}

// collectProcessServices returns the list of services to run as local processes.
func collectProcessServices(cfg *config.WorkflowConfig) []processServiceSpec {
	var specs []processServiceSpec

	if len(cfg.Services) > 0 {
		for name, svc := range cfg.Services {
			if svc == nil {
				continue
			}
			spec := processServiceSpec{
				name:   name,
				binary: cmp(svc.Binary, "./cmd/"+name),
			}
			specs = append(specs, spec)
		}
		return specs
	}

	// Single-service: look for a cmd/server or cmd/app package.
	for _, candidate := range []string{"./cmd/server", "./cmd/app", "."} {
		if dirExists(candidate) {
			specs = append(specs, processServiceSpec{
				name:   "app",
				binary: candidate,
			})
			break
		}
	}
	return specs
}

// startServiceProcess compiles (if needed) and starts a service as a subprocess.
func startServiceProcess(
	ctx context.Context,
	svc processServiceSpec,
	color string,
	procs map[string]*managedProcess,
	mu *sync.Mutex,
) error {
	// Build the binary.
	binPath := filepath.Join(os.TempDir(), "wfctl-dev-"+svc.name)
	buildCmd := exec.CommandContext(ctx, "go", "build", "-o", binPath, svc.binary) //nolint:gosec
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	fmt.Printf("[wfctl] Building %s (%s)...\n", svc.name, svc.binary)
	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("build %s: %w", svc.name, err)
	}

	// Start the binary.
	procCtx, procCancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(procCtx, binPath, svc.args...) //nolint:gosec
	if svc.workDir != "" {
		cmd.Dir = svc.workDir
	}
	for k, v := range svc.env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Env = append(cmd.Env, os.Environ()...)

	// Multiplex output with colored prefix.
	prefix := fmt.Sprintf("%s[%s]%s ", color, svc.name, colorReset)
	cmd.Stdout = &prefixWriter{w: os.Stdout, prefix: prefix}
	cmd.Stderr = &prefixWriter{w: os.Stderr, prefix: prefix}

	if err := cmd.Start(); err != nil {
		procCancel()
		return fmt.Errorf("start %s: %w", svc.name, err)
	}
	fmt.Printf("[wfctl] Started %s (pid %d)\n", svc.name, cmd.Process.Pid)

	mp := &managedProcess{name: svc.name, cmd: cmd, cancel: procCancel}
	mu.Lock()
	procs[svc.name] = mp
	mu.Unlock()

	// Wait in background to reap the process.
	go func() {
		_ = cmd.Wait()
	}()

	return nil
}

// prefixWriter prepends a colored service name to each line of output.
type prefixWriter struct {
	w      io.Writer
	prefix string
	buf    []byte
}

func (pw *prefixWriter) Write(p []byte) (int, error) {
	pw.buf = append(pw.buf, p...)
	for {
		idx := strings.IndexByte(string(pw.buf), '\n')
		if idx < 0 {
			break
		}
		line := pw.buf[:idx+1]
		pw.buf = pw.buf[idx+1:]
		if _, err := fmt.Fprintf(pw.w, "%s%s", pw.prefix, string(line)); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// collectWatchDirs returns all subdirectories under root that contain .go files,
// skipping vendor and hidden directories.
func collectWatchDirs(root string) []string {
	var dirs []string
	seen := map[string]bool{}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasPrefix(base, ".") || base == "vendor" || base == "node_modules" {
			return filepath.SkipDir
		}
		if !seen[path] {
			seen[path] = true
			dirs = append(dirs, path)
		}
		return nil
	})
	return dirs
}

// isGoFile returns true for .go source files.
func isGoFile(path string) bool {
	return strings.HasSuffix(path, ".go")
}

// dirExists returns true if the path exists and is a directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
