package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// doctorAppFailureClass classifies why an app-probe request did not return a
// healthy 2xx response. Classification uses only the HTTP response's
// Content-Type header and body shape — never a provider-specific marker — so
// the same heuristic works unmodified in front of any edge proxy
// (DigitalOcean App Platform, AWS ALB, nginx, or anything else fronting a
// deployed app).
type doctorAppFailureClass string

const (
	// doctorAppFailureHealthy means the probe returned a 2xx response.
	doctorAppFailureHealthy doctorAppFailureClass = "healthy"
	// doctorAppFailurePlatformEdge means the request never reached the app:
	// a reverse proxy or load balancer answered with its own HTML error
	// page. This is the distinction that cost hours during a real staging
	// incident: an edge proxy's default HTML error page looks like a
	// completely different failure than the app's own structured error, but
	// both surface to a human as "the health check failed."
	doctorAppFailurePlatformEdge doctorAppFailureClass = "platform-edge"
	// doctorAppFailureAppOrigin means the request reached the app, which
	// returned its own structured (JSON) error body.
	doctorAppFailureAppOrigin doctorAppFailureClass = "app-origin"
	// doctorAppFailureTransport means no HTTP response was ever received
	// (connection refused, timeout, TLS failure, DNS failure, ...).
	doctorAppFailureTransport doctorAppFailureClass = "transport-error"
	// doctorAppFailureUnknown means a response was received but its
	// content-type and body shape matched neither heuristic.
	doctorAppFailureUnknown doctorAppFailureClass = "unknown"
)

// classifyAppProbeFailure distinguishes a platform-edge failure from an
// app-origin failure using only the response's Content-Type header and body
// shape. It intentionally contains no provider-specific logic (no check for
// "DigitalOcean", "ALB", or "nginx" by name): every edge proxy's default
// error page observed in practice is HTML, and every app-origin structured
// error observed in practice is JSON, so the two generic heuristics below
// cover all of them without hardcoding any one platform.
func classifyAppProbeFailure(statusCode int, contentType string, body []byte) doctorAppFailureClass {
	if statusCode >= 200 && statusCode < 300 {
		return doctorAppFailureHealthy
	}
	trimmed := bytes.TrimSpace(body)
	if hasJSONBodyShape(trimmed) || strings.Contains(strings.ToLower(contentType), "json") {
		return doctorAppFailureAppOrigin
	}
	if hasHTMLBodyShape(trimmed) || strings.Contains(strings.ToLower(contentType), "text/html") {
		return doctorAppFailurePlatformEdge
	}
	return doctorAppFailureUnknown
}

func hasJSONBodyShape(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	return body[0] == '{' || body[0] == '['
}

func hasHTMLBodyShape(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	lower := bytes.ToLower(body)
	head := lower[:min(len(lower), 512)]
	return bytes.Contains(head, []byte("<html")) || bytes.Contains(head, []byte("<!doctype html"))
}

