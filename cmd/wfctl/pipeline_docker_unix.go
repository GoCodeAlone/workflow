//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/GoCodeAlone/workflow/sandbox"
	"golang.org/x/sys/unix"
)

const (
	pipelineDockerCallTimeout     = 5 * time.Second
	pipelineDockerCleanupTimeout  = 30 * time.Second
	pipelineDockerCleanupLabelKey = "wfctl.pipeline.cleanup"
)

type pipelineDockerClient struct {
	Identity pipelineDockerIdentity

	mu        sync.RWMutex
	identity  pipelineDockerIdentity
	binary    string
	env       []string
	args      []string
	configDir string
	tls       bool
	verifyTLS bool
	tlsFiles  [3]bool
	closed    bool
}

type pipelineDockerContext struct {
	Name      string
	Endpoints map[string]struct {
		Host          string
		SkipTLSVerify bool
	}
	TLSMaterial map[string][]string
	Storage     struct{ TLSPath string }
}

type pipelineDockerProcessGroupKey struct{}

var pipelineDockerTLSNames = [3]string{"ca.pem", "cert.pem", "key.pem"}

func resolvePipelineDockerClient(ctx context.Context) (*pipelineDockerClient, error) {
	// Only discovery receives a snapshot of Docker's transport-selection env.
	// All daemon calls use a private empty config, explicit host/TLS flags, and
	// a minimal env, avoiding auth helpers, config proxies, and context changes.
	snapshot := make(map[string]string)
	for _, item := range os.Environ() {
		key, value, _ := strings.Cut(item, "=")
		snapshot[key] = value
	}
	binary, err := exec.LookPath("docker")
	if err != nil {
		return nil, fmt.Errorf("pipeline Docker CLI unavailable")
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		return nil, fmt.Errorf("pipeline Docker CLI unavailable")
	}
	home := snapshot["HOME"]
	if home == "" {
		return nil, fmt.Errorf("pipeline Docker home unavailable")
	}
	config := snapshot["DOCKER_CONFIG"]
	if config == "" {
		config = filepath.Join(home, ".docker")
	}
	config, err = filepath.Abs(config)
	if err != nil {
		return nil, fmt.Errorf("pipeline Docker config unavailable")
	}
	env := []string{"PATH=" + snapshot["PATH"], "HOME=" + home, "LANG=C", "LC_ALL=C"}
	discoveryEnv := slices.Clone(env)
	for _, key := range []string{"DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_TLS", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH"} {
		if value := snapshot[key]; value != "" {
			discoveryEnv = append(discoveryEnv, key+"="+value)
		}
	}
	out, err := runPipelineDockerCLI(ctx, binary, discoveryEnv, pipelineDockerCallTimeout, "--config", config, "context", "inspect")
	if err != nil {
		return nil, fmt.Errorf("pipeline Docker context resolution failed")
	}
	var contexts []pipelineDockerContext
	if json.Unmarshal([]byte(out.Stdout), &contexts) != nil || len(contexts) != 1 {
		return nil, fmt.Errorf("pipeline Docker context metadata invalid")
	}
	metadata := contexts[0]
	endpoint, ok := metadata.Endpoints["docker"]
	if !ok || metadata.Name == "" {
		return nil, fmt.Errorf("pipeline Docker context endpoint missing")
	}
	if err := validatePipelineDockerEndpoint(endpoint.Host); err != nil {
		return nil, err
	}
	var material [3][]byte
	tlsEnabled := false
	verifyTLS := false
	if strings.HasPrefix(endpoint.Host, "tcp://") {
		if metadata.Name == "default" {
			tlsEnabled = snapshot["DOCKER_TLS"] != "" || snapshot["DOCKER_TLS_VERIFY"] != ""
			verifyTLS = snapshot["DOCKER_TLS_VERIFY"] != ""
			if tlsEnabled {
				certPath := snapshot["DOCKER_CERT_PATH"]
				if certPath == "" {
					certPath = config
				}
				for i, name := range pipelineDockerTLSNames {
					material[i], err = readPipelineDockerFile(filepath.Join(certPath, name), false, false)
					if err != nil {
						return nil, err
					}
				}
			}
		} else {
			names := metadata.TLSMaterial["docker"]
			tlsEnabled = len(names) != 0 || endpoint.SkipTLSVerify
			verifyTLS = tlsEnabled && !endpoint.SkipTLSVerify
			seen := make(map[string]bool)
			for _, name := range names {
				i := slices.Index(pipelineDockerTLSNames[:], name)
				if i < 0 || seen[name] || !filepath.IsAbs(metadata.Storage.TLSPath) {
					return nil, fmt.Errorf("pipeline Docker TLS metadata invalid")
				}
				seen[name] = true
				material[i], err = readPipelineDockerFile(filepath.Join(metadata.Storage.TLSPath, "docker", name), true, false)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	if (len(material[1]) == 0) != (len(material[2]) == 0) {
		return nil, fmt.Errorf("pipeline Docker TLS client material incomplete")
	}
	dir, err := os.MkdirTemp("", "wfctl-pipeline-docker-")
	if err != nil {
		return nil, fmt.Errorf("pipeline Docker private config unavailable")
	}
	c := &pipelineDockerClient{binary: binary, env: env, configDir: dir, tls: tlsEnabled, verifyTLS: verifyTLS}
	success := false
	defer func() {
		if !success {
			_ = c.Close()
		}
	}()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}\n"), 0600); err != nil {
		return nil, fmt.Errorf("pipeline Docker private config unavailable")
	}
	c.args = []string{"--config", dir, "--host", endpoint.Host, "--tls=" + strconv.FormatBool(tlsEnabled)}
	if tlsEnabled {
		c.args = append(c.args, "--tlsverify="+strconv.FormatBool(verifyTLS))
		for i, flag := range []string{"--tlscacert", "--tlscert", "--tlskey"} {
			path := ""
			if len(material[i]) > 0 {
				path = filepath.Join(dir, pipelineDockerTLSNames[i])
				if err := os.WriteFile(path, material[i], 0600); err != nil {
					return nil, fmt.Errorf("pipeline Docker private TLS material unavailable")
				}
				c.tlsFiles[i] = true
			}
			c.args = append(c.args, flag+"="+path)
		}
	}
	serverID, err := c.serverID(ctx)
	if err != nil {
		return nil, err
	}
	c.identity = pipelineDockerIdentity{Endpoint: endpoint.Host, TLSHash: hashPipelineDockerTLS(tlsEnabled, verifyTLS, material), ServerID: serverID}
	c.Identity = c.identity
	success = true
	return c, nil
}

func validatePipelineDockerEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err == nil && u.Scheme == "ssh" {
		return fmt.Errorf("pipeline Docker SSH endpoints are unsupported in record mode")
	}
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(endpoint, "\x00\r\n\t ") {
		return fmt.Errorf("pipeline Docker endpoint invalid")
	}
	switch u.Scheme {
	case "unix":
		if u.Host == "" && filepath.IsAbs(u.Path) && filepath.Clean(u.Path) == u.Path && u.Path != "/" {
			return nil
		}
	case "tcp":
		port, err := strconv.ParseUint(u.Port(), 10, 16)
		if u.Hostname() != "" && err == nil && port > 0 && u.Path == "" {
			return nil
		}
	}
	return fmt.Errorf("pipeline Docker endpoint unsupported or invalid")
}

