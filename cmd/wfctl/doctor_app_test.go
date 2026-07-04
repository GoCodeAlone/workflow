package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Real-world platform-edge default error page shapes. Each is HTML with a
// non-2xx status and no JSON content-type — the generic body-shape/
// content-type heuristic in classifyAppProbeFailure must classify all three
// identically as platform-edge without any provider-specific matching, which
// is exactly what these three regression fixtures prove.

const edgeShapeDigitalOceanViaUpstream = `<html>
<head><title>504 Gateway Time-out</title></head>
<body>
<center><h1>504 Gateway Time-out</h1></center>
<hr><center>via_upstream</center>
</body>
</html>
`

const edgeShapeAWSALBDefault = `<html>
<head><title>503 Service Temporarily Unavailable</title></head>
<body bgcolor="white">
<center><h1>503 Service Temporarily Unavailable</h1></center>
</body>
</html>
`

const edgeShapeNginxDefault = `<html>
<head><title>502 Bad Gateway</title></head>
<body>
<center><h1>502 Bad Gateway</h1></center>
<hr><center>nginx</center>
</body>
</html>
`

const appOriginBusyJSON = `{"error":"auth state busy","retryAfter":1}`

func TestClassifyAppProbeFailureHealthy(t *testing.T) {
	class := classifyAppProbeFailure(200, "application/json", []byte(`{"status":"ok"}`))
	if class != doctorAppFailureHealthy {
		t.Fatalf("class = %q, want healthy", class)
	}
}

func TestClassifyAppProbeFailurePlatformEdgeShapes(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		contentType string
		body        string
	}{
		{"digitalocean_via_upstream", 504, "text/html", edgeShapeDigitalOceanViaUpstream},
		{"aws_alb_default", 503, "text/html", edgeShapeAWSALBDefault},
		{"nginx_default", 502, "text/html", edgeShapeNginxDefault},
		{"html_body_no_content_type_header", 502, "", edgeShapeNginxDefault},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			class := classifyAppProbeFailure(tc.status, tc.contentType, []byte(tc.body))
			if class != doctorAppFailurePlatformEdge {
				t.Fatalf("class = %q, want platform-edge", class)
			}
		})
	}
}

func TestClassifyAppProbeFailureAppOrigin(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
	}{
		{"explicit_json_content_type", "application/json"},
		{"no_content_type_but_json_body", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			class := classifyAppProbeFailure(503, tc.contentType, []byte(appOriginBusyJSON))
			if class != doctorAppFailureAppOrigin {
				t.Fatalf("class = %q, want app-origin", class)
			}
		})
	}
}

func TestClassifyAppProbeFailureUnknown(t *testing.T) {
	class := classifyAppProbeFailure(500, "text/plain", []byte("internal error, try again"))
	if class != doctorAppFailureUnknown {
		t.Fatalf("class = %q, want unknown", class)
	}
}

func TestClassifyAppProbeFailureEmptyBody(t *testing.T) {
	class := classifyAppProbeFailure(500, "", nil)
	if class != doctorAppFailureUnknown {
		t.Fatalf("class = %q, want unknown for empty body with no content-type", class)
	}
}

func TestPercentile(t *testing.T) {
	sorted := []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	if p50 := percentile(sorted, 0.5); p50 != 55 {
		t.Fatalf("p50 = %v, want 55", p50)
	}
	if p99 := percentile(sorted, 0.99); p99 < 99 || p99 > 100 {
		t.Fatalf("p99 = %v, want in [99,100]", p99)
	}
	if got := percentile(nil, 0.5); got != 0 {
		t.Fatalf("percentile(nil) = %v, want 0", got)
	}
	if got := percentile([]float64{42}, 0.99); got != 42 {
		t.Fatalf("percentile(single) = %v, want 42", got)
	}
}

