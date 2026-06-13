package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRewriteEnvRefsInStringOnlyMappedRefs(t *testing.T) {
	got := rewriteEnvRefsInString("${NAMECHEAP_API_KEY}:${UNCHANGED}:${NAMECHEAP_API_KEY_SUFFIX}", map[string]string{
		"NAMECHEAP_API_KEY": "GCA_NC_API_KEY",
	})
	want := "${GCA_NC_API_KEY}:${UNCHANGED}:${NAMECHEAP_API_KEY_SUFFIX}"
	if got != want {
		t.Fatalf("rewrite = %q, want %q", got, want)
	}
}

func TestRewriteEnvRefsInStringDoesNotChainMappings(t *testing.T) {
	got := rewriteEnvRefsInString("${A}:${B}", map[string]string{
		"A": "B",
		"B": "C",
	})
	want := "${B}:${C}"
	if got != want {
		t.Fatalf("rewrite = %q, want one-pass %q", got, want)
	}
}

func TestRewriteEnvRefsInFilePreservesCommentsAndUnrelatedRefs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "infra.yaml")
	if err := os.WriteFile(path, []byte(`# provider config
providers:
  namecheap:
    api_key: ${NAMECHEAP_API_KEY}
    api_user: ${NAMECHEAP_API_USER}
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	changed, err := rewriteEnvRefsInFile(path, map[string]string{
		"NAMECHEAP_API_KEY": "GCA_NC_API_KEY",
	})
	if err != nil {
		t.Fatalf("rewriteEnvRefsInFile: %v", err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read rewritten config: %v", err)
	}
	text := string(data)
	for _, want := range []string{"# provider config", "${GCA_NC_API_KEY}", "${NAMECHEAP_API_USER}"} {
		if !strings.Contains(text, want) {
			t.Fatalf("rewritten config missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "${NAMECHEAP_API_KEY}") {
		t.Fatalf("old env ref still present:\n%s", text)
	}
}