func readPipelineDockerFile(path string, required, private bool) ([]byte, error) {
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK
	if private {
		flags |= unix.O_NOFOLLOW
	}
	fd, err := unix.Open(path, flags, 0)
	if err != nil {
		if !required && errors.Is(err, unix.ENOENT) {
			return nil, nil
		}
		return nil, fmt.Errorf("pipeline Docker material unreadable")
	}
	file := os.NewFile(uintptr(fd), "pipeline Docker material") // #nosec G115 -- successful unix.Open returns a nonnegative file descriptor.
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || (private && validatePipelinePrivateFile(info, 0600, false) != nil) {
		return nil, fmt.Errorf("pipeline Docker material unsafe")
	}
	data, err := io.ReadAll(io.LimitReader(file, sandbox.MaxOutputBytes+1))
	if err != nil || len(data) > sandbox.MaxOutputBytes || len(data) == 0 {
		return nil, fmt.Errorf("pipeline Docker material invalid")
	}
	return data, nil
}

func hashPipelineDockerTLS(enabled, verify bool, material [3][]byte) string {
	data, _ := json.Marshal(struct {
		TLS      bool
		Verify   bool
		Material [3][]byte
	}{enabled, verify, material})
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func (c *pipelineDockerClient) validateSnapshot() error {
	if c.closed || c.Identity != c.identity || c.identity.ServerID == "" {
		return fmt.Errorf("pipeline Docker client identity unavailable or changed")
	}
	info, err := os.Lstat(c.configDir)
	if err != nil || validatePipelinePrivateFile(info, 0700, true) != nil || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("pipeline Docker private config unsafe")
	}
	config, err := readPipelineDockerFile(filepath.Join(c.configDir, "config.json"), true, true)
	if err != nil || string(config) != "{}\n" {
		return fmt.Errorf("pipeline Docker private config changed")
	}
	var material [3][]byte
	for i, exists := range c.tlsFiles {
		if exists {
			material[i], err = readPipelineDockerFile(filepath.Join(c.configDir, pipelineDockerTLSNames[i]), true, true)
			if err != nil {
				return err
			}
		}
	}
	if hashPipelineDockerTLS(c.tls, c.verifyTLS, material) != c.identity.TLSHash {
		return fmt.Errorf("pipeline Docker TLS identity changed")
	}
	return nil
}

