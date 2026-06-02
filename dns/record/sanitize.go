package record

import (
	"net"
	"strings"
)

// Sanitize replaces sensitive data in p in-place so the portfolio can be
// committed to a public repository:
//
//   - A/AAAA record values that are public (routable) IPs are replaced with
//     RFC-5737 (192.0.2.x/198.51.100.x/203.0.113.x) or RFC-3849
//     (2001:db8::) example ranges.
//   - TXT record data that looks like a secret (DKIM public key, long base64
//     blobs) is replaced with "[redacted]".
//   - _workflow-dns-policy TXT records (identified by record NAME and/or the
//     heritage=wfinfra-v1 value prefix) are left intact — they are policy
//     declarations, not secrets.
//   - Private/reserved IP ranges (RFC-1918, RFC-6598 CGNAT, loopback,
//     link-local, IPv6 ULA, RFC-5737/3849 documentation) are left as-is.
//
// authorityAllowList is the exhaustive set of keys permitted in
// Snapshot.Authority after sanitization. Any key not in this set is removed.
var authorityAllowList = map[string]bool{
	"registrar_nameservers": true,
	"live_nameservers":      true,
}

// Sanitize sets p.Sanitized = true.
func Sanitize(p *Portfolio) {
	for si := range p.Snapshots {
		for ri := range p.Snapshots[si].Records {
			r := &p.Snapshots[si].Records[ri]
			switch r.Type {
			case "A":
				if isPublicIPv4(r.Value) {
					r.Value = exampleIPv4(si, ri)
				}
			case "AAAA":
				if isPublicIPv6(r.Value) {
					r.Value = "2001:db8::"
				}
			case "TXT":
				if isWFInfraPolicyTXT(r.Name, r.Value) {
					// Leave _workflow-dns-policy TXT intact.
					continue
				}
				if looksLikeSecret(r.Value) {
					r.Value = "[redacted]"
				}
			}
		}
		// Strip any Authority key not in the allow-list.
		for key := range p.Snapshots[si].Authority {
			if !authorityAllowList[key] {
				delete(p.Snapshots[si].Authority, key)
			}
		}
	}
	p.Sanitized = true
}

// isWFInfraPolicyTXT reports whether a TXT record is the wfinfra-v1 ownership
// policy record. These must never be redacted — they are policy declarations,
// not secrets. M-2: the record NAME (_workflow-dns-policy) is the authoritative
// identifier; the heritage= value prefix is a secondary signal.
func isWFInfraPolicyTXT(name, value string) bool {
	if strings.HasPrefix(name, "_workflow-dns-policy") {
		return true
	}
	return strings.HasPrefix(value, "heritage=wfinfra")
}

// looksLikeSecret returns true for TXT values that are likely secrets:
// DKIM public-key records or long base64-like blobs.
//
// C-2: a `p=` substring alone is NOT enough — legitimate DMARC records carry
// `p=reject|quarantine|none` policy keywords and can exceed 80 chars. We only
// treat a value as a DKIM secret when it self-identifies as DKIM (v=DKIM1) AND
// carries a long base64-ish `p=` blob, or when the whole value is a long
// base64 blob.
func looksLikeSecret(v string) bool {
	// DKIM key record: explicit v=DKIM1 tag + a long base64-ish public key.
	if strings.Contains(v, "v=DKIM1") && hasLongBase64PField(v) {
		return true
	}
	// Any TXT value that is mostly base64 and long is treated as a secret.
	if len(v) > 100 && isBase64Like(v) {
		return true
	}
	return false
}

// hasLongBase64PField reports whether the value contains a `p=` field whose
// blob is a long base64-ish key (>= 32 chars of base64 alphabet). This
// distinguishes a DKIM public key (`p=MIIB...long...`) from a DMARC policy
// keyword (`p=reject`).
func hasLongBase64PField(v string) bool {
	idx := strings.Index(v, "p=")
	if idx < 0 {
		return false
	}
	blob := v[idx+2:]
	// Trim leading whitespace.
	blob = strings.TrimLeft(blob, " \t")
	// Read the base64-ish run (byte-wise; base64 alphabet is all ASCII).
	n := 0
	for i := 0; i < len(blob); i++ {
		if !isBase64Char(blob[i]) {
			break
		}
		n++
	}
	return n >= 32
}

