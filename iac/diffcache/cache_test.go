package diffcache

import (
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestCache_FilesystemRoundtrip verifies the basic Get/Put roundtrip on
// a filesystem-backed cache: Put a DiffResult under a Key, then Get the
// same key returns the same DiffResult with hit=true.
func TestCache_FilesystemRoundtrip(t *testing.T) {
	dir := t.TempDir()
	c := NewFilesystem(dir)
	key := Key{
		PluginVersion: "do@v0.10.0",
		Type:          "infra.vpc",
		ProviderID:    "vpc-abc",
		SHAConfig:     "deadbeef",
		SHAOutputs:    "cafebabe",
	}
	want := interfaces.DiffResult{
		NeedsUpdate: true,
		Changes: []interfaces.FieldChange{
			{Path: "size", Old: "s-1vcpu-1gb", New: "s-2vcpu-2gb"},
		},
	}
	c.Put(key, want)
	got, hit := c.Get(key)
	if !hit {
		t.Fatal("expected cache hit after Put")
	}
	if got.NeedsUpdate != want.NeedsUpdate {
		t.Errorf("NeedsUpdate: got %v want %v", got.NeedsUpdate, want.NeedsUpdate)
	}
	if len(got.Changes) != 1 || got.Changes[0].Path != "size" {
		t.Errorf("Changes: got %+v want one entry with Path=size", got.Changes)
	}
}

// TestCache_FilesystemMissOnDifferentKey verifies key isolation: a Put
// under one key does not service a Get under any of the 5 fields'
// distinct values. Each field must contribute to the key independently.
func TestCache_FilesystemMissOnDifferentKey(t *testing.T) {
	dir := t.TempDir()
	c := NewFilesystem(dir)
	base := Key{PluginVersion: "v1", Type: "T", ProviderID: "P", SHAConfig: "C", SHAOutputs: "O"}
	c.Put(base, interfaces.DiffResult{NeedsUpdate: true})

	cases := map[string]Key{
		"diff-pluginversion": {PluginVersion: "v2", Type: "T", ProviderID: "P", SHAConfig: "C", SHAOutputs: "O"},
		"diff-type":          {PluginVersion: "v1", Type: "U", ProviderID: "P", SHAConfig: "C", SHAOutputs: "O"},
		"diff-providerid":    {PluginVersion: "v1", Type: "T", ProviderID: "Q", SHAConfig: "C", SHAOutputs: "O"},
		"diff-shaconfig":     {PluginVersion: "v1", Type: "T", ProviderID: "P", SHAConfig: "D", SHAOutputs: "O"},
		"diff-shaoutputs":    {PluginVersion: "v1", Type: "T", ProviderID: "P", SHAConfig: "C", SHAOutputs: "X"},
	}
	for name, k := range cases {
		t.Run(name, func(t *testing.T) {
			if _, hit := c.Get(k); hit {
				t.Errorf("expected miss for distinct key field; got hit on %+v", k)
			}
		})
	}
}

// TestCache_MemoryRoundtrip is the in-memory cache's basic test.
// Same contract as the filesystem cache.
func TestCache_MemoryRoundtrip(t *testing.T) {
	c := NewMemory()
	key := Key{Type: "infra.vpc", ProviderID: "vpc-abc"}
	want := interfaces.DiffResult{NeedsReplace: true}
	c.Put(key, want)
	got, hit := c.Get(key)
	if !hit {
		t.Fatal("expected cache hit after Put")
	}
	if got.NeedsReplace != want.NeedsReplace {
		t.Errorf("NeedsReplace: got %v want %v", got.NeedsReplace, want.NeedsReplace)
	}
}

// TestCache_NoopAlwaysMisses verifies the disabled cache: Put is a
// no-op, every Get returns hit=false.
func TestCache_NoopAlwaysMisses(t *testing.T) {
	c := NewNoop()
	key := Key{Type: "x"}
	c.Put(key, interfaces.DiffResult{NeedsUpdate: true})
	if _, hit := c.Get(key); hit {
		t.Error("noop cache should always miss")
	}
}

// TestCache_EnvDispatch verifies the New() factory's env-var driven
// dispatch: WFCTL_DIFFCACHE=disabled → noop; =:memory: → memory;
// default → filesystem (we just verify it's not noop/memory).
func TestCache_EnvDispatch(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		t.Setenv("WFCTL_DIFFCACHE", "disabled")
		c := New()
		if _, ok := c.(*noopCache); !ok {
			t.Errorf("WFCTL_DIFFCACHE=disabled should yield *noopCache; got %T", c)
		}
	})
	t.Run("memory", func(t *testing.T) {
		t.Setenv("WFCTL_DIFFCACHE", ":memory:")
		c := New()
		if _, ok := c.(*memoryCache); !ok {
			t.Errorf("WFCTL_DIFFCACHE=:memory: should yield *memoryCache; got %T", c)
		}
	})
	t.Run("default", func(t *testing.T) {
		// Set HOME to a tempdir so we don't pollute the real cache dir.
		t.Setenv("HOME", t.TempDir())
		t.Setenv("WFCTL_DIFFCACHE", "")
		c := New()
		if _, ok := c.(*filesystemCache); !ok {
			t.Errorf("default should yield *filesystemCache; got %T", c)
		}
	})
}
