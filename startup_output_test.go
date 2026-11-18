package workflow

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

// Benchmark discovery runs package initialization before listing benchmark
// names. Diagnostics from an incompatible dependency must not contaminate that
// output on the architectures supported by the benchmark dependency stack.
func TestPackageInitializationIsQuiet(t *testing.T) {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skip("benchmark dependencies use a fallback on this architecture")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	// Re-execute the real test binary to include dependency init functions.
	// An empty listing selects no tests or benchmarks and cannot recurse here.
	output, err := exec.CommandContext(ctx, executable, "-test.list=^$").CombinedOutput()
	if err != nil {
		t.Fatalf("package initialization failed: %v\n%s", err, output)
	}
	if len(output) != 0 {
		t.Fatalf("package initialization polluted benchmark discovery output:\n%s", output)
	}
}