func TestDoctorAppFlipFlopRate(t *testing.T) {
	healthy := func(seq int, at time.Time) doctorAppProbeResult {
		return doctorAppProbeResult{Sequence: seq, StartedAt: at, Class: doctorAppFailureHealthy}
	}
	unhealthy := func(seq int, at time.Time) doctorAppProbeResult {
		return doctorAppProbeResult{Sequence: seq, StartedAt: at, Class: doctorAppFailurePlatformEdge}
	}
	base := time.Now()
	at := func(offset int) time.Time { return base.Add(time.Duration(offset) * time.Millisecond) }

	stable := []doctorAppProbeResult{healthy(0, at(0)), healthy(1, at(1)), healthy(2, at(2))}
	if rate := doctorAppFlipFlopRate(stable); rate != 0 {
		t.Fatalf("stable healthy rate = %v, want 0", rate)
	}

	allFlip := []doctorAppProbeResult{healthy(0, at(0)), unhealthy(1, at(1)), healthy(2, at(2)), unhealthy(3, at(3))}
	if rate := doctorAppFlipFlopRate(allFlip); rate != 1 {
		t.Fatalf("alternating rate = %v, want 1", rate)
	}

	oneFlip := []doctorAppProbeResult{healthy(0, at(0)), healthy(1, at(1)), unhealthy(2, at(2)), unhealthy(3, at(3))}
	if rate := doctorAppFlipFlopRate(oneFlip); rate != 1.0/3.0 {
		t.Fatalf("one-flip rate = %v, want %v", rate, 1.0/3.0)
	}

	if rate := doctorAppFlipFlopRate(nil); rate != 0 {
		t.Fatalf("empty rate = %v, want 0", rate)
	}
	if rate := doctorAppFlipFlopRate([]doctorAppProbeResult{healthy(0, at(0))}); rate != 0 {
		t.Fatalf("single-result rate = %v, want 0", rate)
	}

	// Ordering by StartedAt (not by Sequence/append order) must drive the
	// transition count, since sequential and concurrent probes are appended
	// in phase order, not timeline order.
	outOfOrder := []doctorAppProbeResult{unhealthy(1, at(5)), healthy(0, at(0))}
	if rate := doctorAppFlipFlopRate(outOfOrder); rate != 1 {
		t.Fatalf("out-of-append-order rate = %v, want 1 (started-at order: healthy then unhealthy)", rate)
	}
}

// TestRunDoctorAppProbesAgainstHealthyServer exercises the full HTTP path
// (sequential + concurrent phases) against a real httptest server that is
// always healthy, verifying probe counts and classification end-to-end.
func TestRunDoctorAppProbesAgainstHealthyServer(t *testing.T) {
	var requestCount atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		if r.URL.Path != "/healthz" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	results := runDoctorAppProbes(t.Context(), srv.Client(), doctorAppOptions{
		URL: srv.URL, HealthPath: "/healthz", Probes: 4, Concurrency: 3, Timeout: 5 * time.Second,
	})
	if len(results) != 7 {
		t.Fatalf("got %d results, want 7", len(results))
	}
	if got := requestCount.Load(); got != 7 {
		t.Fatalf("server saw %d requests, want 7", got)
	}
	sequential, concurrent := 0, 0
	for _, r := range results {
		if r.Class != doctorAppFailureHealthy {
			t.Errorf("result %+v: class = %q, want healthy", r, r.Class)
		}
		if r.Concurrent {
			concurrent++
		} else {
			sequential++
		}
	}
	if sequential != 4 || concurrent != 3 {
		t.Fatalf("sequential=%d concurrent=%d, want 4/3", sequential, concurrent)
	}
}

