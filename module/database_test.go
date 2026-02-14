package module

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestNewWorkflowDatabase(t *testing.T) {
	config := DatabaseConfig{
		Driver:       "postgres",
		DSN:          "host=localhost dbname=test",
		MaxOpenConns: 10,
		MaxIdleConns: 5,
	}
	db := NewWorkflowDatabase("test-db", config)

	if db.Name() != "test-db" {
		t.Errorf("expected name 'test-db', got %q", db.Name())
	}
	if db.config.Driver != "postgres" {
		t.Errorf("expected driver 'postgres', got %q", db.config.Driver)
	}
	if db.config.MaxOpenConns != 10 {
		t.Errorf("expected MaxOpenConns 10, got %d", db.config.MaxOpenConns)
	}
	if db.DB() != nil {
		t.Error("expected nil DB before Open")
	}
}

func TestWorkflowDatabase_Init(t *testing.T) {
	app := CreateIsolatedApp(t)
	db := NewWorkflowDatabase("my-db", DatabaseConfig{})
	if err := db.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
}

func TestWorkflowDatabase_CloseWithoutOpen(t *testing.T) {
	db := NewWorkflowDatabase("test-db", DatabaseConfig{})
	if err := db.Close(); err != nil {
		t.Fatalf("Close without Open should not error, got %v", err)
	}
}

func TestWorkflowDatabase_PingWithoutOpen(t *testing.T) {
	db := NewWorkflowDatabase("test-db", DatabaseConfig{})
	err := db.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error when pinging without open database")
	}
	if err.Error() != "database not open" {
		t.Errorf("expected 'database not open' error, got %q", err.Error())
	}
}

func TestWorkflowDatabase_QueryWithoutOpen(t *testing.T) {
	db := NewWorkflowDatabase("test-db", DatabaseConfig{})
	_, err := db.Query(context.Background(), "SELECT 1")
	if err == nil {
		t.Fatal("expected error when querying without open database")
	}
}

func TestWorkflowDatabase_ExecuteWithoutOpen(t *testing.T) {
	db := NewWorkflowDatabase("test-db", DatabaseConfig{})
	_, err := db.Execute(context.Background(), "INSERT INTO t VALUES (1)")
	if err == nil {
		t.Fatal("expected error when executing without open database")
	}
}

func TestWorkflowDatabase_InsertRowEmpty(t *testing.T) {
	db := NewWorkflowDatabase("test-db", DatabaseConfig{})
	_, err := db.InsertRow(context.Background(), "users", map[string]any{})
	if err == nil {
		t.Fatal("expected error for empty data insert")
	}
	if err.Error() != "no data to insert" {
		t.Errorf("expected 'no data to insert' error, got %q", err.Error())
	}
}

func TestWorkflowDatabase_UpdateRowsEmpty(t *testing.T) {
	db := NewWorkflowDatabase("test-db", DatabaseConfig{})
	_, err := db.UpdateRows(context.Background(), "users", map[string]any{}, "id = $1", 1)
	if err == nil {
		t.Fatal("expected error for empty data update")
	}
	if err.Error() != "no data to update" {
		t.Errorf("expected 'no data to update' error, got %q", err.Error())
	}
}

// Test SQL building functions

