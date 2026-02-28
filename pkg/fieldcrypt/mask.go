package fieldcrypt

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// MaskValue applies masking based on LogBehavior and optional pattern.
func MaskValue(value string, behavior LogBehavior, pattern string) string {
	switch behavior {
	case LogRedact:
		return RedactValue()
	case LogHash:
		return HashValue(value)
	case LogAllow:
		return value
	case LogMask:
		if pattern != "" {
			return applyPattern(value, pattern)
		}
		// Auto-detect: try email first, then phone-like, then generic mask.
		if strings.Contains(value, "@") {
			return MaskEmail(value)
		}
		if looksLikePhone(value) {
			return MaskPhone(value)
		}
		return genericMask(value)
	default:
		return RedactValue()
	}
}

// MaskEmail masks an email: "j***@e***.com".
func MaskEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return genericMask(email)
	}
	local := email[:at]
	domain := email[at+1:]

	maskedLocal := maskPart(local)
	// Mask domain but keep TLD.
	dot := strings.LastIndex(domain, ".")
	if dot > 0 {
		maskedDomain := maskPart(domain[:dot]) + domain[dot:]
		return maskedLocal + "@" + maskedDomain
	}
	return maskedLocal + "@" + maskPart(domain)
}

// MaskPhone masks all but last 4 digits: "***-***-1234".
func MaskPhone(phone string) string {
	digits := extractDigits(phone)
	if len(digits) <= 4 {
		return "****"
	}
	last4 := string(digits[len(digits)-4:])
	return "***-***-" + last4
}

// HashValue returns SHA256 hex of the value.
func HashValue(value string) string {
	h := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", h)
}

// RedactValue returns "[REDACTED]".
func RedactValue() string {
	return "[REDACTED]"
}

// maskPart keeps first char and replaces rest with ***.
func maskPart(s string) string {
	if len(s) == 0 {
		return ""
	}
	return string(s[0]) + "***"
}

func genericMask(s string) string {
	if len(s) <= 1 {
		return "***"
	}
	return string(s[0]) + "***"
}

func looksLikePhone(s string) bool {
	digits := extractDigits(s)
	return len(digits) >= 7 && len(digits) <= 15
}

func extractDigits(s string) []byte {
	var digits []byte
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			digits = append(digits, s[i])
		}
	}
	return digits
}

func applyPattern(value, pattern string) string {
	digits := extractDigits(value)
	di := 0
	var result []byte
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '#' {
			if di < len(digits) {
				result = append(result, digits[di])
				di++
			} else {
				result = append(result, '#')
			}
		} else {
			result = append(result, pattern[i])
		}
	}
	return string(result)
}
