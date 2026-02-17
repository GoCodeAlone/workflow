package storebrowser

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

type handler struct {
	db         *sql.DB
	eventStore store.EventStore
	dlqStore   store.DLQStore
}

// sanitizeReadOnlyQuery validates that the query is a single SELECT statement
// without statement separators or SQL comments.
func sanitizeReadOnlyQuery(q string) (string, error) {
	trimmed := strings.TrimSpace(q)
	if trimmed == "" {
		return "", fmt.Errorf("query is empty")
	}
	upper := strings.ToUpper(trimmed)
	if !strings.HasPrefix(upper, "SELECT ") && upper != "SELECT" {
		return "", fmt.Errorf("only SELECT statements are allowed")
	}
	if strings.Contains(trimmed, ";") {
		return "", fmt.Errorf("multiple statements are not allowed")
	}
	if strings.Contains(upper, "--") || strings.Contains(upper, "/*") {
		return "", fmt.Errorf("SQL comments are not allowed")
	}
	return trimmed, nil
}

func (h *handler) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /tables", h.listTables)
	mux.HandleFunc("GET /tables/{name}/schema", h.tableSchema)
	mux.HandleFunc("GET /tables/{name}/rows", h.tableRows)
	mux.HandleFunc("POST /query", h.execQuery)
	mux.HandleFunc("GET /events", h.listEvents)
	mux.HandleFunc("GET /dlq", h.listDLQ)
	mux.HandleFunc("GET /dlq/{id}", h.getDLQ)
	mux.HandleFunc("POST /dlq/{id}/retry", h.retryDLQ)
	mux.HandleFunc("POST /dlq/{id}/discard", h.discardDLQ)
}

// ---------------------------------------------------------------------------
// Table endpoints
// ---------------------------------------------------------------------------

func (h *handler) listTables(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		writeError(w, http.StatusServiceUnavailable, "database not available")
		return
	}

	tables, err := getValidTables(h.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("list tables: %v", err))
		return
	}

	names := make([]string, 0, len(tables))
	for name := range tables {
		names = append(names, name)
	}
	// Sort for deterministic output.
	sortStrings(names)

	writeJSON(w, http.StatusOK, map[string]any{"tables": names})
}

func (h *handler) tableSchema(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		writeError(w, http.StatusServiceUnavailable, "database not available")
		return
	}

	tableName := r.PathValue("name")
	if !isValidTableName(tableName) {
		writeError(w, http.StatusBadRequest, "invalid table name")
		return
	}

	tables, err := getValidTables(h.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("list tables: %v", err))
		return
	}
	if !tables[tableName] {
		writeError(w, http.StatusNotFound, "table not found")
		return
	}

	rows, err := h.db.QueryContext(r.Context(), fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("table info: %v", err))
		return
	}
	defer rows.Close()

	type columnInfo struct {
		CID        int     `json:"cid"`
		Name       string  `json:"name"`
		Type       string  `json:"type"`
		NotNull    bool    `json:"notnull"`
		DefaultVal *string `json:"dflt_value"`
		PK         bool    `json:"pk"`
	}

	var columns []columnInfo
	for rows.Next() {
		var c columnInfo
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&c.CID, &c.Name, &c.Type, &notNull, &dflt, &pk); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("scan column: %v", err))
			return
		}
		c.NotNull = notNull != 0
		c.PK = pk != 0
		if dflt.Valid {
			c.DefaultVal = &dflt.String
		}
		columns = append(columns, c)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("iterate columns: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"table": tableName, "columns": columns})
}

func (h *handler) tableRows(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		writeError(w, http.StatusServiceUnavailable, "database not available")
		return
	}

	tableName := r.PathValue("name")
	if !isValidTableName(tableName) {
		writeError(w, http.StatusBadRequest, "invalid table name")
		return
	}

	tables, err := getValidTables(h.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("list tables: %v", err))
		return
	}
	if !tables[tableName] {
		writeError(w, http.StatusNotFound, "table not found")
		return
	}

	limit, offset := parsePagination(r)

	// Validate sort column if provided.
	sortCol := r.URL.Query().Get("sort")
	order := strings.ToUpper(r.URL.Query().Get("order"))
	if order != "DESC" {
		order = "ASC"
	}

	orderClause := ""
	if sortCol != "" {
		// Validate the column name exists in this table.
		validCols, err := getTableColumns(h.db, tableName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("get columns: %v", err))
			return
		}
		if !validCols[sortCol] {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid sort column: %s", sortCol))
			return
		}
		orderClause = fmt.Sprintf(" ORDER BY %s %s", sortCol, order)
	}

	// tableName is validated against the allowlist returned by getValidTables() above,
	// and orderClause uses a column validated against getTableColumns(). Both are safe
	// from injection. Parameters are bound via ? placeholders.
	query := fmt.Sprintf("SELECT * FROM %s%s LIMIT ? OFFSET ?", tableName, orderClause) //nolint:gosec // tableName and orderClause are validated against DB schema above
	rows, err := h.db.QueryContext(r.Context(), query, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("query rows: %v", err))
		return
	}
	defer rows.Close()

	result, err := scanDynamicRows(rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("scan rows: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"table":  tableName,
		"rows":   result,
		"limit":  limit,
		"offset": offset,
	})
}

