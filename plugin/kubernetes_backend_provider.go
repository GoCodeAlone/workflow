package plugin

import proto "github.com/GoCodeAlone/workflow/plugin/external/proto"

// KubernetesBackendProvider is the optional interface an external-plugin adapter
// implements when its plugin serves one or more platform.kubernetes cluster-type
// backends. The engine type-asserts loaded plugins against it (same
// pattern as IaCStateBackendProvider) and populates module's kubernetes backend
// registry.
//
// Per ADR 0037 a kubernetes backend is served over the existing ResourceDriver
// contract — no new proto surface — so the returned clients are
// proto.ResourceDriverClient values keyed by cluster type name.
type KubernetesBackendProvider interface {
	KubernetesBackendClients() (map[string]proto.ResourceDriverClient, error)
}
