package mcp

const (
	mcpGithubActionsCheckoutRef  = "actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6.0.3"
	mcpGithubActionsSetupGoRef   = "actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6.4.0"
	mcpGithubActionsSetupNodeRef = "actions/setup-node@48b55a011bda9f5d6aeb4c2d9c7362e8dae4041e # v6.4.0"
	// #nosec G101 -- action commit SHA, not a credential.
	mcpGithubActionsSetupWfctlRef        = "GoCodeAlone/setup-wfctl@bcd880980f5bbe8d192d0c20ff6279d25331f956 # v1"
	mcpGithubActionsDockerLoginRef       = "docker/login-action@c94ce9fb468520275223c153574b00df6fe4bcc9 # v3"
	mcpGithubActionsDockerSetupBuildxRef = "docker/setup-buildx-action@f7ce87c1d6bead3e36075b2ce75da1f6cc28aaca # v3.9.0"
	mcpGithubActionsDockerBuildPushRef   = "docker/build-push-action@4f58ea79222b3b9dc2c8bbdd6debcef730109a75 # v6.9.0"
)
