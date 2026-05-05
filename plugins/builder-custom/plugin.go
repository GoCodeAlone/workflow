// Package buildercustom provides the built-in custom builder plugin for wfctl build.
package buildercustom

import "github.com/GoCodeAlone/workflow/plugin/builder"

func init() {
	builder.Register(New())
}
