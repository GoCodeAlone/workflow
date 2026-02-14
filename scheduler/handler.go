package scheduler

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// Handler provides HTTP endpoints for scheduled job management.
type Handler struct {
	scheduler *CronScheduler
}

// NewHandler creates a new scheduler HTTP handler.
func NewHandler(scheduler *CronScheduler) *Handler {
	return &Handler{scheduler: scheduler}
}

// RegisterRoutes registers scheduler API routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/schedules", h.listJobs)
	mux.HandleFunc("POST /api/schedules", h.createJob)
	mux.HandleFunc("GET /api/schedules/{id}", h.getJob)
	mux.HandleFunc("PUT /api/schedules/{id}", h.updateJob)
	mux.HandleFunc("DELETE /api/schedules/{id}", h.deleteJob)
	mux.HandleFunc("POST /api/schedules/{id}/pause", h.pauseJob)
	mux.HandleFunc("POST /api/schedules/{id}/resume", h.resumeJob)
	mux.HandleFunc("POST /api/schedules/{id}/execute", h.executeJob)
	mux.HandleFunc("GET /api/schedules/{id}/history", h.jobHistory)
	mux.HandleFunc("GET /api/schedules/preview", h.previewNextRuns)
}

func (h *Handler) listJobs(w http.ResponseWriter, r *http.Request) {
	jobs := h.scheduler.List()
	writeJSON(w, http.StatusOK, map[string]any{"items": jobs, "total": len(jobs)})
}

func (h *Handler) createJob(w http.ResponseWriter, r *http.Request) {
	var job ScheduledJob
	if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if err := h.scheduler.Create(&job); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, job)
}

func (h *Handler) getJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	job, ok := h.scheduler.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *Handler) updateJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Name         string         `json:"name"`
		CronExpr     string         `json:"cronExpr"`
		WorkflowType string         `json:"workflowType"`
		Action       string         `json:"action"`
		Params       map[string]any `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if err := h.scheduler.Update(id, body.Name, body.CronExpr, body.WorkflowType, body.Action, body.Params); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	job, _ := h.scheduler.Get(id)
	writeJSON(w, http.StatusOK, job)
}

func (h *Handler) deleteJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.scheduler.Delete(id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) pauseJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.scheduler.Pause(id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	job, _ := h.scheduler.Get(id)
	writeJSON(w, http.StatusOK, job)
}

func (h *Handler) resumeJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.scheduler.Resume(id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	job, _ := h.scheduler.Get(id)
	writeJSON(w, http.StatusOK, job)
}

func (h *Handler) executeJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, err := h.scheduler.ExecuteNow(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (h *Handler) jobHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	recs := h.scheduler.History(id)
	writeJSON(w, http.StatusOK, map[string]any{"items": recs, "total": len(recs)})
}

func (h *Handler) previewNextRuns(w http.ResponseWriter, r *http.Request) {
	cronExpr := r.URL.Query().Get("cron")
	if cronExpr == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cron query parameter required"})
		return
	}
	countStr := r.URL.Query().Get("count")
	count := 5
	if countStr != "" {
		if n, err := strconv.Atoi(countStr); err == nil && n > 0 && n <= 20 {
			count = n
		}
	}

	times, err := h.scheduler.NextRuns(cronExpr, count)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"cronExpr": cronExpr, "nextRuns": times})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