// ---------------------------------------------------------------------------
// Query endpoint
// ---------------------------------------------------------------------------

// dangerousKeywords are SQL keywords that indicate a write operation.
var dangerousKeywords = regexp.MustCompile(`(?i)\b(DROP|DELETE|INSERT|UPDATE|ALTER|CREATE|REPLACE|ATTACH|DETACH|REINDEX|VACUUM)\b`)

func (h *handler) execQuery(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		writeError(w, http.StatusServiceUnavailable, "database not available")
		return
	}

	var body struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	q := strings.TrimSpace(body.Query)
	if q == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}

	if dangerousKeywords.MatchString(q) {
		writeError(w, http.StatusForbidden, "write operations are not allowed")
		return
	}

	safeQuery, err := sanitizeReadOnlyQuery(q)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	q = safeQuery

	// Execute in a read-only transaction.
	tx, err := h.db.BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("begin tx: %v", err))
		return
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(r.Context(), q)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("query error: %v", err))
		return
	}
	defer rows.Close()

	// Capture columns before iterating rows (some drivers clear them after scan).
	cols, _ := rows.Columns()

	result, err := scanDynamicRows(rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("scan rows: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"columns": cols,
		"rows":    result,
		"count":   len(result),
	})
}

// ---------------------------------------------------------------------------
// Event endpoints
// ---------------------------------------------------------------------------

func (h *handler) listEvents(w http.ResponseWriter, r *http.Request) {
	if h.eventStore == nil {
		writeError(w, http.StatusServiceUnavailable, "event store not available")
		return
	}

	executionID := r.URL.Query().Get("execution_id")
	limit, offset := parsePagination(r)

	if executionID != "" {
		eid, err := uuid.Parse(executionID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid execution_id")
			return
		}
		events, err := h.eventStore.GetEvents(r.Context(), eid)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("get events: %v", err))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"events": events, "count": len(events)})
		return
	}

	filter := store.ExecutionEventFilter{
		Limit:  limit,
		Offset: offset,
	}
	if pipeline := r.URL.Query().Get("pipeline"); pipeline != "" {
		filter.Pipeline = pipeline
	}
	if status := r.URL.Query().Get("status"); status != "" {
		filter.Status = status
	}

	executions, err := h.eventStore.ListExecutions(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("list executions: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"executions": executions, "count": len(executions)})
}

// ---------------------------------------------------------------------------
// DLQ endpoints
// ---------------------------------------------------------------------------

func (h *handler) listDLQ(w http.ResponseWriter, r *http.Request) {
	if h.dlqStore == nil {
		writeError(w, http.StatusServiceUnavailable, "DLQ store not available")
		return
	}

	limit, offset := parsePagination(r)
	filter := store.DLQFilter{
		Limit:  limit,
		Offset: offset,
	}
	if status := r.URL.Query().Get("status"); status != "" {
		filter.Status = store.DLQStatus(status)
	}
	if pipeline := r.URL.Query().Get("pipeline"); pipeline != "" {
		filter.PipelineName = pipeline
	}
	if step := r.URL.Query().Get("step"); step != "" {
		filter.StepName = step
	}

	entries, err := h.dlqStore.List(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("list DLQ: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "count": len(entries)})
}

func (h *handler) getDLQ(w http.ResponseWriter, r *http.Request) {
	if h.dlqStore == nil {
		writeError(w, http.StatusServiceUnavailable, "DLQ store not available")
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid DLQ entry ID")
		return
	}

	entry, err := h.dlqStore.Get(r.Context(), id)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "DLQ entry not found")
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("get DLQ: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (h *handler) retryDLQ(w http.ResponseWriter, r *http.Request) {
	if h.dlqStore == nil {
		writeError(w, http.StatusServiceUnavailable, "DLQ store not available")
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid DLQ entry ID")
		return
	}

	if err := h.dlqStore.Retry(r.Context(), id); err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "DLQ entry not found")
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("retry DLQ: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "retrying"})
}

func (h *handler) discardDLQ(w http.ResponseWriter, r *http.Request) {
	if h.dlqStore == nil {
		writeError(w, http.StatusServiceUnavailable, "DLQ store not available")
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid DLQ entry ID")
		return
	}

	if err := h.dlqStore.Discard(r.Context(), id); err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "DLQ entry not found")
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("discard DLQ: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "discarded"})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func getValidTables(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tables := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables[name] = true
	}
	return tables, rows.Err()
}

func getTableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

// isValidTableName checks that a table name contains only safe characters.
var tableNameRegexp = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func isValidTableName(name string) bool {
	return tableNameRegexp.MatchString(name)
}

func parsePagination(r *http.Request) (limit, offset int) {
	limit = 50
	offset = 0

	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 1000 {
		limit = 1000
	}

	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return
}

// scanDynamicRows scans all rows dynamically into []map[string]any.
func scanDynamicRows(rows *sql.Rows) ([]map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var result []map[string]any
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}

		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}

		row := make(map[string]any, len(cols))
		for i, col := range cols {
			val := values[i]
			// Convert []byte to string for JSON readability.
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}
		result = append(result, row)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		result = []map[string]any{}
	}
	return result, nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