// exampleIPv4 returns an RFC-5737 example IP, varied by snapshot and record
// index so multiple sanitized records get distinct addresses.
func exampleIPv4(snapIdx, recIdx int) string {
	// RFC-5737 blocks: 192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24.
	// Cycle through octets 1-254 using (snapIdx*16 + recIdx + 1) % 254.
	octet := (snapIdx*16+recIdx+1)%254 + 1
	switch snapIdx % 3 {
	case 0:
		return "192.0.2." + itoa(octet)
	case 1:
		return "198.51.100." + itoa(octet)
	default:
		return "203.0.113." + itoa(octet)
	}
}

// isPublicIPv4 reports whether v is a routable IPv4 address that should be
// redacted. I-2: uses net.ParseIP + the stdlib special-range predicates and
// explicitly excludes RFC-5737 (documentation), RFC-6598 (CGNAT 100.64/10),
// and the all-zeros/broadcast addresses.
func isPublicIPv4(v string) bool {
	ip := net.ParseIP(v)
	if ip == nil {
		return false
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return false // not an IPv4 address
	}
	if ip4.IsPrivate() || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() ||
		ip4.IsLinkLocalMulticast() || ip4.IsMulticast() || ip4.IsUnspecified() {
		return false
	}
	// RFC-5737 documentation ranges — already example IPs.
	if inRFC5737(ip4) {
		return false
	}
	// RFC-6598 CGNAT: 100.64.0.0/10 (100.64.0.0 – 100.127.255.255).
	if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
		return false
	}
	// Limited broadcast.
	if ip4[0] == 255 && ip4[1] == 255 && ip4[2] == 255 && ip4[3] == 255 {
		return false
	}
	return true
}

func inRFC5737(ip4 net.IP) bool {
	switch {
	case ip4[0] == 192 && ip4[1] == 0 && ip4[2] == 2: // 192.0.2.0/24
		return true
	case ip4[0] == 198 && ip4[1] == 51 && ip4[2] == 100: // 198.51.100.0/24
		return true
	case ip4[0] == 203 && ip4[1] == 0 && ip4[2] == 113: // 203.0.113.0/24
		return true
	}
	return false
}

// rfc3849 is the IPv6 documentation prefix 2001:db8::/32 (RFC-3849).
var rfc3849Net = func() *net.IPNet {
	_, n, _ := net.ParseCIDR("2001:db8::/32")
	return n
}()

// isPublicIPv6 reports whether v is a routable IPv6 address that should be
// redacted. I-2: uses net.ParseIP; redacts only a global-unicast,
// non-private address that is NOT in the RFC-3849 documentation range.
// ULA (fc00::/7) is IsPrivate; loopback/link-local are excluded too.
func isPublicIPv6(v string) bool {
	ip := net.ParseIP(v)
	if ip == nil {
		return false
	}
	if ip.To4() != nil {
		return false // an IPv4 address, not IPv6
	}
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}
	// Already an RFC-3849 documentation address.
	if rfc3849Net != nil && rfc3849Net.Contains(ip) {
		return false
	}
	return true
}

// ── small helpers ─────────────────────────────────────────────────────────

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}

func isBase64Char(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') || c == '+' || c == '/' || c == '='
}

func isBase64Like(s string) bool {
	if len(s) == 0 {
		return false
	}
	// Returns true if the string consists mostly of base64 characters
	// with at most 5% non-base64 chars.
	base64Chars := 0
	for i := 0; i < len(s); i++ {
		if isBase64Char(s[i]) {
			base64Chars++
		}
	}
	return base64Chars*100/len(s) >= 95
}