// normalizeHealthPath ensures path has a leading "/" before it is appended to
// a base URL. Without this, a health path passed as "healthz" (no leading
// slash) would concatenate directly onto the trimmed base URL with no
// separator (e.g. "https://example.comhealthz"), producing an invalid URL
// instead of a clear error.
func normalizeHealthPath(path string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

// doctorAppProbeResult is one HTTP round trip against the health path.
type doctorAppProbeResult struct {
	Sequence   int
	Concurrent bool
	StartedAt  time.Time
	Duration   time.Duration
	StatusCode int
	Class      doctorAppFailureClass
	Err        error
}

type doctorAppOptions struct {
	URL         string
	HealthPath  string
	Probes      int
	Concurrency int
	Timeout     time.Duration
}

// runDoctorAppProbes runs opts.Probes sequential requests followed by
// opts.Concurrency lightly-concurrent requests (fired together, after the
// sequential phase completes) against the target health path. It is
// explicitly-online and read-only: every request is a GET against a
// caller-supplied URL, with no retries beyond what net/http already does at
// the transport layer, and no request volume beyond N+M total.
func runDoctorAppProbes(ctx context.Context, client *http.Client, opts doctorAppOptions) []doctorAppProbeResult {
	target := strings.TrimRight(opts.URL, "/") + normalizeHealthPath(opts.HealthPath)
	results := make([]doctorAppProbeResult, 0, opts.Probes+opts.Concurrency)

	probeOnce := func(seq int, concurrent bool) doctorAppProbeResult {
		reqCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
		start := time.Now()
		//nolint:gosec // G107: target is explicitly provided by the operator as the doctor app probe URL
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, target, nil)
		if err != nil {
			return doctorAppProbeResult{
				Sequence: seq, Concurrent: concurrent, StartedAt: start,
				Duration: time.Since(start), Class: doctorAppFailureTransport, Err: err,
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			return doctorAppProbeResult{
				Sequence: seq, Concurrent: concurrent, StartedAt: start,
				Duration: time.Since(start), Class: doctorAppFailureTransport, Err: err,
			}
		}
		defer func() { _ = resp.Body.Close() }()
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		duration := time.Since(start)
		if readErr != nil {
			// A failed body read (truncated connection, mid-stream reset)
			// leaves body incomplete; classifying against a partial body
			// would misreport a transport failure as a healthy/platform-edge/
			// app-origin response depending on what happened to be read
			// before the failure. Report it as its own transport error
			// instead of guessing from a partial read.
			return doctorAppProbeResult{
				Sequence: seq, Concurrent: concurrent, StartedAt: start,
				Duration: duration, StatusCode: resp.StatusCode, Class: doctorAppFailureTransport, Err: readErr,
			}
		}
		return doctorAppProbeResult{
			Sequence:   seq,
			Concurrent: concurrent,
			StartedAt:  start,
			Duration:   duration,
			StatusCode: resp.StatusCode,
			Class:      classifyAppProbeFailure(resp.StatusCode, resp.Header.Get("Content-Type"), body),
		}
	}

	for i := 0; i < opts.Probes; i++ {
		results = append(results, probeOnce(i, false))
	}

	if opts.Concurrency > 0 {
		var mu sync.Mutex
		var wg sync.WaitGroup
		wg.Add(opts.Concurrency)
		for i := 0; i < opts.Concurrency; i++ {
			go func(seq int) {
				defer wg.Done()
				r := probeOnce(seq, true)
				mu.Lock()
				results = append(results, r)
				mu.Unlock()
			}(opts.Probes + i)
		}
		wg.Wait()
	}

	return results
}

type doctorAppLatency struct {
	P50Ms float64 `json:"p50Ms"`
	P99Ms float64 `json:"p99Ms"`
}

func doctorAppLatencyStats(results []doctorAppProbeResult) doctorAppLatency {
	durations := make([]float64, 0, len(results))
	for _, r := range results {
		durations = append(durations, float64(r.Duration.Microseconds())/1000.0)
	}
	sort.Float64s(durations)
	return doctorAppLatency{
		P50Ms: percentile(durations, 0.50),
		P99Ms: percentile(durations, 0.99),
	}
}

// percentile returns the p-th percentile (0..1) of sorted ascending values
// using linear interpolation between the two nearest ranks. Returns 0 for an
// empty slice.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := p * float64(len(sorted)-1)
	lo := int(rank)
	hi := lo + 1
	if hi >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	frac := rank - float64(lo)
	return sorted[lo] + frac*(sorted[hi]-sorted[lo])
}

// doctorAppFlipFlopRate orders results by start time (spanning both the
// sequential and concurrent phases) and reports the fraction of consecutive
// pairs that cross the healthy/unhealthy boundary. A stably healthy or
// stably unhealthy window scores 0; a service alternating on every probe
// scores close to 1.
func doctorAppFlipFlopRate(results []doctorAppProbeResult) float64 {
	if len(results) < 2 {
		return 0
	}
	ordered := make([]doctorAppProbeResult, len(results))
	copy(ordered, results)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].StartedAt.Before(ordered[j].StartedAt) })

	transitions := 0
	prevHealthy := ordered[0].Class == doctorAppFailureHealthy
	for _, r := range ordered[1:] {
		healthy := r.Class == doctorAppFailureHealthy
		if healthy != prevHealthy {
			transitions++
		}
		prevHealthy = healthy
	}
	return float64(transitions) / float64(len(ordered)-1)
}

