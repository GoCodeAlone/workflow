package actors

import "sync"

const grainPoolRegistryID = "workflow-grain-pools"

// grainPoolRegistry keeps runtime services local to the actor system. GrainOf
// constructs zero-value grains, which resolve their pool during activation.
type grainPoolRegistry struct {
	pools sync.Map
}

func (*grainPoolRegistry) ID() string { return grainPoolRegistryID }

// grainPoolReference carries only the pool name through grain recreation;
// registries, applications and loggers are resolved on the owning system.
type grainPoolReference struct {
	poolName string
}

func (*grainPoolReference) ID() string { return "workflow-grain-pool" }

func (r *grainPoolReference) MarshalBinary() ([]byte, error) {
	return []byte(r.poolName), nil
}

func (r *grainPoolReference) UnmarshalBinary(data []byte) error {
	r.poolName = string(data)
	return nil
}
