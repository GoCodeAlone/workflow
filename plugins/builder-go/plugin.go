// Package buildergo provides the built-in go builder plugin for wfctl build.
package buildergo

import "github.com/GoCodeAlone/workflow/plugin/builder"

func init() {
	builder.Register(New())
}
