package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestSQLiteEventStorePragmaConfiguration(t *testing.T) {
	for _, name := range []string{"new_file", "existing_file", "memory"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "event store.db")
			wantJournal := "wal"
			if name == "memory" {
				path = ":memory:"
				wantJournal = "memory"
			}
			if name == "existing_file" {
				db, err := sql.Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = db.Close() })
				if _, err := db.Exec("CREATE TABLE legacy_record (value TEXT)"); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec("INSERT INTO legacy_record VALUES (?)", "retained"); err != nil {
					t.Fatal(err)
				}
				var mode string
				if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
					t.Fatal(err)
				}
				if mode != "delete" {
					t.Fatalf("legacy fixture journal_mode = %q, want delete", mode)
				}
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			}

			s, err := NewSQLiteEventStore(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.Close() })
			assertSQLiteEventStorePragmas(t, s, map[string]string{
				"journal_mode": wantJournal,
				"busy_timeout": "5000",
				"synchronous":  "2",
				"foreign_keys": "1",
			})

			id := uuid.New()
			if err := s.Append(t.Context(), id, EventExecutionStarted, map[string]any{"pipeline": "pragma-test"}); err != nil {
				t.Fatal(err)
			}
			events, err := s.GetEvents(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			if len(events) != 1 || events[0].EventType != EventExecutionStarted {
				t.Fatalf("append/read returned unexpected events: %#v", events)
			}
			if name == "existing_file" {
				var value string
				if err := s.db.QueryRowContext(t.Context(), "SELECT value FROM legacy_record").Scan(&value); err != nil {
					t.Fatal(err)
				}
				if value != "retained" {
					t.Fatalf("legacy data = %q, want retained", value)
				}
			}
		})
	}
}

func TestSQLiteEventStorePragmasOnConnectionReplacement(t *testing.T) {
	s, err := NewSQLiteEventStore(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	before := s.db.Stats().MaxIdleClosed
	s.db.SetMaxIdleConns(0)
	if s.db.Stats().MaxIdleClosed <= before {
		t.Fatal("did not close the constructor's idle connection")
	}

	assertSQLiteEventStorePragmas(t, s, map[string]string{
		"journal_mode": "wal",
		"busy_timeout": "5000",
		"synchronous":  "2",
	})
}

func assertSQLiteEventStorePragmas(t *testing.T, s *SQLiteEventStore, want map[string]string) {
	t.Helper()
	conn, err := s.db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for name, expected := range want {
		var got string
		if err := conn.QueryRowContext(t.Context(), "PRAGMA "+name).Scan(&got); err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if got != expected {
			t.Errorf("%s = %q, want %q", name, got, expected)
		}
	}
}
