package module

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestCleanupPending_PostgresLifecycleLiveRoundTrip(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_URL")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_URL not set; needs a disposable PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("cannot connect to disposable PostgreSQL database")
	}
	defer admin.Close(context.Background()) //nolint:errcheck
	schema := "iac_cleanup_" + uuid.NewString()
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error("failed to clean up test schema")
		}
	}()
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("POSTGRES_TEST_URL must be a PostgreSQL URL")
	}
	query := parsed.Query()
	query.Set("search_path", quoted)
	parsed.RawQuery = query.Encode()
	// Exercise additive migration from a pre-lifecycle native state table.
	legacy := `CREATE TABLE ` + quoted + `.iac_resources (
name TEXT PRIMARY KEY, type TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT '',
applied_config JSONB NOT NULL DEFAULT '{}', outputs JSONB NOT NULL DEFAULT '{}',
created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`
	if _, err := admin.Exec(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "INSERT INTO "+quoted+".iac_resources (name, type) VALUES ('legacy', 'infra.database')"); err != nil {
		t.Fatal(err)
	}
	open := func() *PostgresIaCStateStore {
		t.Helper()
		store, err := NewPostgresIaCStateStore(ctx, parsed.String())
		if err != nil {
			t.Fatal("native state backend failed to migrate/open disposable schema")
		}
		return store
	}
	store := open()
	defer func() { store.conn.Close() }()
	old, err := store.GetState(ctx, "legacy")
	if err != nil || old == nil || old.Lifecycle != nil {
		t.Fatalf("legacy row compatibility failed: state=%+v err=%v", old, err)
	}
	for _, phase := range []interfaces.ResourcePhase{interfaces.ResourcePhaseActive, interfaces.ResourcePhaseSecretRoutingPending, interfaces.ResourcePhaseCloudDeletePending, interfaces.ResourcePhaseCloudDeletedSecretCleanupPending} {
		want := &IaCState{ResourceID: "database", ResourceType: "infra.database", ProviderID: "exact-id", Status: "active", Config: map[string]any{"password": "${PRIVATE_TOKEN}"}, Outputs: map[string]any{"public": "consumer-payload"}, Lifecycle: &interfaces.ResourceLifecycle{Generation: "generation-1", Phase: phase, Secrets: []interfaces.RoutedSecretReference{{Key: "exact-key", Store: "named", Provider: "file", Scope: "directory", Subject: "test-directory", VerifiedAbsent: phase == interfaces.ResourcePhaseCloudDeletedSecretCleanupPending}}}}
		if err := store.SaveState(ctx, want); err != nil {
			t.Fatal(err)
		}
		store.conn.Close()
		store = open()
		got, err := store.GetState(ctx, want.ResourceID)
		if err != nil || got == nil || !reflect.DeepEqual(got.Lifecycle, want.Lifecycle) || got.Config["password"] != "${PRIVATE_TOKEN}" || got.Outputs["public"] != "consumer-payload" {
			t.Fatalf("native SQL lifecycle lost across reconnect: state=%+v err=%v", got, err)
		}
		listed, err := store.ListStates(ctx, map[string]string{"resource_type": "infra.database"})
		if err != nil || len(listed) != 2 {
			t.Fatalf("native SQL list failed: count=%d err=%v", len(listed), err)
		}
		var data []byte
		if err := admin.QueryRow(ctx, "SELECT lifecycle FROM "+quoted+".iac_resources WHERE name=$1", want.ResourceID).Scan(&data); err != nil {
			t.Fatal(err)
		}
		var wire interfaces.ResourceLifecycle
		if err := json.Unmarshal(data, &wire); err != nil || !reflect.DeepEqual(&wire, want.Lifecycle) {
			t.Fatalf("actual JSONB does not match lifecycle: lifecycle=%+v err=%v", wire, err)
		}
	}
	if err := store.DeleteState(ctx, "database"); err != nil {
		t.Fatal(err)
	}
	if got, err := store.GetState(ctx, "database"); err != nil || got != nil {
		t.Fatalf("native SQL delete failed: state=%+v err=%v", got, err)
	}
}
