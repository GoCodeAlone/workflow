package module

import "net/http"

// SnapshotDefaultTransport captures the current value of http.DefaultTransport.
// Call this before a plugin is initialized to record the baseline.
func SnapshotDefaultTransport() http.RoundTripper {
	return http.DefaultTransport
}

// RestoreDefaultTransport resets http.DefaultTransport to the given snapshot.
func RestoreDefaultTransport(snapshot http.RoundTripper) {
	http.DefaultTransport = snapshot
}

// DetectTransportMutation reports whether http.DefaultTransport differs from
// the snapshot, indicating that something mutated the global transport.
func DetectTransportMutation(snapshot http.RoundTripper) bool {
	return http.DefaultTransport != snapshot
}
