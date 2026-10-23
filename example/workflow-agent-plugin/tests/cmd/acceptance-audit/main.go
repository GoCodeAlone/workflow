// acceptance-audit lints retained sanitized host records without contacting a
// host or provider. A zero exit status is never a semantic acceptance verdict.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/GoCodeAlone/workflow/example/workflow-agent-plugin/tests/internal/acceptance"
)

const maxEvidenceBytes = 64 << 20

func auditInput(input io.Reader, output io.Writer) int {
	raw, err := io.ReadAll(io.LimitReader(input, maxEvidenceBytes+1))
	if err != nil || len(raw) > maxEvidenceBytes {
		return 2
	}
	var run acceptance.Run
	if json.Unmarshal(raw, &run) != nil {
		return 2
	}
	report := acceptance.Audit(run)
	hash := sha256.Sum256(raw)
	value := struct {
		acceptance.Report
		SourceSHA256 string `json:"source_SHA256"`
	}{report, hex.EncodeToString(hash[:])}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if encoder.Encode(value) != nil {
		return 2
	}
	if report.ViolatingCalls > 0 || len(report.InconsistentEvents) > 0 {
		return 1
	}
	return 0
}

func run(args []string, output io.Writer) int {
	if len(args) != 1 {
		return 2
	}
	file, err := os.Open(args[0])
	if err != nil {
		return 2
	}
	code := auditInput(file, output)
	if file.Close() != nil {
		return 2
	}
	return code
}

func main() {
	code := run(os.Args[1:], os.Stdout)
	if code == 2 {
		fmt.Fprintln(os.Stderr, "acceptance audit unavailable: provide one readable sanitized JSON record file within the 64 MiB limit")
	}
	os.Exit(code)
}
