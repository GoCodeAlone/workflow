package inventory

import "strings"

// FilterOptions describes first-class filters shared by capability ecosystem,
// catalog, cross-reference, app, and check outputs.
type FilterOptions struct {
	Capabilities []string
	Categories   []string
	Providers    []string
	Sources      []string
	EvidenceKind []string
	Usage        []string
	FindingCodes []string
	Tags         []string
	Exact        bool
}

// Empty reports whether no filter terms are active.
func (o FilterOptions) Empty() bool {
	return len(o.Capabilities) == 0 &&
		len(o.Categories) == 0 &&
		len(o.Providers) == 0 &&
		len(o.Sources) == 0 &&
		len(o.EvidenceKind) == 0 &&
		len(o.Usage) == 0 &&
		len(o.FindingCodes) == 0 &&
		len(o.Tags) == 0
}

// FilterInventory returns a copy of inv reduced to capability rows matching all
// active filter fields. Repeated values within one field are ORed.
func FilterInventory(inv *Inventory, opts FilterOptions) *Inventory {
	if inv == nil {
		return nil
	}
	out := &Inventory{
		Metadata:     filteredMetadata(inv.Metadata),
		Capabilities: make([]Capability, 0, len(inv.Capabilities)),
	}
	for i := range inv.Capabilities {
		cap := filterCapability(inv.Capabilities[i], opts)
		if !capMatches(cap, opts) {
			continue
		}
		out.Capabilities = append(out.Capabilities, cap)
	}
	out.Findings = filterFindings(inv.Findings, opts)
	for i := range out.Capabilities {
		out.Findings = append(out.Findings, out.Capabilities[i].Findings...)
	}
	out.Findings = dedupeFindings(out.Findings)
	setFilteredCounts(out.Metadata.Counts, len(out.Capabilities), len(out.Findings))
	return out
}

// FilterAppProfile returns a copy of profile reduced to usage rows and findings
// matching all active filter fields.
func FilterAppProfile(profile *AppProfile, opts FilterOptions) *AppProfile {
	if profile == nil {
		return nil
	}
	out := &AppProfile{
		Metadata: filteredMetadata(profile.Metadata),
		Usage:    make([]Usage, 0, len(profile.Usage)),
	}
	for i := range profile.Usage {
		usage := filterUsage(profile.Usage[i], opts)
		if !usageMatches(usage, opts) {
			continue
		}
		out.Usage = append(out.Usage, usage)
	}
	out.Findings = filterFindings(profile.Findings, opts)
	for i := range out.Usage {
		out.Findings = append(out.Findings, out.Usage[i].Findings...)
	}
	out.Findings = dedupeFindings(out.Findings)
	if out.Metadata.Counts == nil {
		out.Metadata.Counts = make(map[string]int)
	}
	out.Metadata.Counts["usage"] = len(out.Usage)
	out.Metadata.Counts["findings"] = len(out.Findings)
	return out
}

// FilterFindings returns a copy of findings reduced by the active finding,
// capability, source, and evidence filters.
func FilterFindings(findings []Finding, opts FilterOptions) []Finding {
	return filterFindings(findings, opts)
}

func filterCapability(cap Capability, opts FilterOptions) Capability {
	cap.Tags = append([]string(nil), cap.Tags...)
	cap.Findings = filterFindings(cap.Findings, opts)
	cap.Evidence = filterEvidence(cap.Evidence, opts)
	cap.Providers = filterProviders(cap.Providers, opts)
	return cap
}

func filterUsage(usage Usage, opts FilterOptions) Usage {
	usage.Evidence = filterEvidence(usage.Evidence, opts)
	usage.Findings = filterFindings(usage.Findings, opts)
	return usage
}

func filterProviders(providers []Provider, opts FilterOptions) []Provider {
	out := make([]Provider, 0, len(providers))
	for i := range providers {
		provider := providers[i]
		if len(opts.Providers) > 0 && !providerMatches(provider, opts) {
			continue
		}
		if len(opts.Sources) > 0 && !matchAny(opts.Sources, opts.Exact, provider.Source) {
			continue
		}
		provider.Capabilities = append([]string(nil), provider.Capabilities...)
		provider.Dependencies = append([]string(nil), provider.Dependencies...)
		out = append(out, provider)
	}
	return out
}

