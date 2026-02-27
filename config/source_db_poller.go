package config

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// DatabasePoller periodically checks a DatabaseSource for config changes.
type DatabasePoller struct {
	source   *DatabaseSource
	interval time.Duration
	onChange func(ConfigChangeEvent)
	logger   *slog.Logger
	lastHash string

	done     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewDatabasePoller creates a DatabasePoller that calls onChange whenever the
// config stored in source changes.
func NewDatabasePoller(source *DatabaseSource, interval time.Duration, onChange func(ConfigChangeEvent), logger *slog.Logger) *DatabasePoller {
	return &DatabasePoller{
		source:   source,
		interval: interval,
		onChange: onChange,
		logger:   logger,
		done:     make(chan struct{}),
	}
}

// Start fetches the initial hash and launches the background polling goroutine.
func (p *DatabasePoller) Start(ctx context.Context) error {
	hash, err := p.source.Hash(ctx)
	if err != nil {
		return fmt.Errorf("db poller: initial hash: %w", err)
	}
	p.lastHash = hash

	p.wg.Add(1)
	go p.loop(ctx)
	return nil
}

// Stop signals the polling goroutine to exit and waits for it to finish.
// It is safe to call Stop multiple times.
func (p *DatabasePoller) Stop() {
	p.stopOnce.Do(func() { close(p.done) })
	p.wg.Wait()
}

func (p *DatabasePoller) loop(ctx context.Context) {
	defer p.wg.Done()
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.checkForChanges(ctx)
		}
	}
}

func (p *DatabasePoller) checkForChanges(ctx context.Context) {
	hash, err := p.source.Hash(ctx)
	if err != nil {
		p.logger.Error("DB config poll failed", "error", err)
		return
	}
	if hash == p.lastHash {
		return
	}

	cfg, err := p.source.Load(ctx)
	if err != nil {
		p.logger.Error("DB config load failed", "error", err)
		return
	}

	oldHash := p.lastHash
	p.lastHash = hash

	p.onChange(ConfigChangeEvent{
		Source:  p.source.Name(),
		OldHash: oldHash,
		NewHash: hash,
		Config:  cfg,
		Time:    time.Now(),
	})
}
