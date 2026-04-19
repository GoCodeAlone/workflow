// Package buildernodejs provides the built-in nodejs builder plugin for wfctl build.
package buildernodejs

import "github.com/GoCodeAlone/workflow/plugin/builder"

func init() {
	builder.Register(New())
}