func filterEvidence(evidence []Evidence, opts FilterOptions) []Evidence {
	out := make([]Evidence, 0, len(evidence))
	for _, item := range evidence {
		if len(opts.EvidenceKind) > 0 && !matchAny(opts.EvidenceKind, opts.Exact, item.SourceKind) {
			continue
		}
		if len(opts.Sources) > 0 && !matchAny(opts.Sources, opts.Exact, item.SourcePath, item.Detail) {
			continue
		}
		if len(opts.Usage) > 0 && !matchAny(opts.Usage, opts.Exact, item.SourceKind, item.SourcePath, item.JSONPath, item.Detail) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func filterFindings(findings []Finding, opts FilterOptions) []Finding {
	out := make([]Finding, 0, len(findings))
	for _, finding := range findings {
		if !findingMatches(finding, opts) {
			continue
		}
		finding.Evidence = filterEvidence(finding.Evidence, opts)
		out = append(out, finding)
	}
	return out
}

func capMatches(cap Capability, opts FilterOptions) bool {
	if len(opts.Capabilities) > 0 && !matchAny(opts.Capabilities, opts.Exact, cap.ID, cap.Name, cap.Description) {
		return false
	}
	if len(opts.Categories) > 0 && !matchAny(opts.Categories, opts.Exact, cap.Category) {
		return false
	}
	if len(opts.Tags) > 0 && !matchAny(opts.Tags, opts.Exact, cap.Tags...) {
		return false
	}
	if len(opts.Providers) > 0 && len(cap.Providers) == 0 {
		return false
	}
	if len(opts.Sources) > 0 && len(cap.Providers) == 0 && len(cap.Evidence) == 0 {
		return false
	}
	if len(opts.EvidenceKind) > 0 && len(cap.Evidence) == 0 {
		return false
	}
	if len(opts.FindingCodes) > 0 && len(cap.Findings) == 0 {
		return false
	}
	return true
}

func usageMatches(usage Usage, opts FilterOptions) bool {
	if len(opts.Capabilities) > 0 && !matchAny(opts.Capabilities, opts.Exact, usage.CapabilityID, usage.Name) {
		return false
	}
	if len(opts.Categories) > 0 && !matchAny(opts.Categories, opts.Exact, usage.Category) {
		return false
	}
	if len(opts.Usage) > 0 && !matchAny(opts.Usage, opts.Exact, usage.Mode, usage.Confidence) && len(usage.Evidence) == 0 {
		return false
	}
	if len(opts.Sources) > 0 && len(usage.Evidence) == 0 {
		return false
	}
	if len(opts.EvidenceKind) > 0 && len(usage.Evidence) == 0 {
		return false
	}
	if len(opts.FindingCodes) > 0 && len(usage.Findings) == 0 {
		return false
	}
	return true
}

func providerMatches(provider Provider, opts FilterOptions) bool {
	values := []string{provider.Name, provider.Kind, provider.Source, provider.Version, provider.ReleaseStatus}
	values = append(values, provider.Capabilities...)
	values = append(values, provider.Dependencies...)
	return matchAny(opts.Providers, opts.Exact, values...)
}

func findingMatches(finding Finding, opts FilterOptions) bool {
	if len(opts.FindingCodes) > 0 && !matchAny(opts.FindingCodes, opts.Exact, finding.Code, finding.Level, finding.Message) {
		return false
	}
	if len(opts.Capabilities) > 0 && !matchAny(opts.Capabilities, opts.Exact, finding.CapabilityID) {
		return false
	}
	if (len(opts.Sources) > 0 || len(opts.EvidenceKind) > 0) && len(filterEvidence(finding.Evidence, opts)) == 0 {
		return false
	}
	return true
}

func matchAny(needles []string, exact bool, values ...string) bool {
	if len(needles) == 0 {
		return true
	}
	for _, needle := range needles {
		needle = strings.ToLower(strings.TrimSpace(needle))
		if needle == "" {
			continue
		}
		for _, value := range values {
			value = strings.ToLower(strings.TrimSpace(value))
			if exact {
				if value == needle {
					return true
				}
				continue
			}
			if strings.Contains(value, needle) {
				return true
			}
		}
	}
	return false
}

func filteredMetadata(meta Metadata) Metadata {
	meta.Counts = copyCounts(meta.Counts)
	if meta.Counts == nil {
		meta.Counts = make(map[string]int)
	}
	return meta
}

func setFilteredCounts(counts map[string]int, capabilities, findings int) {
	if counts == nil {
		return
	}
	counts["capabilities"] = capabilities
	counts["findings"] = findings
}

func dedupeFindings(findings []Finding) []Finding {
	if len(findings) == 0 {
		return nil
	}
	out := make([]Finding, 0, len(findings))
	seen := make(map[string]struct{}, len(findings))
	for _, finding := range findings {
		key := findingKey(finding)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, finding)
	}
	return out
}
