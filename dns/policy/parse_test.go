package policy

import (
	"errors"
	"strings"
	"testing"
)

func TestParse_HappyPath(t *testing.T) {
	rrs := []string{
		`heritage=wfinfra-v1 o=sre d=true`,
		`heritage=wfinfra-v1 o=multisite p=www,admin,_acme-challenge.www`,
	}
	p, err := Parse("gocodealone.tech", rrs)
	if err != nil {
		t.Fatal(err)
	}
	if p.Zone != "gocodealone.tech" {
		t.Errorf("zone=%q", p.Zone)
	}
	if len(p.Entries) != 2 {
		t.Fatalf("entries=%d want 2", len(p.Entries))
	}
}

func TestParse_IgnoresUnknownHeritage(t *testing.T) {
	rrs := []string{
		`heritage=wfinfra-v1 o=sre d=true`,
		`v=spf1 -all`,                       // SPF — ignored
		`heritage=wfinfra-v999 o=alien p=*`, // future schema — ignored
	}
	p, err := Parse("z", rrs)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Entries) != 1 {
		t.Errorf("entries=%d want 1", len(p.Entries))
	}
}

func TestParse_MultipleDefaults(t *testing.T) {
	rrs := []string{
		`heritage=wfinfra-v1 o=sre d=true`,
		`heritage=wfinfra-v1 o=multisite d=true p=www`,
	}
	_, err := Parse("z", rrs)
	if !errors.Is(err, ErrMultipleDefaults) {
		t.Errorf("want ErrMultipleDefaults, got %v", err)
	}
}

func TestParse_EmptyOwner(t *testing.T) {
	rrs := []string{`heritage=wfinfra-v1 o= p=www`}
	_, err := Parse("z", rrs)
	if !errors.Is(err, ErrEmptyOwner) {
		t.Errorf("want ErrEmptyOwner, got %v", err)
	}
}

func TestSerialize_DeterministicSort(t *testing.T) {
	p := &Policy{
		Zone: "z",
		Entries: []Entry{
			{Owner: "multisite", Patterns: []string{"www", "admin", "_acme-challenge.www"}},
			{Owner: "sre", Default: true},
		},
	}
	out1, err := Serialize(p)
	if err != nil {
		t.Fatal(err)
	}
	out2, _ := Serialize(p)
	if strings.Join(out1, "\n") != strings.Join(out2, "\n") {
		t.Errorf("serialize not deterministic")
	}
	// patterns within entry sorted alphabetically
	found := false
	for _, rr := range out1 {
		if strings.Contains(rr, "o=multisite") {
			if !strings.Contains(rr, "p=_acme-challenge.www,admin,www") {
				t.Errorf("patterns not sorted within entry: %s", rr)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("multisite RR missing from %v", out1)
	}
}

func TestSerialize_MultipleDefaultsRejected(t *testing.T) {
	p := &Policy{Zone: "z", Entries: []Entry{{Owner: "a", Default: true}, {Owner: "b", Default: true}}}
	_, err := Serialize(p)
	if !errors.Is(err, ErrMultipleDefaults) {
		t.Errorf("Serialize should refuse multiple defaults, got %v", err)
	}
}

func TestParseSerialize_RoundTrip(t *testing.T) {
	rrs := []string{
		`heritage=wfinfra-v1 o=sre d=true`,
		`heritage=wfinfra-v1 o=multisite p=admin,www`,
	}
	p1, _ := Parse("z", rrs)
	out1, _ := Serialize(p1)
	p2, _ := Parse("z", out1)
	out2, _ := Serialize(p2)
	if strings.Join(out1, "\n") != strings.Join(out2, "\n") {
		t.Errorf("Parse(Serialize(p)) not idempotent\nout1=%v\nout2=%v", out1, out2)
	}
}
