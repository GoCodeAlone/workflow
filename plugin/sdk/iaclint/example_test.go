package iaclint_test

import (
	"github.com/GoCodeAlone/workflow/plugin/sdk/iaclint"
)

// Example shows a typical IaC plugin test importing iaclint to assert all
// three bug-class invariants on a single driver.
func Example() {
	// In a real plugin test:
	//
	//   driver := &MyFirewallDriver{client: mockClient}
	//   iaclint.AssertOutputsRoundTripStructpb(t, mustCreate(t, driver).Outputs)
	//   iaclint.AssertDiffPopulatesAllOutputFields(t, driver, sampleSpec)
	//   iaclint.AssertValidationMatrix(t, parsePort, "port", iaclint.KindTCPPort)
	//
	// See docs/IAC_PLUGIN_REVIEW_CHECKLIST.md for the bug-class taxonomy.
	_ = iaclint.KindTCPPort
	// Output:
}
