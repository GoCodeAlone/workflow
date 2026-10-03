//go:build linux || darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

type transportConfig struct {
	Root        string `json:"root"`
	BlockCreate bool   `json:"block_create"`
	LateCreate  bool   `json:"late_create"`
	Unavailable bool   `json:"unavailable"`
}

const transportID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// This dependency records Docker transport events, not container execution.
// Gates wait for explicit test releases; no lifecycle race depends on a sleep.
func runRecordDockerTransport(args []string) bool {
	binary, err := os.Executable()
	if err != nil {
		os.Exit(65)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(binary), "transport.json"))
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	var cfg transportConfig
	if err != nil || json.Unmarshal(data, &cfg) != nil || !filepath.IsAbs(cfg.Root) {
		os.Exit(65)
	}
	log, err := os.OpenFile(filepath.Join(cfg.Root, "calls.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(65)
	}
	err = json.NewEncoder(log).Encode(map[string]any{"pid": os.Getpid(), "pgid": syscall.Getpgrp(), "args": args})
	if errors.Join(err, log.Sync(), log.Close()) != nil {
		os.Exit(65)
	}
	if args[0] == "context" {
		return false
	}
	if args[0] == "info" {
		if cfg.Unavailable {
			fmt.Fprintln(os.Stderr, "fixture-unavailable-daemon-private-canary")
			os.Exit(7)
		}
		return false
	}
	state, err := readTransportState(cfg.Root)
	if err != nil {
		os.Exit(65)
	}
	switch args[0] {
	case "create":
		label := strings.TrimPrefix(transportFlag(args, "--label"), "wfctl.pipeline.cleanup=")
		cidfile := transportFlag(args, "--cidfile")
		if label == "" || cidfile == "" {
			os.Exit(65)
		}
		if cfg.LateCreate {
			helper := exec.Command(binary, "--record-fixture-late-create", cfg.Root, label, cidfile)
			helper.Stdout, helper.Stderr = os.Stdout, os.Stderr
			if err := helper.Start(); err != nil {
				os.Exit(65)
			}
			go func() { _ = helper.Wait() }()
		}
		if cfg.BlockCreate {
			if writeTransportJSON(filepath.Join(cfg.Root, "create.ready"), map[string]any{"pid": os.Getpid(), "pgid": syscall.Getpgrp(), "label": label}) != nil ||
				waitTransportRelease(filepath.Join(cfg.Root, "create.release")) != nil {
				os.Exit(65)
			}
		}
		if createTransportContainer(cfg.Root, label, cidfile) != nil {
			os.Exit(65)
		}
		fmt.Println(transportID)
	case "start", "wait":
		if len(args) < 2 || state[args[len(args)-1]] == "" {
			os.Exit(65)
		}
		if args[0] == "start" {
			fmt.Print("captured output")
		} else {
			fmt.Println("0")
		}
	case "ps":
		filter := transportFlag(args, "--filter")
		var ids []string
		for id, label := range state {
			if filter == "id="+id || filter == "label=wfctl.pipeline.cleanup="+label {
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
		for _, id := range ids {
			fmt.Println(id)
		}
	case "inspect":
		id := args[len(args)-1]
		label, exists := state[id]
		if !exists {
			os.Exit(1)
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"id": id, "labels": map[string]string{"wfctl.pipeline.cleanup": label}})
	case "rm":
		delete(state, args[len(args)-1])
		if writeTransportJSON(filepath.Join(cfg.Root, "containers.json"), state) != nil {
			os.Exit(65)
		}
	default:
		os.Exit(64)
	}
	return true
}

func transportFlag(args []string, name string) string {
	for i, arg := range args {
		if value, ok := strings.CutPrefix(arg, name+"="); ok {
			return value
		}
		if arg == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func readTransportState(root string) (map[string]string, error) {
	state := map[string]string{}
	data, err := os.ReadFile(filepath.Join(root, "containers.json"))
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return nil, err
	}
	return state, json.Unmarshal(data, &state)
}

func createTransportContainer(root, label, cidfile string) error {
	state, err := readTransportState(root)
	if err != nil {
		return err
	}
	state[transportID] = label
	if err := writeTransportJSON(filepath.Join(root, "containers.json"), state); err != nil {
		return err
	}
	return os.WriteFile(cidfile, []byte(transportID+"\n"), 0600)
}

func writeTransportJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".fixture-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, err = file.Write(append(data, '\n'))
	if err := errors.Join(err, file.Sync(), file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func waitTransportRelease(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func runRecordDockerLateCreate() {
	if len(os.Args) != 5 {
		os.Exit(65)
	}
	root, label, cidfile := os.Args[2], os.Args[3], os.Args[4]
	signal.Ignore(syscall.SIGTERM)
	if writeTransportJSON(filepath.Join(root, "late.ready"), map[string]any{"pid": os.Getpid(), "pgid": syscall.Getpgrp(), "label": label}) != nil ||
		waitTransportRelease(filepath.Join(root, "late.release")) != nil {
		os.Exit(65)
	}
	if createTransportContainer(root, label, cidfile) != nil ||
		writeTransportJSON(filepath.Join(root, "late.created"), map[string]any{"pid": os.Getpid(), "label": label}) != nil {
		os.Exit(65)
	}
}
