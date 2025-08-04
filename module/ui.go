package module

import (
	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/ui"
)

// NewUIModule creates a new UI module
func NewUIModule(name string, config map[string]interface{}) modular.Module {
	return ui.NewUIModule(name, config)
}