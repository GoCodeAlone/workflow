package storebrowser

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestMux(h *handler) *http.ServeMux {
	mux := http.NewServeMux()
	h.registerRoutes(mux)
	return mux
}

func TestListTables(t *testing.T) {
	db := newTestDB(t)
	_, err := db.Exec("CREATE TABLE test_items (id INTEGER PRIMARY KEY, name TEXT)")
	if err != nil {
		t.Fatal(err)
	}

	h := &handler{db: db}
	mux := newTestMux(h)

	req := httptest.NewRequest("GET", "/tables", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Tables []string `json:"tables"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	found := false
	for _, name := range resp.Tables {
		if name == "test_items" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected test_items in tables list, got %v", resp.Tables)
	}
}

func TestTableSchema(t *testing.T) {
	db := newTestDB(t)
	_, err := db.Exec("CREATE TABLE test_schema (id INTEGER PRIMARY KEY, name TEXT NOT NULL, value REAL)")
	if err != nil {
		t.Fatal(err)
	}

	h := &handler{db: db}
	mux := newTestMux(h)

	req := httptest.NewRequest("GET", "/tables/test_schema/schema", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Table   string `json:"table"`
		Columns []struct {
			Name    string `json:"name"`
			Type    string `json:"type"`
			NotNull bool   `json:"notnull"`
			PK      bool   `json:"pk"`
		} `json:"columns"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if resp.Table != "test_schema" {
		t.Errorf("expected table name test_schema, got %s", resp.Table)
	}
	if len(resp.Columns) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(resp.Columns))
	}
	if resp.Columns[0].Name != "id" || resp.Columns[1].Name != "name" || resp.Columns[2].Name != "value" {
		t.Errorf("unexpected column names: %+v", resp.Columns)
	}
}

