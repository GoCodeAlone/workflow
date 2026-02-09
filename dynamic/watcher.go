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

// WatcherOption configures a Watcher.
type WatcherOption func(*Watcher)

// WithDebounce sets the debounce duration for file change events.
func WithDebounce(d time.Duration) WatcherOption {
	return func(w *Watcher) {
		w.debounce = d
	}
}

// WithLogger sets the logger for the watcher.
func WithLogger(l *log.Logger) WatcherOption {
	return func(w *Watcher) {
		w.logger = l
	}
}

// Watcher monitors a directory for .go file changes and hot-reloads components.
type Watcher struct {
	loader   *Loader
	dir      string
	debounce time.Duration
	logger   *log.Logger

	fsWatcher *fsnotify.Watcher
	done      chan struct{}
	wg        sync.WaitGroup

	mu       sync.Mutex
	pending  map[string]time.Time // path -> last event time
}

// NewWatcher creates a file system watcher that automatically reloads components
// when .go files in the watched directory change.
func NewWatcher(loader *Loader, dir string, opts ...WatcherOption) *Watcher {
	w := &Watcher{
		loader:   loader,
		dir:      dir,
		debounce: 500 * time.Millisecond,
		logger:   log.New(os.Stderr, "[dynamic-watcher] ", log.LstdFlags),
		done:     make(chan struct{}),
		pending:  make(map[string]time.Time),
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Start begins watching the directory for changes.
func (w *Watcher) Start() error {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	w.fsWatcher = fsw

	if err := fsw.Add(w.dir); err != nil {
		fsw.Close()
		return err
	}

	w.wg.Add(1)
	go w.loop()
	return nil
}

// Stop terminates the watcher.
func (w *Watcher) Stop() error {
	close(w.done)
	w.wg.Wait()
	if w.fsWatcher != nil {
		return w.fsWatcher.Close()
	}
	return nil
}

func (w *Watcher) loop() {
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

func (w *Watcher) processPending() {
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

func (w *Watcher) handleChange(path string) {
	id := fileToID(path)
	data, err := os.ReadFile(path)
	if err != nil {
		w.logger.Printf("failed to read %s: %v", path, err)
		return
	}

	_, err = w.loader.Reload(id, string(data))
	if err != nil {
		w.logger.Printf("failed to reload component %s: %v", id, err)
		return
	}
	w.logger.Printf("reloaded component %s from %s", id, path)
}

func (w *Watcher) handleRemove(path string) {
	id := fileToID(path)
	if err := w.loader.registry.Unregister(id); err != nil {
		w.logger.Printf("failed to unregister component %s: %v", id, err)
		return
	}
	w.logger.Printf("unregistered component %s (file removed)", id)
}

func isGoFile(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
}

func fileToID(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
