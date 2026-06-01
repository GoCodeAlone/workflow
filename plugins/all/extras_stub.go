//go:build scenario_stub

package all

import (
	pluginlocalauthz "github.com/GoCodeAlone/workflow/plugins/localauthz"
	pluginstub "github.com/GoCodeAlone/workflow/plugins/stubprovider"
)

func init() {
	scenarioExtras = append(scenarioExtras,
		pluginstub.New(),
		pluginlocalauthz.New(),
	)
}