func (c *pipelineDockerClient) serverID(ctx context.Context) (string, error) {
	result, err := c.raw(ctx, pipelineDockerCallTimeout, "info", "--format", "{{json .ID}}")
	if err != nil {
		return "", fmt.Errorf("pipeline Docker daemon identity unavailable")
	}
	var id string
	if json.Unmarshal([]byte(result.Stdout), &id) != nil || len(id) == 0 || len(id) > 256 {
		return "", fmt.Errorf("pipeline Docker daemon server ID invalid")
	}
	for _, char := range id {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && !strings.ContainsRune("-_:.", char) {
			return "", fmt.Errorf("pipeline Docker daemon server ID invalid")
		}
	}
	return id, nil
}

func (c *pipelineDockerClient) raw(ctx context.Context, timeout time.Duration, args ...string) (*sandbox.ExecResult, error) {
	return runPipelineDockerCLI(ctx, c.binary, c.env, timeout, append(slices.Clone(c.args), args...)...)
}

// RunInProcessGroup keeps broker CLI processes and their helpers in the child's
// already-journaled group. Restart reconciliation must kill that group before
// probing the daemon, including when the parent died during Docker create.
func (c *pipelineDockerClient) RunInProcessGroup(ctx context.Context, process pipelineProcessIdentity, args ...string) (*sandbox.ExecResult, error) {
	if err := validatePipelineDockerProcessGroup(process); err != nil {
		return nil, err
	}
	return c.Run(context.WithValue(ctx, pipelineDockerProcessGroupKey{}, process), args...)
}

func validatePipelineDockerProcessGroup(process pipelineProcessIdentity) error {
	if process.PID <= 0 || process.PGID != process.PID || process.Start == "" {
		return fmt.Errorf("pipeline Docker recorded process identity invalid")
	}
	token, err := pipelineProcessStartToken(process.PID)
	if err != nil || token != process.Start {
		return fmt.Errorf("pipeline Docker recorded process identity unavailable or changed")
	}
	group, err := unix.Getpgid(process.PID)
	if err != nil || group != process.PGID {
		return fmt.Errorf("pipeline Docker recorded process group mismatch")
	}
	active, err := pipelineProcessGroupActive(process.PGID)
	if err != nil || !active {
		return fmt.Errorf("pipeline Docker recorded process group unavailable")
	}
	return nil
}

