// Fixture for wfctl plugin validate-contract good case (workflow#758).
// NOT compiled — fixture only; lives under testdata/.
package main

import (
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
	"github.com/example/internal"
)

func main() {
	sdk.ServeIaCPlugin(internal.NewIaCServer(), sdk.IaCServeOptions{
		BuildVersion: sdk.ResolveBuildVersion(internal.Version),
	})
}
