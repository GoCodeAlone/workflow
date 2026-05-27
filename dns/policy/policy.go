package policy

import "fmt"

var protectedTypes = map[string]bool{"SOA": true, "NS": true}

// CheckAllowed returns nil if owner may upsert (name, recordType) under this policy.
// Returns an error describing the denial otherwise.
//
// Priority semantics (closes plan-cycle-1 C-3):
//  1. Explicit pattern claims take precedence over default-owner fallback.
//  2. If any owner (including non-caller) has an explicit pattern matching
//     (name, recordType), only that owner may mutate.
//  3. Default owner catches only unmatched records.
//  4. SOA/NS protected unless explicitly listed in the owner's Types.
func (p *Policy) CheckAllowed(name, recordType, owner string) error {
	// Phase 1: find any explicit pattern claim (any owner) — explicit beats default
	var explicitClaimer string
	for _, e := range p.Entries {
		if e.Default && len(e.Patterns) == 0 {
			continue // skip pure default-only entries in phase 1
		}
		if matchesEntry(e, name, recordType) {
			explicitClaimer = e.Owner
			if e.Owner == owner {
				if protectedTypes[recordType] && !isProtectedAllowed(e, recordType) {
					return fmt.Errorf("dnspolicy: record type %s never delegated (zone-level only)", recordType)
				}
				return nil // explicit claim by caller → allow
			}
		}
	}
	if explicitClaimer != "" {
		return fmt.Errorf("dnspolicy: denied — name=%q type=%s owner=%q; explicitly claimed by owner=%q", name, recordType, owner, explicitClaimer)
	}
	// Phase 2: no explicit claim exists → fall back to default owner if caller is default.
	// (Closes plan-cycle-2 I-3) — also apply Types restriction here; non-empty e.Types restricts the default owner too.
	for _, e := range p.Entries {
		if e.Default && e.Owner == owner {
			// Types restriction: empty = all-types-except-protected; non-empty = exact list
			if len(e.Types) > 0 {
				ok := false
				for _, t := range e.Types {
					if t == recordType {
						ok = true
						break
					}
				}
				if !ok {
					return fmt.Errorf("dnspolicy: denied — name=%q type=%s owner=%q; default owner restricted to types %v", name, recordType, owner, e.Types)
				}
			}
			if protectedTypes[recordType] && !isProtectedAllowed(e, recordType) {
				return fmt.Errorf("dnspolicy: record type %s never delegated (zone-level only)", recordType)
			}
			return nil
		}
	}
	// Phase 3: no match anywhere → fail-closed
	return fmt.Errorf("dnspolicy: denied — name=%q type=%s owner=%q matches no delegate and no default owner exists for this caller", name, recordType, owner)
}

// matchesEntry returns true if entry's patterns + types cover (name, recordType).
// Does NOT consider e.Default — that's caller's job (see CheckAllowed phase 1 skip).
func matchesEntry(e Entry, name, recordType string) bool {
	// Type scoping
	if len(e.Types) > 0 {
		ok := false
		for _, t := range e.Types {
			if t == recordType {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	// Pattern match (default-only entries with no patterns are handled by caller)
	for _, pat := range e.Patterns {
		if MatchPattern(pat, name) {
			return true
		}
	}
	return false
}

func isProtectedAllowed(e Entry, recordType string) bool {
	for _, t := range e.Types {
		if t == recordType {
			return true
		}
	}
	return false
}