// TestRunDoctorAppProbesAgainstEdgeAndAppOriginFailures runs the real HTTP
// path against a server alternating between the three platform-edge fixture
// shapes and an app-origin JSON error, proving classification survives an
// actual round trip (headers, body reading, status codes) and not just the
// pure classifier function.
func TestRunDoctorAppProbesAgainstEdgeAndAppOriginFailures(t *testing.T) {
	responses := []struct {
		status      int
		contentType string
		body        string
	}{
		{504, "text/html", edgeShapeDigitalOceanViaUpstream},
		{503, "text/html", edgeShapeAWSALBDefault},
		{502, "text/html", edgeShapeNginxDefault},
		{503, "application/json", appOriginBusyJSON},
	}
	var idx atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		i := int(idx.Add(1)-1) % len(responses)
		resp := responses[i]
		w.Header().Set("Content-Type", resp.contentType)
		w.WriteHeader(resp.status)
		_, _ = w.Write([]byte(resp.body))
	}))
	defer srv.Close()

	results := runDoctorAppProbes(t.Context(), srv.Client(), doctorAppOptions{
		URL: srv.URL, HealthPath: "/healthz", Probes: 4, Concurrency: 0, Timeout: 5 * time.Second,
	})
	if len(results) != 4 {
		t.Fatalf("got %d results, want 4", len(results))
	}
	want := []doctorAppFailureClass{
		doctorAppFailurePlatformEdge,
		doctorAppFailurePlatformEdge,
		doctorAppFailurePlatformEdge,
		doctorAppFailureAppOrigin,
	}
	for i, r := range results {
		if r.Class != want[i] {
			t.Errorf("result[%d] class = %q, want %q", i, r.Class, want[i])
		}
	}
}

func TestRunDoctorAppProbesTransportFailure(t *testing.T) {
	results := runDoctorAppProbes(t.Context(), http.DefaultClient, doctorAppOptions{
		URL: "http://127.0.0.1:1", HealthPath: "/healthz", Probes: 1, Concurrency: 0, Timeout: time.Second,
	})
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Class != doctorAppFailureTransport {
		t.Fatalf("class = %q, want transport-error", results[0].Class)
	}
	if results[0].Err == nil {
		t.Fatal("expected a non-nil Err for a transport failure")
	}
}

func TestNormalizeHealthPath(t *testing.T) {
	cases := map[string]string{
		"/healthz": "/healthz",
		"healthz":  "/healthz",
		"":         "/",
		"/":        "/",
	}
	for in, want := range cases {
		if got := normalizeHealthPath(in); got != want {
			t.Errorf("normalizeHealthPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRunDoctorAppProbesHealthPathWithoutLeadingSlash proves the missing
// separator is fixed end-to-end (not just in the unit-level
// normalizeHealthPath): a --health-path value with no leading slash must
// still hit the right route, not get silently concatenated onto the host.
func TestRunDoctorAppProbesHealthPathWithoutLeadingSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	results := runDoctorAppProbes(t.Context(), srv.Client(), doctorAppOptions{
		URL: srv.URL, HealthPath: "healthz", Probes: 1, Concurrency: 0, Timeout: 5 * time.Second,
	})
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Class != doctorAppFailureHealthy {
		t.Fatalf("class = %q, want healthy (request: %+v)", results[0].Class, results[0])
	}
	if gotPath != "/healthz" {
		t.Fatalf("server saw path %q, want /healthz", gotPath)
	}
}

// erroringBody is an io.ReadCloser that returns some bytes and then a fixed
// error, simulating a truncated connection or mid-stream reset.
type erroringBody struct {
	remaining []byte
	err       error
}

func (b *erroringBody) Read(p []byte) (int, error) {
	if len(b.remaining) == 0 {
		return 0, b.err
	}
	n := copy(p, b.remaining)
	b.remaining = b.remaining[n:]
	return n, nil
}

func (b *erroringBody) Close() error { return nil }

// TestRunDoctorAppProbesBodyReadFailureIsTransportError proves a failed body
// read (as opposed to a failed request/connection) is reported as its own
// transport error rather than silently classifying against whatever partial
// bytes were read before the failure.
func TestRunDoctorAppProbesBodyReadFailureIsTransportError(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       &erroringBody{remaining: []byte(`{"stat`), err: io.ErrUnexpectedEOF},
			}, nil
		}),
	}
	results := runDoctorAppProbes(t.Context(), client, doctorAppOptions{
		URL: "http://example.invalid", HealthPath: "/healthz", Probes: 1, Concurrency: 0, Timeout: time.Second,
	})
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Class != doctorAppFailureTransport {
		t.Fatalf("class = %q, want transport-error (body read failed)", results[0].Class)
	}
	if results[0].Err == nil {
		t.Fatal("expected a non-nil Err when the body read fails")
	}
}

