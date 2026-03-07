package module

// CLICommandFunc is the type for CLI command implementations. It matches the
// signature used by wfctl command handlers (func(args []string) error).
type CLICommandFunc func(args []string) error

// CLICommandRegistryServiceName is the well-known service name used to register
// CLICommandRegistry in the application service registry so that step.cli_invoke
// can locate it at execution time.
const CLICommandRegistryServiceName = "cliCommandRegistry"

// CLICommandRegistry is a shared service that maps CLI command names to their
// Go function implementations. It is registered in the app under the name
// CLICommandRegistryServiceName before BuildFromConfig is called so that
// step.cli_invoke can look up and invoke the correct function at execution time.
//
// Usage in cmd/wfctl/main.go:
//
//	registry := module.NewCLICommandRegistry()
//	for name, fn := range commands {
//	    registry.Register(name, module.CLICommandFunc(fn))
//	}
//	engineInst.App().RegisterService(module.CLICommandRegistryServiceName, registry)
type CLICommandRegistry struct {
	runners map[string]CLICommandFunc
}

// NewCLICommandRegistry creates a new empty CLICommandRegistry.
func NewCLICommandRegistry() *CLICommandRegistry {
	return &CLICommandRegistry{runners: make(map[string]CLICommandFunc)}
}

// Register adds or replaces the function for the named command.
func (r *CLICommandRegistry) Register(name string, fn CLICommandFunc) {
	r.runners[name] = fn
}

// Get returns the function registered for name, if any.
func (r *CLICommandRegistry) Get(name string) (CLICommandFunc, bool) {
	fn, ok := r.runners[name]
	return fn, ok
}