type doctorAppReport struct {
	Status       doctorStatus     `json:"status"`
	URL          string           `json:"url"`
	HealthPath   string           `json:"healthPath"`
	Sequential   int              `json:"sequentialProbes"`
	Concurrent   int              `json:"concurrentProbes"`
	Latency      doctorAppLatency `json:"latencyMs"`
	FlipFlopRate float64          `json:"flipFlopRate"`
	ClassCounts  map[string]int   `json:"classCounts"`
	Findings     []doctorCheck    `json:"findings"`
}

// doctorAppClassOrder fixes the display/report order for failure-origin
// counts so text and JSON output are stable across runs.
var doctorAppClassOrder = []doctorAppFailureClass{
	doctorAppFailureHealthy,
	doctorAppFailurePlatformEdge,
	doctorAppFailureAppOrigin,
	doctorAppFailureTransport,
	doctorAppFailureUnknown,
}

func buildDoctorAppReport(opts doctorAppOptions, results []doctorAppProbeResult) doctorAppReport {
	counts := map[string]int{}
	for _, r := range results {
		counts[string(r.Class)]++
	}
	flipFlop := doctorAppFlipFlopRate(results)

	report := doctorAppReport{
		URL:          opts.URL,
		HealthPath:   opts.HealthPath,
		Sequential:   opts.Probes,
		Concurrent:   opts.Concurrency,
		Latency:      doctorAppLatencyStats(results),
		FlipFlopRate: flipFlop,
		ClassCounts:  counts,
	}

	total := len(results)
	healthy := counts[string(doctorAppFailureHealthy)]
	platformEdge := counts[string(doctorAppFailurePlatformEdge)]
	appOrigin := counts[string(doctorAppFailureAppOrigin)]
	transport := counts[string(doctorAppFailureTransport)]
	unknown := counts[string(doctorAppFailureUnknown)]

	var findings []doctorCheck
	switch healthy {
	case total:
		findings = append(findings, doctorCheck{
			Status:  doctorStatusOK,
			Message: fmt.Sprintf("all %d probes returned a healthy 2xx response", total),
		})
	case 0:
		findings = append(findings, doctorCheck{
			Status:  doctorStatusError,
			Message: fmt.Sprintf("0/%d probes were healthy — the service was unreachable or failing for the entire probe window", total),
		})
	default:
		findings = append(findings, doctorCheck{
			Status:  doctorStatusWarn,
			Message: fmt.Sprintf("%d/%d probes were healthy; the rest failed during the probe window", healthy, total),
		})
	}
	if platformEdge > 0 {
		findings = append(findings, doctorCheck{
			Status:  doctorStatusWarn,
			Message: fmt.Sprintf("%d/%d probes classified as platform-edge (HTML error page; the request never reached the app)", platformEdge, total),
			Fix:     "check the edge proxy or load balancer (health check config, timeouts, upstream target health), not the app",
		})
	}
	if appOrigin > 0 {
		findings = append(findings, doctorCheck{
			Status:  doctorStatusWarn,
			Message: fmt.Sprintf("%d/%d probes classified as app-origin (structured error from the app itself)", appOrigin, total),
			Fix:     "check app logs for the reported error; under concurrency this often indicates lock/lease contention",
		})
	}
	if transport > 0 {
		findings = append(findings, doctorCheck{
			Status:  doctorStatusError,
			Message: fmt.Sprintf("%d/%d probes failed at the transport level (no HTTP response received)", transport, total),
			Fix:     "check DNS, TLS, network reachability, or whether the app is running at all",
		})
	}
	if unknown > 0 {
		findings = append(findings, doctorCheck{
			Status:  doctorStatusWarn,
			Message: fmt.Sprintf("%d/%d probes returned a response that matched neither the platform-edge nor app-origin body shape", unknown, total),
		})
	}
	if flipFlop > 0 {
		findings = append(findings, doctorCheck{
			Status:  doctorStatusWarn,
			Message: fmt.Sprintf("health flip-flopped across the probe window (rate %.0f%%) — the service is not stably healthy", flipFlop*100),
		})
	}
	report.Findings = findings

	status := doctorStatusOK
	for _, f := range findings {
		status = worseDoctorStatus(status, f.Status)
	}
	report.Status = status
	return report
}

