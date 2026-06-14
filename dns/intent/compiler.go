package intent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/dns/record"
)

const (
	SchemaV1       = "workflow.domain-intent.v1"
	LegacySchemaV1 = "gocodealone.domain-intent.v1"
	ReportSchemaV1 = "workflow.domain-intent.report.v1"
)

type Document struct {
	Schema  string                  `json:"schema" yaml:"schema"`
	Domains map[string]DomainIntent `json:"domains" yaml:"domains"`
}

type DomainIntent struct {
	Registrar                  string   `json:"registrar" yaml:"registrar"`
	DNSHost                    string   `json:"dns_host" yaml:"dns_host"`
	StageDNS                   *bool    `json:"stage_dns,omitempty" yaml:"stage_dns,omitempty"`
	NameserverCutover          bool     `json:"nameserver_cutover" yaml:"nameserver_cutover"`
	RecordsPolicy              string   `json:"records_policy" yaml:"records_policy"`
	ExpectedCurrentNameservers []string `json:"expected_current_nameservers,omitempty" yaml:"expected_current_nameservers,omitempty"`
	AllowDiscardNonparked      bool     `json:"allow_discard_nonparked,omitempty" yaml:"allow_discard_nonparked,omitempty"`
}

type Options struct {
	IntentPath     string
	PortfolioGlobs []string
	DomainFilter   string
	StateDir       string
}

type Bundle struct {
	Config config.WorkflowConfig `json:"config" yaml:"config"`
	Report Report                `json:"report" yaml:"report"`
}

type Report struct {
	Schema         string         `json:"schema" yaml:"schema"`
	Domains        []DomainReport `json:"domains" yaml:"domains"`
	BlockedDomains int            `json:"blocked_domains" yaml:"blocked_domains"`
	ActionCount    int            `json:"action_count" yaml:"action_count"`
}

type DomainReport struct {
	Domain                string   `json:"domain" yaml:"domain"`
	Registrar             string   `json:"registrar" yaml:"registrar"`
	DNSHost               string   `json:"dns_host" yaml:"dns_host"`
	CloudflareNameservers []string `json:"cloudflare_nameservers" yaml:"cloudflare_nameservers"`
	RecordsPolicy         string   `json:"records_policy" yaml:"records_policy"`
	Actions               []Action `json:"actions" yaml:"actions"`
	Blockers              []string `json:"blockers" yaml:"blockers"`
}

type Action struct {
	Type                       string   `json:"type" yaml:"type"`
	Provider                   string   `json:"provider" yaml:"provider"`
	Resource                   string   `json:"resource" yaml:"resource"`
	RecordCount                *int     `json:"record_count,omitempty" yaml:"record_count,omitempty"`
	ManageUnlisted             *bool    `json:"manage_unlisted,omitempty" yaml:"manage_unlisted,omitempty"`
	RecordsPolicy              string   `json:"records_policy,omitempty" yaml:"records_policy,omitempty"`
	DesiredNameservers         []string `json:"desired_nameservers,omitempty" yaml:"desired_nameservers,omitempty"`
	ExpectedCurrentNameservers []string `json:"expected_current_nameservers,omitempty" yaml:"expected_current_nameservers,omitempty"`
}

type recordPlan struct {
	records        []map[string]any
	manageUnlisted bool
	blockers       []string
}

func Compile(opts Options) (*Bundle, error) {
	if opts.IntentPath == "" {
		return nil, fmt.Errorf("intent path required")
	}
	if len(opts.PortfolioGlobs) == 0 {
		return nil, fmt.Errorf("at least one portfolio path or glob is required")
	}
	if opts.StateDir == "" {
		opts.StateDir = ".state/domain-intent/"
	}
	intentDoc, err := loadDocument(opts.IntentPath)
	if err != nil {
		return nil, err
	}
	snapshots, err := loadSnapshots(opts.PortfolioGlobs)
	if err != nil {
		return nil, err
	}
	domainFilter := normalizeDomain(opts.DomainFilter)
	domainMap := make(map[string]DomainIntent, len(intentDoc.Domains))
	for domain, cfg := range intentDoc.Domains {
		domainMap[normalizeDomain(domain)] = cfg
	}
	if domainFilter != "" {
		cfg, ok := domainMap[domainFilter]
		if !ok {
			return nil, fmt.Errorf("domain %s not found in intent", domainFilter)
		}
		domainMap = map[string]DomainIntent{domainFilter: cfg}
	}

	domains := make([]string, 0, len(domainMap))
	for domain := range domainMap {
		domains = append(domains, domain)
	}
	sort.Strings(domains)

	results := make([]compileResult, 0, len(domains))
	for _, domain := range domains {
		results = append(results, reconcileDomain(domain, domainMap[domain], snapshots))
	}

	return buildBundle(results, opts.StateDir), nil
}

func loadDocument(path string) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read intent %q: %w", path, err)
	}
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse intent %q: %w", path, err)
	}
	if doc.Schema != SchemaV1 && doc.Schema != LegacySchemaV1 {
		return nil, fmt.Errorf("unsupported intent schema: %s", doc.Schema)
	}
	if len(doc.Domains) == 0 {
		return nil, fmt.Errorf("intent has no domains")
	}
	return &doc, nil
}

func loadSnapshots(globs []string) ([]record.Snapshot, error) {
	var files []string
	for _, pattern := range globs {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("portfolio glob %q: %w", pattern, err)
		}
		if len(matches) == 0 {
			if _, statErr := os.Stat(pattern); statErr == nil {
				matches = []string{pattern}
			}
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no portfolio files matched")
	}
	sort.Strings(files)
	var snapshots []record.Snapshot
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read portfolio %q: %w", file, err)
		}
		var p record.Portfolio
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("parse portfolio %q: %w", file, err)
		}
		if err := p.Validate(); err != nil {
			return nil, fmt.Errorf("portfolio %q: %w", file, err)
		}
		snapshots = append(snapshots, p.Snapshots...)
	}
	return snapshots, nil
}

type compileResult struct {
	report  DomainReport
	modules []config.ModuleConfig
}

func reconcileDomain(domain string, cfg DomainIntent, snapshots []record.Snapshot) compileResult {
	group := snapshotsForDomain(snapshots, domain)
	cfSnapshot, _ := firstSnapshotByProvider(group, "cloudflare")
	cfNameservers := normalizeNameservers(stringSliceFromAuthority(cfSnapshot.Authority, "name_servers"))
	desiredDNSHost := strings.ToLower(strings.TrimSpace(cfg.DNSHost))
	registrar := strings.ToLower(strings.TrimSpace(cfg.Registrar))
	recordsPolicy := cfg.RecordsPolicy
	if recordsPolicy == "" {
		recordsPolicy = "preserve_authoritative"
	}

	blockers := []string{}
	if desiredDNSHost == "cloudflare" && len(cfNameservers) == 0 {
		blockers = append(blockers, "Cloudflare authority.name_servers missing; stage/import Cloudflare zone first")
	}
	if desiredDNSHost != "cloudflare" {
		blockers = append(blockers, fmt.Sprintf("dns_host %q is not supported yet", desiredDNSHost))
	}
	if cfg.NameserverCutover && registrar != "hover" {
		blockers = append(blockers, fmt.Sprintf("registrar %q nameserver cutover is not supported yet", registrar))
	}

	plan := recordPlan{}
	if stageDNS(cfg) {
		plan = planRecords(domain, cfg, recordsPolicy, group, cfSnapshot)
		blockers = append(blockers, plan.blockers...)
	}

	report := DomainReport{
		Domain:                domain,
		Registrar:             registrar,
		DNSHost:               desiredDNSHost,
		CloudflareNameservers: cfNameservers,
		RecordsPolicy:         recordsPolicy,
		Blockers:              blockers,
	}
	if len(blockers) > 0 {
		return compileResult{report: report}
	}

	var modules []config.ModuleConfig
	if stageDNS(cfg) {
		resource := resourceName("cf", domain)
		report.Actions = append(report.Actions, Action{
			Type:           "stage_dns",
			Provider:       "cloudflare",
			Resource:       resource,
			RecordCount:    intPtr(len(plan.records)),
			ManageUnlisted: boolPtr(plan.manageUnlisted),
			RecordsPolicy:  recordsPolicy,
		})
		modules = append(modules, config.ModuleConfig{
			Name: resource,
			Type: "infra.dns",
			Config: map[string]any{
				"provider":        "cloudflare",
				"account_id":      "${CLOUDFLARE_ACCOUNT_ID}",
				"domain":          domain,
				"manage_unlisted": plan.manageUnlisted,
				"records":         plan.records,
			},
		})
	}
	if cfg.NameserverCutover {
		resource := resourceName("hover-delegation", domain)
		expected := normalizeNameservers(cfg.ExpectedCurrentNameservers)
		report.Actions = append(report.Actions, Action{
			Type:                       "set_nameservers",
			Provider:                   "hover",
			Resource:                   resource,
			DesiredNameservers:         cfNameservers,
			ExpectedCurrentNameservers: expected,
		})
		modules = append(modules, config.ModuleConfig{
			Name: resource,
			Type: "infra.dns_delegation",
			Config: map[string]any{
				"provider":    "hover",
				"domain":      domain,
				"nameservers": cfNameservers,
			},
		})
	}
	return compileResult{report: report, modules: modules}
}

func buildBundle(results []compileResult, stateDir string) *Bundle {
	var modules []config.ModuleConfig
	needCloudflare := false
	needHover := false
	report := Report{Schema: ReportSchemaV1}
	for i := range results {
		result := &results[i]
		report.Domains = append(report.Domains, result.report)
		if len(result.report.Blockers) > 0 {
			report.BlockedDomains++
		}
		report.ActionCount += len(result.report.Actions)
		for j := range result.report.Actions {
			action := &result.report.Actions[j]
			switch action.Provider {
			case "cloudflare":
				needCloudflare = true
			case "hover":
				needHover = true
			}
		}
	}
	if needCloudflare {
		modules = append(modules, config.ModuleConfig{
			Name: "cloudflare",
			Type: "iac.provider",
			Config: map[string]any{ //nolint:gosec // Values are env placeholders, not literal credentials.
				"provider":   "cloudflare",
				"api_token":  "${CLOUDFLARE_API_TOKEN}",
				"account_id": "${CLOUDFLARE_ACCOUNT_ID}",
			},
		})
	}
	if needHover {
		modules = append(modules, config.ModuleConfig{
			Name: "hover",
			Type: "iac.provider",
			Config: map[string]any{ //nolint:gosec // Values are env placeholders, not literal credentials.
				"provider":            "hover",
				"username":            "${HOVER_USERNAME}",
				"password":            "${HOVER_PASSWORD}",
				"totp_secret":         "${HOVER_TOTP_SECRET}",
				"browser_profile_dir": "${HOVER_BROWSER_PROFILE_DIR}",
				"browser_headless":    true,
				"browser_download":    true,
			},
		})
	}
	modules = append(modules, config.ModuleConfig{
		Name: "iac-state",
		Type: "iac.state",
		Config: map[string]any{
			"backend":   "filesystem",
			"directory": stateDir,
		},
	})
	for i := range results {
		modules = append(modules, results[i].modules...)
	}
	return &Bundle{
		Config: config.WorkflowConfig{Modules: modules},
		Report: report,
	}
}

func planRecords(domain string, cfg DomainIntent, policy string, group []record.Snapshot, cfSnapshot record.Snapshot) recordPlan {
	hoverSnapshot, hasHover := firstSnapshotByProvider(group, "hover")
	switch policy {
	case "discard_parked":
		if hasHover && parkedHoverRecords(hoverSnapshot.Records) {
			return recordPlan{records: []map[string]any{}, manageUnlisted: true}
		}
		if cfg.AllowDiscardNonparked {
			return recordPlan{records: []map[string]any{}, manageUnlisted: true}
		}
		return recordPlan{blockers: []string{"records_policy discard_parked requested but Hover records do not match parked-record pattern"}}
	case "preserve_authoritative":
		if selected, ok := selectSource(group); ok {
			return recordPlan{records: cloudflareRecords(domain, selected.Records)}
		}
		return recordPlan{blockers: []string{"no portfolio snapshot available for records"}}
	case "preserve_cloudflare":
		if cfSnapshot.Provider != "" {
			return recordPlan{records: cloudflareRecords(domain, cfSnapshot.Records)}
		}
		return recordPlan{blockers: []string{"records_policy preserve_cloudflare requested but no Cloudflare snapshot exists"}}
	default:
		return recordPlan{blockers: []string{fmt.Sprintf("unsupported records_policy %q", policy)}}
	}
}

func stageDNS(cfg DomainIntent) bool {
	if cfg.StageDNS == nil {
		return true
	}
	return *cfg.StageDNS
}

func snapshotsForDomain(snapshots []record.Snapshot, domain string) []record.Snapshot {
	var out []record.Snapshot
	for _, snapshot := range snapshots {
		if normalizeDomain(snapshot.Domain) == domain {
			out = append(out, snapshot)
		}
	}
	return out
}

func firstSnapshotByProvider(snapshots []record.Snapshot, provider string) (record.Snapshot, bool) {
	for _, snapshot := range snapshots {
		if strings.EqualFold(snapshot.Provider, provider) {
			return snapshot, true
		}
	}
	return record.Snapshot{}, false
}

func selectSource(group []record.Snapshot) (record.Snapshot, bool) {
	if len(group) == 0 {
		return record.Snapshot{}, false
	}
	sort.SliceStable(group, func(i, j int) bool {
		return sourceRank(group[i], group) < sourceRank(group[j], group)
	})
	return group[0], true
}

func sourceRank(snapshot record.Snapshot, group []record.Snapshot) int {
	provider := strings.ToLower(snapshot.Provider)
	authorityProvider := nameserverProvider(snapshot)
	switch {
	case provider == "cloudflare":
		return 0
	case authorityProvider != "" && provider == authorityProvider:
		return 1
	case provider == "digitalocean" && hoverDelegatesTo(group, "digitalocean"):
		return 1
	case provider == "namecheap":
		return 2
	case provider == "hover" && authorityProvider == "hover":
		return 2
	case provider == "digitalocean":
		return 3
	case provider == "hover":
		return 4
	default:
		return 9
	}
}

func hoverDelegatesTo(group []record.Snapshot, provider string) bool {
	for _, snapshot := range group {
		if strings.EqualFold(snapshot.Provider, "hover") && nameserverProvider(snapshot) == provider {
			return true
		}
	}
	return false
}

func nameserverProvider(snapshot record.Snapshot) string {
	ns := normalizeNameservers(append(
		stringSliceFromAuthority(snapshot.Authority, "live_nameservers"),
		stringSliceFromAuthority(snapshot.Authority, "registrar_nameservers")...,
	))
	for _, value := range ns {
		switch {
		case strings.Contains(value, "cloudflare.com"):
			return "cloudflare"
		case strings.Contains(value, "digitalocean.com"):
			return "digitalocean"
		case strings.Contains(value, "hover.com"):
			return "hover"
		case strings.Contains(value, "registrar-servers.com"):
			return "namecheap"
		}
	}
	return ""
}

func stringSliceFromAuthority(authority map[string]any, key string) []string {
	if authority == nil {
		return nil
	}
	raw, ok := authority[key]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func parkedHoverRecords(records []record.Record) bool {
	if len(records) == 0 {
		return false
	}
	for _, rec := range records {
		recType := strings.ToUpper(rec.Type)
		name := strings.ToLower(defaultName(rec.Name))
		value := strings.TrimSuffix(strings.ToLower(rec.Value), ".")
		priority := 0
		if rec.Priority != nil {
			priority = *rec.Priority
		}
		switch recType {
		case "A":
			if (name != "@" && name != "*") || value != "216.40.34.41" {
				return false
			}
		case "CNAME":
			if name != "mail" || value != "mail.hover.com.cust.hostedemail.com" {
				return false
			}
		case "MX":
			if name != "@" || !strings.HasSuffix(value, "mx.hover.com.cust.hostedemail.com") || (priority != 0 && priority != 10) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func cloudflareRecords(domain string, records []record.Record) []map[string]any {
	out := make([]map[string]any, 0, len(records))
	for _, rec := range records {
		recType := strings.ToUpper(rec.Type)
		if !supportedCloudflareType(recType) {
			continue
		}
		name := defaultName(rec.Name)
		normalizedName := normalizeDomain(name)
		if recType == "NS" && (name == "@" || normalizedName == domain) {
			continue
		}
		data := rec.Value
		if recType == "CNAME" || recType == "MX" || recType == "NS" || recType == "SRV" {
			data = strings.TrimSuffix(data, ".")
		}
		ttl := rec.TTL
		if ttl == 1 {
			ttl = 1
		} else if ttl < 60 {
			ttl = 60
		}
		item := map[string]any{
			"type": recType,
			"name": name,
			"data": data,
			"ttl":  ttl,
		}
		if (recType == "MX" || recType == "SRV") && rec.Priority != nil {
			item["priority"] = *rec.Priority
		}
		out = append(out, item)
	}
	return out
}

func supportedCloudflareType(recType string) bool {
	switch recType {
	case "A", "AAAA", "CAA", "CNAME", "MX", "NS", "SRV", "TXT":
		return true
	default:
		return false
	}
}

func defaultName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "@"
	}
	return name
}

func normalizeNameservers(values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		ns := normalizeDomain(value)
		if ns != "" {
			seen[ns] = true
		}
	}
	out := make([]string, 0, len(seen))
	for ns := range seen {
		out = append(out, ns)
	}
	sort.Strings(out)
	return out
}

func normalizeDomain(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
}

var resourceNamePattern = regexp.MustCompile(`[^a-z0-9]+`)

func resourceName(prefix, domain string) string {
	slug := resourceNamePattern.ReplaceAllString(normalizeDomain(domain), "-")
	slug = strings.Trim(slug, "-")
	return prefix + "-" + slug
}

func intPtr(v int) *int { return &v }

func boolPtr(v bool) *bool { return &v }
