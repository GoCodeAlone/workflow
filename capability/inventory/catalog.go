package inventory

import (
	"sort"
	"strings"
)

// BuildCatalog converts the evidence-rich ecosystem inventory into the public
// docs catalog. Raw uncategorized rows stay in the ecosystem inventory and
// maintainer counts instead of dominating user-facing docs.
func BuildCatalog(inv *Inventory) *Catalog {
	if inv == nil {
		return &Catalog{
			Metadata: Metadata{Generator: "wfctl capability catalog"},
		}
	}
	catalog := &Catalog{
		Metadata:     catalogMetadata(inv, "wfctl capability catalog"),
		Capabilities: make([]CatalogCapability, 0, len(inv.Capabilities)),
		Findings:     publicFindings(inv.Findings),
	}
	hidden := 0
	for _, cap := range inv.Capabilities {
		if isUncategorizedCapability(cap.ID, cap.Category) {
			hidden++
			continue
		}
		catalog.Capabilities = append(catalog.Capabilities, CatalogCapability{
			ID:          cap.ID,
			Category:    cap.Category,
			Name:        cap.Name,
			Description: cap.Description,
			Lifecycle:   cap.Lifecycle,
			Tags:        append([]string(nil), cap.Tags...),
			Providers:   append([]Provider(nil), cap.Providers...),
		})
	}
	sort.Slice(catalog.Capabilities, func(i, j int) bool {
		if catalog.Capabilities[i].Category == catalog.Capabilities[j].Category {
			return catalog.Capabilities[i].ID < catalog.Capabilities[j].ID
		}
		return catalog.Capabilities[i].Category < catalog.Capabilities[j].Category
	})
	if catalog.Metadata.Counts == nil {
		catalog.Metadata.Counts = make(map[string]int)
	}
	catalog.Metadata.Counts["capabilities"] = len(catalog.Capabilities)
	catalog.Metadata.Counts["hiddenUncategorized"] = hidden
	return catalog
}

// BuildCapabilityCrossrefs builds a graph index for docs and agents.
func BuildCapabilityCrossrefs(inv *Inventory) *CapabilityCrossrefs {
	refs := &CapabilityCrossrefs{
		Metadata:     catalogMetadata(inv, "wfctl capability crossrefs"),
		Plugins:      make(map[string]PluginReference),
		Capabilities: make(map[string]CapabilityReference),
	}
	if inv == nil {
		return refs
	}
	for _, cap := range inv.Capabilities {
		uncategorized := isUncategorizedCapability(cap.ID, cap.Category)
		var capRef CapabilityReference
		if !uncategorized {
			capRef = refs.Capabilities[cap.ID]
			capRef.ID = cap.ID
			capRef.Category = cap.Category
			capRef.Name = cap.Name
		}
		for _, provider := range cap.Providers {
			pluginRef := refs.Plugins[provider.Name]
			pluginRef.Name = provider.Name
			pluginRef.Kind = firstNonEmpty(pluginRef.Kind, provider.Kind)
			pluginRef.Version = firstNonEmpty(pluginRef.Version, provider.Version)
			pluginRef.ReleaseStatus = firstNonEmpty(pluginRef.ReleaseStatus, provider.ReleaseStatus)
			pluginRef.Source = firstNonEmpty(pluginRef.Source, provider.Source)
			if uncategorized {
				pluginRef.RawCapabilities = mergeStrings(pluginRef.RawCapabilities, provider.Capabilities)
			} else {
				capRef.Providers = mergeStrings(capRef.Providers, []string{provider.Name})
				pluginRef.Capabilities = mergeStrings(pluginRef.Capabilities, []string{cap.ID})
			}
			pluginRef.Dependencies = mergeStrings(pluginRef.Dependencies, provider.Dependencies)
			refs.Plugins[provider.Name] = pluginRef
		}
		if !uncategorized {
			sort.Strings(capRef.Providers)
			refs.Capabilities[cap.ID] = capRef
		}
	}
	if refs.Metadata.Counts == nil {
		refs.Metadata.Counts = make(map[string]int)
	}
	refs.Metadata.Counts["plugins"] = len(refs.Plugins)
	refs.Metadata.Counts["capabilities"] = len(refs.Capabilities)
	return refs
}

func catalogMetadata(inv *Inventory, generator string) Metadata {
	meta := Metadata{Generator: generator}
	if inv != nil {
		meta = inv.Metadata
		meta.Generator = generator
		meta.Counts = copyCounts(inv.Metadata.Counts)
	}
	if meta.Counts == nil {
		meta.Counts = make(map[string]int)
	}
	return meta
}

func copyCounts(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func publicFindings(findings []Finding) []Finding {
	out := make([]Finding, 0, len(findings))
	for _, finding := range findings {
		if isUncategorizedCapability(finding.CapabilityID, "") {
			continue
		}
		out = append(out, finding)
	}
	return out
}

func isUncategorizedCapability(id, category string) bool {
	return category == "uncategorized" || strings.HasPrefix(id, "uncategorized:")
}
