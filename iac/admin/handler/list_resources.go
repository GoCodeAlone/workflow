package handler

import (
	"context"

	"github.com/GoCodeAlone/workflow/iac/admin/catalog"
	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// ListResources implements InfraAdminService.ListResources by reading
// every ResourceState from the host's iac.state backend, applying the
// type / provider / app-context filters from the input, and
// returning AdminResourceSummary rows (no per-resource secrets —
// outputs are intentionally absent from the list view; the detail
// view uses GetResource).
//
// Per design §Handler library + plan §Task 5: the function takes
// `providers` + `catalog` as parameters so the host module and CLI
// callers can share one dispatch. v1 of this list endpoint does not
// consult providers or catalog directly — every populated field on
// AdminResourceSummary derives from the ResourceState itself — but
// the parameters are preserved in the signature for symmetry with
// ListResourceTypes / GenerateConfig and to keep the dispatch shape
// stable when T6 / future enhancements need to cross-check against
// the live providers map (e.g. dropping resources whose
// state.ProviderRef no longer matches a registered module).
//
// Per design §Authz row: default-deny when evidence is missing or
// either authz_checked / authz_allowed is false; refusal surfaces
// via Output.error rather than a Go-level error so the HTTP
// transport returns 200 OK with the typed payload (consumers sniff
// for non-empty error per the proto tag-100 discriminator).
func ListResources(
	ctx context.Context,
	store interfaces.IaCStateStore,
	providers map[string]interfaces.IaCProvider, //nolint:revive // reserved for symmetry + future use; see godoc
	fieldCat *catalog.FieldSpecCatalog, //nolint:revive // reserved for symmetry + future use; see godoc
	in *adminpb.AdminListResourcesInput,
) (*adminpb.AdminListResourcesOutput, error) {
	if msg := authzError(in.GetEvidence()); msg != "" {
		return &adminpb.AdminListResourcesOutput{Error: msg}, nil
	}
	if store == nil {
		return &adminpb.AdminListResourcesOutput{Error: "list: no state store configured"}, nil
	}

	states, err := store.ListResources(ctx)
	if err != nil {
		// Intentional nilerr: per proto tag-100 convention, errors
		// surface via Output.error (HTTP transport returns 200 + typed
		// payload; consumers sniff for non-empty error). A Go-level
		// error would force the HTTP layer into a 5xx and lose the
		// typed shape.
		return &adminpb.AdminListResourcesOutput{Error: "list state: " + err.Error()}, nil //nolint:nilerr
	}

	out := &adminpb.AdminListResourcesOutput{}
	for i := range states {
		s := &states[i]
		summary := stateToSummary(s)

		if in.GetTypeFilter() != "" && summary.Type != in.GetTypeFilter() {
			continue
		}
		if in.GetProviderFilter() != "" && summary.ProviderModule != in.GetProviderFilter() {
			continue
		}
		if in.GetAppContextFilter() != "" && summary.AppContext != in.GetAppContextFilter() {
			continue
		}

		out.Resources = append(out.Resources, summary)
	}
	return out, nil
}

// stateToSummary projects a ResourceState into the AdminResourceSummary
// wire shape. Shared with GetResource (which wraps the same summary
// inside AdminResourceDetail) — extracted so the field mapping is
// single-sourced.
//
// Provider fields:
//   - ProviderModule  ← state.ProviderRef (host YAML `name:` of the
//     iac.provider module that owns this resource)
//   - ProviderType    ← state.Provider     (cloud provider string,
//     e.g. "digitalocean", "aws")
//
// app_context derives from state.AppliedConfig["labels"]["app_context"]
// per design §App context. Falls back to empty string when the label
// is absent (so AppContextFilter == "" matches every resource).
//
// Status is hardcoded "active" in v1 because interfaces.ResourceState
// has no Status field; the on-disk StateRecord.Status IS captured by
// the wfctlhelpers fs/postgres backends but dropped during the
// ResourceState conversion. Promoting Status into interfaces.
// ResourceState is a v1.1 item that lands alongside reconciliation
// (design §Personas explicitly excludes mid-cycle states from v1).
// Per spec-reviewer + code-reviewer T5 M-1 on commit 5fe88fe45.
func stateToSummary(s *interfaces.ResourceState) *adminpb.AdminResourceSummary {
	out := &adminpb.AdminResourceSummary{
		Name:           s.Name,
		Type:           s.Type,
		ProviderModule: s.ProviderRef,
		ProviderType:   s.Provider,
		ProviderId:     s.ProviderID,
		Status:         "active", // TODO(v1.1): promote Status to interfaces.ResourceState
		Dependencies:   append([]string(nil), s.Dependencies...),
		AppContext:     extractAppContext(s.AppliedConfig),
	}
	// Guard against zero time.Time → year-1 BCE Unix epoch (per
	// code-reviewer T5 M-2). The JS fmtTs helper checks `!unix`, so
	// a 0 here renders as "—" rather than a misleading "0001-01-01".
	if !s.UpdatedAt.IsZero() {
		out.UpdatedAtUnix = s.UpdatedAt.Unix()
	}
	return out
}

// extractAppContext digs labels.app_context out of an AppliedConfig
// map. The labels sub-map is the design's convention for free-form
// resource tagging; v1 supports just "app_context" but the shape is
// future-proof for additional well-known labels.
func extractAppContext(cfg map[string]any) string {
	if cfg == nil {
		return ""
	}
	labels, _ := cfg["labels"].(map[string]any)
	if labels == nil {
		return ""
	}
	v, _ := labels["app_context"].(string)
	return v
}
