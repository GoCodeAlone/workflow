package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCIContainerTarget_UnmarshalAllFields(t *testing.T) {
	src := `
ci:
  build:
    containers:
      - name: server
        method: dockerfile
        dockerfile: server/Dockerfile
        context: .
        registry: docr
        tag: "latest"
        platforms: [linux/amd64, linux/arm64]
        build_args:
          GO_VERSION: "1.26"
        secrets:
          - id: releases_token
            env: RELEASES_TOKEN
        cache:
          from:
            - ref: ghcr.io/org/cache:latest
          to:
            - ref: ghcr.io/org/cache:latest
        target: builder
        labels:
          org.opencontainers.image.source: https://github.com/GoCodeAlone/workflow
        extra_flags: ["--no-cache"]
        push_to: [docr, ghcr]
      - name: ko-server
        method: ko
        ko_package: github.com/GoCodeAlone/workflow/cmd/server
        ko_base_image: gcr.io/distroless/static:nonroot
        ko_bare: true
        push_to: [docr]
      - name: external
        external: true
        source:
          ref: ghcr.io/upstream/image:latest
        push_to: [docr]
`
	var cfg WorkflowConfig
	if err := yaml.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.CI == nil || cfg.CI.Build == nil || len(cfg.CI.Build.Containers) != 3 {
		t.Fatalf("expected 3 containers, got %v", cfg.CI)
	}

	server := cfg.CI.Build.Containers[0]
	if server.Method != "dockerfile" {
		t.Errorf("want method=dockerfile, got %q", server.Method)
	}
	if len(server.Platforms) != 2 {
		t.Errorf("want 2 platforms, got %d", len(server.Platforms))
	}
	if server.BuildArgs["GO_VERSION"] != "1.26" {
		t.Errorf("want build_args GO_VERSION=1.26, got %v", server.BuildArgs)
	}
	if len(server.Secrets) != 1 || server.Secrets[0].ID != "releases_token" {
		t.Errorf("unexpected secrets: %+v", server.Secrets)
	}
	if server.Cache == nil || len(server.Cache.From) != 1 {
		t.Errorf("unexpected cache: %+v", server.Cache)
	}
	if server.Target != "builder" {
		t.Errorf("want target=builder, got %q", server.Target)
	}
	if len(server.Labels) != 1 {
		t.Errorf("want 1 label, got %d", len(server.Labels))
	}
	if len(server.ExtraFlags) != 1 || server.ExtraFlags[0] != "--no-cache" {
		t.Errorf("unexpected extra_flags: %v", server.ExtraFlags)
	}
	if len(server.PushTo) != 2 {
		t.Errorf("want 2 push_to, got %d", len(server.PushTo))
	}

	ko := cfg.CI.Build.Containers[1]
	if ko.Method != "ko" {
		t.Errorf("want method=ko, got %q", ko.Method)
	}
	if ko.KoPackage != "github.com/GoCodeAlone/workflow/cmd/server" {
		t.Errorf("unexpected ko_package: %q", ko.KoPackage)
	}
	if !ko.KoBare {
		t.Errorf("want ko_bare=true")
	}

	ext := cfg.CI.Build.Containers[2]
	if !ext.External {
		t.Errorf("want external=true")
	}
	if ext.Source == nil || ext.Source.Ref != "ghcr.io/upstream/image:latest" {
		t.Errorf("unexpected source: %+v", ext.Source)
	}
}
