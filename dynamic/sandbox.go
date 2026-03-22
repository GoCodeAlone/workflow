package dynamic

import (
	"log/slog"
	"net/http"
)

// snapshotDefaultTransport captures the current http.DefaultTransport value.
func snapshotDefaultTransport() http.RoundTripper {
	return http.DefaultTransport
}

// restoreDefaultTransport resets http.DefaultTransport to the snapshot.
func restoreDefaultTransport(snapshot http.RoundTripper) {
	http.DefaultTransport = snapshot
}

// guardTransport checks whether http.DefaultTransport was mutated relative to
// the snapshot and, if so, logs a warning and restores the original to prevent
// dynamic components from affecting the rest of the process's HTTP behavior.
func guardTransport(snapshot http.RoundTripper, componentID string) {
	if http.DefaultTransport != snapshot {
		slog.Warn("dynamic component mutated http.DefaultTransport; restoring original",
			"component", componentID)
		restoreDefaultTransport(snapshot)
	}
}

// AllowedPackages defines the standard library packages that dynamically loaded
// components are permitted to import. Packages not in this list will be rejected
// during source validation.
var AllowedPackages = map[string]bool{
	// Safe standard library packages
	"fmt":             true,
	"strings":         true,
	"strconv":         true,
	"encoding/json":   true,
	"encoding/xml":    true,
	"encoding/csv":    true,
	"encoding/base64": true,
	"context":         true,
	"time":            true,
	"math":            true,
	"math/rand":       true,
	"sort":            true,
	"sync":            true,
	"sync/atomic":     true,
	"errors":          true,
	"io":              true,
	"bytes":           true,
	"bufio":           true,
	"unicode":         true,
	"unicode/utf8":    true,
	"regexp":          true,
	"path":            true,
	"net/url":         true,
	"net/http":        true,
	"log":             true,
	"maps":            true,
	"slices":          true,
	"crypto/aes":      true,
	"crypto/cipher":   true,
	"crypto/rand":     true,
	"crypto/sha256":   true,
	"crypto/hmac":     true,
	"crypto/md5":      true,
	"encoding/hex":    true,
	"hash":            true,
	"html":            true,
	"html/template":   true,
	"text/template":   true,
}

// BlockedPackages defines packages that are explicitly forbidden for security reasons.
var BlockedPackages = map[string]bool{
	"os/exec":        true,
	"syscall":        true,
	"unsafe":         true,
	"plugin":         true,
	"runtime/debug":  true,
	"reflect":        true,
	"os":             true,
	"net":            true,
	"crypto/tls":     true,
	"debug/elf":      true,
	"debug/macho":    true,
	"debug/pe":       true,
	"debug/plan9obj": true,
}

// IsPackageAllowed checks if a given import path is permitted in dynamic components.
func IsPackageAllowed(pkg string) bool {
	if BlockedPackages[pkg] {
		return false
	}
	return AllowedPackages[pkg]
}
