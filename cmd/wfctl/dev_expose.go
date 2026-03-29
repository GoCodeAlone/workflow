package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/config"
)

// ExposedService describes a locally running service that should be exposed.
type ExposedService struct {
	Name string
	Port int
}

// TailscaleExposeCfg is a local mirror of config.TailscaleConfig for the expose layer.
type TailscaleExposeCfg struct {
	Funnel   bool
	Hostname string
}

// collectExposedServices returns the list of services with exposed ports from cfg.
func collectExposedServices(cfg *config.WorkflowConfig) []ExposedService {
	var services []ExposedService

	if len(cfg.Services) > 0 {
		for name, svc := range cfg.Services {
			if svc == nil {
				continue
			}
			for _, exp := range svc.Expose {
				services = append(services, ExposedService{Name: name, Port: exp.Port})
			}
		}
		return services
	}

	// Single-service: scan http.server modules.
	for _, mod := range cfg.Modules {
		if mod.Type == "http.server" || mod.Type == "http.router" || mod.Type == "http.gateway" {
			port, _ := extractModulePort(mod)
			if port > 0 {
				services = append(services, ExposedService{Name: "app", Port: port})
			}
		}
	}
	return services
}

// ── Tailscale Funnel ─────────────────────────────────────────────────────────

// exposeTailscale exposes services via Tailscale Funnel.
func exposeTailscale(services []ExposedService, cfg *TailscaleExposeCfg) error {
	if _, err := exec.LookPath("tailscale"); err != nil {
		return fmt.Errorf("tailscale not found in PATH; install from https://tailscale.com/download")
	}

	if len(services) == 0 {
		return fmt.Errorf("no services with exposed ports found")
	}

	// Determine whether to use funnel or serve.
	useFunnel := cfg == nil || cfg.Funnel // default to funnel when cfg is nil

	for _, svc := range services {
		var args []string
		if useFunnel {
			args = []string{"funnel", "--bg", fmt.Sprintf("%d", svc.Port)}
		} else {
			args = []string{"serve", "--bg", fmt.Sprintf("%d", svc.Port)}
		}

		cmd := exec.Command("tailscale", args...) //nolint:gosec
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		fmt.Printf("[expose/tailscale] Exposing %s (port %d) via Tailscale Funnel...\n", svc.Name, svc.Port)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("tailscale funnel port %d: %w", svc.Port, err)
		}

		// Print the Tailscale hostname.
		hostname, err := tailscaleHostname()
		if err == nil && hostname != "" {
			fmt.Printf("[expose/tailscale] %s → https://%s\n", svc.Name, hostname)
		}
	}
	return nil
}

// tailscaleHostname retrieves the machine's Tailscale hostname via the CLI.
func tailscaleHostname() (string, error) {
	out, err := exec.Command("tailscale", "status", "--json").Output() //nolint:gosec
	if err != nil {
		return "", err
	}
	var status struct {
		Self struct {
			DNSName string `json:"DNSName"`
		} `json:"Self"`
	}
	if err := json.Unmarshal(out, &status); err != nil {
		return "", err
	}
	// DNSName ends with a trailing dot; strip it.
	return strings.TrimSuffix(status.Self.DNSName, "."), nil
}

// ── Cloudflare Tunnel ────────────────────────────────────────────────────────

// CloudflareTunnelExposeCfg holds Cloudflare Tunnel settings.
type CloudflareTunnelExposeCfg struct {
	TunnelName string
	Domain     string
}

// exposeCloudflare exposes services via Cloudflare Tunnel (cloudflared).
func exposeCloudflare(services []ExposedService, cfg *CloudflareTunnelExposeCfg) error {
	if _, err := exec.LookPath("cloudflared"); err != nil {
		return fmt.Errorf("cloudflared not found in PATH; install from https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/")
	}

	if len(services) == 0 {
		return fmt.Errorf("no services with exposed ports found")
	}

	for _, svc := range services {
		url := fmt.Sprintf("http://localhost:%d", svc.Port)
		args := []string{"tunnel", "--url", url}

		// If a named tunnel is configured, use it.
		if cfg != nil && cfg.TunnelName != "" {
			args = []string{"tunnel", "run", cfg.TunnelName}
		}

		fmt.Printf("[expose/cloudflare] Starting Cloudflare Tunnel for %s (port %d)...\n", svc.Name, svc.Port)
		cmd := exec.Command("cloudflared", args...) //nolint:gosec
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		// Run in background.
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("cloudflared tunnel port %d: %w", svc.Port, err)
		}
		fmt.Printf("[expose/cloudflare] %s tunnel started (pid %d). Check output above for URL.\n", svc.Name, cmd.Process.Pid)
	}
	return nil
}

// ── ngrok ────────────────────────────────────────────────────────────────────

// exposeNgrok exposes services via ngrok.
func exposeNgrok(services []ExposedService) error {
	if _, err := exec.LookPath("ngrok"); err != nil {
		return fmt.Errorf("ngrok not found in PATH; install from https://ngrok.com/download")
	}

	if len(services) == 0 {
		return fmt.Errorf("no services with exposed ports found")
	}

	for _, svc := range services {
		args := []string{"http", fmt.Sprintf("%d", svc.Port)}
		fmt.Printf("[expose/ngrok] Starting ngrok tunnel for %s (port %d)...\n", svc.Name, svc.Port)
		cmd := exec.Command("ngrok", args...) //nolint:gosec
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("ngrok port %d: %w", svc.Port, err)
		}

		// Give ngrok a moment to start, then fetch the public URL from its API.
		time.Sleep(500 * time.Millisecond)
		if url, err := ngrokPublicURL(); err == nil {
			fmt.Printf("[expose/ngrok] %s → %s\n", svc.Name, url)
		} else {
			fmt.Printf("[expose/ngrok] %s tunnel started (pid %d). Check ngrok dashboard for URL.\n", svc.Name, cmd.Process.Pid)
		}
	}
	return nil
}

// ngrokPublicURL fetches the public URL from the ngrok local API.
func ngrokPublicURL() (string, error) {
	resp, err := http.Get("http://localhost:4040/api/tunnels") //nolint:gosec,noctx
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var result struct {
		Tunnels []struct {
			PublicURL string `json:"public_url"`
			Proto     string `json:"proto"`
		} `json:"tunnels"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	for _, t := range result.Tunnels {
		if strings.HasPrefix(t.PublicURL, "https://") {
			return t.PublicURL, nil
		}
	}
	if len(result.Tunnels) > 0 {
		return result.Tunnels[0].PublicURL, nil
	}
	return "", fmt.Errorf("no tunnels found")
}
