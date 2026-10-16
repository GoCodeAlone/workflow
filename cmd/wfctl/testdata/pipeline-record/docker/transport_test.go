//go:build linux || darwin

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestTransportIdentityConsumesFramedRequest(t *testing.T) {
	for _, test := range []struct {
		endpoint, request string
		valid             bool
	}{
		{"unix:///fixture.sock", "GET /info HTTP/1.1\r\nHost: api.moby.localhost\r\nConnection: close\r\n\r\n", true},
		{"tcp://selected.example:2376", "GET /info HTTP/1.1\r\nHost: selected.example:2376\r\nConnection: close\r\n\r\n", true},
		{"tcp://[::1]:2376", "GET /info HTTP/1.1\r\nHost: [::1]:2376\r\nConnection: close\r\n\r\n", true},
		{"tcp://[fe80::1%25en0]:2376", "GET /info HTTP/1.1\r\nHost: [fe80::1]:2376\r\nConnection: close\r\n\r\n", true},
		{"tcp://[fe80::1%25en0]:2376", "GET /info HTTP/1.1\r\nHost: [fe80::1%en0]:2376\r\nConnection: close\r\n\r\n", false},
		{"tcp://bücher.example:2376", "GET /info HTTP/1.1\r\nHost: xn--bcher-kva.example:2376\r\nConnection: close\r\n\r\n", true},
		{"unix:///fixture.sock", "GET /info HTTP/1.1\r\nHost: wrong-backend\r\nConnection: close\r\n\r\n", false},
		{"unix:///fixture.sock", "POST /info HTTP/1.1\r\nHost: api.moby.localhost\r\nConnection: close\r\n\r\n", false},
		{"unix:///fixture.sock", "GET /other HTTP/1.1\r\nHost: api.moby.localhost\r\nConnection: close\r\n\r\n", false},
		{"unix:///fixture.sock", "GET /info HTTP/1.1\r\nHost: api.moby.localhost\r\n\r\n", false},
		{"unix:///fixture.sock", "GET /info HTTP/1.1\r\nHost: api.moby.localhost\r\nConnection: close\r\nContent-Length: 1\r\n\r\nx", false},
		{"unix:///fixture.sock", "malformed", false},
	} {
		t.Run(test.request, func(t *testing.T) {
			var output bytes.Buffer
			err := serveRecordDockerIdentity(test.endpoint, bytes.NewBufferString(test.request), &output)
			if !test.valid {
				if err == nil || output.Len() != 0 {
					t.Fatal("invalid request produced an identity response")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			response, err := http.ReadResponse(bufio.NewReader(&output), &http.Request{Method: "GET"})
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			var body map[string]any
			if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&body) != nil || body["ID"] != "record-fixture-daemon" {
				t.Fatal("fixture omitted its framed daemon identity")
			}
		})
	}
}

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
