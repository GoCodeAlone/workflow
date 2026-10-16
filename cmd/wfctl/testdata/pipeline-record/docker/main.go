//go:build linux || darwin

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--record-fixture-late-create" {
		runRecordDockerLateCreate()
		return
	}
	args := os.Args[1:]
	host := "unix:///record-fixture-daemon.sock"
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		flag, value, inline := strings.Cut(args[0], "=")
		args = args[1:]
		if !inline && flag != "--tls" && flag != "--tlsverify" {
			if len(args) == 0 {
				os.Exit(64)
			}
			value, args = args[0], args[1:]
		}
		if flag == "--host" {
			host = value
		}
	}
	if len(args) == 0 {
		os.Exit(64)
	}
	if runRecordDockerTransport(args) {
		return
	}
	// Only daemon identity and empty lifecycle queries are substituted. A
	// container operation is never simulated by this fixture.
	switch args[0] {
	case "system":
		if len(args) != 2 || args[1] != "dial-stdio" || serveRecordDockerIdentity(host, os.Stdin, os.Stdout) != nil {
			os.Exit(64)
		}
	case "context":
		if len(args) < 2 {
			os.Exit(64)
		}
		switch args[1] {
		case "show":
			fmt.Println("record-fixture-context")
		case "inspect":
			fmt.Println(`[{"Name":"record-fixture-context","Endpoints":{"docker":{"Host":"unix:///record-fixture-daemon.sock","SkipTLSVerify":false}},"TLSMaterial":{},"Storage":{}}]`)
		default:
			os.Exit(64)
		}
	case "info":
		for _, arg := range args[1:] {
			if arg == "{{json .ID}}" {
				_ = json.NewEncoder(os.Stdout).Encode("record-fixture-daemon")
				return
			}
		}
		fmt.Println("record-fixture-daemon")
	case "ps":
		// This test dependency has no containers, including cleanup-label matches.
	default:
		fmt.Fprintln(os.Stderr, "fixture Docker supports only context, identity, and empty ps queries")
		os.Exit(64)
	}
}

func serveRecordDockerIdentity(endpoint string, input io.Reader, output io.Writer) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("fixture endpoint invalid")
	}
	host := "api.moby.localhost"
	if u.Scheme == "tcp" {
		host = u.Host
	}
	// Request.Write applies Docker's HTTP authority rules, including stripping
	// IPv6 zones and converting international hostnames to their wire form.
	var authority bytes.Buffer
	probe := &http.Request{Method: http.MethodGet, URL: &url.URL{Scheme: "http", Host: host, Path: "/info"}, Host: host}
	if probe.Write(&authority) != nil {
		return fmt.Errorf("fixture authority invalid")
	}
	canonical, err := http.ReadRequest(bufio.NewReader(&authority))
	if err != nil {
		return fmt.Errorf("fixture authority invalid")
	}
	host = canonical.Host
	request, err := http.ReadRequest(bufio.NewReader(input))
	if err != nil || request.Method != http.MethodGet || request.RequestURI != "/info" || request.Host != host || !request.Close || request.ContentLength != 0 || len(request.TransferEncoding) != 0 {
		return fmt.Errorf("fixture identity request invalid")
	}
	body := "{\"ID\":\"record-fixture-daemon\",\"Containers\":0}\n"
	_, err = fmt.Fprintf(output, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
	return err
}
