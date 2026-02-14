package schema

import (
	"encoding/json"
	"net/http"
)

// RegisterRoutes registers the schema API endpoint on the given mux.
func RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/schema", handleGetSchema)
}

func handleGetSchema(w http.ResponseWriter, _ *http.Request) {
	s := GenerateWorkflowSchema()
	w.Header().Set("Content-Type", "application/schema+json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		http.Error(w, "failed to encode schema", http.StatusInternalServerError)
	}
}
