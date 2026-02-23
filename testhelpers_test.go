package workflow

import (
	"testing"

	"github.com/GoCodeAlone/workflow/plugin"
	pluginai "github.com/GoCodeAlone/workflow/plugins/ai"
	pluginapi "github.com/GoCodeAlone/workflow/plugins/api"
	pluginauth "github.com/GoCodeAlone/workflow/plugins/auth"
	plugincicd "github.com/GoCodeAlone/workflow/plugins/cicd"
	pluginff "github.com/GoCodeAlone/workflow/plugins/featureflags"
	pluginhttp "github.com/GoCodeAlone/workflow/plugins/http"
	pluginintegration "github.com/GoCodeAlone/workflow/plugins/integration"
	pluginlicense "github.com/GoCodeAlone/workflow/plugins/license"
	pluginmessaging "github.com/GoCodeAlone/workflow/plugins/messaging"
	pluginopenapi "github.com/GoCodeAlone/workflow/plugins/openapi"
	pluginmodcompat "github.com/GoCodeAlone/workflow/plugins/modularcompat"
	pluginobs "github.com/GoCodeAlone/workflow/plugins/observability"
	pluginpipeline "github.com/GoCodeAlone/workflow/plugins/pipelinesteps"
	pluginplatform "github.com/GoCodeAlone/workflow/plugins/platform"
	pluginscheduler "github.com/GoCodeAlone/workflow/plugins/scheduler"
	pluginsecrets "github.com/GoCodeAlone/workflow/plugins/secrets"
	pluginsm "github.com/GoCodeAlone/workflow/plugins/statemachine"
	pluginstorage "github.com/GoCodeAlone/workflow/plugins/storage"
)

// allPlugins returns all engine plugins for test use.
func allPlugins() []plugin.EnginePlugin {
	return []plugin.EnginePlugin{
		pluginhttp.New(),
		pluginobs.New(),
		pluginmessaging.New(),
		pluginsm.New(),
		pluginauth.New(),
		pluginstorage.New(),
		pluginapi.New(),
		pluginpipeline.New(),
		plugincicd.New(),
		pluginff.New(),
		pluginsecrets.New(),
		pluginmodcompat.New(),
		pluginscheduler.New(),
		pluginintegration.New(),
		pluginai.New(),
		pluginplatform.New(),
		pluginlicense.New(),
		pluginopenapi.New(),
	}
}

// loadAllPlugins loads all 15 plugins into an engine, failing the test on error.
func loadAllPlugins(t *testing.T, engine *StdEngine) {
	t.Helper()
	for _, p := range allPlugins() {
		if err := engine.LoadPlugin(p); err != nil {
			t.Fatalf("LoadPlugin(%s) failed: %v", p.Name(), err)
		}
	}
}