// Run revalidates the snapshot and server ID before each operation. No caller
// may override global transport flags; '--' separates a create command payload.
func (c *pipelineDockerClient) Run(ctx context.Context, args ...string) (*sandbox.ExecResult, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if err := c.validateSnapshot(); err != nil {
		return nil, err
	}
	if len(args) == 0 || !slices.Contains([]string{"create", "start", "attach", "wait", "ps", "inspect", "rm"}, args[0]) {
		return nil, fmt.Errorf("pipeline Docker operation unsupported")
	}
	if _, grouped := ctx.Value(pipelineDockerProcessGroupKey{}).(pipelineProcessIdentity); args[0] == "create" && !grouped {
		return nil, fmt.Errorf("pipeline Docker create requires a recorded process group")
	}
	if args[0] != "create" && args[0] != "ps" && !validPipelineDockerID(args[len(args)-1]) {
		return nil, fmt.Errorf("pipeline Docker operation requires a full container ID")
	}
	for _, arg := range args[1:] {
		if arg == "--" {
			break
		}
		flag, _, _ := strings.Cut(arg, "=")
		if strings.HasPrefix(arg, "-H") || strings.HasPrefix(arg, "-c") ||
			slices.Contains([]string{"--host", "--context", "--config", "--tls", "--tlsverify", "--tlscacert", "--tlscert", "--tlskey"}, flag) {
			return nil, fmt.Errorf("pipeline Docker transport override forbidden")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, sandbox.RecordMaxTimeout)
	defer cancel()
	id, err := c.serverID(ctx)
	if err != nil {
		return nil, err
	}
	if id != c.identity.ServerID {
		return nil, fmt.Errorf("pipeline Docker daemon identity mismatch")
	}
	timeout := pipelineDockerCallTimeout
	if slices.Contains([]string{"start", "attach", "wait"}, args[0]) {
		timeout = sandbox.RecordMaxTimeout
	}
	return c.raw(ctx, timeout, args...)
}

type pipelineDockerOutput struct {
	buffer   bytes.Buffer
	overflow bool
}

func (w *pipelineDockerOutput) Write(data []byte) (int, error) {
	count := len(data)
	remaining := sandbox.MaxOutputBytes - w.buffer.Len()
	if len(data) > remaining {
		w.overflow = true
		data = data[:remaining]
	}
	_, _ = w.buffer.Write(data)
	return count, nil
}

func runPipelineDockerCLI(ctx context.Context, binary string, env []string, timeout time.Duration, args ...string) (*sandbox.ExecResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	process, grouped := ctx.Value(pipelineDockerProcessGroupKey{}).(pipelineProcessIdentity)
	if grouped {
		if err := validatePipelineDockerProcessGroup(process); err != nil {
			return nil, err
		}
	}
	// Docker CLI newCIDFile uses os.Create (cli/command/container/create.go).
	// Set only the CLI umask; exec preserves PID/PGID and literal positional argv.
	argv := append([]string{"-c", `umask 077; exec "$@"`, "wfctl-docker", binary}, args...)
	cmd := exec.CommandContext(ctx, "/bin/sh", argv...) // #nosec G204,G702 -- fixed shell program; values remain positional argv.
	cmd.Env = env
	cmd.WaitDelay = 250 * time.Millisecond
	// Broker helpers belong to recorded child custody; discovery/cleanup helpers
	// get a temporary group. Never kill the recorded group from a CLI cancel.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if grouped {
		cmd.SysProcAttr.Pgid = process.PGID
	}
	cmd.Cancel = func() error {
		if grouped {
			return cmd.Process.Kill()
		}
		err := unix.Kill(-cmd.Process.Pid, unix.SIGKILL)
		if errors.Is(err, unix.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	var stdout, stderr pipelineDockerOutput
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if cmd.Process != nil && !grouped {
		_ = unix.Kill(-cmd.Process.Pid, unix.SIGKILL)
	}
	result := &sandbox.ExecResult{ExitCode: -1, Stdout: stdout.buffer.String(), Stderr: stderr.buffer.String()}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if ctx.Err() != nil {
		return result, fmt.Errorf("pipeline Docker operation canceled or timed out")
	}
	if stdout.overflow || stderr.overflow {
		return result, fmt.Errorf("pipeline Docker output exceeded capture limit")
	}
	if err != nil {
		return result, fmt.Errorf("pipeline Docker operation failed")
	}
	return result, nil
}

func validPipelineDockerID(id string) bool {
	if len(id) != 64 || strings.ToLower(id) != id {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func (c *pipelineDockerClient) list(ctx context.Context, filter string) ([]string, error) {
	result, err := c.Run(ctx, "ps", "--all", "--quiet", "--no-trunc", "--filter", filter)
	if err != nil {
		return nil, err
	}
	if result.Stdout == "" {
		return nil, nil
	}
	ids := strings.Split(strings.TrimSuffix(result.Stdout, "\n"), "\n")
	seen := make(map[string]bool)
	for _, id := range ids {
		if !validPipelineDockerID(id) || seen[id] {
			return nil, fmt.Errorf("pipeline Docker container list invalid")
		}
		seen[id] = true
	}
	return ids, nil
}

func (c *pipelineDockerClient) listID(ctx context.Context, id string) ([]string, error) {
	ids, err := c.list(ctx, "id="+id)
	if err != nil {
		return nil, err
	}
	if len(ids) > 1 || len(ids) == 1 && ids[0] != id {
		return nil, fmt.Errorf("pipeline Docker exact ID query mismatch")
	}
	return ids, nil
}

func (c *pipelineDockerClient) verifyContainer(ctx context.Context, id, label string) error {
	result, err := c.Run(ctx, "inspect", "--type", "container", "--format", `{"id":{{json .Id}},"labels":{{json .Config.Labels}}}`, id)
	if err != nil {
		return err
	}
	var inspected struct {
		ID     string            `json:"id"`
		Labels map[string]string `json:"labels"`
	}
	if json.Unmarshal([]byte(result.Stdout), &inspected) != nil || inspected.ID != id || inspected.Labels[pipelineDockerCleanupLabelKey] != label {
		return fmt.Errorf("pipeline Docker container identity or cleanup label mismatch")
	}
	return nil
}

// Cleanup never mutates journal/CID files. Only the parent can retire them after
// this exact-ID and exact-label reconciliation has successfully read back empty.
func (c *pipelineDockerClient) Cleanup(ctx context.Context, entry pipelineCleanupEntry) error {
	if entry.Docker != c.Identity || !validPipelineCleanupLabel(entry.Label) {
		return fmt.Errorf("pipeline Docker cleanup identity mismatch")
	}
	ids := make(map[string]bool)
	for _, id := range entry.IDs {
		if !validPipelineDockerID(id) || ids[id] {
			return fmt.Errorf("pipeline Docker journaled ID invalid")
		}
		ids[id] = true
	}
	ctx, cancel := context.WithTimeout(ctx, pipelineDockerCleanupTimeout)
	defer cancel()
	filter := "label=" + pipelineDockerCleanupLabelKey + "=" + entry.Label
	labelIDs, err := c.list(ctx, filter)
	if err != nil {
		return err
	}
	for _, id := range labelIDs {
		ids[id] = true
	}
	all := make([]string, 0, len(ids))
	for id := range ids {
		all = append(all, id)
	}
	slices.Sort(all)
	var present []string
	for _, id := range all {
		found, err := c.listID(ctx, id)
		if err != nil {
			return err
		}
		if len(found) != 0 {
			if err := c.verifyContainer(ctx, id, entry.Label); err != nil {
				return err
			}
			present = append(present, id)
		}
	}
	// Validate all ownership before removing any ID, including journaled IDs
	// that no longer match the label query. Labels on a full container ID are immutable.
	for _, id := range present {
		if _, err := c.Run(ctx, "rm", "--force", "--", id); err != nil {
			return err
		}
	}
	remaining, err := c.list(ctx, filter)
	if err != nil {
		return err
	}
	if len(remaining) != 0 {
		return fmt.Errorf("pipeline Docker cleanup label readback nonempty")
	}
	for _, id := range all {
		remaining, err := c.listID(ctx, id)
		if err != nil {
			return err
		}
		if len(remaining) != 0 {
			return fmt.Errorf("pipeline Docker cleanup ID readback nonempty")
		}
	}
	return nil
}

// CreateArgs returns arguments after 'create', for RunInProcessGroup.
func (c *pipelineDockerClient) CreateArgs(cfg sandbox.SandboxConfig, command []string, label, cidfile string) ([]string, error) {
	if err := sandbox.ValidateRecordConfig(cfg); err != nil {
		return nil, err
	}
	if !validPipelineCleanupLabel(label) || !filepath.IsAbs(cidfile) || filepath.Clean(cidfile) != cidfile ||
		strings.ContainsAny(cidfile, "\x00\r\n") || !strings.HasPrefix(filepath.Base(cidfile), label+"-") || !strings.HasSuffix(cidfile, ".cid") {
		return nil, fmt.Errorf("pipeline Docker cleanup label or CID path invalid")
	}
	if len(command) == 0 || command[0] == "" {
		return nil, fmt.Errorf("pipeline Docker command required")
	}
	for _, arg := range command {
		if strings.ContainsRune(arg, '\x00') {
			return nil, fmt.Errorf("pipeline Docker command invalid")
		}
	}
	args := []string{"--label", pipelineDockerCleanupLabelKey + "=" + label, "--cidfile", cidfile,
		"--user", cfg.User, "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--pids-limit", strconv.FormatInt(cfg.PidsLimit, 10), "--memory", strconv.FormatInt(cfg.MemoryLimit, 10),
		"--cpus", strconv.FormatFloat(cfg.CPULimit, 'f', -1, 64), "--network", cfg.NetworkMode}
	if cfg.WorkDir != "" {
		args = append(args, "--workdir", cfg.WorkDir)
	}
	keys := make([]string, 0, len(cfg.Env))
	for key := range cfg.Env {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		args = append(args, "--env", key+"="+cfg.Env[key])
	}
	keys = keys[:0]
	for key := range cfg.Tmpfs {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		args = append(args, "--tmpfs", key+":"+cfg.Tmpfs[key])
	}
	args = append(args, "--", cfg.Image)
	return append(args, command...), nil
}

func (c *pipelineDockerClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.configDir != "" {
		if err := os.RemoveAll(c.configDir); err != nil {
			return fmt.Errorf("pipeline Docker private config cleanup failed")
		}
	}
	return nil
}