func TestBuildInsertSQL(t *testing.T) {
	data := map[string]any{
		"name":  "Alice",
		"email": "alice@example.com",
		"age":   30,
	}

	sqlStr, values, err := BuildInsertSQL("users", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Keys are sorted: age, email, name
	expectedSQL := "INSERT INTO users (age, email, name) VALUES ($1, $2, $3)"
	if sqlStr != expectedSQL {
		t.Errorf("expected SQL:\n  %s\ngot:\n  %s", expectedSQL, sqlStr)
	}

	if len(values) != 3 {
		t.Fatalf("expected 3 values, got %d", len(values))
	}
	if values[0] != 30 {
		t.Errorf("expected first value 30, got %v", values[0])
	}
	if values[1] != "alice@example.com" {
		t.Errorf("expected second value 'alice@example.com', got %v", values[1])
	}
	if values[2] != "Alice" {
		t.Errorf("expected third value 'Alice', got %v", values[2])
	}
}

func TestBuildInsertSQL_Empty(t *testing.T) {
	sqlStr, values, err := BuildInsertSQL("users", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sqlStr != "" {
		t.Errorf("expected empty SQL, got %q", sqlStr)
	}
	if values != nil {
		t.Errorf("expected nil values, got %v", values)
	}
}

func TestBuildInsertSQL_SingleColumn(t *testing.T) {
	data := map[string]any{
		"name": "Bob",
	}

	sqlStr, values, err := BuildInsertSQL("users", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedSQL := "INSERT INTO users (name) VALUES ($1)"
	if sqlStr != expectedSQL {
		t.Errorf("expected SQL:\n  %s\ngot:\n  %s", expectedSQL, sqlStr)
	}
	if len(values) != 1 || values[0] != "Bob" {
		t.Errorf("unexpected values: %v", values)
	}
}

func TestBuildUpdateSQL(t *testing.T) {
	data := map[string]any{
		"name":  "Bob",
		"email": "bob@example.com",
	}

	sqlStr, values, err := BuildUpdateSQL("users", data, "id = $3", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Keys sorted: email, name
	expectedSQL := "UPDATE users SET email = $1, name = $2 WHERE id = $3"
	if sqlStr != expectedSQL {
		t.Errorf("expected SQL:\n  %s\ngot:\n  %s", expectedSQL, sqlStr)
	}

	if len(values) != 3 {
		t.Fatalf("expected 3 values, got %d", len(values))
	}
	if values[0] != "bob@example.com" {
		t.Errorf("expected first value 'bob@example.com', got %v", values[0])
	}
	if values[1] != "Bob" {
		t.Errorf("expected second value 'Bob', got %v", values[1])
	}
	if values[2] != 42 {
		t.Errorf("expected third value 42, got %v", values[2])
	}
}

func TestBuildUpdateSQL_NoWhere(t *testing.T) {
	data := map[string]any{
		"status": "active",
	}

	sqlStr, values, err := BuildUpdateSQL("users", data, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedSQL := "UPDATE users SET status = $1"
	if sqlStr != expectedSQL {
		t.Errorf("expected SQL:\n  %s\ngot:\n  %s", expectedSQL, sqlStr)
	}
	if len(values) != 1 || values[0] != "active" {
		t.Errorf("unexpected values: %v", values)
	}
}

func TestBuildUpdateSQL_Empty(t *testing.T) {
	sqlStr, values, err := BuildUpdateSQL("users", map[string]any{}, "id = 1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sqlStr != "" {
		t.Errorf("expected empty SQL, got %q", sqlStr)
	}
	if values != nil {
		t.Errorf("expected nil values, got %v", values)
	}
}

func TestBuildDeleteSQL(t *testing.T) {
	sqlStr, values, err := BuildDeleteSQL("users", "id = $1", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedSQL := "DELETE FROM users WHERE id = $1"
	if sqlStr != expectedSQL {
		t.Errorf("expected SQL:\n  %s\ngot:\n  %s", expectedSQL, sqlStr)
	}
	if len(values) != 1 || values[0] != 42 {
		t.Errorf("unexpected values: %v", values)
	}
}

func TestBuildDeleteSQL_NoWhere(t *testing.T) {
	sqlStr, values, err := BuildDeleteSQL("users", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedSQL := "DELETE FROM users"
	if sqlStr != expectedSQL {
		t.Errorf("expected SQL:\n  %s\ngot:\n  %s", expectedSQL, sqlStr)
	}
	if values != nil {
		t.Errorf("expected nil values, got %v", values)
	}
}

func TestBuildDeleteSQL_MultipleArgs(t *testing.T) {
	sqlStr, values, err := BuildDeleteSQL("orders", "status = $1 AND created_at < $2", "cancelled", "2024-01-01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sqlStr, "WHERE status = $1 AND created_at < $2") {
		t.Errorf("unexpected SQL: %s", sqlStr)
	}
	if len(values) != 2 {
		t.Fatalf("expected 2 values, got %d", len(values))
	}
}

func TestBuildSQL_InvalidTableName(t *testing.T) {
	// SQL injection attempt in table name
	_, _, err := BuildInsertSQL("users; DROP TABLE users--", map[string]any{"name": "test"})
	if err == nil {
		t.Fatal("expected error for SQL injection in table name")
	}
	if !strings.Contains(err.Error(), "invalid table name") {
		t.Errorf("expected 'invalid table name' error, got %q", err.Error())
	}

	_, _, err = BuildUpdateSQL("users; DROP TABLE users--", map[string]any{"name": "test"}, "id = $1", 1)
	if err == nil {
		t.Fatal("expected error for SQL injection in table name")
	}

	_, _, err = BuildDeleteSQL("users; DROP TABLE users--", "id = $1", 1)
	if err == nil {
		t.Fatal("expected error for SQL injection in table name")
	}
}

func TestBuildSQL_InvalidColumnName(t *testing.T) {
	_, _, err := BuildInsertSQL("users", map[string]any{"name; DROP TABLE users--": "test"})
	if err == nil {
		t.Fatal("expected error for SQL injection in column name")
	}
	if !strings.Contains(err.Error(), "invalid column name") {
		t.Errorf("expected 'invalid column name' error, got %q", err.Error())
	}
}

func TestValidateIdentifier(t *testing.T) {
	valid := []string{"users", "user_roles", "schema1.users", "my_table_2", "_private"}
	for _, id := range valid {
		if err := validateIdentifier(id); err != nil {
			t.Errorf("expected %q to be valid, got error: %v", id, err)
		}
	}

	invalid := []string{"", "1starts_with_number", "has space", "semi;colon", "dash-name", "quote'inject", "paren(s)", "users--", "DROP TABLE"}
	for _, id := range invalid {
		if err := validateIdentifier(id); err == nil {
			t.Errorf("expected %q to be invalid, but it was accepted", id)
		}
	}
}

// DatabaseIntegrationConnector tests

func TestNewDatabaseIntegrationConnector(t *testing.T) {
	db := NewWorkflowDatabase("test-db", DatabaseConfig{})
	conn := NewDatabaseIntegrationConnector("db-conn", db)

	if conn.GetName() != "db-conn" {
		t.Errorf("expected name 'db-conn', got %q", conn.GetName())
	}
	if conn.IsConnected() {
		t.Error("expected not connected initially")
	}
}

func TestDatabaseIntegrationConnector_ExecuteNotConnected(t *testing.T) {
	db := NewWorkflowDatabase("test-db", DatabaseConfig{})
	conn := NewDatabaseIntegrationConnector("db-conn", db)

	_, err := conn.Execute(context.Background(), "query", map[string]any{
		"sql": "SELECT 1",
	})
	if err == nil {
		t.Fatal("expected error when not connected")
	}
	if err.Error() != "connector not connected" {
		t.Errorf("expected 'connector not connected' error, got %q", err.Error())
	}
}

func TestDatabaseIntegrationConnector_ExecuteUnsupportedAction(t *testing.T) {
	db := NewWorkflowDatabase("test-db", DatabaseConfig{})
	conn := NewDatabaseIntegrationConnector("db-conn", db)
	conn.connected = true // bypass connection for unit testing

	_, err := conn.Execute(context.Background(), "unknown_action", map[string]any{})
	if err == nil {
		t.Fatal("expected error for unsupported action")
	}
	if !strings.Contains(err.Error(), "unsupported action") {
		t.Errorf("expected 'unsupported action' in error, got %q", err.Error())
	}
}

func TestDatabaseIntegrationConnector_QueryMissingSql(t *testing.T) {
	db := NewWorkflowDatabase("test-db", DatabaseConfig{})
	conn := NewDatabaseIntegrationConnector("db-conn", db)
	conn.connected = true

	_, err := conn.Execute(context.Background(), "query", map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing sql parameter")
	}
	if !strings.Contains(err.Error(), "sql parameter required") {
		t.Errorf("expected 'sql parameter required' in error, got %q", err.Error())
	}
}

func TestDatabaseIntegrationConnector_ExecuteMissingSql(t *testing.T) {
	db := NewWorkflowDatabase("test-db", DatabaseConfig{})
	conn := NewDatabaseIntegrationConnector("db-conn", db)
	conn.connected = true

	_, err := conn.Execute(context.Background(), "execute", map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing sql parameter")
	}
}

func TestDatabaseIntegrationConnector_InsertMissingTable(t *testing.T) {
	db := NewWorkflowDatabase("test-db", DatabaseConfig{})
	conn := NewDatabaseIntegrationConnector("db-conn", db)
	conn.connected = true

	_, err := conn.Execute(context.Background(), "insert", map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing table parameter")
	}
	if !strings.Contains(err.Error(), "table parameter required") {
		t.Errorf("expected 'table parameter required' in error, got %q", err.Error())
	}
}

func TestDatabaseIntegrationConnector_InsertMissingData(t *testing.T) {
	db := NewWorkflowDatabase("test-db", DatabaseConfig{})
	conn := NewDatabaseIntegrationConnector("db-conn", db)
	conn.connected = true

	_, err := conn.Execute(context.Background(), "insert", map[string]any{
		"table": "users",
	})
	if err == nil {
		t.Fatal("expected error for missing data parameter")
	}
	if !strings.Contains(err.Error(), "data parameter required") {
		t.Errorf("expected 'data parameter required' in error, got %q", err.Error())
	}
}

func TestDatabaseIntegrationConnector_UpdateMissingTable(t *testing.T) {
	db := NewWorkflowDatabase("test-db", DatabaseConfig{})
	conn := NewDatabaseIntegrationConnector("db-conn", db)
	conn.connected = true

	_, err := conn.Execute(context.Background(), "update", map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing table parameter")
	}
}

func TestDatabaseIntegrationConnector_UpdateMissingData(t *testing.T) {
	db := NewWorkflowDatabase("test-db", DatabaseConfig{})
	conn := NewDatabaseIntegrationConnector("db-conn", db)
	conn.connected = true

	_, err := conn.Execute(context.Background(), "update", map[string]any{
		"table": "users",
	})
	if err == nil {
		t.Fatal("expected error for missing data parameter")
	}
}

func TestDatabaseIntegrationConnector_DeleteMissingTable(t *testing.T) {
	db := NewWorkflowDatabase("test-db", DatabaseConfig{})
	conn := NewDatabaseIntegrationConnector("db-conn", db)
	conn.connected = true

	_, err := conn.Execute(context.Background(), "delete", map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing table parameter")
	}
}

func TestExtractArgs(t *testing.T) {
	// Test with slice
	params := map[string]any{
		"args": []any{"a", "b", "c"},
	}
	args := extractArgs(params)
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(args))
	}

	// Test with single value
	params = map[string]any{
		"args": "single",
	}
	args = extractArgs(params)
	if len(args) != 1 || args[0] != "single" {
		t.Errorf("expected single arg 'single', got %v", args)
	}

	// Test with no args
	params = map[string]any{}
	args = extractArgs(params)
	if args != nil {
		t.Errorf("expected nil args, got %v", args)
	}
}

func TestDatabaseIntegrationConnector_DisconnectNotConnected(t *testing.T) {
	db := NewWorkflowDatabase("test-db", DatabaseConfig{})
	conn := NewDatabaseIntegrationConnector("db-conn", db)

	err := conn.Disconnect(context.Background())
	if err != nil {
		t.Fatalf("Disconnect should succeed even if not connected, got %v", err)
	}
}

func TestDatabaseIntegrationConnector_ActionDispatch(t *testing.T) {
	// Verify all supported actions are recognized (will fail at DB level since no real DB)
	db := NewWorkflowDatabase("test-db", DatabaseConfig{})
	conn := NewDatabaseIntegrationConnector("db-conn", db)
	conn.connected = true

	actions := []struct {
		action      string
		params      map[string]any
		errContains string
	}{
		{
			action:      "query",
			params:      map[string]any{"sql": "SELECT 1"},
			errContains: "database not open",
		},
		{
			action:      "execute",
			params:      map[string]any{"sql": "UPDATE t SET x = 1"},
			errContains: "database not open",
		},
		{
			action: "insert",
			params: map[string]any{
				"table": "t",
				"data":  map[string]any{"col": "val"},
			},
			errContains: "database not open",
		},
		{
			action: "update",
			params: map[string]any{
				"table": "t",
				"data":  map[string]any{"col": "val"},
			},
			errContains: "database not open",
		},
		{
			action: "delete",
			params: map[string]any{
				"table": "t",
			},
			errContains: "database not open",
		},
	}

	for _, tc := range actions {
		t.Run(tc.action, func(t *testing.T) {
			_, err := conn.Execute(context.Background(), tc.action, tc.params)
			if err == nil {
				t.Fatalf("expected error for action %q without open DB", tc.action)
			}
			if !strings.Contains(err.Error(), tc.errContains) {
				t.Errorf("expected error containing %q, got %q", tc.errContains, err.Error())
			}
		})
	}
}

func TestWorkflowDatabase_OpenInvalidDriver(t *testing.T) {
	// sql.Open with an unregistered driver will succeed but Ping will fail
	// However, we can test that Open itself works with a valid pattern
	db := NewWorkflowDatabase("test-db", DatabaseConfig{
		Driver: "invalid_driver_xyz",
		DSN:    "fake_dsn",
	})

	_, err := db.Open()
	if err == nil {
		// sql.Open may not fail immediately for unknown drivers
		// but let's close it if it succeeded
		_ = db.Close()
	}
	// Either way is fine - the important thing is no panic
	_ = fmt.Sprintf("Open result: %v", err)
}
