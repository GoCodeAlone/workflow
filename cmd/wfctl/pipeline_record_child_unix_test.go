//go:build linux || darwin

package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	"golang.org/x/sys/unix"
)

const pipelineCustodyFixtureMarker = "--wfctl-child-custody-fixture"

func pipelineCustodyFixtureCommand(t *testing.T, role, root, mode string) *exec.Cmd {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestPipelineRecordChildCustodyHelper$", "--", pipelineCustodyFixtureMarker, role, root, mode)
	// Synthetic helpers must not linger in the race runtime's exit sleep
	// after custody has returned. Keep instrumentation and reporting enabled.
	cmd.Env = append(os.Environ(), "GORACE="+os.Getenv("GORACE")+" atexit_sleep_ms=0")
	return cmd
}

func TestPipelineRecordChildCustodyFixtureRaceOptions(t *testing.T) {
	const options = "exitcode=77 halt_on_error=1 atexit_sleep_ms=1000"
	t.Setenv("GORACE", options)
	cmd := pipelineCustodyFixtureCommand(t, "child", t.TempDir(), "empty")
	for _, variable := range cmd.Environ() {
		if value, ok := strings.CutPrefix(variable, "GORACE="); ok {
			if value != options+" atexit_sleep_ms=0" {
				t.Fatalf("fixture changed race reporting options: %q", value)
			}
			return
		}
	}
	t.Fatal("fixture did not normalize its race-runtime exit sleep")
}

func writePipelineCustodyIdentity(t *testing.T, path string, pid int) pipelineProcessIdentity {
	t.Helper()
	start, err := pipelineProcessStartToken(pid)
	if err != nil {
		t.Fatal(err)
	}
	pgid, err := unix.Getpgid(pid)
	if err != nil {
		t.Fatal(err)
	}
	identity := pipelineProcessIdentity{PID: pid, PGID: pgid, Start: start}
	data, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return identity
}

