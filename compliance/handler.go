package compliance

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// ComplianceHandler serves SOC2 compliance endpoints over HTTP.
type ComplianceHandler struct {
	auditLog  AuditLog
	registry  *ControlRegistry
	retention *RetentionManager
}

// NewComplianceHandler creates a new compliance HTTP handler.
func NewComplianceHandler(auditLog AuditLog, registry *ControlRegistry, retention *RetentionManager) *ComplianceHandler {
	return &ComplianceHandler{
		auditLog:  auditLog,
		registry:  registry,
		retention: retention,
	}
}

// RegisterComplianceRoutes registers SOC2 compliance API endpoints on the given mux.
func (h *ComplianceHandler) RegisterComplianceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/compliance/audit", h.handleQueryAudit)
	mux.HandleFunc("GET /api/v1/compliance/audit/export", h.handleExportAudit)
	mux.HandleFunc("GET /api/v1/compliance/controls", h.handleListControls)
	mux.HandleFunc("GET /api/v1/compliance/controls/{id}", h.handleGetControl)
	mux.HandleFunc("GET /api/v1/compliance/report", h.handleGenerateReport)
	mux.HandleFunc("GET /api/v1/compliance/score", h.handleComplianceScore)
	mux.HandleFunc("GET /api/v1/compliance/retention", h.handleListRetention)
}

// ---------- GET /api/v1/compliance/audit ----------

func (h *ComplianceHandler) handleQueryAudit(w http.ResponseWriter, r *http.Request) {
	filter := parseAuditFilter(r)

	entries, err := h.auditLog.Query(r.Context(), filter)
	if err != nil {
		writeComplianceJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	count, _ := h.auditLog.Count(r.Context(), filter)

	writeComplianceJSON(w, http.StatusOK, map[string]any{
		"items": entries,
		"total": count,
	})
}

// ---------- GET /api/v1/compliance/audit/export ----------

func (h *ComplianceHandler) handleExportAudit(w http.ResponseWriter, r *http.Request) {
	filter := parseAuditFilter(r)
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	data, err := h.auditLog.Export(r.Context(), filter, format)
	if err != nil {
		writeComplianceJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	switch format {
	case "csv":
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=audit_export.csv")
	default:
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", "attachment; filename=audit_export.json")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// ---------- GET /api/v1/compliance/controls ----------

func (h *ComplianceHandler) handleListControls(w http.ResponseWriter, r *http.Request) {
	category := r.URL.Query().Get("category")
	controls := h.registry.List(category)
	writeComplianceJSON(w, http.StatusOK, map[string]any{
		"items": controls,
		"total": len(controls),
	})
}

// ---------- GET /api/v1/compliance/controls/{id} ----------

func (h *ComplianceHandler) handleGetControl(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	control, ok := h.registry.Get(id)
	if !ok {
		writeComplianceJSON(w, http.StatusNotFound, map[string]string{"error": "control not found"})
		return
	}
	writeComplianceJSON(w, http.StatusOK, control)
}

// ---------- GET /api/v1/compliance/report ----------

func (h *ComplianceHandler) handleGenerateReport(w http.ResponseWriter, _ *http.Request) {
	report := h.registry.GenerateReport()
	writeComplianceJSON(w, http.StatusOK, report)
}

// ---------- GET /api/v1/compliance/score ----------

func (h *ComplianceHandler) handleComplianceScore(w http.ResponseWriter, _ *http.Request) {
	score := h.registry.ComplianceScore()
	writeComplianceJSON(w, http.StatusOK, map[string]any{
		"score":      score,
		"percentage": score,
	})
}

// ---------- GET /api/v1/compliance/retention ----------

func (h *ComplianceHandler) handleListRetention(w http.ResponseWriter, _ *http.Request) {
	policies := h.retention.ListPolicies()
	writeComplianceJSON(w, http.StatusOK, map[string]any{
		"items": policies,
		"total": len(policies),
	})
}

// ---------- Helpers ----------

func parseAuditFilter(r *http.Request) AuditFilter {
	q := r.URL.Query()
	f := AuditFilter{
		ActorID:  q.Get("actor_id"),
		Action:   q.Get("action"),
		Resource: q.Get("resource"),
		TenantID: q.Get("tenant_id"),
	}
	if s := q.Get("start_time"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err == nil {
			f.StartTime = &t
		}
	}
	if s := q.Get("end_time"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err == nil {
			f.EndTime = &t
		}
	}
	if s := q.Get("success"); s != "" {
		b, err := strconv.ParseBool(s)
		if err == nil {
			f.Success = &b
		}
	}
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err == nil && n > 0 {
			f.Limit = n
		}
	}
	if s := q.Get("offset"); s != "" {
		n, err := strconv.Atoi(s)
		if err == nil && n >= 0 {
			f.Offset = n
		}
	}
	return f
}

func writeComplianceJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
