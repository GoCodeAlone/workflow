package billing

import (
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// Handler exposes billing endpoints over HTTP.
type Handler struct {
	meter    UsageMeter
	provider BillingProvider
}

// NewHandler creates a new billing HTTP handler.
func NewHandler(meter UsageMeter, provider BillingProvider) *Handler {
	return &Handler{
		meter:    meter,
		provider: provider,
	}
}

// RegisterRoutes registers billing endpoints on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/billing/plans", h.handleListPlans)
	mux.HandleFunc("GET /api/v1/billing/usage", h.handleGetUsage)
	mux.HandleFunc("POST /api/v1/billing/subscribe", h.handleSubscribe)
	mux.HandleFunc("DELETE /api/v1/billing/subscribe", h.handleCancelSubscription)
	mux.HandleFunc("POST /api/v1/billing/webhook", h.handleWebhook)
}

// ---------- GET /api/v1/billing/plans ----------

func (h *Handler) handleListPlans(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, AllPlans)
}

// ---------- GET /api/v1/billing/usage ----------

func (h *Handler) handleGetUsage(w http.ResponseWriter, r *http.Request) {
	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		http.Error(w, `{"error":"tenant_id is required"}`, http.StatusBadRequest)
		return
	}

	period := time.Now()
	if p := r.URL.Query().Get("period"); p != "" {
		t, err := time.Parse("2006-01", p)
		if err != nil {
			http.Error(w, `{"error":"invalid period, expected YYYY-MM"}`, http.StatusBadRequest)
			return
		}
		period = t
	}

	report, err := h.meter.GetUsage(r.Context(), tenantID, period)
	if err != nil {
		http.Error(w, `{"error":"failed to fetch usage"}`, http.StatusInternalServerError)
		return
	}

	allowed, remaining, err := h.meter.CheckLimit(r.Context(), tenantID)
	if err != nil {
		http.Error(w, `{"error":"failed to check limits"}`, http.StatusInternalServerError)
		return
	}

	resp := struct {
		*UsageReport
		Allowed   bool  `json:"allowed"`
		Remaining int64 `json:"remaining"`
	}{
		UsageReport: report,
		Allowed:     allowed,
		Remaining:   remaining,
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---------- POST /api/v1/billing/subscribe ----------

type subscribeRequest struct {
	TenantID   string `json:"tenant_id"`
	Email      string `json:"email"`
	PlanID     string `json:"plan_id"`
	CustomerID string `json:"customer_id,omitempty"` // optional, created if empty
}

type subscribeResponse struct {
	CustomerID     string `json:"customer_id"`
	SubscriptionID string `json:"subscription_id"`
}

func (h *Handler) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	var req subscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.TenantID == "" || req.PlanID == "" {
		http.Error(w, `{"error":"tenant_id and plan_id are required"}`, http.StatusBadRequest)
		return
	}

	if PlanByID(req.PlanID) == nil {
		http.Error(w, `{"error":"unknown plan"}`, http.StatusBadRequest)
		return
	}

	customerID := req.CustomerID
	if customerID == "" {
		if req.Email == "" {
			http.Error(w, `{"error":"email is required when no customer_id is provided"}`, http.StatusBadRequest)
			return
		}
		var err error
		customerID, err = h.provider.CreateCustomer(r.Context(), req.TenantID, req.Email)
		if err != nil {
			http.Error(w, `{"error":"failed to create customer"}`, http.StatusInternalServerError)
			return
		}
	}

	subID, err := h.provider.CreateSubscription(r.Context(), customerID, req.PlanID)
	if err != nil {
		http.Error(w, `{"error":"failed to create subscription"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, subscribeResponse{
		CustomerID:     customerID,
		SubscriptionID: subID,
	})
}

// ---------- DELETE /api/v1/billing/subscribe ----------

type cancelRequest struct {
	SubscriptionID string `json:"subscription_id"`
}

func (h *Handler) handleCancelSubscription(w http.ResponseWriter, r *http.Request) {
	var req cancelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.SubscriptionID == "" {
		http.Error(w, `{"error":"subscription_id is required"}`, http.StatusBadRequest)
		return
	}

	if err := h.provider.CancelSubscription(r.Context(), req.SubscriptionID); err != nil {
		http.Error(w, `{"error":"failed to cancel subscription"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// ---------- POST /api/v1/billing/webhook ----------

func (h *Handler) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to read body"}`, http.StatusBadRequest)
		return
	}

	signature := r.Header.Get("Stripe-Signature")
	if err := h.provider.HandleWebhook(r.Context(), body, signature); err != nil {
		http.Error(w, `{"error":"webhook processing failed"}`, http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
