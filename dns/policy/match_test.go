package policy

import "testing"

func TestMatchPattern(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"www", "www", true},
		{"www", "admin", false},
		{"@", "@", true},
		{"@", "www", false},
		{"*", "www", true},
		{"*", "anything", true},
		{"*", "", false},        // empty name → false (closes plan-cycle-1 m-1)
		{"*", "www.sub", false}, // * = single label only
		{"_acme-challenge.*", "_acme-challenge.www", true},
		{"_acme-challenge.*", "_acme-challenge.www.sub", false}, // * is single
		{"**", "anything.multi.label", true},                    // ** spans
		{"**", "single", true},
		{"a.**", "a.b.c", true},
		{"a.**", "b.c", false},
		{"tour.*", "tour.bandname", true},
		{"tour.*", "other.bandname", false},
	}
	for _, c := range cases {
		got := MatchPattern(c.pattern, c.name)
		if got != c.want {
			t.Errorf("MatchPattern(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}
