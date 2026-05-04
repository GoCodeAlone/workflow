package diffcache

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// filesystemCache is the file-backed Cache implementation. Cache files
// live under cache.dir, named by the sha256 fingerprint of the Key
// with a .json extension, e.g.,
// `<cache.dir>/abcdef0123456789....json`. The schema-version envelope
// gates silent eviction on a future schema bump.
//
// Concurrency: Get is serialized through the read filesystem; Put
// uses an os.WriteFile that is atomic on POSIX-compliant filesystems
// for sizes <= PIPE_BUF, and for larger writes the worst case is a
// truncated file that the next Get treats as corruption (silent
// eviction). The eviction-on-overflow scan is guarded by evictMu so
// concurrent Puts don't multiply-evict.
//
// Logging: a single info-level log fires the first time corruption is
// observed in this process so an operator has a breadcrumb without
// log spam.
type filesystemCache struct {
	dir         string
	maxEntries  int
	maxBytes    int64
	evictMu     sync.Mutex
	corruptOnce sync.Once
}

// NewFilesystem returns a Cache whose entries are persisted under dir.
// The directory is created on first Put if absent. Callers may pass
// any directory; the [New] factory uses `~/.cache/wfctl/diff/`.
func NewFilesystem(dir string) Cache {
	return &filesystemCache{
		dir:        dir,
		maxEntries: defaultMaxEntries,
		maxBytes:   defaultMaxBytes,
	}
}

// pathFor returns the on-disk path for key. Exported (within-package)
// so tests can mutate the file directly to exercise the corruption
// recovery path.
func (c *filesystemCache) pathFor(k Key) string {
	return filepath.Join(c.dir, keyFingerprint(k)+".json")
}

func (c *filesystemCache) Get(k Key) (interfaces.DiffResult, bool) {
	path := c.pathFor(k)
	data, err := os.ReadFile(path)
	if err != nil {
		// Either file-not-found (cache miss, normal) or some other read
		// error (permissions, transient I/O). Both are treated as miss
		// — apply correctness must not hinge on Get success.
		return interfaces.DiffResult{}, false
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		c.handleCorruption(path, err)
		return interfaces.DiffResult{}, false
	}
	if env.SchemaVersion != cacheSchemaVersion {
		c.handleCorruption(path, errors.New("schema-version mismatch"))
		return interfaces.DiffResult{}, false
	}
	return env.Result, true
}

func (c *filesystemCache) Put(k Key, result interfaces.DiffResult) {
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		// Disk-side errors during Put are intentionally silent: the
		// next Get will miss (correct), and the operator already has
		// "stuff isn't working" signal from elsewhere.
		return
	}
	env := envelope{SchemaVersion: cacheSchemaVersion, Result: result}
	data, err := json.Marshal(env)
	if err != nil {
		return
	}
	path := c.pathFor(k)
	_ = os.WriteFile(path, data, 0o644)
	c.maybeEvict()
}

// handleCorruption silently deletes the corrupt file and emits a
// once-per-process info log. The single log gives operators a
// breadcrumb without spamming on every Get when an attacker or a
// rogue tool has filled the cache directory with garbage files.
func (c *filesystemCache) handleCorruption(path string, cause error) {
	_ = os.Remove(path)
	c.corruptOnce.Do(func() {
		log.Printf("info: diffcache: detected corrupted cache file %q (%v); silently re-Diffing. This message logs once per process.", path, cause)
	})
}

// maybeEvict scans the cache directory and, if either cap is exceeded,
// evicts the oldest evictionFraction (10%) of entries by mtime. The
// scan + sort cost is amortized: a single Put after N entries
// triggers one full scan; the next eviction-fraction Puts after that
// see no scan because the cap is back below threshold.
func (c *filesystemCache) maybeEvict() {
	c.evictMu.Lock()
	defer c.evictMu.Unlock()
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	// Filter to *.json files (defensive in case of stray files).
	type fileInfo struct {
		path  string
		mtime int64
		size  int64
	}
	files := make([]fileInfo, 0, len(entries))
	var totalBytes int64
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{
			path:  filepath.Join(c.dir, e.Name()),
			mtime: info.ModTime().UnixNano(),
			size:  info.Size(),
		})
		totalBytes += info.Size()
	}
	overCount := len(files) > c.maxEntries
	overBytes := totalBytes > c.maxBytes
	if !overCount && !overBytes {
		return
	}
	// Sort oldest-first by mtime. Files written in close succession
	// may share an mtime under some filesystems; the secondary sort
	// by path makes the order deterministic.
	sort.Slice(files, func(i, j int) bool {
		if files[i].mtime != files[j].mtime {
			return files[i].mtime < files[j].mtime
		}
		return files[i].path < files[j].path
	})
	// Evict the oldest fraction in one pass. Always at least 1 so
	// tiny caps still make progress.
	evictCount := max(int(float64(len(files))*evictionFraction), 1)
	evictCount = min(evictCount, len(files))
	for i := range evictCount {
		_ = os.Remove(files[i].path)
	}
}
