package sdk

import (
	"strings"
	"testing"
)

func TestEmbedManifestHappyPath(t *testing.T) {
	src := []byte(`{
        "name": "test-plugin",
        "version": "0.2.0",
        "author": "GoCodeAlone",
        "description": "test plugin",
        "configMutable": true,
        "sampleCategory": "iac"
    }`)
	m, err := EmbedManifest(src)
	if err != nil {
		t.Fatalf("EmbedManifest: %v", err)
	}
	if m.Name != "test-plugin" {
		t.Fatalf("Name = %q, want test-plugin", m.Name)
	}
	if m.Version != "0.2.0" {
		t.Fatalf("Version = %q, want 0.2.0", m.Version)
	}
	if !m.ConfigMutable {
		t.Fatalf("ConfigMutable = false, want true")
	}
	if m.SampleCategory != "iac" {
		t.Fatalf("SampleCategory = %q, want iac", m.SampleCategory)
	}
}

func TestEmbedManifestRejectsEmpty(t *testing.T) {
	_, err := EmbedManifest(nil)
	if err == nil {
		t.Fatalf("EmbedManifest(nil): want error, got nil")
	}
	_, err = EmbedManifest([]byte{})
	if err == nil {
		t.Fatalf("EmbedManifest([]byte{}): want error, got nil")
	}
}

func TestEmbedManifestRejectsMalformedJSON(t *testing.T) {
	_, err := EmbedManifest([]byte(`{not json`))
	if err == nil {
		t.Fatalf("EmbedManifest(malformed): want error, got nil")
	}
	if !strings.Contains(err.Error(), "parse embedded plugin.json") {
		t.Fatalf("error message = %q, want containing 'parse embedded plugin.json'", err.Error())
	}
}

func TestEmbedManifestRejectsMissingName(t *testing.T) {
	_, err := EmbedManifest([]byte(`{"version": "1.0.0", "author": "x", "description": "x"}`))
	if err == nil {
		t.Fatalf("EmbedManifest without name: want error, got nil")
	}
	if !strings.Contains(err.Error(), "validate") {
		t.Fatalf("error message = %q, want containing 'validate'", err.Error())
	}
}

func TestEmbedManifestRejectsMissingVersion(t *testing.T) {
	_, err := EmbedManifest([]byte(`{"name": "x", "author": "x", "description": "x"}`))
	if err == nil {
		t.Fatalf("EmbedManifest without version: want error, got nil")
	}
}

// F4 — Validate() also requires Author + Description. EmbedManifest must
// surface these as actionable errors, not silent acceptance.
func TestEmbedManifestRejectsMissingAuthor(t *testing.T) {
	_, err := EmbedManifest([]byte(`{"name": "x", "version": "1.0.0", "description": "x"}`))
	if err == nil {
		t.Fatalf("EmbedManifest without author: want error, got nil")
	}
}

func TestEmbedManifestRejectsMissingDescription(t *testing.T) {
	_, err := EmbedManifest([]byte(`{"name": "x", "version": "1.0.0", "author": "x"}`))
	if err == nil {
		t.Fatalf("EmbedManifest without description: want error, got nil")
	}
}

// R2-2 — Validate also enforces semver shape on Version and regex shape on Name.
func TestEmbedManifestRejectsInvalidSemver(t *testing.T) {
	_, err := EmbedManifest([]byte(`{"name": "x", "version": "abc", "author": "a", "description": "d"}`))
	if err == nil {
		t.Fatalf("EmbedManifest with non-semver version: want error, got nil")
	}
}

func TestEmbedManifestRejectsInvalidNameShape(t *testing.T) {
	_, err := EmbedManifest([]byte(`{"name": "BadName", "version": "1.0.0", "author": "a", "description": "d"}`))
	if err == nil {
		t.Fatalf("EmbedManifest with uppercase name: want error, got nil")
	}
}

func TestMustEmbedManifestPanicsOnError(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("MustEmbedManifest(malformed): want panic, got none")
		}
	}()
	_ = MustEmbedManifest([]byte(`{bad`))
}
