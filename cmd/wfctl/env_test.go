package main

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestRunEnvUsage(t *testing.T) {
	var out bytes.Buffer
	err := runEnvWithOutput([]string{}, &out)
	if err == nil || !strings.Contains(out.String(), "Usage: wfctl env") {
		t.Fatalf("expected usage, got err=%v out=%s", err, out.String())
	}
}

func TestRunEnvHelp(t *testing.T) {
	var out bytes.Buffer
	err := runEnvWithOutput([]string{"-h"}, &out)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v, want flag.ErrHelp", err)
	}
	text := out.String()
	for _, want := range []string{"Usage: wfctl env", "setup", "environment input setup"} {
		if !strings.Contains(text, want) {
			t.Fatalf("help missing %q:\n%s", want, text)
		}
	}
}

func TestRunEnvSetupHelp(t *testing.T) {
	var out bytes.Buffer
	err := runEnvWithOutput([]string{"setup", "-h"}, &out)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v, want flag.ErrHelp", err)
	}
	text := out.String()
	for _, want := range []string{"Usage: wfctl env setup", "--kind", "secret", "var"} {
		if !strings.Contains(text, want) {
			t.Fatalf("setup help missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "manifest setup") {
		t.Fatalf("setup help must not use manifest setup wording:\n%s", text)
	}
}

func TestEmbeddedCLIRegistersEnv(t *testing.T) {
	if _, ok := commands["env"]; !ok {
		t.Fatal("commands does not register env")
	}
	cfg, err := config.LoadFromBytes(wfctlConfigBytes)
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	workflow, ok := cfg.Workflows["cli"].(map[string]any)
	if !ok {
		t.Fatal("cli workflow missing")
	}
	commands, ok := workflow["commands"].([]any)
	if !ok {
		t.Fatalf("commands has type %T", workflow["commands"])
	}
	for _, command := range commands {
		entry, ok := command.(map[string]any)
		if ok && entry["name"] == "env" {
			return
		}
	}
	t.Fatal("embedded CLI config does not list env")
}
