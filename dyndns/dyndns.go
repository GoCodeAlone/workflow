// Package dyndns implements a dynamic-DNS daemon that periodically
// detects a host's public IP and pushes updates to a DNS provider
// when the IP changes.
//
// Per docs/plans/2026-05-20-dns-providers.md T14..T16.
//
// The package is intentionally provider-agnostic: callers supply an
// Updater closure that talks to their DNS driver of choice (DO,
// Namecheap, Hover, etc.) via wfctl's existing infra.dns surface.
package dyndns

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// IPDetector returns the public IP this host appears to be reaching
// the internet from. Implementations should be lightweight; a single
// HTTPS GET is the canonical shape.
type IPDetector interface {
	Detect(ctx context.Context) (net.IP, error)
	Name() string
}

// Updater applies the new IP to a DNS record. Implementations talk to
// a DNS provider (DO/Namecheap/Hover) via the wfctl IaC ResourceDriver.
//
// Called only when the detected IP differs from the previously-known
// value; idempotent re-runs are still safe.
type Updater func(ctx context.Context, ip net.IP) error

// Config controls the daemon loop.
type Config struct {
	// Detectors quorum the public IP. Default: HTTPDetector against
	// icanhazip.com + ifconfig.me + ipify.org (need ≥ 2 agreeing).
	Detectors []IPDetector

	// PollInterval is the steady-state interval between IP checks.
	// Default 5m. Must be >= 30s.
	PollInterval time.Duration

	// QuorumSize is the number of detectors that must agree before
	// an update fires. Default = (len(Detectors)+1)/2 — simple
	// majority. Set to 1 for single-source mode.
	QuorumSize int

	// MaxBackoff caps the exponential backoff applied after
	// consecutive Update failures. Default 1h.
	MaxBackoff time.Duration

	// Update is the callback that applies a new IP to DNS.
	Update Updater

	// Now is injectable for tests. Defaults to time.Now.
	Now func() time.Time

	// Sleep is injectable for tests. Defaults to time.Sleep.
	Sleep func(time.Duration)
}

// Daemon runs the detect → diff → update loop.
type Daemon struct {
	cfg          Config
	mu           sync.Mutex
	current      net.IP
	failures     int
	lastSuccess  time.Time
	totalUpdates int
}

// New builds a Daemon. Returns an error if Config is missing fields.
func New(cfg Config) (*Daemon, error) {
	if cfg.Update == nil {
		return nil, errors.New("dyndns: Update callback required")
	}
	if len(cfg.Detectors) == 0 {
		cfg.Detectors = DefaultDetectors()
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 5 * time.Minute
	}
	if cfg.PollInterval < 30*time.Second {
		return nil, fmt.Errorf("dyndns: PollInterval %v < 30s minimum", cfg.PollInterval)
	}
	if cfg.QuorumSize == 0 {
		cfg.QuorumSize = (len(cfg.Detectors) + 1) / 2
		if cfg.QuorumSize < 1 {
			cfg.QuorumSize = 1
		}
	}
	if cfg.QuorumSize > len(cfg.Detectors) {
		return nil, fmt.Errorf("dyndns: QuorumSize %d > %d detectors", cfg.QuorumSize, len(cfg.Detectors))
	}
	if cfg.MaxBackoff == 0 {
		cfg.MaxBackoff = 1 * time.Hour
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Sleep == nil {
		cfg.Sleep = time.Sleep
	}
	return &Daemon{cfg: cfg}, nil
}

// Current returns the most recently confirmed IP. Empty until first
// successful detection.
func (d *Daemon) Current() net.IP {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.current == nil {
		return nil
	}
	cp := make(net.IP, len(d.current))
	copy(cp, d.current)
	return cp
}

// TotalUpdates reports the cumulative count of successful Update calls.
func (d *Daemon) TotalUpdates() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.totalUpdates
}

// Tick executes one detect/diff/update cycle. Tests call this
// directly to bypass the timer; Run() invokes it in a loop.
func (d *Daemon) Tick(ctx context.Context) error {
	ip, err := d.detectQuorum(ctx)
	if err != nil {
		d.recordFailure()
		return err
	}

	d.mu.Lock()
	currentSame := d.current != nil && d.current.Equal(ip)
	d.mu.Unlock()
	if currentSame {
		d.recordSuccess()
		return nil
	}

	if err := d.cfg.Update(ctx, ip); err != nil {
		d.recordFailure()
		return fmt.Errorf("dyndns: update IP %s: %w", ip, err)
	}

	d.mu.Lock()
	d.current = ip
	d.totalUpdates++
	d.mu.Unlock()
	d.recordSuccess()
	return nil
}

