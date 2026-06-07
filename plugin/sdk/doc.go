// Package sdk contains authoring tools for Workflow plugins.
//
// The package is used by wfctl plugin init and by tests that need to generate
// realistic plugin repositories. TemplateGenerator creates a buildable plugin
// project with plugin.json, Go code, release workflows, GoReleaser metadata,
// and contract descriptor files. Runtime plugin binaries should use
// github.com/GoCodeAlone/workflow/plugin/external/sdk.
package sdk
