package main

import (
	"flag"
	"fmt"
)

// runDev dispatches wfctl dev subcommands.
func runDev(args []string) error {
	if len(args) < 1 {
		return devUsage()
	}
	switch args[0] {
	case "up":
		return runDevUp(args[1:])
	case "down":
		return runDevDown(args[1:])
	case "logs":
		return runDevLogs(args[1:])
	case "status":
		return runDevStatus(args[1:])
	case "restart":
		return runDevRestart(args[1:])
	default:
		return devUsage()
	}
}

func devUsage() error {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl dev <action> [options]

Manage a local development cluster for a workflow application.

Actions:
  up       Start local development cluster (default: docker-compose mode)
  down     Stop local development cluster
  logs     Stream logs from services
  status   Show status of running services
  restart  Restart a service

Mode flags (for 'up'):
  --local  Run app services as local Go processes with hot-reload
  --k8s    Deploy to local minikube cluster

Exposure flags (for 'up'):
  --expose tailscale    Expose services via Tailscale Funnel
  --expose cloudflare   Expose services via Cloudflare Tunnel
  --expose ngrok        Expose services via ngrok

Options:
  --config <file>    Workflow config file (default: app.yaml)
  --service <name>   Target a specific service (for logs/restart)
  --follow           Follow log output (for logs)
  --verbose          Show detailed output

Examples:
  wfctl dev up
  wfctl dev up --local
  wfctl dev up --k8s
  wfctl dev up --expose tailscale
  wfctl dev down
  wfctl dev logs --follow
  wfctl dev status
  wfctl dev restart --service api
`)
	return fmt.Errorf("missing or unknown action")
}

func runDevUp(args []string) error {
	fs := flag.NewFlagSet("dev up", flag.ContinueOnError)
	configFile := fs.String("config", "", "Workflow config file")
	local := fs.Bool("local", false, "Run app services as local Go processes with hot-reload")
	k8s := fs.Bool("k8s", false, "Deploy to local minikube cluster")
	expose := fs.String("expose", "", "Exposure method: tailscale, cloudflare, ngrok")
	verbose := fs.Bool("verbose", false, "Show detailed output")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl dev up [options]\n\nStart local development cluster.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfgPath, err := resolveConfigFile(*configFile, fs.Args())
	if err != nil {
		return err
	}

	cfg, err := loadDevConfig(cfgPath)
	if err != nil {
		return err
	}

	// Determine exposure from flag or config.
	exposeMethod := *expose
	if exposeMethod == "" && cfg.Environments != nil {
		if localEnv, ok := cfg.Environments["local"]; ok && localEnv.Exposure != nil {
			exposeMethod = localEnv.Exposure.Method
		}
	}

	// Build local artifacts before starting services.
	if err := runDevBuild(cfgPath, "local"); err != nil {
		return fmt.Errorf("dev build: %w", err)
	}

	switch {
	case *k8s:
		if err := runDevK8s(cfg, *verbose); err != nil {
			return err
		}
	case *local:
		if err := runDevProcess(cfg, *verbose); err != nil {
			return err
		}
	default:
		if err := runDevCompose(cfg, cfgPath, *verbose); err != nil {
			return err
		}
	}

	// Run exposure after services are up.
	if exposeMethod != "" {
		services := collectExposedServices(cfg)
		var exposeErr error
		switch exposeMethod {
		case "tailscale":
			var tsCfg *TailscaleExposeCfg
			if cfg.Environments != nil {
				if localEnv, ok := cfg.Environments["local"]; ok && localEnv.Exposure != nil && localEnv.Exposure.Tailscale != nil {
					tsCfg = &TailscaleExposeCfg{
						Funnel:   localEnv.Exposure.Tailscale.Funnel,
						Hostname: localEnv.Exposure.Tailscale.Hostname,
					}
				}
			}
			exposeErr = exposeTailscale(services, tsCfg)
		case "cloudflare":
			exposeErr = exposeCloudflare(services, nil)
		case "ngrok":
			exposeErr = exposeNgrok(services)
		default:
			exposeErr = fmt.Errorf("unknown expose method %q (supported: tailscale, cloudflare, ngrok)", exposeMethod)
		}
		if exposeErr != nil {
			return exposeErr
		}
	}

	return nil
}

func runDevDown(args []string) error {
	fs := flag.NewFlagSet("dev down", flag.ContinueOnError)
	configFile := fs.String("config", "", "Workflow config file")
	k8s := fs.Bool("k8s", false, "Delete minikube dev namespace")
	verbose := fs.Bool("verbose", false, "Show detailed output")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl dev down [options]\n\nStop local development cluster.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	_ = *configFile

	if *k8s {
		return devK8sDown(*verbose)
	}
	return devComposeDown(*verbose)
}

func runDevLogs(args []string) error {
	fs := flag.NewFlagSet("dev logs", flag.ContinueOnError)
	service := fs.String("service", "", "Service name to stream logs from")
	follow := fs.Bool("follow", false, "Follow log output")
	k8s := fs.Bool("k8s", false, "Stream logs from minikube pods")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl dev logs [options]\n\nStream logs from running services.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *k8s {
		return devK8sLogs(*service, *follow)
	}
	return devComposeLogs(*service, *follow)
}

func runDevStatus(args []string) error {
	fs := flag.NewFlagSet("dev status", flag.ContinueOnError)
	k8s := fs.Bool("k8s", false, "Show minikube pod status")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl dev status [options]\n\nShow status of running services.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *k8s {
		return devK8sStatus()
	}
	return devComposeStatus()
}

func runDevRestart(args []string) error {
	fs := flag.NewFlagSet("dev restart", flag.ContinueOnError)
	service := fs.String("service", "", "Service name to restart")
	k8s := fs.Bool("k8s", false, "Restart in minikube")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl dev restart [options]\n\nRestart a service.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *k8s {
		return devK8sRestart(*service)
	}
	return devComposeRestart(*service)
}
