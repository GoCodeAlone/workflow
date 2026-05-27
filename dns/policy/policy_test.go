package policy

import (
	"strings"
	"testing"
)

func TestCheckAllowed(t *testing.T) {
	p := &Policy{Zone: "z", Entries: []Entry{
		{Owner: "sre", Default: true},
		{Owner: "multisite", Patterns: []string{"www", "admin", "tour.*"}, Types: []string{"A", "CNAME"}},
	}}

	cases := []struct {
		name, recordType, owner string
		wantErr                 bool
		errSub                  string
	}{
		{"www", "A", "multisite", false, ""},               // pattern + type match
		{"www", "A", "sre", true, "denied"},                // owner mismatch (sre is default)
		{"bandname", "A", "sre", false, ""},                // sre default catches unmatched
		{"bandname", "A", "multisite", true, "denied"},     // no pattern match
		{"www", "MX", "multisite", true, "type"},           // type not in list
		{"www", "MX", "sre", false, ""},                    // sre owns all types (no type restriction)
		{"tour.bandname", "CNAME", "multisite", false, ""}, // glob match
		{"www", "SOA", "sre", true, "SOA never delegated"}, // SOA always SRE
		{"www", "NS", "sre", true, "NS never delegated"},   // NS always SRE
	}
	for _, c := range cases {
		err := p.CheckAllowed(c.name, c.recordType, c.owner)
		if (err != nil) != c.wantErr {
			t.Errorf("CheckAllowed(%q,%q,%q) err=%v wantErr=%v", c.name, c.recordType, c.owner, err, c.wantErr)
		}
		if err != nil && c.errSub != "" && !strings.Contains(err.Error(), c.errSub) {
			t.Errorf("CheckAllowed(%q,%q,%q) err=%q want substring %q", c.name, c.recordType, c.owner, err, c.errSub)
		}
	}
}

func TestCheckAllowed_NoDefaultFailsClosed(t *testing.T) {
	p := &Policy{Zone: "z", Entries: []Entry{
		{Owner: "multisite", Patterns: []string{"www"}},
	}}
	err := p.CheckAllowed("bandname", "A", "anyone")
	if err == nil {
		t.Errorf("expected fail-closed denial for unmatched name with zero defaults")
	}
}
