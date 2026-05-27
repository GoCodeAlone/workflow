package module

import "time"

// defaultNowUnix is the production clock for the audit subsystem.
// Factored out so tests can override `nowUnix` (declared as a var
// in infra_admin.go) with a fixed-clock fake without pulling time
// into the test surface.
func defaultNowUnix() int64 { return time.Now().UTC().Unix() }
