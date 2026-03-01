package config

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatcherOption configures a ConfigWatcher.
type WatcherOption func(*ConfigWatcher)

// WithWatchDebounce sets the debounce duration for file change events.
func WithWatchDebounce(d time.Duration) WatcherOption {
	return func(w *ConfigWatcher) { w.debounce = d }
}

// WithWatchLogger sets the logger for the watcher.
func WithWatchLogger(l *slog.Logger) WatcherOption {
	return func(w *ConfigWatcher) { w.logger = l }
}

// ConfigWatcher monitors a config file for changes and invokes a callback.
// It watches the directory containing the file for atomic-save compatibility.
type ConfigWatcher struct {
	source   *FileSource
	debounce time.Duration
	logger   *slog.Logger
	onChange func(ConfigChangeEvent)

	fsWatcher *fsnotify.Watcher
	done      chan struct{}
	stopOnce  sync.Once
	wg        sync.WaitGroup
	lastHash  string

	mu      sync.Mutex
	pending map[string]time.Time // path -> last event time
}

// NewConfigWatcher creates a ConfigWatcher for the given FileSource.
// onChange is called with a ConfigChangeEvent whenever the config changes.
func NewConfigWatcher(source *FileSource, onChange func(ConfigChangeEvent), opts ...WatcherOption) *ConfigWatcher {
	w := &ConfigWatcher{
		source:   source,
		debounce: 500 * time.Millisecond,
		logger:   slog.Default(),
		onChange: onChange,
		done:     make(chan struct{}),
		pending:  make(map[string]time.Time),
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Start begins watching the config file's directory for changes.
func (w *ConfigWatcher) Start() error {
	ctx := context.Background()
	hash, err := w.source.Hash(ctx)
	if err != nil {
		return fmt.Errorf("config watcher: initial hash: %w", err)
	}
	w.lastHash = hash

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("config watcher: create fsnotify: %w", err)
	}
	w.fsWatcher = fsw

	// Watch the directory so we catch atomic saves (rename-over).
	dir := filepath.Dir(w.source.Path())
	if err := fsw.Add(dir); err != nil {
		_ = fsw.Close()
		return fmt.Errorf("config watcher: watch %s: %w", dir, err)
	}

	w.wg.Add(1)
	go w.loop()
	return nil
}

// Stop terminates the watcher and waits for the background goroutine to exit.
// It is safe to call Stop multiple times.
func (w *ConfigWatcher) Stop() error {
	w.stopOnce.Do(func() { close(w.done) })
	w.wg.Wait()
	if w.fsWatcher != nil {
		return w.fsWatcher.Close()
	}
	return nil
}

func (w *ConfigWatcher) loop() {
	defer w.wg.Done()

	ticker := time.NewTicker(w.debounce)
	defer ticker.Stop()

	for {
		select {
		case <-w.done:
			return

		case event, ok := <-w.fsWatcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
				// On any write/create/rename in the watched directory, enqueue
				// the config path for a hash check. This handles:
				// - Direct YAML file writes
				// - Atomic saves (rename-over) by editors
				// - Kubernetes ConfigMap updates (symlink swaps on ..data)
				// The hash check in processChange prevents spurious reloads.
				w.mu.Lock()
				w.pending[w.source.Path()] = time.Now()
				w.mu.Unlock()
			}

		case err, ok := <-w.fsWatcher.Errors:
			if !ok {
				return
			}
			w.logger.Error("config watcher error", "err", err)

		case <-ticker.C:
			w.processPending()
		}
	}
}

func (w *ConfigWatcher) processPending() {
	w.mu.Lock()
	now := time.Now()
	var ready []string
	for path, t := range w.pending {
		if now.Sub(t) >= w.debounce {
			ready = append(ready, path)
		}
	}
	for _, path := range ready {
		delete(w.pending, path)
	}
	w.mu.Unlock()

	for _, path := range ready {
		w.processChange(path)
	}
}

// processChange loads the config, computes its hash, and calls onChange if
// the content has actually changed since the last known hash.
func (w *ConfigWatcher) processChange(path string) {
	// Only react to events for the file we care about.
	if filepath.Clean(path) != filepath.Clean(w.source.Path()) {
		return
	}

	ctx := context.Background()

	cfg, err := w.source.Load(ctx)
	if err != nil {
		w.logger.Error("config watcher: failed to load config", "path", path, "err", err)
		return
	}

	newHash, err := w.source.Hash(ctx)
	if err != nil {
		w.logger.Error("config watcher: failed to hash config", "path", path, "err", err)
		return
	}

	if newHash == w.lastHash {
		w.logger.Debug("config watcher: content unchanged, skipping", "path", path)
		return
	}

	oldHash := w.lastHash
	w.lastHash = newHash

	w.logger.Info("config changed", "path", path, "old_hash", oldHash[:8], "new_hash", newHash[:8])

	w.onChange(ConfigChangeEvent{
		Source:  w.source.Name(),
		OldHash: oldHash,
		NewHash: newHash,
		Config:  cfg,
		Time:    time.Now(),
	})
}

func isYAMLFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".yaml" || ext == ".yml"
}
