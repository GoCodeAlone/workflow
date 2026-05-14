package plugin

import proto "github.com/GoCodeAlone/workflow/plugin/external/proto"

// IaCStateBackendProvider is the optional interface an external-plugin adapter
// implements when its plugin serves one or more iac.state backends. The engine
// type-asserts loaded plugins against it (same pattern as stepRegistrySetter)
// and populates module's iac.state backend registry.
type IaCStateBackendProvider interface {
	IaCStateBackendClients() (map[string]proto.IaCStateBackendClient, error)
}
