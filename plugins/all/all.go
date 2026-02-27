// Package all provides a single import point for all built-in workflow engine plugins.
// Applications that embed the workflow engine can call [LoadAll] to register every
// standard plugin in one step instead of importing and wiring each plugin package
// individually.
//
// Example – minimal embedded engine setup:
//
//	engine := workflow.NewStdEngine(app, logger)
//	if err := all.LoadAll(engine); err != nil {
//	    log.Fatalf("failed to load plugins: %v", err)
//	}
//
// If you need finer control (e.g. to skip a plugin or add your own), use
// [DefaultPlugins] to obtain the slice and modify it before loading:
//
//	plugins := all.DefaultPlugins()
//	plugins = append(plugins, myCustomPlugin)
//	for _, p := range plugins {
//	    engine.LoadPlugin(p)
//	}
package all

import (
	"github.com/GoCodeAlone/workflow/plugin"
	pluginai "github.com/GoCodeAlone/workflow/plugins/ai"
	pluginapi "github.com/GoCodeAlone/workflow/plugins/api"
	pluginauth "github.com/GoCodeAlone/workflow/plugins/auth"
	plugincicd "github.com/GoCodeAlone/workflow/plugins/cicd"
	plugincloud "github.com/GoCodeAlone/workflow/plugins/cloud"
	plugindatastores "github.com/GoCodeAlone/workflow/plugins/datastores"
	plugindlq "github.com/GoCodeAlone/workflow/plugins/dlq"
	pluginevstore "github.com/GoCodeAlone/workflow/plugins/eventstore"
	pluginff "github.com/GoCodeAlone/workflow/plugins/featureflags"
	plugingitlab "github.com/GoCodeAlone/workflow/plugins/gitlab"
	pluginhttp "github.com/GoCodeAlone/workflow/plugins/http"
	pluginintegration "github.com/GoCodeAlone/workflow/plugins/integration"
	pluginlicense "github.com/GoCodeAlone/workflow/plugins/license"
	pluginmarketplace "github.com/GoCodeAlone/workflow/plugins/marketplace"
	pluginmessaging "github.com/GoCodeAlone/workflow/plugins/messaging"
	pluginmodcompat "github.com/GoCodeAlone/workflow/plugins/modularcompat"
	pluginobs "github.com/GoCodeAlone/workflow/plugins/observability"
	pluginpipeline "github.com/GoCodeAlone/workflow/plugins/pipelinesteps"
	pluginplatform "github.com/GoCodeAlone/workflow/plugins/platform"
	pluginpolicy "github.com/GoCodeAlone/workflow/plugins/policy"
	pluginscheduler "github.com/GoCodeAlone/workflow/plugins/scheduler"
	pluginsecrets "github.com/GoCodeAlone/workflow/plugins/secrets"
	pluginsm "github.com/GoCodeAlone/workflow/plugins/statemachine"
	pluginstorage "github.com/GoCodeAlone/workflow/plugins/storage"
	plugintimeline "github.com/GoCodeAlone/workflow/plugins/timeline"
)

// PluginLoader is the minimal interface required by [LoadAll].
// *workflow.StdEngine satisfies this interface.
type PluginLoader interface {
	LoadPlugin(p plugin.EnginePlugin) error
}

// DefaultPlugins returns the standard set of built-in engine plugins.
// The slice is freshly allocated on each call so callers may safely append
// custom plugins without affecting other callers.
func DefaultPlugins() []plugin.EnginePlugin {
	return []plugin.EnginePlugin{
		pluginlicense.New(),
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
		pluginevstore.New(),
		plugintimeline.New(),
		plugindlq.New(),
		pluginsecrets.New(),
		pluginmodcompat.New(),
		pluginscheduler.New(),
		pluginintegration.New(),
		pluginai.New(),
		pluginplatform.New(),
		plugincloud.New(),
		plugingitlab.New(),
		plugindatastores.New(),
		pluginpolicy.New(),
		pluginmarketplace.New(),
	}
}

// LoadAll loads all default built-in plugins into the given engine.
// It is equivalent to calling engine.LoadPlugin for each plugin returned by
// [DefaultPlugins]. The first error encountered is returned immediately.
func LoadAll(engine PluginLoader) error {
	for _, p := range DefaultPlugins() {
		if err := engine.LoadPlugin(p); err != nil {
			return err
		}
	}
	return nil
}
