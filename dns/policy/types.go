package policy

type Policy struct {
	Zone    string
	Entries []Entry
}

type Entry struct {
	Owner    string
	Patterns []string
	Types    []string
	Default  bool
}
