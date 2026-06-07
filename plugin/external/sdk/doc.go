// Package sdk is the public runtime SDK for out-of-process Workflow plugins.
//
// Plugin binaries implement PluginProvider plus optional provider interfaces
// such as StepProvider, TypedStepProvider, ModuleProvider, ContractProvider, or
// CLIProvider. A typical main function constructs the provider and calls Serve:
//
//	func main() {
//		sdk.Serve(internal.NewProvider(),
//			sdk.WithBuildVersion(sdk.ResolveBuildVersion(internal.Version)),
//		)
//	}
//
// Plugins that also expose CLI commands or build hooks can use ServePluginFull.
// IaC provider plugins should use ServeIaCPlugin so typed IaC gRPC services are
// registered and advertised consistently.
package sdk