func renderDoctorAppText(out io.Writer, report doctorAppReport) {
	fmt.Fprintln(out, "wfctl doctor app")
	fmt.Fprintf(out, "Overall: %s\n", report.Status)
	fmt.Fprintf(out, "Target: %s%s\n", report.URL, report.HealthPath)
	fmt.Fprintf(out, "Probes: %d sequential + %d concurrent\n", report.Sequential, report.Concurrent)
	fmt.Fprintf(out, "Latency: p50=%.1fms p99=%.1fms\n", report.Latency.P50Ms, report.Latency.P99Ms)
	fmt.Fprintf(out, "Flip-flop rate: %.0f%%\n", report.FlipFlopRate*100)

	fmt.Fprintln(out, "\n[Failure origin]")
	for _, class := range doctorAppClassOrder {
		if count, ok := report.ClassCounts[string(class)]; ok {
			fmt.Fprintf(out, "- %s: %d\n", class, count)
		}
	}

	fmt.Fprintln(out, "\n[Findings]")
	for _, f := range report.Findings {
		fmt.Fprintf(out, "- %s %s\n", f.Status, f.Message)
		if f.Fix != "" {
			fmt.Fprintf(out, "  Fix: %s\n", f.Fix)
		}
	}
}

// runDoctorAppWithOutput implements "wfctl doctor app <url>". It is reached
// only through runDoctor's dispatch on the literal "app" subcommand (see
// doctor.go); there is no separate top-level "doctor-app" command wired into
// main.go's commands map.
func runDoctorAppWithOutput(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("doctor app", flag.ContinueOnError)
	fs.SetOutput(out)
	healthPath := fs.String("health-path", "/healthz", "Health check path to probe")
	probes := fs.Int("probes", 5, "Number of sequential probes")
	concurrency := fs.Int("concurrency", 3, "Number of lightly-concurrent probes fired after the sequential probes")
	timeout := fs.Duration("timeout", 10*time.Second, "Per-request timeout")
	format := fs.String("format", "text", "Output format: text or json")
	strict := fs.Bool("strict", false, "Exit non-zero when warnings or errors are found")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl doctor app <url> [options]

Explicitly-online, read-only probe of a deployed app's health endpoint. Runs
N sequential requests followed by M lightly-concurrent requests and reports
latency spread (p50/p99), failure-origin classification (platform-edge HTML
vs app-origin structured error), and health flip-flop rate across the probe
window. Makes no state-changing requests.

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("wfctl doctor app requires exactly one <url> argument, got %d", fs.NArg())
	}
	targetURL := fs.Arg(0)
	if *probes < 1 {
		return fmt.Errorf("--probes must be >= 1, got %d", *probes)
	}
	if *concurrency < 0 {
		return fmt.Errorf("--concurrency must be >= 0, got %d", *concurrency)
	}
	if *timeout <= 0 {
		return fmt.Errorf("--timeout must be > 0, got %s", *timeout)
	}

	opts := doctorAppOptions{
		URL:         targetURL,
		HealthPath:  *healthPath,
		Probes:      *probes,
		Concurrency: *concurrency,
		Timeout:     *timeout,
	}
	client := &http.Client{Timeout: *timeout + time.Second} // safety net above the per-request context timeout
	results := runDoctorAppProbes(context.Background(), client, opts)
	report := buildDoctorAppReport(opts, results)

	switch *format {
	case "text":
		renderDoctorAppText(out, report)
	case "json":
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return fmt.Errorf("encode doctor app report: %w", err)
		}
	default:
		return fmt.Errorf("--format must be text or json, got %q", *format)
	}

	if *strict && report.Status != doctorStatusOK {
		return fmt.Errorf("wfctl doctor app found %s diagnostics", report.Status)
	}
	return nil
}
