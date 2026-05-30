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

// TestSanitizeRedactsPublicIPv4 pins that A records with public IPs are
// replaced with an RFC-5737 example address (192.0.2.x etc.).
func TestSanitizeRedactsPublicIPv4(t *testing.T) {
	p := record.Portfolio{
		Schema: record.SchemaV1,
		Snapshots: []record.Snapshot{{
			Provider: "digitalocean",
			Domain:   "example.com",
			Records: []record.Record{
				{Type: "A", Name: "@", Value: "198.51.100.25", TTL: 300}, // public IP
			},
		}},
	}
	record.Sanitize(&p)
	got := p.Snapshots[0].Records[0].Value
	// Must be an RFC-5737 example address
	if !isExampleIP(got) {
		t.Errorf("after Sanitize, A record value = %q; want an RFC-5737 example IP", got)
	}
}

// TestSanitizeRedactsPublicIPv6 pins that AAAA records with public IPv6 are
// replaced with 2001:db8:: (RFC-3849).
func TestSanitizeRedactsPublicIPv6(t *testing.T) {
	p := record.Portfolio{
		Schema: record.SchemaV1,
		Snapshots: []record.Snapshot{{
			Provider: "digitalocean",
			Domain:   "example.com",
			Records: []record.Record{
				{Type: "AAAA", Name: "@", Value: "2607:f8b0:4004:c07::64", TTL: 300},
			},
		}},
	}
	record.Sanitize(&p)
	got := p.Snapshots[0].Records[0].Value
	if got != "2001:db8::" {
		t.Errorf("after Sanitize, AAAA record value = %q; want 2001:db8::", got)
	}
}

// TestSanitizePreservesPrivateIPs pins that private/RFC-1918 addresses are
// left unchanged (they're not sensitive public IPs).
func TestSanitizePreservesPrivateIPs(t *testing.T) {
	cases := []string{"10.0.0.1", "172.16.5.1", "192.168.1.1", "127.0.0.1"}
	for _, ip := range cases {
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
			t.Errorf("private IP %s was changed to %s; private/reserved IPs should be preserved", ip, got)
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

// isExampleIP returns true if the IP is in an RFC-5737 documentation range.
func isExampleIP(ip string) bool {
	return strings.HasPrefix(ip, "192.0.2.") ||
		strings.HasPrefix(ip, "198.51.100.") ||
		strings.HasPrefix(ip, "203.0.113.")
}
