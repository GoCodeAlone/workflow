//go:build linux || darwin

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTransportCreateWritesOwnedStateAndCID(t *testing.T) {
	root := t.TempDir()
	cidfile := filepath.Join(root, "fixture.cid")
	if err := createTransportContainer(root, "fixture-label", cidfile); err != nil {
		t.Fatal(err)
	}
	state, err := readTransportState(root)
	if err != nil || len(state) != 1 || state[transportID] != "fixture-label" {
		t.Fatalf("transport lost its full ID/label state: %v, %v", state, err)
	}
	data, err := os.ReadFile(cidfile)
	if err != nil || string(data) != transportID+"\n" {
		t.Fatalf("transport CID differs from create output: %q, %v", data, err)
	}
}

func TestTransportReleaseUsesExplicitEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "release")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := waitTransportRelease(path); err != nil {
		t.Fatalf("existing release event was ignored: %v", err)
	}
}

func TestTransportFlagForms(t *testing.T) {
	for _, args := range [][]string{
		{"create", "--label", "wfctl.pipeline.cleanup=fixture-label", "--cidfile", "fixture.cid"},
		{"create", "--label=wfctl.pipeline.cleanup=fixture-label", "--cidfile=fixture.cid"},
	} {
		if got := transportFlag(args, "--label"); got != "wfctl.pipeline.cleanup=fixture-label" {
			t.Fatalf("label argument was not preserved: %q", got)
		}
		if got := transportFlag(args, "--cidfile"); got != "fixture.cid" {
			t.Fatalf("CID argument was not preserved: %q", got)
		}
	}
}
