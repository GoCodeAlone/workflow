package dyndns

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubDetector returns a fixed IP or err.
type stubDetector struct {
	name string
	ip   net.IP
	err  error
}

func (s stubDetector) Detect(_ context.Context) (net.IP, error) {
	return s.ip, s.err
}
func (s stubDetector) Name() string { return s.name }

func TestDaemon_Tick_UpdatesOnIPChange(t *testing.T) {
	var calls int
	var updatedIP net.IP
	updater := func(_ context.Context, ip net.IP) error {
		calls++
		updatedIP = ip
		return nil
	}

	d, err := New(Config{
		Detectors:    []IPDetector{stubDetector{name: "a", ip: net.ParseIP("1.2.3.4")}},
		PollInterval: 30 * time.Second,
		QuorumSize:   1,
		Update:       updater,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := d.Tick(context.Background()); err != nil {
		t.Fatalf("first Tick: %v", err)
	}
	if calls != 1 {
		t.Errorf("updater calls = %d want 1", calls)
	}
	if !updatedIP.Equal(net.ParseIP("1.2.3.4")) {
		t.Errorf("updater IP = %v want 1.2.3.4", updatedIP)
	}
	if !d.Current().Equal(net.ParseIP("1.2.3.4")) {
		t.Errorf("daemon current = %v", d.Current())
	}
}

func TestDaemon_Tick_NoopWhenIPUnchanged(t *testing.T) {
	calls := 0
	updater := func(_ context.Context, _ net.IP) error {
		calls++
		return nil
	}
	d, _ := New(Config{
		Detectors:    []IPDetector{stubDetector{name: "a", ip: net.ParseIP("5.6.7.8")}},
		PollInterval: 30 * time.Second,
		QuorumSize:   1,
		Update:       updater,
	})
	_ = d.Tick(context.Background())
	_ = d.Tick(context.Background())
	_ = d.Tick(context.Background())
	if calls != 1 {
		t.Errorf("updater fired %d times; want 1 (subsequent ticks should be noops)", calls)
	}
	if d.TotalUpdates() != 1 {
		t.Errorf("TotalUpdates = %d want 1", d.TotalUpdates())
	}
}

func TestDaemon_Tick_QuorumRequiresMajority(t *testing.T) {
	updater := func(_ context.Context, _ net.IP) error { return nil }
	d, _ := New(Config{
		Detectors: []IPDetector{
			stubDetector{name: "a", ip: net.ParseIP("1.1.1.1")},
			stubDetector{name: "b", ip: net.ParseIP("2.2.2.2")},
			stubDetector{name: "c", ip: net.ParseIP("3.3.3.3")},
		},
		PollInterval: 30 * time.Second,
		Update:       updater,
	})
	err := d.Tick(context.Background())
	if err == nil {
		t.Fatal("expected quorum failure when no two detectors agree")
	}
	if !strings.Contains(err.Error(), "quorum") {
		t.Errorf("err = %v; want quorum error", err)
	}
}

func TestDaemon_Tick_QuorumSatisfiedBy2Of3(t *testing.T) {
	calls := 0
	updater := func(_ context.Context, _ net.IP) error {
		calls++
		return nil
	}
	d, _ := New(Config{
		Detectors: []IPDetector{
			stubDetector{name: "a", ip: net.ParseIP("9.9.9.9")},
			stubDetector{name: "b", ip: net.ParseIP("9.9.9.9")},
			stubDetector{name: "c", ip: net.ParseIP("1.1.1.1")},
		},
		PollInterval: 30 * time.Second,
		Update:       updater,
	})
	if err := d.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 update on 2-of-3 quorum; got %d", calls)
	}
	if !d.Current().Equal(net.ParseIP("9.9.9.9")) {
		t.Errorf("Current = %v want 9.9.9.9", d.Current())
	}
}

func TestDaemon_Tick_TolerateOneDetectorErr(t *testing.T) {
	d, _ := New(Config{
		Detectors: []IPDetector{
			stubDetector{name: "a", ip: net.ParseIP("1.2.3.4")},
			stubDetector{name: "b", err: errors.New("network")},
			stubDetector{name: "c", ip: net.ParseIP("1.2.3.4")},
		},
		PollInterval: 30 * time.Second,
		QuorumSize:   2,
		Update:       func(_ context.Context, _ net.IP) error { return nil },
	})
	if err := d.Tick(context.Background()); err != nil {
		t.Errorf("should succeed with 2 of 3: %v", err)
	}
}

func TestDaemon_Tick_FailureBackoffEscalates(t *testing.T) {
	d, _ := New(Config{
		Detectors: []IPDetector{
			stubDetector{name: "a", err: errors.New("x")},
			stubDetector{name: "b", err: errors.New("x")},
		},
		PollInterval: 30 * time.Second,
		Update:       func(_ context.Context, _ net.IP) error { return nil },
	})

	_ = d.Tick(context.Background())
	first := d.nextSleep()

	_ = d.Tick(context.Background())
	second := d.nextSleep()

	// second should be ≥ first (exponential backoff). With jitter ±10%
	// the strict inequality may not hold, so compare against a low
	// bound: second >= 2 * (PollInterval - 10%).
	minSecond := 2 * 30 * time.Second * 9 / 10
	if second < minSecond {
		t.Errorf("backoff did not escalate: first=%v second=%v (minExpected=%v)", first, second, minSecond)
	}
}

func TestNew_RequiresUpdater(t *testing.T) {
	_, err := New(Config{Detectors: []IPDetector{stubDetector{name: "a", ip: net.ParseIP("1.1.1.1")}}})
	if err == nil {
		t.Fatal("expected error when Update is nil")
	}
}

func TestNew_RequiresMinimumPollInterval(t *testing.T) {
	_, err := New(Config{
		Detectors:    []IPDetector{stubDetector{name: "a", ip: net.ParseIP("1.1.1.1")}},
		PollInterval: 10 * time.Second,
		Update:       func(_ context.Context, _ net.IP) error { return nil },
	})
	if err == nil {
		t.Fatal("expected error on <30s interval")
	}
}

func TestNew_QuorumDefaults(t *testing.T) {
	d, _ := New(Config{
		Detectors: []IPDetector{
			stubDetector{name: "a", ip: net.ParseIP("1.1.1.1")},
			stubDetector{name: "b", ip: net.ParseIP("1.1.1.1")},
			stubDetector{name: "c", ip: net.ParseIP("1.1.1.1")},
		},
		PollInterval: 30 * time.Second,
		Update:       func(_ context.Context, _ net.IP) error { return nil },
	})
	if d.cfg.QuorumSize != 2 {
		t.Errorf("default quorum for 3 detectors = %d want 2", d.cfg.QuorumSize)
	}
}

func TestDaemon_Tick_FailureSurfacesUpdaterError(t *testing.T) {
	updater := func(_ context.Context, _ net.IP) error {
		return errors.New("simulated DNS failure")
	}
	d, _ := New(Config{
		Detectors:    []IPDetector{stubDetector{name: "a", ip: net.ParseIP("1.2.3.4")}},
		PollInterval: 30 * time.Second,
		QuorumSize:   1,
		Update:       updater,
	})
	err := d.Tick(context.Background())
	if err == nil {
		t.Fatal("expected error from updater")
	}
	if !strings.Contains(err.Error(), "simulated DNS failure") {
		t.Errorf("err = %v want wrapped", err)
	}
	// Current should NOT be set when update fails.
	if d.Current() != nil {
		t.Errorf("Current should be nil after failed update; got %v", d.Current())
	}
}

func TestDaemon_Run_ExitsOnContextCancel(t *testing.T) {
	updater := func(_ context.Context, _ net.IP) error { return nil }
	d, _ := New(Config{
		Detectors:    []IPDetector{stubDetector{name: "a", ip: net.ParseIP("1.2.3.4")}},
		PollInterval: 30 * time.Second,
		QuorumSize:   1,
		Update:       updater,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run err = %v want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestDaemon_ConcurrentTickSafe(t *testing.T) {
	d, _ := New(Config{
		Detectors:    []IPDetector{stubDetector{name: "a", ip: net.ParseIP("1.2.3.4")}},
		PollInterval: 30 * time.Second,
		QuorumSize:   1,
		Update:       func(_ context.Context, _ net.IP) error { return nil },
	})
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = d.Tick(context.Background())
		}()
	}
	wg.Wait()
	if d.TotalUpdates() < 1 {
		t.Errorf("at least one update should fire from concurrent ticks; got %d", d.TotalUpdates())
	}
}
