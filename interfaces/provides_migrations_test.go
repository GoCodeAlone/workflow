package interfaces_test

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// fakeMigrationProvider implements MigrationProvider for compile-time check.
type fakeMigrationProvider struct {
	files  fstest.MapFS
	deps   []string
	fsErr  error
}

func (f *fakeMigrationProvider) ProvidesMigrations() (fs.FS, error) {
	return f.files, f.fsErr
}

func (f *fakeMigrationProvider) MigrationsDependencies() []string {
	return f.deps
}

func TestMigrationProvider_Interface(t *testing.T) {
	var _ interfaces.MigrationProvider = (*fakeMigrationProvider)(nil)
}

func TestMigrationProvider_InMemoryFS(t *testing.T) {
	migrations := fstest.MapFS{
		"20260422000001_init.up.sql":   &fstest.MapFile{Data: []byte("CREATE TABLE tenants (id TEXT);")},
		"20260422000001_init.down.sql": &fstest.MapFile{Data: []byte("DROP TABLE tenants;")},
	}
	p := &fakeMigrationProvider{files: migrations, deps: []string{"workflow.auth"}}

	mfs, err := p.ProvidesMigrations()
	if err != nil {
		t.Fatalf("ProvidesMigrations() error: %v", err)
	}

	entries, err := fs.ReadDir(mfs, ".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 migration files, got %d", len(entries))
	}

	deps := p.MigrationsDependencies()
	if len(deps) != 1 || deps[0] != "workflow.auth" {
		t.Errorf("unexpected deps: %v", deps)
	}
}

func TestMigrationProvider_NoDependencies(t *testing.T) {
	p := &fakeMigrationProvider{files: fstest.MapFS{}}
	if deps := p.MigrationsDependencies(); deps != nil && len(deps) != 0 {
		t.Errorf("expected nil/empty deps, got %v", deps)
	}
}
