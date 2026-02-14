package dynamic

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// PluginWatcherOption configures a PluginWatcher.
type PluginWatcherOption func(*PluginWatcher)

// WithPluginDebounce sets the debounce duration for file change events.
func WithPluginDebounce(d time.Duration) PluginWatcherOption {
	return func(w *PluginWatcher) {
		w.debounce = d
	}
}

// WithPluginLogger sets the logger for the watcher.
func WithPluginLogger(l *log.Logger) PluginWatcherOption {
	return func(w *PluginWatcher) {
		w.logger = l
	}
}

// WithDevMode enables development mode which relaxes validation for faster
// iteration (allows all stdlib imports and skips contract validation).
func WithDevMode(enabled bool) PluginWatcherOption {
	return func(w *PluginWatcher) {
		w.devMode = enabled
	}
}

// WithOnReload sets a callback invoked after a plugin is reloaded.
func WithOnReload(fn func(id string, err error)) PluginWatcherOption {
	return func(w *PluginWatcher) {
		w.onReload = fn
	}
}

// PluginWatcher monitors one or more plugin directories for .go file changes
// and hot-reloads components. Unlike the base Watcher, it supports watching
// multiple directories and has a dev mode for relaxed validation.
type PluginWatcher struct {
	loader   *Loader
	dirs     []string
	debounce time.Duration
	devMode  bool
	logger   *log.Logger
	onReload func(id string, err error)

	fsWatcher *fsnotify.Watcher
	done      chan struct{}
	wg        sync.WaitGroup

	mu      sync.Mutex
	pending map[string]time.Time
}

// NewPluginWatcher creates a watcher that monitors plugin directories for changes.
func NewPluginWatcher(loader *Loader, dirs []string, opts ...PluginWatcherOption) *PluginWatcher {
	w := &PluginWatcher{
		loader:   loader,
		dirs:     dirs,
		debounce: 500 * time.Millisecond,
		logger:   log.New(os.Stderr, "[plugin-watcher] ", log.LstdFlags),
		done:     make(chan struct{}),
		pending:  make(map[string]time.Time),
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Start begins watching all plugin directories for changes.
func (w *PluginWatcher) Start() error {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	w.fsWatcher = fsw

	for _, dir := range w.dirs {
		// Ensure directory exists
		if err := os.MkdirAll(dir, 0755); err != nil {
			_ = fsw.Close()
			return err
		}
		if err := fsw.Add(dir); err != nil {
			_ = fsw.Close()
			return err
		}
		w.logger.Printf("watching plugin directory: %s (dev_mode=%v)", dir, w.devMode)

		// Load existing plugins on startup
		w.loadExistingPlugins(dir)
	}

	w.wg.Add(1)
	go w.loop()
	return nil
}

// Stop terminates the watcher.
func (w *PluginWatcher) Stop() error {
	close(w.done)
	w.wg.Wait()
	if w.fsWatcher != nil {
		return w.fsWatcher.Close()
	}
	return nil
}

// DevMode returns whether development mode is enabled.
func (w *PluginWatcher) DevMode() bool {
	return w.devMode
}

func (w *PluginWatcher) loadExistingPlugins(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		w.logger.Printf("failed to read directory %s: %v", dir, err)
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !isGoFile(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		w.handleChange(path)
	}
}

func (w *PluginWatcher) loop() {
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
			if !isGoFile(event.Name) {
				continue
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				w.mu.Lock()
				w.pending[event.Name] = time.Now()
				w.mu.Unlock()
			}
			if event.Op&fsnotify.Remove != 0 {
				w.handleRemove(event.Name)
			}

		case err, ok := <-w.fsWatcher.Errors:
			if !ok {
				return
			}
			w.logger.Printf("watcher error: %v", err)

		case <-ticker.C:
			w.processPending()
		}
	}
}

func (w *PluginWatcher) processPending() {
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
		w.handleChange(path)
	}
}

func (w *PluginWatcher) handleChange(path string) {
	id := pluginFileToID(path)
	data, err := os.ReadFile(path)
	if err != nil {
		w.logger.Printf("failed to read %s: %v", path, err)
		w.notifyReload(id, err)
		return
	}

	source := string(data)

	// In dev mode, skip source validation for faster iteration
	if !w.devMode {
		if err := ValidateSource(source); err != nil {
			w.logger.Printf("validation failed for %s: %v", path, err)
			w.notifyReload(id, err)
			return
		}
	}

	_, err = w.loader.Reload(id, source)
	if err != nil {
		w.logger.Printf("failed to reload plugin %s: %v", id, err)
		w.notifyReload(id, err)
		return
	}
	w.logger.Printf("reloaded plugin %s from %s", id, path)
	w.notifyReload(id, nil)
}

func (w *PluginWatcher) handleRemove(path string) {
	id := pluginFileToID(path)
	if err := w.loader.registry.Unregister(id); err != nil {
		w.logger.Printf("failed to unregister plugin %s: %v", id, err)
		return
	}
	w.logger.Printf("unregistered plugin %s (file removed)", id)
}

func (w *PluginWatcher) notifyReload(id string, err error) {
	if w.onReload != nil {
		w.onReload(id, err)
	}
}

func pluginFileToID(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
