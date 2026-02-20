package module

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/GoCodeAlone/workflow/featureflag"
	"github.com/GoCodeAlone/workflow/featureflag/generic"
)

// FeatureFlagAdminAdapter bridges the featureflag.Service and generic.Store
// to the FeatureFlagAdmin interface required by V1APIHandler.
type FeatureFlagAdminAdapter struct {
	service *featureflag.Service
	store   *generic.Store
}

// NewFeatureFlagAdminAdapter creates an adapter implementing FeatureFlagAdmin.
func NewFeatureFlagAdminAdapter(service *featureflag.Service, store *generic.Store) *FeatureFlagAdminAdapter {
	return &FeatureFlagAdminAdapter{service: service, store: store}
}

func (a *FeatureFlagAdminAdapter) ListFlags() ([]any, error) {
	flags, err := a.store.ListFlags(context.Background())
	if err != nil {
		return nil, err
	}
	result := make([]any, len(flags))
	for i := range flags {
		result[i] = flags[i]
	}
	return result, nil
}

func (a *FeatureFlagAdminAdapter) GetFlag(key string) (any, error) {
	flag, err := a.store.GetFlag(context.Background(), key)
	if err != nil {
		return nil, fmt.Errorf("flag %q not found", key)
	}
	return flag, nil
}

type createFlagRequest struct {
	Key         string   `json:"key"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enabled     *bool    `json:"enabled"`
	DefaultVal  string   `json:"default_val"`
	Tags        []string `json:"tags"`
	Percentage  float64  `json:"percentage"`
}

func (a *FeatureFlagAdminAdapter) CreateFlag(data json.RawMessage) (any, error) {
	var req createFlagRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("invalid flag data: %w", err)
	}
	if req.Key == "" {
		return nil, fmt.Errorf("flag key is required")
	}
	if req.Type == "" {
		req.Type = "boolean"
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	row := &generic.FlagRow{
		Key:         req.Key,
		Type:        req.Type,
		Description: req.Description,
		Enabled:     enabled,
		DefaultVal:  req.DefaultVal,
		Tags:        req.Tags,
		Percentage:  req.Percentage,
	}
	if row.Tags == nil {
		row.Tags = []string{}
	}
	if row.DefaultVal == "" {
		row.DefaultVal = "false"
	}

	if err := a.store.UpsertFlag(context.Background(), row); err != nil {
		return nil, err
	}
	// Re-read to get timestamps
	return a.store.GetFlag(context.Background(), req.Key)
}

func (a *FeatureFlagAdminAdapter) UpdateFlag(key string, data json.RawMessage) (any, error) {
	existing, err := a.store.GetFlag(context.Background(), key)
	if err != nil {
		return nil, fmt.Errorf("flag %q not found", key)
	}

	// Merge updates onto existing flag
	var updates map[string]json.RawMessage
	if err := json.Unmarshal(data, &updates); err != nil {
		return nil, fmt.Errorf("invalid update data: %w", err)
	}

	if v, ok := updates["description"]; ok {
		json.Unmarshal(v, &existing.Description) //nolint:errcheck
	}
	if v, ok := updates["enabled"]; ok {
		json.Unmarshal(v, &existing.Enabled) //nolint:errcheck
	}
	if v, ok := updates["default_val"]; ok {
		json.Unmarshal(v, &existing.DefaultVal) //nolint:errcheck
	}
	if v, ok := updates["type"]; ok {
		json.Unmarshal(v, &existing.Type) //nolint:errcheck
	}
	if v, ok := updates["tags"]; ok {
		json.Unmarshal(v, &existing.Tags) //nolint:errcheck
	}
	if v, ok := updates["percentage"]; ok {
		json.Unmarshal(v, &existing.Percentage) //nolint:errcheck
	}

	if err := a.store.UpsertFlag(context.Background(), existing); err != nil {
		return nil, err
	}
	return a.store.GetFlag(context.Background(), key)
}

func (a *FeatureFlagAdminAdapter) DeleteFlag(key string) error {
	return a.store.DeleteFlag(context.Background(), key)
}

type overrideRequest struct {
	Overrides []generic.OverrideRow `json:"overrides"`
}

func (a *FeatureFlagAdminAdapter) SetOverrides(key string, data json.RawMessage) (any, error) {
	var req overrideRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("invalid overrides data: %w", err)
	}

	// Delete existing overrides for this flag, then insert new ones
	existing, err := a.store.GetOverrides(context.Background(), key)
	if err != nil {
		return nil, err
	}
	for _, o := range existing {
		if err := a.store.DeleteOverride(context.Background(), o.ID); err != nil {
			return nil, err
		}
	}

	for i := range req.Overrides {
		req.Overrides[i].FlagKey = key
		if err := a.store.UpsertOverride(context.Background(), &req.Overrides[i]); err != nil {
			return nil, err
		}
	}

	// Return the flag with its new overrides
	flag, err := a.store.GetFlag(context.Background(), key)
	if err != nil {
		return nil, err
	}
	overrides, _ := a.store.GetOverrides(context.Background(), key)
	return map[string]any{
		"flag":      flag,
		"overrides": overrides,
	}, nil
}

func (a *FeatureFlagAdminAdapter) EvaluateFlag(key string, user string, group string) (any, error) {
	evalCtx := featureflag.EvaluationContext{
		UserKey:    user,
		Attributes: map[string]string{},
	}
	if group != "" {
		evalCtx.Attributes["group"] = group
	}
	val, err := a.service.Evaluate(context.Background(), key, evalCtx)
	if err != nil {
		return nil, err
	}
	return val, nil
}

func (a *FeatureFlagAdminAdapter) SSEHandler() http.Handler {
	return a.service.SSEHandler()
}