func TestTableSchemaNotFound(t *testing.T) {
	db := newTestDB(t)
	h := &handler{db: db}
	mux := newTestMux(h)

	req := httptest.NewRequest("GET", "/tables/nonexistent/schema", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestTableRows(t *testing.T) {
	db := newTestDB(t)
	_, err := db.Exec("CREATE TABLE test_rows (id INTEGER PRIMARY KEY, name TEXT)")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		_, err := db.Exec("INSERT INTO test_rows (id, name) VALUES (?, ?)", i, "item")
		if err != nil {
			t.Fatal(err)
		}
	}

	h := &handler{db: db}
	mux := newTestMux(h)

	// Test basic pagination.
	req := httptest.NewRequest("GET", "/tables/test_rows/rows?limit=2&offset=1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Rows   []map[string]any `json:"rows"`
		Limit  int              `json:"limit"`
		Offset int              `json:"offset"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if len(resp.Rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(resp.Rows))
	}
	if resp.Limit != 2 {
		t.Errorf("expected limit 2, got %d", resp.Limit)
	}
	if resp.Offset != 1 {
		t.Errorf("expected offset 1, got %d", resp.Offset)
	}
}

func TestTableRowsSort(t *testing.T) {
	db := newTestDB(t)
	_, err := db.Exec("CREATE TABLE test_sort (id INTEGER PRIMARY KEY, name TEXT)")
	if err != nil {
		t.Fatal(err)
	}
	db.Exec("INSERT INTO test_sort (id, name) VALUES (1, 'charlie')")
	db.Exec("INSERT INTO test_sort (id, name) VALUES (2, 'alice')")
	db.Exec("INSERT INTO test_sort (id, name) VALUES (3, 'bob')")

	h := &handler{db: db}
	mux := newTestMux(h)

	req := httptest.NewRequest("GET", "/tables/test_sort/rows?sort=name&order=asc", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Rows []map[string]any `json:"rows"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	if len(resp.Rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(resp.Rows))
	}
	if resp.Rows[0]["name"] != "alice" {
		t.Errorf("expected first row alice, got %v", resp.Rows[0]["name"])
	}
}

func TestTableRowsInvalidSort(t *testing.T) {
	db := newTestDB(t)
	_, err := db.Exec("CREATE TABLE test_invalid_sort (id INTEGER PRIMARY KEY)")
	if err != nil {
		t.Fatal(err)
	}

	h := &handler{db: db}
	mux := newTestMux(h)

	req := httptest.NewRequest("GET", "/tables/test_invalid_sort/rows?sort=nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestSQLQuery(t *testing.T) {
	db := newTestDB(t)
	_, err := db.Exec("CREATE TABLE query_test (id INTEGER, val TEXT)")
	if err != nil {
		t.Fatal(err)
	}
	db.Exec("INSERT INTO query_test VALUES (1, 'hello')")
	db.Exec("INSERT INTO query_test VALUES (2, 'world')")

	h := &handler{db: db}
	mux := newTestMux(h)

	body, _ := json.Marshal(map[string]string{"query": "SELECT * FROM query_test WHERE id = 1"})
	req := httptest.NewRequest("POST", "/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Rows  []map[string]any `json:"rows"`
		Count int              `json:"count"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Count != 1 {
		t.Errorf("expected 1 row, got %d", resp.Count)
	}
}

func TestSQLQueryRejectWrite(t *testing.T) {
	db := newTestDB(t)
	h := &handler{db: db}
	mux := newTestMux(h)

	tests := []struct {
		name  string
		query string
	}{
		{"INSERT", "INSERT INTO foo VALUES (1)"},
		{"UPDATE", "UPDATE foo SET x = 1"},
		{"DELETE", "DELETE FROM foo"},
		{"DROP", "DROP TABLE foo"},
		{"ALTER", "ALTER TABLE foo ADD COLUMN x"},
		{"CREATE", "CREATE TABLE foo (id INT)"},
		{"insert_lower", "insert into foo values (1)"},
		{"mixed_case", "Insert INTO foo VALUES (1)"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{"query": tc.query})
			req := httptest.NewRequest("POST", "/query", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusForbidden {
				t.Errorf("expected 403 for %q, got %d", tc.query, w.Code)
			}
		})
	}
}

func TestSQLQueryEmptyQuery(t *testing.T) {
	db := newTestDB(t)
	h := &handler{db: db}
	mux := newTestMux(h)

	body, _ := json.Marshal(map[string]string{"query": ""})
	req := httptest.NewRequest("POST", "/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestEventsNilStore(t *testing.T) {
	h := &handler{}
	mux := newTestMux(h)

	req := httptest.NewRequest("GET", "/events", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestEventsListExecutions(t *testing.T) {
	es := store.NewInMemoryEventStore()
	execID := uuid.New()
	ctx := context.Background()
	es.Append(ctx, execID, store.EventExecutionStarted, map[string]any{"pipeline": "test-pipe"})
	es.Append(ctx, execID, store.EventExecutionCompleted, nil)

	h := &handler{eventStore: es}
	mux := newTestMux(h)

	req := httptest.NewRequest("GET", "/events", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Executions []store.MaterializedExecution `json:"executions"`
		Count      int                           `json:"count"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Count != 1 {
		t.Errorf("expected 1 execution, got %d", resp.Count)
	}
}

func TestEventsGetByExecution(t *testing.T) {
	es := store.NewInMemoryEventStore()
	execID := uuid.New()
	ctx := context.Background()
	es.Append(ctx, execID, store.EventExecutionStarted, map[string]any{"pipeline": "p1"})

	h := &handler{eventStore: es}
	mux := newTestMux(h)

	req := httptest.NewRequest("GET", "/events?execution_id="+execID.String(), nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Events []store.ExecutionEvent `json:"events"`
		Count  int                    `json:"count"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Count != 1 {
		t.Errorf("expected 1 event, got %d", resp.Count)
	}
}

func TestDLQList(t *testing.T) {
	dlq := store.NewInMemoryDLQStore()
	ctx := context.Background()

	entry := &store.DLQEntry{
		ID:           uuid.New(),
		PipelineName: "pipe1",
		StepName:     "step1",
		ErrorMessage: "something failed",
		ErrorType:    "runtime",
		Status:       store.DLQStatusPending,
	}
	if err := dlq.Add(ctx, entry); err != nil {
		t.Fatal(err)
	}

	h := &handler{dlqStore: dlq}
	mux := newTestMux(h)

	req := httptest.NewRequest("GET", "/dlq", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Entries []*store.DLQEntry `json:"entries"`
		Count   int               `json:"count"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Count != 1 {
		t.Errorf("expected 1 entry, got %d", resp.Count)
	}
	if resp.Entries[0].PipelineName != "pipe1" {
		t.Errorf("expected pipe1, got %s", resp.Entries[0].PipelineName)
	}
}

func TestDLQGet(t *testing.T) {
	dlq := store.NewInMemoryDLQStore()
	ctx := context.Background()

	id := uuid.New()
	entry := &store.DLQEntry{
		ID:           id,
		PipelineName: "pipe1",
		StepName:     "step1",
		ErrorMessage: "failed",
		ErrorType:    "runtime",
	}
	dlq.Add(ctx, entry)

	h := &handler{dlqStore: dlq}
	mux := newTestMux(h)

	req := httptest.NewRequest("GET", "/dlq/"+id.String(), nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp store.DLQEntry
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.ID != id {
		t.Errorf("expected ID %s, got %s", id, resp.ID)
	}
}

func TestDLQGetNotFound(t *testing.T) {
	dlq := store.NewInMemoryDLQStore()
	h := &handler{dlqStore: dlq}
	mux := newTestMux(h)

	req := httptest.NewRequest("GET", "/dlq/"+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDLQRetry(t *testing.T) {
	dlq := store.NewInMemoryDLQStore()
	ctx := context.Background()

	id := uuid.New()
	entry := &store.DLQEntry{
		ID:           id,
		PipelineName: "pipe1",
		StepName:     "step1",
		ErrorMessage: "failed",
		ErrorType:    "runtime",
		Status:       store.DLQStatusPending,
	}
	dlq.Add(ctx, entry)

	h := &handler{dlqStore: dlq}
	mux := newTestMux(h)

	req := httptest.NewRequest("POST", "/dlq/"+id.String()+"/retry", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify status changed.
	updated, err := dlq.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != store.DLQStatusRetrying {
		t.Errorf("expected status retrying, got %s", updated.Status)
	}
	if updated.RetryCount != 1 {
		t.Errorf("expected retry count 1, got %d", updated.RetryCount)
	}
}

func TestDLQDiscard(t *testing.T) {
	dlq := store.NewInMemoryDLQStore()
	ctx := context.Background()

	id := uuid.New()
	entry := &store.DLQEntry{
		ID:           id,
		PipelineName: "pipe1",
		StepName:     "step1",
		ErrorMessage: "failed",
		ErrorType:    "runtime",
		Status:       store.DLQStatusPending,
	}
	dlq.Add(ctx, entry)

	h := &handler{dlqStore: dlq}
	mux := newTestMux(h)

	req := httptest.NewRequest("POST", "/dlq/"+id.String()+"/discard", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	updated, err := dlq.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != store.DLQStatusDiscarded {
		t.Errorf("expected status discarded, got %s", updated.Status)
	}
}

func TestDLQNilStore(t *testing.T) {
	h := &handler{}
	mux := newTestMux(h)

	endpoints := []struct {
		method string
		path   string
	}{
		{"GET", "/dlq"},
		{"GET", "/dlq/" + uuid.New().String()},
		{"POST", "/dlq/" + uuid.New().String() + "/retry"},
		{"POST", "/dlq/" + uuid.New().String() + "/discard"},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusServiceUnavailable {
				t.Errorf("expected 503, got %d", w.Code)
			}
		})
	}
}

func TestDBNilStore(t *testing.T) {
	h := &handler{}
	mux := newTestMux(h)

	endpoints := []struct {
		method string
		path   string
	}{
		{"GET", "/tables"},
		{"GET", "/tables/foo/schema"},
		{"GET", "/tables/foo/rows"},
		{"POST", "/query"},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			var body *bytes.Reader
			if ep.method == "POST" {
				b, _ := json.Marshal(map[string]string{"query": "SELECT 1"})
				body = bytes.NewReader(b)
			}
			var req *http.Request
			if body != nil {
				req = httptest.NewRequest(ep.method, ep.path, body)
			} else {
				req = httptest.NewRequest(ep.method, ep.path, nil)
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusServiceUnavailable {
				t.Errorf("expected 503, got %d", w.Code)
			}
		})
	}
}

func TestInvalidTableName(t *testing.T) {
	db := newTestDB(t)
	h := &handler{db: db}
	mux := newTestMux(h)

	// SQL injection attempt — use a name with spaces.
	req := httptest.NewRequest("GET", "/tables/test%20bad/schema", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid table name, got %d", w.Code)
	}
}
