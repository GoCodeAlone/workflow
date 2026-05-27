package policy

import "strings"

// MatchPattern returns true if name matches pattern.
// Pattern syntax:
//
//	"@"   matches the apex literal "@"
//	"*"   matches a SINGLE DNS label segment
//	"**"  matches one or more label segments
//	"<literal>.<rest>" matches recursively
//
// All matches are case-sensitive (DNS names are case-insensitive by spec
// but our pattern compare requires lowercase normalization at call sites).
func MatchPattern(pattern, name string) bool {
	if pattern == "@" {
		return name == "@"
	}
	if pattern == "**" {
		return true
	}
	if pattern == "*" {
		return !strings.Contains(name, ".") && name != ""
	}
	// Recursive: split on first dot
	pParts := strings.SplitN(pattern, ".", 2)
	nParts := strings.SplitN(name, ".", 2)
	head := pParts[0]
	// Head match: literal or single-* or **-spanning
	if head == "**" {
		// ** consumes anything from here
		return true
	}
	if head == "*" {
		if len(nParts) == 0 {
			return false
		}
		// * matches one label; require both have a tail OR both have no tail
		if len(pParts) == 1 { // pattern "*" alone (no dot) — handled above; safety
			return !strings.Contains(name, ".")
		}
		if len(nParts) == 1 {
			return false // pattern has tail, name doesn't
		}
		return MatchPattern(pParts[1], nParts[1])
	}
	// Literal head
	if len(nParts) == 0 || nParts[0] != head {
		return false
	}
	if len(pParts) == 1 { // pattern has no tail
		return len(nParts) == 1
	}
	if len(nParts) == 1 {
		return false // pattern has tail, name doesn't
	}
	return MatchPattern(pParts[1], nParts[1])
}
