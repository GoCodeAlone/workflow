package main

import (
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
)

// The CLI supplies provider-bound discovery; the shared writer retains
// canonical-only behavior for callers that do not supply a classifier.
func writeEnvResolvedConfig(cfgFile, envName string) (tmpPath string, err error) {
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return "", err
	}
	discovery, err := newIaCResourceDiscovery(cfg, envName)
	if err != nil {
		return "", err
	}
	return wfctlhelpers.WriteEnvResolvedConfigWithClassifier(cfgFile, envName, discovery.classify)
}