func waitPipelineCustodyFile(t *testing.T, path string) []byte {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil {
			return data
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("custody fixture did not reach %s", path)
	return nil
}

func pipelineCustodyExitTrace(root string) string {
	path := filepath.Join(root, "exit")
	data, readErr := os.ReadFile(path)
	info, statErr := os.Stat(path)
	if readErr != nil || statErr != nil {
		return fmt.Sprintf("helper_exit=%q read=%v stat=%v", data, readErr, statErr)
	}
	return fmt.Sprintf("helper_exit=%s exit_marker_age=%s", data, time.Since(info.ModTime()))
}

func TestPipelineRecordChildCustodyHelper(t *testing.T) {
	index := slices.Index(os.Args, pipelineCustodyFixtureMarker)
	if index < 0 {
		return
	}
	role, root, mode := os.Args[index+1], os.Args[index+2], os.Args[index+3]
	switch role {
	case "actor":
		if mode == "resistant" {
			signal.Ignore(syscall.SIGTERM)
		}
		writePipelineCustodyIdentity(t, filepath.Join(root, "actor.json"), os.Getpid())
	case "child":
		for _, fd := range []int{3, 4, 5, 6} {
			unix.CloseOnExec(fd)
		}
		if mode != "empty" {
			actor := pipelineCustodyFixtureCommand(t, "actor", root, mode)
			if err := actor.Start(); err != nil {
				t.Fatal(err)
			}
			go func() { _ = actor.Wait() }()
			waitPipelineCustodyFile(t, filepath.Join(root, "actor.json"))
		}
		if mode == "stdout" || mode == "stderr" {
			go func() {
				waitPipelineCustodyFile(t, filepath.Join(root, "emit"))
				writer := os.Stdout
				if mode == "stderr" {
					writer = os.Stderr
				}
				// Match go-plugin's grpcStdioClient.Run forwarding operation.
				_, err := io.Copy(writer, bytes.NewReader([]byte("late SDK output\n")))
				outcome := "not EPIPE"
				if errors.Is(err, syscall.EPIPE) {
					outcome = "EPIPE"
				}
				_ = os.WriteFile(filepath.Join(root, "output-error"), []byte(outcome), 0600)
			}()
		}
		code := runPipelineRecordChild()
		_ = os.WriteFile(filepath.Join(root, "exit"), []byte(fmt.Sprint(code)), 0600)
		os.Exit(code)
	case "parent":
		inputRead, inputWrite, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		resultRead, resultWrite, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		controlRead, controlWrite, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, fd := range sockets {
			unix.CloseOnExec(fd)
		}
		brokerParent := os.NewFile(uintptr(sockets[0]), "fixture parent broker")
		brokerChild := os.NewFile(uintptr(sockets[1]), "fixture child broker")
		defer brokerParent.Close()
		child := pipelineCustodyFixtureCommand(t, "child", root, mode)
		log, err := os.Create(filepath.Join(root, "child.log"))
		if err != nil {
			t.Fatal(err)
		}
		defer log.Close()
		child.Stderr = log
		child.ExtraFiles = []*os.File{inputRead, resultWrite, controlRead, brokerChild}
		child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		for _, file := range []*os.File{inputRead, resultWrite, controlRead, brokerChild} {
			_ = file.Close()
		}
		defer controlWrite.Close()
		writePipelineCustodyIdentity(t, filepath.Join(root, "child.json"), child.Process.Pid)
		parentStart, err := pipelineProcessStartToken(os.Getpid())
		if err != nil {
			t.Fatal(err)
		}
		request := pipelineRecordRequest{ParentPID: os.Getpid(), ParentStart: parentStart}
		if mode == "epipe" {
			request.Config, err = config.LoadFromBytes([]byte(`
modules: []
pipelines:
  selected:
    steps:
      - name: result
        type: step.set
        config:
          values: {payload: "{{ .payload }}"}
`))
			if err != nil {
				t.Fatal(err)
			}
			request.Pipeline, request.ResultStep = "selected", "result"
			request.Input = map[string]any{"payload": strings.Repeat("x", 1<<18)}
		}
		data, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := inputWrite.Write(data); err != nil {
			t.Fatal(err)
		}
		_ = inputWrite.Close()
		// A large real engine result stays blocked in Write until parent death
		// closes the only reader; the small control reaches ctx.Done instead.
		if mode == "epipe" {
			var first [1]byte
			if _, err := io.ReadFull(resultRead, first[:]); err != nil {
				t.Fatal(err)
			}
		} else {
			var header [8]byte
			if _, err := io.ReadFull(resultRead, header[:]); err != nil {
				t.Fatal(err)
			}
			if _, err := io.CopyN(io.Discard, resultRead, int64(binary.BigEndian.Uint32(header[4:]))); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(root, "ready"), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for {
		time.Sleep(time.Minute)
	}
}

func TestPipelineRecordChildClosedCaptureKeepsCustody(t *testing.T) {
	for _, mode := range []string{"stdout", "stderr"} {
		t.Run(mode, func(t *testing.T) {
			t.Logf("toolchain=%s capture=%s", runtime.Version(), mode)
			root := t.TempDir()
			pipe := func() (*os.File, *os.File) {
				t.Helper()
				read, write, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = read.Close(); _ = write.Close() })
				return read, write
			}
			inputRead, inputWrite := pipe()
			resultRead, resultWrite := pipe()
			controlRead, controlWrite := pipe()
			captureRead, captureWrite := pipe()
			sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, fd := range sockets {
				unix.CloseOnExec(fd)
			}
			brokerParent := os.NewFile(uintptr(sockets[0]), "capture fixture parent broker")
			brokerChild := os.NewFile(uintptr(sockets[1]), "capture fixture child broker")
			t.Cleanup(func() { _ = brokerParent.Close(); _ = brokerChild.Close() })
			child := pipelineCustodyFixtureCommand(t, "child", root, mode)
			if mode == "stdout" {
				child.Stdout = captureWrite
			} else {
				child.Stderr = captureWrite
			}
			child.ExtraFiles = []*os.File{inputRead, resultWrite, controlRead, brokerChild}
			child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			reaped := make(chan error, 1)
			go func() { reaped <- child.Wait() }()
			childReaped := false
			t.Cleanup(func() {
				for _, name := range []string{"actor.json", "child.json"} {
					data, err := os.ReadFile(filepath.Join(root, name))
					var identity pipelineProcessIdentity
					if err == nil && json.Unmarshal(data, &identity) == nil {
						if token, err := pipelineProcessStartToken(identity.PID); err == nil && token == identity.Start {
							_ = unix.Kill(identity.PID, unix.SIGKILL)
						}
					}
				}
				if !childReaped {
					_ = child.Process.Kill()
					<-reaped
				}
			})
			for _, file := range []*os.File{inputRead, resultWrite, controlRead, brokerChild, captureWrite} {
				_ = file.Close()
			}
			identity := writePipelineCustodyIdentity(t, filepath.Join(root, "child.json"), child.Process.Pid)
			parentStart, err := pipelineProcessStartToken(os.Getpid())
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(pipelineRecordRequest{ParentPID: os.Getpid(), ParentStart: parentStart})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := inputWrite.Write(data); err != nil {
				t.Fatal(err)
			}
			_ = inputWrite.Close()
			_ = resultRead.SetReadDeadline(time.Now().Add(5 * time.Second))
			var header [8]byte
			if _, err := io.ReadFull(resultRead, header[:]); err != nil {
				t.Fatal(err)
			}
			if _, err := io.CopyN(io.Discard, resultRead, int64(binary.BigEndian.Uint32(header[4:]))); err != nil {
				t.Fatal(err)
			}
			// Reader loss matches parent death; EOF makes the real child enter
			// custody while the forwarding goroutine writes to FD1 or FD2.
			_ = captureRead.Close()
			_ = controlWrite.Close()
			if err := os.WriteFile(filepath.Join(root, "emit"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-reaped:
				childReaped = true
				status := child.ProcessState.Sys().(syscall.WaitStatus)
				active, censusErr := pipelineProcessGroupActiveExcept(identity.PGID, identity.PID)
				t.Fatalf("capture loss bypassed custody: wait=%v signal=%v SIGPIPE=%t descendants_active=%v census_err=%v", err, status.Signal(), status.Signal() == syscall.SIGPIPE, active, censusErr)
			case <-time.After(350 * time.Millisecond):
			}
			if outcome := string(waitPipelineCustodyFile(t, filepath.Join(root, "output-error"))); outcome != "EPIPE" {
				t.Fatalf("forwarding did not return EPIPE: %q", outcome)
			}
			if token, err := pipelineProcessStartToken(identity.PID); err != nil || token != identity.Start {
				t.Fatalf("capture loss abandoned the group leader: %q, %v", token, err)
			}
			if err := stopPipelineProcessGroup(identity, 500*time.Millisecond); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-reaped:
				childReaped = true
				if err != nil {
					t.Fatalf("child failed normal supervised shutdown: %v %s", err, pipelineCustodyExitTrace(root))
				}
			case <-time.After(2 * time.Second):
				t.Fatal("child did not finish supervised shutdown")
			}
			if active, err := pipelineProcessGroupActive(identity.PGID); err != nil || active {
				t.Fatalf("capture loss left an active process group: %v, %v", active, err)
			}
		})
	}
}

func TestPipelineRecordChildOrphanCustody(t *testing.T) {
	for _, mode := range []string{"control", "epipe", "empty", "resistant"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			parent := pipelineCustodyFixtureCommand(t, "parent", root, mode)
			if err := parent.Start(); err != nil {
				t.Fatal(err)
			}
			parentReaped := false
			t.Cleanup(func() {
				if !parentReaped {
					_ = parent.Process.Kill()
					_ = parent.Wait()
				}
				for _, name := range []string{"child.json", "actor.json"} {
					data, err := os.ReadFile(filepath.Join(root, name))
					var identity pipelineProcessIdentity
					if err == nil && json.Unmarshal(data, &identity) == nil {
						if token, err := pipelineProcessStartToken(identity.PID); err == nil && token == identity.Start {
							_ = unix.Kill(identity.PID, unix.SIGKILL)
						}
					}
				}
			})
			waitPipelineCustodyFile(t, filepath.Join(root, "ready"))
			var child pipelineProcessIdentity
			if err := json.Unmarshal(waitPipelineCustodyFile(t, filepath.Join(root, "child.json")), &child); err != nil {
				t.Fatal(err)
			}
			if err := parent.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			_ = parent.Wait()
			parentReaped = true
			if mode != "empty" {
				time.Sleep(350 * time.Millisecond)
				if token, err := pipelineProcessStartToken(child.PID); err != nil || token != child.Start {
					data, _ := os.ReadFile(filepath.Join(root, "actor.json"))
					exit, _ := os.ReadFile(filepath.Join(root, "exit"))
					log, _ := os.ReadFile(filepath.Join(root, "child.log"))
					t.Logf("actor=%s child_exit=%s trace=%s", data, exit, log)
					t.Fatalf("orphan child abandoned live descendants and its PID/PGID custody: %q, %v", token, err)
				}
				if mode != "resistant" {
					if err := stopPipelineProcessGroup(child, 500*time.Millisecond); err != nil {
						t.Fatalf("restart could not reconcile the pinned orphan group: %v", err)
					}
				}
			}
			deadline := time.Now().Add(time.Second)
			if mode == "resistant" {
				deadline = time.Now().Add(12 * time.Second)
			}
			for time.Now().Before(deadline) {
				if active, err := pipelineProcessGroupActive(child.PGID); err == nil && !active {
					if mode == "empty" {
						exit, err := os.ReadFile(filepath.Join(root, "exit"))
						if err != nil || string(exit) != "0" {
							t.Fatalf("empty descendants did not preserve normal exit: %s, %v", exit, err)
						}
					}
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			data, _ := os.ReadFile(filepath.Join(root, "actor.json"))
			exit, _ := os.ReadFile(filepath.Join(root, "exit"))
			t.Fatalf("orphan child did not empty its process group: actor=%s child_exit=%s %s", data, exit, pipelineCustodyExitTrace(root))
		})
	}
}
