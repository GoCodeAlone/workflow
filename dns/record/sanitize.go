package record

// Sanitize replaces sensitive data in p in-place so the portfolio can be
// committed to a public repository:
//
//   - A/AAAA record values that are public (routable) IPs are replaced with
//     RFC-5737 (192.0.2.x/198.51.100.x/203.0.113.x) or RFC-3849
//     (2001:db8::) example ranges.
//   - TXT record data that looks like a secret (DKIM p= base64, long base64
//     strings) is replaced with "[redacted]".
//   - _workflow-dns-policy TXT records (heritage=wfinfra-v1) are left intact
//     because they are policy declarations, not secrets.
//   - Private/reserved IP ranges (RFC-1918, loopback, link-local) are left
//     as-is; they are not public.
//
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
				if isWFInfraPolicyTXT(r.Value) {
					// Leave _workflow-dns-policy TXT intact.
					continue
				}
				if looksLikeSecret(r.Value) {
					r.Value = "[redacted]"
				}
			}
		}
	}
	p.Sanitized = true
}

// isWFInfraPolicyTXT reports whether a TXT value is the wfinfra-v1 policy record.
// These must never be redacted — they are policy declarations, not secrets.
func isWFInfraPolicyTXT(v string) bool {
	return len(v) >= 16 && v[:16] == "heritage=wfinfra"
}

// looksLikeSecret returns true for TXT values that are likely secrets:
// DKIM public-key records (contain "p=") or long base64-like strings.
func looksLikeSecret(v string) bool {
	// DKIM key record: contains "p=" followed by a long base64 blob.
	if containsSubstring(v, "p=") && len(v) > 80 {
		return true
	}
	// Any TXT value that is entirely base64-like and long is treated as a secret.
	if len(v) > 100 && isBase64Like(v) {
		return true
	}
	return false
}

// exampleIPv4 returns an RFC-5737 example IP, varied by snapshot and record
// index so multiple sanitized records get distinct addresses.
func exampleIPv4(snapIdx, recIdx int) string {
	// RFC-5737 blocks: 192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24
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

// isPublicIPv4 returns true when v is a dotted-decimal IPv4 address that is
// NOT in a private/reserved range (RFC-1918, loopback, link-local, RFC-5737).
func isPublicIPv4(v string) bool {
	parts := splitIPv4(v)
	if len(parts) != 4 {
		return false
	}
	a, b := parts[0], parts[1]
	// Already an example IP (RFC-5737): leave it alone.
	if a == 192 && b == 0 && parts[2] == 2 {
		return false
	}
	if a == 198 && b == 51 && parts[2] == 100 {
		return false
	}
	if a == 203 && b == 0 && parts[2] == 113 {
		return false
	}
	// Private ranges: 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, loopback,
	// link-local (169.254.0.0/16), RFC-3927, broadcast (255.255.255.255).
	if a == 10 {
		return false
	}
	if a == 172 && b >= 16 && b <= 31 {
		return false
	}
	if a == 192 && b == 168 {
		return false
	}
	if a == 127 {
		return false
	}
	if a == 169 && b == 254 {
		return false
	}
	if a == 0 {
		return false
	}
	return true
}

// isPublicIPv6 returns true for a global-unicast IPv6 address that is not
// already in the RFC-3849 documentation range (2001:db8::/32).
func isPublicIPv6(v string) bool {
	if len(v) == 0 {
		return false
	}
	// Already a documentation address.
	if len(v) >= 7 && v[:7] == "2001:db" {
		return false
	}
	// Loopback (::1) and link-local (fe80::) are not public.
	if v == "::1" || (len(v) >= 4 && (v[:4] == "fe80" || v[:4] == "FE80")) {
		return false
	}
	// Treat any other colon-containing string as a public IPv6 address.
	for _, c := range v {
		if c == ':' {
			return true
		}
	}
	return false
}

// ── small helpers (no imports needed) ────────────────────────────────────────

func splitIPv4(s string) []int {
	var parts []int
	cur := 0
	hasCur := false
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '.' {
			if hasCur {
				parts = append(parts, cur)
			}
			cur = 0
			hasCur = false
		} else if s[i] >= '0' && s[i] <= '9' {
			cur = cur*10 + int(s[i]-'0')
			hasCur = true
		} else {
			return nil // not a valid dotted-decimal
		}
	}
	return parts
}

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

func containsSubstring(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func isBase64Like(s string) bool {
	// Returns true if the string consists mostly of base64 characters
	// (A-Z, a-z, 0-9, +, /, =) with at most 5% non-base64 chars.
	base64Chars := 0
	for _, c := range s {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '+' || c == '/' || c == '=' {
			base64Chars++
		}
	}
	return base64Chars*100/len(s) >= 95
}