func TestBuildDoctorAppReportAllHealthy(t *testing.T) {
	results := []doctorAppProbeResult{
		{Class: doctorAppFailureHealthy, StartedAt: time.Now(), Duration: 10 * time.Millisecond},
		{Class: doctorAppFailureHealthy, StartedAt: time.Now(), Duration: 12 * time.Millisecond},
	}
	report := buildDoctorAppReport(doctorAppOptions{Probes: 2}, results)
	if report.Status != doctorStatusOK {
		t.Fatalf("status = %q, want OK", report.Status)
	}
	if report.FlipFlopRate != 0 {
		t.Fatalf("flip-flop rate = %v, want 0", report.FlipFlopRate)
	}
}

func TestBuildDoctorAppReportTotalOutageIsError(t *testing.T) {
	results := []doctorAppProbeResult{
		{Class: doctorAppFailureTransport, StartedAt: time.Now()},
		{Class: doctorAppFailureTransport, StartedAt: time.Now()},
	}
	report := buildDoctorAppReport(doctorAppOptions{Probes: 2}, results)
	if report.Status != doctorStatusError {
		t.Fatalf("status = %q, want ERROR", report.Status)
	}
}

func TestBuildDoctorAppReportMixedIsWarn(t *testing.T) {
	results := []doctorAppProbeResult{
		{Class: doctorAppFailureHealthy, StartedAt: time.Now()},
		{Class: doctorAppFailurePlatformEdge, StartedAt: time.Now().Add(time.Millisecond)},
	}
	report := buildDoctorAppReport(doctorAppOptions{Probes: 2}, results)
	if report.Status != doctorStatusWarn {
		t.Fatalf("status = %q, want WARN", report.Status)
	}
	foundPlatformEdgeFinding := false
	for _, f := range report.Findings {
		if strings.Contains(f.Message, "platform-edge") {
			foundPlatformEdgeFinding = true
		}
	}
	if !foundPlatformEdgeFinding {
		t.Fatalf("expected a platform-edge finding, got %+v", report.Findings)
	}
}

// TestDoctorAppCLIEndToEndTextAndJSON drives runDoctorAppWithOutput exactly
// as the CLI would, against a real server that is always unhealthy in the
// platform-edge shape, verifying flag parsing, text rendering, and JSON
// encoding together. Overall status is ERROR (not just WARN) because zero of
// the probes were healthy across the whole window — the platform-edge
// classification is an additional diagnostic detail about *why*, not a
// downgrade of that severity.
func TestDoctorAppCLIEndToEndTextAndJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(edgeShapeNginxDefault))
	}))
	defer srv.Close()

	var out bytes.Buffer
	err := runDoctorAppWithOutput([]string{
		"--probes", "2", "--concurrency", "0", "--timeout", "2s", srv.URL,
	}, &out)
	if err != nil {
		t.Fatalf("doctor app: %v\n%s", err, out.String())
	}
	for _, want := range []string{"wfctl doctor app", "Overall: ERROR", "platform-edge: 2", "Fix:"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("text output missing %q:\n%s", want, out.String())
		}
	}

	var jsonOut bytes.Buffer
	err = runDoctorAppWithOutput([]string{
		"--probes", "2", "--concurrency", "0", "--timeout", "2s", "--format", "json", srv.URL,
	}, &jsonOut)
	if err != nil {
		t.Fatalf("doctor app json: %v\n%s", err, jsonOut.String())
	}
	var report doctorAppReport
	if err := json.Unmarshal(jsonOut.Bytes(), &report); err != nil {
		t.Fatalf("doctor app output is not valid JSON: %v\n%s", err, jsonOut.String())
	}
	if report.Status != doctorStatusError {
		t.Fatalf("json status = %q, want ERROR", report.Status)
	}
	if report.ClassCounts["platform-edge"] != 2 {
		t.Fatalf("json classCounts[platform-edge] = %d, want 2", report.ClassCounts["platform-edge"])
	}
}

