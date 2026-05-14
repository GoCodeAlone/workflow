package external

import (
	"os"
	"regexp"
	"testing"
)

// The plugin SDK must NOT install a gRPC interceptor that logs request bodies —
// CreateModule requests carry inline credentials: blocks. This test fails if
// grpc.NewServer / grpc.NewClient anywhere in plugin/external/ is constructed
// with an *Interceptor option, forcing a reviewer to look. Covers Unary AND
// Stream interceptors — CreateModule is unary today, but a future streaming
// RPC carrying credentials must not slip a stream interceptor past this guard.
// See the cloud-sdk-extraction design, Security section.
func TestNoBodyLoggingInterceptor(t *testing.T) {
	interceptorOpt := regexp.MustCompile(`(Chain)?(Unary|Stream)(Server|Client)?Interceptor`)
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !match(name, ".go") || match(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if interceptorOpt.Match(b) {
			t.Fatalf("%s references a gRPC interceptor option — if it logs request "+
				"bodies it can leak inline credentials: blocks. Audit it and, if safe, "+
				"add an explicit allowlist entry to this test.", name)
		}
	}
}

func match(s, suffix string) bool { return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix }
