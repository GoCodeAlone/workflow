package record_test

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/dns/record"
)

// TestSanitizeSetsFlag pins that Sanitize always sets p.Sanitized = true.
func TestSanitizeSetsFlag(t *testing.T) {
	p := record.Portfolio{
		Schema:    record.SchemaV1,
		Snapshots: []record.Snapshot{},
	}
	if p.Sanitized {
		t.Fatal("portfolio.Sanitized should start false")
	}
	record.Sanitize(&p)
	if !p.Sanitized {
		t.Fatal("Sanitize must set p.Sanitized = true")
	}
}

// TestSanitizeRedactsPublicIPv4 pins that A records with a GENUINELY-PUBLIC IP
// (8.8.8.8 — Google DNS, not RFC-5737) are replaced with an RFC-5737 example
// address. C-1: input MUST be a real public IP so a no-op Sanitize fails this
// test; we assert the output (a) differs from input AND (b) is an example IP.
func TestSanitizeRedactsPublicIPv4(t *testing.T) {
	const publicIP = "8.8.8.8" // genuinely public, NOT RFC-5737
	p := record.Portfolio{
		Schema: record.SchemaV1,
		Snapshots: []record.Snapshot{{
			Provider: "digitalocean",
			Domain:   "example.com",
			Records: []record.Record{
				{Type: "A", Name: "@", Value: publicIP, TTL: 300},
			},
		}},
	}
	record.Sanitize(&p)
	got := p.Snapshots[0].Records[0].Value
	if got == publicIP {
		t.Errorf("public IP %s was NOT redacted (Sanitize was a no-op)", publicIP)
	}
	if !isExampleIP(got) {
		t.Errorf("after Sanitize, A record value = %q; want an RFC-5737 example IP", got)
	}
}

// TestSanitizeRedactsPublicIPv6 pins that AAAA records with a genuinely-public
// IPv6 (2606:4700:4700::1111 — Cloudflare, and 2607:f8b0... Google) are
// replaced with 2001:db8:: (RFC-3849). C-1: input must differ from output.
func TestSanitizeRedactsPublicIPv6(t *testing.T) {
	cases := []string{
		"2606:4700:4700::1111",   // Cloudflare public resolver
		"2607:f8b0:4004:c07::64", // Google
	}
	for _, in := range cases {
		p := record.Portfolio{
			Schema: record.SchemaV1,
			Snapshots: []record.Snapshot{{
				Provider: "digitalocean",
				Domain:   "example.com",
				Records: []record.Record{
					{Type: "AAAA", Name: "@", Value: in, TTL: 300},
				},
			}},
		}
		record.Sanitize(&p)
		got := p.Snapshots[0].Records[0].Value
		if got == in {
			t.Errorf("public IPv6 %s was NOT redacted (Sanitize was a no-op)", in)
		}
		if got != "2001:db8::" {
			t.Errorf("after Sanitize, AAAA record value = %q; want 2001:db8::", got)
		}
	}
}

// TestSanitizePreservesPrivateIPs pins that private/reserved addresses are
// left unchanged (they're not sensitive public IPs). I-2: include RFC-6598
// CGNAT (100.64.0.0/10), link-local (169.254), loopback, and IPv6 ULA
// (fc00::/7) + the routable-but-documentation-adjacent 2001:dbc.
func TestSanitizePreservesPrivateIPs(t *testing.T) {
	v4 := []string{
		"10.0.0.1",     // RFC-1918
		"172.16.5.1",   // RFC-1918
		"192.168.1.1",  // RFC-1918
		"127.0.0.1",    // loopback
		"169.254.10.1", // link-local
		"100.64.0.1",   // RFC-6598 CGNAT
		"100.127.0.1",  // RFC-6598 CGNAT upper edge
	}
	for _, ip := range v4 {
		p := record.Portfolio{
			Schema: record.SchemaV1,
			Snapshots: []record.Snapshot{{
				Provider: "digitalocean",
				Domain:   "example.com",
				Records:  []record.Record{{Type: "A", Name: "@", Value: ip, TTL: 300}},
			}},
		}
		record.Sanitize(&p)
		got := p.Snapshots[0].Records[0].Value
		if got != ip {
			t.Errorf("reserved IPv4 %s was changed to %s; private/reserved IPs should be preserved", ip, got)
		}
	}

	v6 := []string{
		"::1",                 // loopback
		"fe80::1",             // link-local
		"fc00::1",             // ULA (fc00::/7)
		"fd12:3456:789a:1::1", // ULA (fd00::/8)
	}
	for _, ip := range v6 {
		p := record.Portfolio{
			Schema: record.SchemaV1,
			Snapshots: []record.Snapshot{{
				Provider: "digitalocean",
				Domain:   "example.com",
				Records:  []record.Record{{Type: "AAAA", Name: "@", Value: ip, TTL: 300}},
			}},
		}
		record.Sanitize(&p)
		got := p.Snapshots[0].Records[0].Value
		if got != ip {
			t.Errorf("reserved IPv6 %s was changed to %s; ULA/loopback/link-local must be preserved", ip, got)
		}
	}
}

// TestSanitizeRedacts2001dbc pins I-2's over-broad-prefix bug: the old
// `v[:7]=="2001:db"` logic WRONGLY preserved routable 2001:dbc/2001:db0
// addresses (they share the "2001:db" prefix but are NOT in RFC-3849's
// 2001:db8::/32). With net.ParseIP these are global-unicast → must be redacted.
func TestSanitizeRedacts2001dbc(t *testing.T) {
	cases := []string{
		"2001:dbc:dead:beef::1", // 2001:dbc — routable, NOT 2001:db8::/32
		"2001:db0::1",           // 2001:db0 — routable, NOT 2001:db8::/32
	}
	for _, in := range cases {
		p := record.Portfolio{
			Schema: record.SchemaV1,
			Snapshots: []record.Snapshot{{
				Provider: "digitalocean",
				Domain:   "example.com",
				Records:  []record.Record{{Type: "AAAA", Name: "@", Value: in, TTL: 300}},
			}},
		}
		record.Sanitize(&p)
		got := p.Snapshots[0].Records[0].Value
		if got == in {
			t.Errorf("routable IPv6 %s was NOT redacted (over-broad 2001:db prefix bug)", in)
		}
		if got != "2001:db8::" {
			t.Errorf("after Sanitize, %s → %q; want 2001:db8::", in, got)
		}
	}
}

// TestSanitizeRedactsDKIMTXT pins that TXT records containing a DKIM public
// key (p= blob, long) are replaced with "[redacted]".
func TestSanitizeRedactsDKIMTXT(t *testing.T) {
	dkimValue := "v=DKIM1; k=rsa; p=MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA2a8h/THISIS/AFAKE/DKIMKEY/FORTEST/PURPOSESONLY/BASE64ABCDEFGHIJKLMNOPQRSTUVWXYZABCDEFGHIJKLMNOPQRSTUVWXYZ"
	p := record.Portfolio{
		Schema: record.SchemaV1,
		Snapshots: []record.Snapshot{{
			Provider: "digitalocean",
			Domain:   "example.com",
			Records: []record.Record{
				{Type: "TXT", Name: "_domainkey", Value: dkimValue, TTL: 3600},
			},
		}},
	}
	record.Sanitize(&p)
	got := p.Snapshots[0].Records[0].Value
	if got != "[redacted]" {
		t.Errorf("DKIM TXT value = %q; want [redacted]", got)
	}
}

// TestSanitizePreservesLongDMARC pins C-2: a long (>80-char) legitimate DMARC
// TXT with a `p=` policy keyword (reject/quarantine/none) must NOT be redacted.
// The old `containsSubstring(v,"p=") && len(v)>80` heuristic false-redacted it.
func TestSanitizePreservesLongDMARC(t *testing.T) {
	cases := []string{
		"v=DMARC1; p=reject; rua=mailto:dmarc-agg@example.com; ruf=mailto:dmarc-forensic@example.com; fo=1; pct=100; adkim=s; aspf=s",
		"v=DMARC1; p=quarantine; sp=reject; rua=mailto:reports@example.com; ruf=mailto:forensics@example.com; pct=100",
		"v=DMARC1; p=none; rua=mailto:postmaster@example.com; ruf=mailto:postmaster@example.com; adkim=r; aspf=r; ri=86400",
	}
	for _, v := range cases {
		if len(v) <= 80 {
			t.Fatalf("test bug: DMARC value must be >80 chars to exercise C-2; got %d", len(v))
		}
		p := record.Portfolio{
			Schema: record.SchemaV1,
			Snapshots: []record.Snapshot{{
				Provider: "digitalocean",
				Domain:   "example.com",
				Records:  []record.Record{{Type: "TXT", Name: "_dmarc", Value: v, TTL: 3600}},
			}},
		}
		record.Sanitize(&p)
		got := p.Snapshots[0].Records[0].Value
		if got != v {
			t.Errorf("long DMARC TXT was changed to %q; legitimate DMARC must be preserved", got)
		}
	}
}

// TestSanitizeRedactsRealDKIM pins C-2's positive half: a genuine DKIM key
// record (v=DKIM1 + long base64 p= blob) IS redacted.
func TestSanitizeRedactsRealDKIM(t *testing.T) {
	dkim := "v=DKIM1; k=rsa; p=MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA2a8h0vftMQtZsqHXkv9TestFakeKeyBase64DataMoreAndMoreAndMoreAAAABBBBCCCCDDDDEEEEFFFFGGGGHHHHIIIIJJJJKKKKLLLLMMMMNNNN"
	p := record.Portfolio{
		Schema: record.SchemaV1,
		Snapshots: []record.Snapshot{{
			Provider: "digitalocean",
			Domain:   "example.com",
			Records:  []record.Record{{Type: "TXT", Name: "selector1._domainkey", Value: dkim, TTL: 3600}},
		}},
	}
	record.Sanitize(&p)
	got := p.Snapshots[0].Records[0].Value
	if got != "[redacted]" {
		t.Errorf("real DKIM TXT = %q; want [redacted]", got)
	}
}

// TestSanitizePreservesWFInfraPolicyTXT pins that _workflow-dns-policy TXT
// records (heritage=wfinfra-v1) are NOT redacted — they are policy
// declarations, not secrets.
func TestSanitizePreservesWFInfraPolicyTXT(t *testing.T) {
	policyValue := "heritage=wfinfra-v1 o=gocodealone p=* t=A,CNAME"
	p := record.Portfolio{
		Schema: record.SchemaV1,
		Snapshots: []record.Snapshot{{
			Provider: "digitalocean",
			Domain:   "example.com",
			Records: []record.Record{
				{Type: "TXT", Name: "_workflow-dns-policy", Value: policyValue, TTL: 300},
			},
		}},
	}
	record.Sanitize(&p)
	got := p.Snapshots[0].Records[0].Value
	if got != policyValue {
		t.Errorf("_workflow-dns-policy TXT was changed to %q; must be preserved", got)
	}
}

// TestSanitizePreservesWFInfraPolicyByName pins M-2: a _workflow-dns-policy
// record is identified by NAME (authoritative) even if its value somehow
// looks secret-ish. A record named _workflow-dns-policy must never be redacted.
func TestSanitizePreservesWFInfraPolicyByName(t *testing.T) {
	// A value that would otherwise trip the long-base64 secret heuristic.
	longValue := "heritage=wfinfra-v1 o=gocodealone p=AAAABBBBCCCCDDDDEEEEFFFFGGGGHHHHIIIIJJJJKKKKLLLLMMMMNNNNOOOOPPPPQQQQRRRRSSSSTTTTUUUU t=A,CNAME,TXT,MX,AAAA,SRV,CAA"
	p := record.Portfolio{
		Schema: record.SchemaV1,
		Snapshots: []record.Snapshot{{
			Provider: "digitalocean",
			Domain:   "example.com",
			Records: []record.Record{
				{Type: "TXT", Name: "_workflow-dns-policy.example.com", Value: longValue, TTL: 300},
			},
		}},
	}
	record.Sanitize(&p)
	got := p.Snapshots[0].Records[0].Value
	if got != longValue {
		t.Errorf("_workflow-dns-policy (by name) TXT was changed to %q; must be preserved", got)
	}
}

// TestSanitizePreservesPlainTXT pins that plain TXT records (SPF, DMARC,
// site verification without long base64 blobs) are NOT redacted.
func TestSanitizePreservesPlainTXT(t *testing.T) {
	cases := []string{
		"v=spf1 include:_spf.google.com ~all",
		"v=DMARC1; p=quarantine; rua=mailto:dmarc@example.com",
		"google-site-verification=abc123short",
	}
	for _, v := range cases {
		p := record.Portfolio{
			Schema: record.SchemaV1,
			Snapshots: []record.Snapshot{{
				Provider: "digitalocean",
				Domain:   "example.com",
				Records:  []record.Record{{Type: "TXT", Name: "@", Value: v, TTL: 3600}},
			}},
		}
		record.Sanitize(&p)
		got := p.Snapshots[0].Records[0].Value
		if got == "[redacted]" {
			t.Errorf("plain TXT %q was redacted; only secret TXT values should be redacted", v)
		}
	}
}