// TestDoctorAppStrictExitsNonZeroOnError covers the total-outage case: 0/1
// probes healthy, which buildDoctorAppReport marks ERROR. Renamed from a
// Copilot review finding on the upstream PR: the previous name and comment
// said "WARN" for this exact fixture, but a single always-failing probe is
// 0/1 healthy, i.e. ERROR, not WARN — see TestDoctorAppStrictExitsNonZeroOnGenuineWarn
// below for the actual WARN case (partial degradation), which this fixture
// never exercised.
func TestDoctorAppStrictExitsNonZeroOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(edgeShapeNginxDefault))
	}))
	defer srv.Close()

	var out bytes.Buffer
	err := runDoctorAppWithOutput([]string{"--probes", "1", "--concurrency", "0", "--strict", srv.URL}, &out)
	if err == nil {
		t.Fatal("expected --strict to return an error on an ERROR report")
	}
	if !strings.Contains(out.String(), "Overall: ERROR") {
		t.Fatalf("expected this fixture to actually produce ERROR, got:\n%s", out.String())
	}
}

// TestDoctorAppStrictExitsNonZeroOnGenuineWarn covers partial degradation
// (some probes healthy, some not) — the real WARN case that
// TestDoctorAppStrictExitsNonZeroOnError's name previously claimed to cover
// but didn't.
func TestDoctorAppStrictExitsNonZeroOnGenuineWarn(t *testing.T) {
	var count atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if count.Add(1) <= 2 {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(edgeShapeNginxDefault))
	}))
	defer srv.Close()

	var out bytes.Buffer
	err := runDoctorAppWithOutput([]string{"--probes", "4", "--concurrency", "0", "--strict", srv.URL}, &out)
	if err == nil {
		t.Fatal("expected --strict to return an error on a WARN report")
	}
	if !strings.Contains(out.String(), "Overall: WARN") {
		t.Fatalf("expected a genuine WARN report, got:\n%s", out.String())
	}
}

func TestDoctorAppRequiresURLArgument(t *testing.T) {
	var out bytes.Buffer
	err := runDoctorAppWithOutput([]string{"--probes", "1"}, &out)
	if err == nil {
		t.Fatal("expected an error when no URL is given")
	}
}

func TestDoctorAppRejectsInvalidFlagValues(t *testing.T) {
	cases := [][]string{
		{"--probes", "0", "http://example.com"},
		{"--concurrency", "-1", "http://example.com"},
		{"--timeout", "0s", "http://example.com"},
	}
	for _, args := range cases {
		var out bytes.Buffer
		if err := runDoctorAppWithOutput(args, &out); err == nil {
			t.Fatalf("args %v: expected an error", args)
		}
	}
}

func TestDoctorAppRejectsInvalidFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var out bytes.Buffer
	err := runDoctorAppWithOutput([]string{"--probes", "1", "--format", "yaml", srv.URL}, &out)
	if err == nil {
		t.Fatal("expected an error for an unsupported --format value")
	}
}

// TestDoctorDispatchesAppSubcommand proves "wfctl doctor app <url>" reaches
// the app-probe path through the top-level doctor entrypoint, not just
// through runDoctorAppWithOutput directly.
func TestDoctorDispatchesAppSubcommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var out bytes.Buffer
	err := runDoctorWithOutput([]string{"app", "--probes", "1", "--concurrency", "0", srv.URL}, &out)
	if err != nil {
		t.Fatalf("doctor app dispatch: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "wfctl doctor app") {
		t.Fatalf("expected doctor to dispatch to the app probe, got:\n%s", out.String())
	}
}
