package sensitiveinputs

import (
	"strings"
	"testing"
)

func TestSensitiveInputDiagnosticRedactorTargetsDeclaredValuesOnly(t *testing.T) {
	config := map[string]any{"password": "private-runtime-value", "nested": map[string]any{"token": "private-runtime-value-extra"}, "customer": "AWS_SECRET_ACCESS_KEY=consumer-payload"}
	redact, err := DiagnosticRedactor(config, []string{"/password", "/nested/token"})
	if err != nil {
		t.Fatal(err)
	}
	got := redact("provider rejected private-runtime-value-extra and private-runtime-value; AWS_SECRET_ACCESS_KEY=consumer-payload")
	if strings.Contains(got, "private-runtime-value") || !strings.Contains(got, "AWS_SECRET_ACCESS_KEY=consumer-payload") {
		t.Fatalf("redactor must affect only declared exact runtime values: %s", got)
	}
	if config["password"] != "private-runtime-value" || config["customer"] != "AWS_SECRET_ACCESS_KEY=consumer-payload" {
		t.Fatal("redactor construction mutated provider inputs")
	}
}
