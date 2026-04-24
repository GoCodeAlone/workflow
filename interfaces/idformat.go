package interfaces

import "strings"

// ValidateProviderID returns true when s is a valid provider ID for the given
// format. Unknown and unrecognized formats always return true (forward compat).
// Freeform requires a non-empty string. Specific formats delegate to their
// dedicated validators.
func ValidateProviderID(s string, format ProviderIDFormat) bool {
	switch format {
	case IDFormatUUID:
		return validateUUID(s)
	case IDFormatDomainName:
		return validateDomainName(s)
	case IDFormatARN:
		return validateARN(s)
	case IDFormatFreeform:
		return s != ""
	default:
		// IDFormatUnknown and any future formats pass through (forward compat).
		return true
	}
}

// validateUUID returns true when s is a canonical UUID in the form
// xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx (8-4-4-4-12 hex digits, case-insensitive).
// No regex — purely positional so it allocates nothing.
func validateUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	// Hyphens must be at positions 8, 13, 18, 23.
	if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !isHex(byte(c)) {
			return false
		}
	}
	return true
}

// isHex returns true when b is a valid hexadecimal digit (0-9, a-f, A-F).
func isHex(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

// validateDomainName returns true when s is a syntactically valid DNS name
// following RFC 1035 relaxed rules:
//   - Total length ≤ 253 characters (excluding any trailing dot).
//   - Each label is 1–63 characters.
//   - Label characters: [a-zA-Z0-9-]; no leading or trailing hyphen.
//   - Labels must not be empty (consecutive dots are rejected).
//   - A single trailing dot is allowed.
func validateDomainName(s string) bool {
	if s == "" {
		return false
	}
	// Strip single trailing dot (FQDN notation).
	fqdn := s
	if len(fqdn) > 0 && fqdn[len(fqdn)-1] == '.' {
		fqdn = fqdn[:len(fqdn)-1]
	}
	if len(fqdn) > 253 {
		return false
	}
	labels := strings.Split(fqdn, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
	}
	return true
}

// validateARN returns true when s is a syntactically valid AWS ARN:
//
//	arn:<partition>:<service>:<region>:<account>:<resource>
//
// Exactly 6 colon-separated segments; the first must be "arn", partition and
// service must be non-empty. Region and account may be empty (e.g. global
// services). Resource may contain additional colons.
func validateARN(s string) bool {
	if !strings.HasPrefix(s, "arn:") {
		return false
	}
	// SplitN(..., 6) gives us [arn, partition, service, region, account, resource].
	// The resource segment may itself contain colons, which is fine.
	parts := strings.SplitN(s, ":", 6)
	if len(parts) < 6 {
		return false
	}
	// parts[0] == "arn" guaranteed by HasPrefix check above.
	partition := parts[1]
	service := parts[2]
	if partition == "" || service == "" {
		return false
	}
	return true
}
