//go:build scenario_stub

package all

import pluginstub "github.com/GoCodeAlone/workflow/plugins/stubprovider"

func init() {
	scenarioExtras = append(scenarioExtras, pluginstub.New())
}
