package diffcache

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

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
	// Refresh mtime so [maybeEvict] (which orders by mtime) treats this
	// entry as recently-used. Without this, frequently-read but
	// infrequently-rewritten entries get evicted as if they were stale —
	// the cache would be FIFO-by-write, not LRU. We chose mtime-touch
	// over a sidecar "last-accessed" file to keep the on-disk shape
	// trivial; the small cost is one extra syscall per cache hit.
	// Errors are intentionally ignored: a Chtimes failure degrades
	// eviction precision (this entry may be evicted earlier than
	// preferred) but never produces wrong cache results.
	now := time.Now()
	_ = os.Chtimes(path, now, now)
	return env.Result, true
}

// Put writes the cache entry atomically via write-temp-then-rename.
// POSIX rename(2) is atomic on the same filesystem — if the process
// crashes mid-write the temp file is orphaned but the final cache
// path is either the prior contents or the new contents, never a
// partial write. The corruption-recovery path in [Get] is still the
// safety net for cross-filesystem renames or NFS edge cases that
// don't honor atomicity, but with this pattern the corruption
// recovery essentially never fires in production.
//
// Concurrency: safe for concurrent use, including concurrent Puts
// of the same Key. Each Put uses [os.CreateTemp] to obtain a unique
// temp filename (`<key>.json.<random>.tmp`) so two goroutines writing
// the same Key cannot clobber each other's temp file. The final
// rename is racy in the sense that one goroutine's payload "wins,"
// but both payloads were derived from the caller's DiffResult so the
// outcome is deterministic from the caller's perspective.
//
// Windows portability: this implementation uses the bare [os.Rename]
// for the atomic publish step, which matches the precedent set by
// other rename sites in this repo (cmd/wfctl/update.go,
// cmd/wfctl/plugin_install.go). On Windows, [os.Rename] fails when
// the destination already exists, so an in-place cache update via
// Put will fail on Windows; the caller treats this as a write
// failure and proceeds without caching (correct, by the cache-as-
// amortization framing in the package godoc — apply remains correct
// on a 100% miss rate). A future improvement is to vendor
// github.com/google/renameio for cross-platform atomic rename;
// doing so here would introduce the first such dependency in the
// repo, so deferred until there's a Windows-supported wfctl use
// case. Tracked as a known limitation in the package godoc.
//
// Disk-side errors during Put are intentionally silent: the next Get
// will miss (correct), and the operator already has "stuff isn't
// working" signal from elsewhere. The cache-as-amortization framing
// in the package godoc sets the expectation.
func (c *filesystemCache) Put(k Key, result interfaces.DiffResult) {
	if err := os.MkdirAll(c.dir, 0o750); err != nil {
		return
	}
	env := envelope{SchemaVersion: cacheSchemaVersion, Result: result}
	data, err := json.Marshal(env)
	if err != nil {
		return
	}
	path := c.pathFor(k)
	// os.CreateTemp gives us a per-call unique tempfile name and an
	// open *os.File. Pattern uses a "*" suffix so the random token
	// lands before ".tmp", giving filenames like
	// `<sha256>.json.123456789.tmp` — easy to spot during eviction
	// and unambiguous about origin. Two concurrent Puts of the same
	// Key end up with two distinct temp files; whichever rename
	// completes last wins for the final cache path.
	tmpFile, err := os.CreateTemp(c.dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return
	}
	tmp := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmp)
		return
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmp)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		// Best-effort cleanup of the orphaned temp file. On Windows
		// this rename can fail when path already exists (see godoc
		// above); on Unix it's atomic-replace. Either way the next
		// Put may succeed and LRU eviction reclaims any orphans.
		_ = os.Remove(tmp)
		return
	}
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