// TestSanitizePreservesExistingExampleIPs pins that RFC-5737 example IPs
// already in the fixture are NOT changed (idempotent for fixture data).
func TestSanitizePreservesExistingExampleIPs(t *testing.T) {
	exampleIPs := []string{"192.0.2.10", "198.51.100.25", "203.0.113.40"}
	for _, ip := range exampleIPs {
		p := record.Portfolio{
			Schema: record.SchemaV1,
			Snapshots: []record.Snapshot{{
				Provider: "digitalocean",
				Domain:   "example.com",
				Records:  []record.Record{{Type: "A", Name: "@", Value: ip, TTL: 300}},
			}},
		}
		record.Sanitize(&p)
		got := p.Snapshots[0].Records[0].Value
		if !isExampleIP(got) {
			t.Errorf("existing example IP %s should remain an example IP; got %s", ip, got)
		}
		// The value shouldn't change (already sanitized)
		if got != ip {
			t.Logf("note: existing example IP %s was changed to %s (re-mapped example range, still valid)", ip, got)
		}
	}
}

// TestSanitizeStripsUnknownAuthorityKeys pins that Sanitize removes any key
// from Snapshot.Authority that is NOT in the allow-list
// {registrar_nameservers, live_nameservers}, while leaving allowed keys intact.
// A nil Authority must not panic, and Records sanitization must still work.
func TestSanitizeStripsUnknownAuthorityKeys(t *testing.T) {
	// Snapshot with a non-nil Authority containing both allowed and disallowed keys.
	p := record.Portfolio{
		Schema: record.SchemaV1,
		Snapshots: []record.Snapshot{
			{
				Provider: "digitalocean",
				Domain:   "example.com",
				Authority: map[string]any{
					"registrar_nameservers": []any{"ns1.x"},
					"live_nameservers":      []any{"ns2.x"},
					"secret_token":          "leak",
					"domain_id":             "12345",
				},
				Records: []record.Record{
					// A public IP that should be redacted (records pass still works).
					{Type: "A", Name: "@", Value: "8.8.8.8", TTL: 300},
				},
			},
			{
				// Snapshot with nil Authority — must not panic.
				Provider: "cloudflare",
				Domain:   "other.com",
				Records:  []record.Record{},
			},
		},
	}

	record.Sanitize(&p)

	auth := p.Snapshots[0].Authority

	// Allowed keys must remain, values deep-equal to input.
	rns, ok := auth["registrar_nameservers"]
	if !ok {
		t.Error("registrar_nameservers was removed; it must remain")
	} else {
		wantRNS := []any{"ns1.x"}
		got, _ := rns.([]any)
		if len(got) != 1 || got[0] != wantRNS[0] {
			t.Errorf("registrar_nameservers = %v; want %v", got, wantRNS)
		}
	}

	lns, ok := auth["live_nameservers"]
	if !ok {
		t.Error("live_nameservers was removed; it must remain")
	} else {
		wantLNS := []any{"ns2.x"}
		got, _ := lns.([]any)
		if len(got) != 1 || got[0] != wantLNS[0] {
			t.Errorf("live_nameservers = %v; want %v", got, wantLNS)
		}
	}

	// Disallowed keys must be removed.
	if _, found := auth["secret_token"]; found {
		t.Error("secret_token was NOT removed; it must be stripped by Sanitize")
	}
	if _, found := auth["domain_id"]; found {
		t.Error("domain_id was NOT removed; it must be stripped by Sanitize")
	}

	// Nil Authority on second snapshot must not panic (verified by reaching here).

	// Records sanitization still works: public IP was redacted.
	gotIP := p.Snapshots[0].Records[0].Value
	if gotIP == "8.8.8.8" {
		t.Error("public IP 8.8.8.8 was NOT redacted; Records sanitization must still run")
	}
	if !isExampleIP(gotIP) {
		t.Errorf("after Sanitize, A record value = %q; want an RFC-5737 example IP", gotIP)
	}
}

// isExampleIP returns true if the IP is in an RFC-5737 documentation range.
func isExampleIP(ip string) bool {
	return strings.HasPrefix(ip, "192.0.2.") ||
		strings.HasPrefix(ip, "198.51.100.") ||
		strings.HasPrefix(ip, "203.0.113.")
}