// Run blocks until ctx is cancelled, ticking every PollInterval.
// Backoff applies after consecutive failures.
func (d *Daemon) Run(ctx context.Context) error {
	for {
		if err := d.Tick(ctx); err != nil {
			// continue the loop; backoff is applied via nextSleep.
		}
		delay := d.nextSleep()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeAfter(delay):
		}
	}
}

// detectQuorum runs every detector in parallel + returns the IP that
// at least QuorumSize detectors agree on. Errors from individual
// detectors are tolerated; only complete consensus-failure is fatal.
func (d *Daemon) detectQuorum(ctx context.Context) (net.IP, error) {
	type result struct {
		ip   net.IP
		name string
		err  error
	}
	results := make(chan result, len(d.cfg.Detectors))
	for _, det := range d.cfg.Detectors {
		go func(det IPDetector) {
			ip, err := det.Detect(ctx)
			results <- result{ip: ip, name: det.Name(), err: err}
		}(det)
	}
	tally := map[string]int{}
	errs := []string{}
	for i := 0; i < len(d.cfg.Detectors); i++ {
		r := <-results
		if r.err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", r.name, r.err))
			continue
		}
		if r.ip == nil {
			continue
		}
		tally[r.ip.String()]++
	}
	var winner string
	for ipStr, votes := range tally {
		if votes >= d.cfg.QuorumSize && votes > tally[winner] {
			winner = ipStr
		}
	}
	if winner == "" {
		return nil, fmt.Errorf("dyndns: no IP reached quorum (%d/%d); errors: %s", d.cfg.QuorumSize, len(d.cfg.Detectors), strings.Join(errs, "; "))
	}
	return net.ParseIP(winner), nil
}

func (d *Daemon) recordSuccess() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.failures = 0
	d.lastSuccess = d.cfg.Now()
}

func (d *Daemon) recordFailure() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.failures++
}

func (d *Daemon) nextSleep() time.Duration {
	d.mu.Lock()
	failures := d.failures
	d.mu.Unlock()
	if failures == 0 {
		return d.cfg.PollInterval
	}
	// Exponential backoff: 2^n × PollInterval, capped at MaxBackoff,
	// with ±10% jitter to avoid thundering herd.
	base := d.cfg.PollInterval
	for i := 0; i < failures && base < d.cfg.MaxBackoff; i++ {
		base *= 2
	}
	if base > d.cfg.MaxBackoff {
		base = d.cfg.MaxBackoff
	}
	jitter := time.Duration(rand.Int64N(int64(base) / 5))
	if rand.IntN(2) == 0 {
		base += jitter
	} else {
		base -= jitter
	}
	return base
}

// timeAfter is injectable for tests but defaults to time.After.
var timeAfter = func(d time.Duration) <-chan time.Time {
	return time.After(d)
}

// HTTPDetector queries a simple "what's my IP" HTTP endpoint.
type HTTPDetector struct {
	URL   string
	Label string
	HTTP  *http.Client
}

// Detect implements IPDetector.
func (h HTTPDetector) Detect(ctx context.Context) (net.IP, error) {
	client := h.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	s := strings.TrimSpace(string(body))
	ip := net.ParseIP(s)
	if ip == nil {
		return nil, fmt.Errorf("not an IP: %q", s)
	}
	return ip, nil
}

// Name implements IPDetector.
func (h HTTPDetector) Name() string {
	if h.Label != "" {
		return h.Label
	}
	return h.URL
}

// DefaultDetectors returns the three-source quorum used when no
// detectors are configured.
func DefaultDetectors() []IPDetector {
	return []IPDetector{
		HTTPDetector{URL: "https://icanhazip.com", Label: "icanhazip"},
		HTTPDetector{URL: "https://ifconfig.me/ip", Label: "ifconfig.me"},
		HTTPDetector{URL: "https://api.ipify.org", Label: "ipify"},
	}
}
