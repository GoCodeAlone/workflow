package module

import (
	"github.com/GoCodeAlone/modular"
)

// InitModule initializes the module and registers any necessary components
func InitModule() {
	// Register any global module components or setup code here
}

// AdaptApp wraps an application with our adapter
func AdaptApp(app modular.Application) *AppAdapter {
	return NewAppAdapter(app)
}
