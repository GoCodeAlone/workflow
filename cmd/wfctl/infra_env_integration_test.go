package main

import "testing"

func TestInfraCommands_AllHonorEnvFlag(t *testing.T) {
	cmds := []string{"plan", "apply", "status", "drift", "bootstrap", "destroy", "import"}
	for _, cmd := range cmds {
		t.Run(cmd, func(t *testing.T) {
			fs := newInfraFlagSet(cmd)
			if fs.Lookup("env") == nil {
				t.Fatalf("%s is missing --env flag", cmd)
			}
		})
	}
}
