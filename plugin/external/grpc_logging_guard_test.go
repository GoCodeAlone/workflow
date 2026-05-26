package external

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// interceptorAllowlist is the set of plugin/external/** Go files (path relative
// to plugin/external/) that are permitted to reference a gRPC interceptor
// option. A file lands here only after a reviewer has confirmed it does NOT log
// request bodies — CreateModule requests carry inline credentials: blocks.
// Empty by design: today nothing legitimately installs an interceptor.
var interceptorAllowlist = map[string]struct{}{}

// isGeneratedProtoFile reports whether path is protoc-generated code. Generated
// *_grpc.pb.go files reference grpc.UnaryServerInterceptor / StreamServerInterceptor
// in their service-registration types — those are type references in generated
// code, not an interceptor being *installed*, so they are not a body-logging risk.
func isGeneratedProtoFile(path string) bool {
	base := filepath.Base(path)
	if strings.HasSuffix(base, ".pb.go") {
		return true
	}
	// Anything under a proto/ directory is generated wire code.
	return strings.Contains(filepath.ToSlash(path), "/proto/") || strings.HasPrefix(filepath.ToSlash(path), "proto/")
}

// TestNoBodyLoggingInterceptor walks the WHOLE plugin/external/ tree (including
// subpackages like sdk/) and fails if any non-generated, non-test, non-allowlisted
// Go file constructs grpc.NewServer / grpc.NewClient with an *Interceptor option.
// A body-logging interceptor on a credential-carrying RPC leaks inline
// credentials: blocks. Covers Unary AND Stream, Server AND Client, plain AND
// Chain* variants. See the cloud-sdk-extraction design, Security section.
func TestNoBodyLoggingInterceptor(t *testing.T) {
	interceptorOpt := regexp.MustCompile(`(Chain)?(Unary|Stream)(Server|Client)?Interceptor`)

	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := filepath.ToSlash(path)
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		if isGeneratedProtoFile(rel) {
			return nil
		}
		if _, ok := interceptorAllowlist[rel]; ok {
			return nil
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if interceptorOpt.Match(b) {
			t.Errorf("%s references a gRPC interceptor option — if it logs request "+
				"bodies it can leak inline credentials: blocks. Audit it and, if safe, "+
				"add its plugin/external-relative path to interceptorAllowlist in this test.", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
