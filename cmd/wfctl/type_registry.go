package main

// ModuleTypeInfo holds metadata about a known module type.
type ModuleTypeInfo struct {
	Type       string   // e.g., "storage.sqlite"
	Plugin     string   // e.g., "storage"
	Stateful   bool     // whether this module manages persistent state
	ConfigKeys []string // known config fields
}

// StepTypeInfo holds metadata about a known step type.
type StepTypeInfo struct {
	Type       string   // e.g., "step.json_response"
	Plugin     string   // e.g., "pipelinesteps"
	ConfigKeys []string // known config fields
}

// KnownModuleTypes returns all module types registered in the engine's plugins.
func KnownModuleTypes() map[string]ModuleTypeInfo {
	return map[string]ModuleTypeInfo{
		// storage plugin
		"storage.s3": {
			Type:       "storage.s3",
			Plugin:     "storage",
			Stateful:   false,
			ConfigKeys: []string{"bucket", "region", "endpoint"},
		},
		"storage.local": {
			Type:       "storage.local",
			Plugin:     "storage",
			Stateful:   false,
			ConfigKeys: []string{"rootDir"},
		},
		"storage.gcs": {
			Type:       "storage.gcs",
			Plugin:     "storage",
			Stateful:   false,
			ConfigKeys: []string{"bucket", "project", "credentialsFile"},
		},
		"storage.sqlite": {
			Type:       "storage.sqlite",
			Plugin:     "storage",
			Stateful:   true,
			ConfigKeys: []string{"dbPath", "maxConnections", "walMode"},
		},
		"database.workflow": {
			Type:       "database.workflow",
			Plugin:     "storage",
			Stateful:   true,
			ConfigKeys: []string{"driver", "dsn", "maxOpenConns", "maxIdleConns"},
		},
		"persistence.store": {
			Type:       "persistence.store",
			Plugin:     "storage",
			Stateful:   true,
			ConfigKeys: []string{"database"},
		},
		"cache.redis": {
			Type:       "cache.redis",
			Plugin:     "storage",
			Stateful:   false,
			ConfigKeys: []string{"address", "password", "db", "prefix", "defaultTTL"},
		},

		// http plugin
		"http.server": {
			Type:       "http.server",
			Plugin:     "http",
			Stateful:   false,
			ConfigKeys: []string{"address", "readTimeout", "writeTimeout", "idleTimeout"},
		},
		"http.router": {
			Type:       "http.router",
			Plugin:     "http",
			Stateful:   false,
			ConfigKeys: []string{"prefix", "middleware"},
		},
		"http.handler": {
			Type:       "http.handler",
			Plugin:     "http",
			Stateful:   false,
			ConfigKeys: []string{"contentType", "routes"},
		},
		"http.proxy": {
			Type:       "http.proxy",
			Plugin:     "http",
			Stateful:   false,
			ConfigKeys: []string{"target", "stripPrefix"},
		},
		"reverseproxy": {
			Type:       "reverseproxy",
			Plugin:     "http",
			Stateful:   false,
			ConfigKeys: []string{"target", "stripPrefix"},
		},
		"http.simple_proxy": {
			Type:       "http.simple_proxy",
			Plugin:     "http",
			Stateful:   false,
			ConfigKeys: []string{"target"},
		},
		"static.fileserver": {
			Type:       "static.fileserver",
			Plugin:     "http",
			Stateful:   false,
			ConfigKeys: []string{"root", "index", "spa"},
		},
		"http.middleware.auth": {
			Type:       "http.middleware.auth",
			Plugin:     "http",
			Stateful:   false,
			ConfigKeys: []string{"type", "header"},
		},
		"http.middleware.logging": {
			Type:       "http.middleware.logging",
			Plugin:     "http",
			Stateful:   false,
			ConfigKeys: []string{"format", "level"},
		},
		"http.middleware.ratelimit": {
			Type:       "http.middleware.ratelimit",
			Plugin:     "http",
			Stateful:   false,
			ConfigKeys: []string{"requestsPerMinute", "burstSize"},
		},
		"http.middleware.cors": {
			Type:       "http.middleware.cors",
			Plugin:     "http",
			Stateful:   false,
			ConfigKeys: []string{"allowOrigins", "allowMethods", "allowHeaders", "maxAge"},
		},
		"http.middleware.requestid": {
			Type:       "http.middleware.requestid",
			Plugin:     "http",
			Stateful:   false,
			ConfigKeys: []string{},
		},
		"http.middleware.securityheaders": {
			Type:       "http.middleware.securityheaders",
			Plugin:     "http",
			Stateful:   false,
			ConfigKeys: []string{},
		},

		// auth plugin
		"auth.jwt": {
			Type:       "auth.jwt",
			Plugin:     "auth",
			Stateful:   false,
			ConfigKeys: []string{"secret", "tokenExpiry", "issuer", "seedFile", "responseFormat"},
		},
		"auth.user-store": {
			Type:       "auth.user-store",
			Plugin:     "auth",
			Stateful:   true,
			ConfigKeys: []string{},
		},
		"auth.oauth2": {
			Type:       "auth.oauth2",
			Plugin:     "auth",
			Stateful:   false,
			ConfigKeys: []string{"providers"},
		},
		"auth.m2m": {
			Type:       "auth.m2m",
			Plugin:     "auth",
			Stateful:   false,
			ConfigKeys: []string{"secret", "algorithm", "privateKey", "tokenExpiry", "issuer", "clients"},
		},

		// messaging plugin
		"messaging.broker": {
			Type:       "messaging.broker",
			Plugin:     "messaging",
			Stateful:   false,
			ConfigKeys: []string{"maxQueueSize", "deliveryTimeout"},
		},
		"messaging.broker.eventbus": {
			Type:       "messaging.broker.eventbus",
			Plugin:     "messaging",
			Stateful:   false,
			ConfigKeys: []string{},
		},
		"messaging.handler": {
			Type:       "messaging.handler",
			Plugin:     "messaging",
			Stateful:   false,
			ConfigKeys: []string{"topic"},
		},
		"messaging.nats": {
			Type:       "messaging.nats",
			Plugin:     "messaging",
			Stateful:   false,
			ConfigKeys: []string{"url"},
		},
		"messaging.kafka": {
			Type:       "messaging.kafka",
			Plugin:     "messaging",
			Stateful:   false,
			ConfigKeys: []string{"brokers", "groupId"},
		},
		"notification.slack": {
			Type:       "notification.slack",
			Plugin:     "messaging",
			Stateful:   false,
			ConfigKeys: []string{"webhookURL", "channel", "username"},
		},
		"webhook.sender": {
			Type:       "webhook.sender",
			Plugin:     "messaging",
			Stateful:   false,
			ConfigKeys: []string{"maxRetries"},
		},

		// statemachine plugin
		"statemachine.engine": {
			Type:       "statemachine.engine",
			Plugin:     "statemachine",
			Stateful:   true,
			ConfigKeys: []string{"maxInstances", "instanceTTL"},
		},
		"state.tracker": {
			Type:       "state.tracker",
			Plugin:     "statemachine",
			Stateful:   true,
			ConfigKeys: []string{"retentionDays"},
		},
		"state.connector": {
			Type:       "state.connector",
			Plugin:     "statemachine",
			Stateful:   false,
			ConfigKeys: []string{},
		},

		// observability plugin
		"metrics.collector": {
			Type:       "metrics.collector",
			Plugin:     "observability",
			Stateful:   false,
			ConfigKeys: []string{"namespace", "subsystem", "metricsPath", "enabledMetrics"},
		},
		"health.checker": {
			Type:       "health.checker",
			Plugin:     "observability",
			Stateful:   false,
			ConfigKeys: []string{"healthPath", "readyPath", "livePath", "checkTimeout", "autoDiscover"},
		},
		"log.collector": {
			Type:       "log.collector",
			Plugin:     "observability",
			Stateful:   false,
			ConfigKeys: []string{"logLevel", "outputFormat", "retentionDays"},
		},
		"observability.otel": {
			Type:       "observability.otel",
			Plugin:     "observability",
			Stateful:   false,
			ConfigKeys: []string{"endpoint", "serviceName"},
		},
		"openapi.generator": {
			Type:       "openapi.generator",
			Plugin:     "observability",
			Stateful:   false,
			ConfigKeys: []string{"title", "version", "description", "servers"},
		},
		"http.middleware.otel": {
			Type:       "http.middleware.otel",
			Plugin:     "observability",
			Stateful:   false,
			ConfigKeys: []string{"serverName"},
		},

		// api plugin
		"api.query": {
			Type:       "api.query",
			Plugin:     "api",
			Stateful:   false,
			ConfigKeys: []string{"delegate", "routes"},
		},
		"api.command": {
			Type:       "api.command",
			Plugin:     "api",
			Stateful:   false,
			ConfigKeys: []string{"delegate", "routes"},
		},
		"api.handler": {
			Type:       "api.handler",
			Plugin:     "api",
			Stateful:   false,
			ConfigKeys: []string{"resourceName", "workflowType", "workflowEngine", "initialTransition", "seedFile", "sourceResourceName", "stateFilter", "fieldMapping", "transitionMap", "summaryFields"},
		},
		"api.gateway": {
			Type:       "api.gateway",
			Plugin:     "api",
			Stateful:   false,
			ConfigKeys: []string{"routes", "globalRateLimit", "cors", "auth"},
		},
		"workflow.registry": {
			Type:       "workflow.registry",
			Plugin:     "api",
			Stateful:   true,
			ConfigKeys: []string{"storageBackend"},
		},
		"data.transformer": {
			Type:       "data.transformer",
			Plugin:     "api",
			Stateful:   false,
			ConfigKeys: []string{},
		},
		"processing.step": {
			Type:       "processing.step",
			Plugin:     "api",
			Stateful:   false,
			ConfigKeys: []string{"componentId", "successTransition", "compensateTransition", "maxRetries", "retryBackoffMs", "timeoutSeconds"},
		},

		// secrets plugin
		"secrets.vault": {
			Type:       "secrets.vault",
			Plugin:     "secrets",
			Stateful:   false,
			ConfigKeys: []string{"mode", "address", "token", "mountPath", "namespace"},
		},
		"secrets.aws": {
			Type:       "secrets.aws",
			Plugin:     "secrets",
			Stateful:   false,
			ConfigKeys: []string{"region", "accessKeyId", "secretAccessKey"},
		},

		// ai plugin
		"dynamic.component": {
			Type:       "dynamic.component",
			Plugin:     "ai",
			Stateful:   false,
			ConfigKeys: []string{"componentId", "source", "provides", "requires"},
		},

		// featureflags plugin
		"featureflag.service": {
			Type:       "featureflag.service",
			Plugin:     "featureflags",
			Stateful:   true,
			ConfigKeys: []string{"provider", "cache_ttl", "sse_enabled", "db_path"},
		},

		// eventstore plugin
		"eventstore.service": {
			Type:       "eventstore.service",
			Plugin:     "eventstore",
			Stateful:   true,
			ConfigKeys: []string{"db_path", "retention_days"},
		},

		// dlq plugin
		"dlq.service": {
			Type:       "dlq.service",
			Plugin:     "dlq",
			Stateful:   true,
			ConfigKeys: []string{"max_retries", "retention_days"},
		},

		// timeline plugin
		"timeline.service": {
			Type:       "timeline.service",
			Plugin:     "timeline",
			Stateful:   false,
			ConfigKeys: []string{"event_store"},
		},

		// modularcompat plugin
		"scheduler.modular": {
			Type:       "scheduler.modular",
			Plugin:     "modularcompat",
			Stateful:   false,
			ConfigKeys: []string{},
		},
		"cache.modular": {
			Type:       "cache.modular",
			Plugin:     "modularcompat",
			Stateful:   false,
			ConfigKeys: []string{},
		},

		// datastores plugin
		"nosql.memory": {
			Type:       "nosql.memory",
			Plugin:     "datastores",
			Stateful:   true,
			ConfigKeys: []string{"collection"},
		},
		"nosql.dynamodb": {
			Type:       "nosql.dynamodb",
			Plugin:     "datastores",
			Stateful:   false,
			ConfigKeys: []string{"tableName", "region", "endpoint", "credentials"},
		},
		"nosql.mongodb": {
			Type:       "nosql.mongodb",
			Plugin:     "datastores",
			Stateful:   false,
			ConfigKeys: []string{"uri", "database", "collection"},
		},
		"nosql.redis": {
			Type:       "nosql.redis",
			Plugin:     "datastores",
			Stateful:   false,
			ConfigKeys: []string{"addr", "password", "db"},
		},

		// storage plugin (artifact)
		"storage.artifact": {
			Type:       "storage.artifact",
			Plugin:     "storage",
			Stateful:   false,
			ConfigKeys: []string{"backend", "basePath", "maxSize", "bucket", "region", "endpoint"},
		},

		// cloud plugin
		"cloud.account": {
			Type:       "cloud.account",
			Plugin:     "cloud",
			Stateful:   false,
			ConfigKeys: []string{"provider", "region", "credentials", "project_id", "subscription_id"},
		},

		// gitlab plugin
		"gitlab.client": {
			Type:       "gitlab.client",
			Plugin:     "gitlab",
			Stateful:   false,
			ConfigKeys: []string{"url", "token"},
		},
		"gitlab.webhook": {
			Type:       "gitlab.webhook",
			Plugin:     "gitlab",
			Stateful:   false,
			ConfigKeys: []string{"secret", "path", "events"},
		},

		// cicd plugin (codebuild module)
		"aws.codebuild": {
			Type:       "aws.codebuild",
			Plugin:     "cicd",
			Stateful:   true,
			ConfigKeys: []string{"account", "region", "service_role", "compute_type", "image", "source_type"},
		},

		// policy plugin (OPA and Cedar are external plugins: workflow-plugin-policy-opa, workflow-plugin-policy-cedar)
		"policy.mock": {
			Type:       "policy.mock",
			Plugin:     "policy",
			Stateful:   false,
			ConfigKeys: []string{"policies"},
		},

		// observability plugin (tracing)
		"tracing.propagation": {
			Type:       "tracing.propagation",
			Plugin:     "observability",
			Stateful:   false,
			ConfigKeys: []string{"format"},
		},

		// platform plugin (region router + DigitalOcean)
		"platform.region_router": {
			Type:       "platform.region_router",
			Plugin:     "platform",
			Stateful:   false,
			ConfigKeys: []string{"module", "mode"},
		},
		"platform.doks": {
			Type:       "platform.doks",
			Plugin:     "platform",
			Stateful:   false,
			ConfigKeys: []string{"account", "cluster_name", "region", "version", "node_pool"},
		},
		"platform.do_networking": {
			Type:       "platform.do_networking",
			Plugin:     "platform",
			Stateful:   false,
			ConfigKeys: []string{"account", "provider", "vpc", "firewalls"},
		},
		"platform.do_dns": {
			Type:       "platform.do_dns",
			Plugin:     "platform",
			Stateful:   false,
			ConfigKeys: []string{"account", "provider", "domain", "records"},
		},
		"platform.do_app": {
			Type:       "platform.do_app",
			Plugin:     "platform",
			Stateful:   false,
			ConfigKeys: []string{"account", "provider", "name", "region", "image", "instances", "http_port", "envs"},
		},
	}
}

// KnownStepTypes returns all step types registered in the engine's plugins.
func KnownStepTypes() map[string]StepTypeInfo {
	return map[string]StepTypeInfo{
		// pipelinesteps plugin
		"step.validate": {
			Type:       "step.validate",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"rules", "required", "schema"},
		},
		"step.transform": {
			Type:       "step.transform",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"mapping", "template"},
		},
		"step.conditional": {
			Type:       "step.conditional",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"condition", "then", "else"},
		},
		"step.set": {
			Type:       "step.set",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"key", "value"},
		},
		"step.log": {
			Type:       "step.log",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"message", "level"},
		},
		"step.delegate": {
			Type:       "step.delegate",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"service", "action"},
		},
		"step.jq": {
			Type:       "step.jq",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"expression", "input", "output"},
		},
		"step.publish": {
			Type:       "step.publish",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"topic", "broker", "payload"},
		},
		"step.event_publish": {
			Type:       "step.event_publish",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"topic", "broker", "payload", "headers", "event_type"},
		},
		"step.http_call": {
			Type:       "step.http_call",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"url", "method", "headers", "body", "timeout"},
		},
		"step.request_parse": {
			Type:       "step.request_parse",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"body", "query", "headers"},
		},
		"step.db_query": {
			Type:       "step.db_query",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"database", "query", "params"},
		},
		"step.db_exec": {
			Type:       "step.db_exec",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"database", "query", "params"},
		},
		"step.json_response": {
			Type:       "step.json_response",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"status", "body", "headers"},
		},
		"step.workflow_call": {
			Type:       "step.workflow_call",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"workflow", "input"},
		},
		"step.validate_path_param": {
			Type:       "step.validate_path_param",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"param", "type", "required"},
		},
		"step.validate_pagination": {
			Type:       "step.validate_pagination",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"maxLimit", "defaultLimit"},
		},
		"step.validate_request_body": {
			Type:       "step.validate_request_body",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"schema", "required"},
		},
		"step.foreach": {
			Type:       "step.foreach",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"collection", "steps"},
		},
		"step.webhook_verify": {
			Type:       "step.webhook_verify",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"secret", "header", "algorithm"},
		},
		"step.cache_get": {
			Type:       "step.cache_get",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"cache", "key", "output"},
		},
		"step.cache_set": {
			Type:       "step.cache_set",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"cache", "key", "value", "ttl"},
		},
		"step.cache_delete": {
			Type:       "step.cache_delete",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"cache", "key"},
		},
		"step.dlq_send": {
			Type:       "step.dlq_send",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"topic", "original_topic", "error", "payload", "broker"},
		},
		"step.dlq_replay": {
			Type:       "step.dlq_replay",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"dlq_topic", "target_topic", "max_messages", "broker"},
		},
		"step.retry_with_backoff": {
			Type:       "step.retry_with_backoff",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"max_retries", "initial_delay", "max_delay", "multiplier", "step"},
		},
		"step.resilient_circuit_breaker": {
			Type:       "step.resilient_circuit_breaker",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"failure_threshold", "reset_timeout", "step", "fallback"},
		},

		// http plugin steps
		"step.rate_limit": {
			Type:       "step.rate_limit",
			Plugin:     "http",
			ConfigKeys: []string{"requestsPerMinute", "burstSize", "key"},
		},
		"step.circuit_breaker": {
			Type:       "step.circuit_breaker",
			Plugin:     "http",
			ConfigKeys: []string{"threshold", "timeout", "halfOpenRequests"},
		},

		// statemachine plugin steps
		"step.statemachine_transition": {
			Type:       "step.statemachine_transition",
			Plugin:     "statemachine",
			ConfigKeys: []string{"engine", "instanceId", "transition"},
		},
		"step.statemachine_get": {
			Type:       "step.statemachine_get",
			Plugin:     "statemachine",
			ConfigKeys: []string{"engine", "instanceId"},
		},

		// ai plugin steps
		"step.ai_complete": {
			Type:       "step.ai_complete",
			Plugin:     "ai",
			ConfigKeys: []string{"model", "prompt", "maxTokens", "temperature"},
		},
		"step.ai_classify": {
			Type:       "step.ai_classify",
			Plugin:     "ai",
			ConfigKeys: []string{"model", "input", "categories"},
		},
		"step.ai_extract": {
			Type:       "step.ai_extract",
			Plugin:     "ai",
			ConfigKeys: []string{"model", "input", "schema"},
		},
		"step.sub_workflow": {
			Type:       "step.sub_workflow",
			Plugin:     "ai",
			ConfigKeys: []string{"workflow", "input"},
		},

		// featureflags plugin steps
		"step.feature_flag": {
			Type:       "step.feature_flag",
			Plugin:     "featureflags",
			ConfigKeys: []string{"flag", "default", "output"},
		},
		"step.ff_gate": {
			Type:       "step.ff_gate",
			Plugin:     "featureflags",
			ConfigKeys: []string{"flag", "condition"},
		},

		// cicd plugin steps
		"step.shell_exec": {
			Type:       "step.shell_exec",
			Plugin:     "cicd",
			ConfigKeys: []string{"command", "args", "env", "workdir", "timeout"},
		},
		"step.artifact_pull": {
			Type:       "step.artifact_pull",
			Plugin:     "cicd",
			ConfigKeys: []string{"registry", "artifact", "tag", "output"},
		},
		"step.artifact_push": {
			Type:       "step.artifact_push",
			Plugin:     "cicd",
			ConfigKeys: []string{"registry", "artifact", "tag"},
		},
		"step.docker_build": {
			Type:       "step.docker_build",
			Plugin:     "cicd",
			ConfigKeys: []string{"context", "dockerfile", "tags", "buildArgs"},
		},
		"step.docker_push": {
			Type:       "step.docker_push",
			Plugin:     "cicd",
			ConfigKeys: []string{"image", "registry", "credentials"},
		},
		"step.docker_run": {
			Type:       "step.docker_run",
			Plugin:     "cicd",
			ConfigKeys: []string{"image", "command", "env", "volumes"},
		},
		"step.scan_sast": {
			Type:       "step.scan_sast",
			Plugin:     "cicd",
			ConfigKeys: []string{"tool", "path", "severity"},
		},
		"step.scan_container": {
			Type:       "step.scan_container",
			Plugin:     "cicd",
			ConfigKeys: []string{"image", "severity"},
		},
		"step.scan_deps": {
			Type:       "step.scan_deps",
			Plugin:     "cicd",
			ConfigKeys: []string{"path", "severity"},
		},
		"step.deploy": {
			Type:       "step.deploy",
			Plugin:     "cicd",
			ConfigKeys: []string{"target", "config", "namespace"},
		},
		"step.gate": {
			Type:       "step.gate",
			Plugin:     "cicd",
			ConfigKeys: []string{"condition", "approvers"},
		},
		"step.build_ui": {
			Type:       "step.build_ui",
			Plugin:     "cicd",
			ConfigKeys: []string{"path", "command"},
		},
		"step.build_from_config": {
			Type:       "step.build_from_config",
			Plugin:     "cicd",
			ConfigKeys: []string{"config", "output"},
		},

		// auth-related steps (from pipelinesteps but auth-aware)
		"step.auth_required": {
			Type:       "step.auth_required",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"roles", "scopes"},
		},
		"step.user_register": {
			Type:       "step.user_register",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"store", "fields"},
		},
		"step.user_login": {
			Type:       "step.user_login",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"store", "auth"},
		},
		"step.user_profile": {
			Type:       "step.user_profile",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"store"},
		},
		"step.org_create": {
			Type:       "step.org_create",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"store"},
		},
		"step.org_list": {
			Type:       "step.org_list",
			Plugin:     "pipelinesteps",
			ConfigKeys: []string{"store"},
		},

		// datastores plugin steps
		"step.nosql_get": {
			Type:       "step.nosql_get",
			Plugin:     "datastores",
			ConfigKeys: []string{"store", "key", "output", "miss_ok"},
		},
		"step.nosql_put": {
			Type:       "step.nosql_put",
			Plugin:     "datastores",
			ConfigKeys: []string{"store", "key", "item"},
		},
		"step.nosql_delete": {
			Type:       "step.nosql_delete",
			Plugin:     "datastores",
			ConfigKeys: []string{"store", "key"},
		},
		"step.nosql_query": {
			Type:       "step.nosql_query",
			Plugin:     "datastores",
			ConfigKeys: []string{"store", "prefix", "output"},
		},

		// storage plugin steps (artifact)
		"step.artifact_upload": {
			Type:       "step.artifact_upload",
			Plugin:     "storage",
			ConfigKeys: []string{"store", "key", "source", "metadata"},
		},
		"step.artifact_download": {
			Type:       "step.artifact_download",
			Plugin:     "storage",
			ConfigKeys: []string{"store", "key", "dest"},
		},
		"step.artifact_list": {
			Type:       "step.artifact_list",
			Plugin:     "storage",
			ConfigKeys: []string{"store", "prefix", "output"},
		},
		"step.artifact_delete": {
			Type:       "step.artifact_delete",
			Plugin:     "storage",
			ConfigKeys: []string{"store", "key"},
		},

		// cloud plugin steps
		"step.cloud_validate": {
			Type:       "step.cloud_validate",
			Plugin:     "cloud",
			ConfigKeys: []string{"account"},
		},

		// cicd plugin steps (build_binary + codebuild)
		"step.build_binary": {
			Type:       "step.build_binary",
			Plugin:     "cicd",
			ConfigKeys: []string{"config_file", "output", "os", "arch"},
		},
		"step.codebuild_create_project": {
			Type:       "step.codebuild_create_project",
			Plugin:     "cicd",
			ConfigKeys: []string{"project"},
		},
		"step.codebuild_start": {
			Type:       "step.codebuild_start",
			Plugin:     "cicd",
			ConfigKeys: []string{"project", "env_vars"},
		},
		"step.codebuild_status": {
			Type:       "step.codebuild_status",
			Plugin:     "cicd",
			ConfigKeys: []string{"project", "build_id"},
		},
		"step.codebuild_logs": {
			Type:       "step.codebuild_logs",
			Plugin:     "cicd",
			ConfigKeys: []string{"project", "build_id"},
		},
		"step.codebuild_list_builds": {
			Type:       "step.codebuild_list_builds",
			Plugin:     "cicd",
			ConfigKeys: []string{"project"},
		},
		"step.codebuild_delete_project": {
			Type:       "step.codebuild_delete_project",
			Plugin:     "cicd",
			ConfigKeys: []string{"project"},
		},

		// gitlab plugin steps
		"step.gitlab_trigger_pipeline": {
			Type:       "step.gitlab_trigger_pipeline",
			Plugin:     "gitlab",
			ConfigKeys: []string{"client", "project", "ref", "variables"},
		},
		"step.gitlab_pipeline_status": {
			Type:       "step.gitlab_pipeline_status",
			Plugin:     "gitlab",
			ConfigKeys: []string{"client", "project", "pipeline_id"},
		},
		"step.gitlab_parse_webhook": {
			Type:       "step.gitlab_parse_webhook",
			Plugin:     "gitlab",
			ConfigKeys: []string{"client"},
		},
		"step.gitlab_create_mr": {
			Type:       "step.gitlab_create_mr",
			Plugin:     "gitlab",
			ConfigKeys: []string{"client", "project", "source_branch", "target_branch", "title", "description"},
		},
		"step.gitlab_mr_comment": {
			Type:       "step.gitlab_mr_comment",
			Plugin:     "gitlab",
			ConfigKeys: []string{"client", "project", "mr_iid", "body"},
		},

		// policy plugin steps
		"step.policy_load": {
			Type:       "step.policy_load",
			Plugin:     "policy",
			ConfigKeys: []string{"engine", "policy_name", "content"},
		},
		"step.policy_evaluate": {
			Type:       "step.policy_evaluate",
			Plugin:     "policy",
			ConfigKeys: []string{"engine", "input_from"},
		},
		"step.policy_list": {
			Type:       "step.policy_list",
			Plugin:     "policy",
			ConfigKeys: []string{"engine"},
		},
		"step.policy_test": {
			Type:       "step.policy_test",
			Plugin:     "policy",
			ConfigKeys: []string{"engine", "sample_input", "expect_allow"},
		},

		// observability plugin steps (tracing)
		"step.trace_start": {
			Type:       "step.trace_start",
			Plugin:     "observability",
			ConfigKeys: []string{"span_name", "attributes"},
		},
		"step.trace_inject": {
			Type:       "step.trace_inject",
			Plugin:     "observability",
			ConfigKeys: []string{"carrier_field", "carrier_type"},
		},
		"step.trace_extract": {
			Type:       "step.trace_extract",
			Plugin:     "observability",
			ConfigKeys: []string{"carrier_field", "carrier_type"},
		},
		"step.trace_annotate": {
			Type:       "step.trace_annotate",
			Plugin:     "observability",
			ConfigKeys: []string{"event_name", "attributes"},
		},
		"step.trace_link": {
			Type:       "step.trace_link",
			Plugin:     "observability",
			ConfigKeys: []string{"parent_field"},
		},

		// marketplace plugin steps
		"step.marketplace_search": {
			Type:       "step.marketplace_search",
			Plugin:     "marketplace",
			ConfigKeys: []string{"query", "category", "tags"},
		},
		"step.marketplace_detail": {
			Type:       "step.marketplace_detail",
			Plugin:     "marketplace",
			ConfigKeys: []string{"plugin"},
		},
		"step.marketplace_install": {
			Type:       "step.marketplace_install",
			Plugin:     "marketplace",
			ConfigKeys: []string{"plugin"},
		},
		"step.marketplace_installed": {
			Type:       "step.marketplace_installed",
			Plugin:     "marketplace",
			ConfigKeys: []string{},
		},
		"step.marketplace_update": {
			Type:       "step.marketplace_update",
			Plugin:     "marketplace",
			ConfigKeys: []string{"plugin"},
		},
		"step.marketplace_uninstall": {
			Type:       "step.marketplace_uninstall",
			Plugin:     "marketplace",
			ConfigKeys: []string{"plugin"},
		},

		// platform plugin steps (region)
		"step.region_deploy": {
			Type:       "step.region_deploy",
			Plugin:     "platform",
			ConfigKeys: []string{"module", "region"},
		},
		"step.region_promote": {
			Type:       "step.region_promote",
			Plugin:     "platform",
			ConfigKeys: []string{"module", "region"},
		},
		"step.region_failover": {
			Type:       "step.region_failover",
			Plugin:     "platform",
			ConfigKeys: []string{"module", "from", "to"},
		},
		"step.region_status": {
			Type:       "step.region_status",
			Plugin:     "platform",
			ConfigKeys: []string{"module"},
		},
		"step.region_weight": {
			Type:       "step.region_weight",
			Plugin:     "platform",
			ConfigKeys: []string{"module", "region", "weight"},
		},
		"step.region_sync": {
			Type:       "step.region_sync",
			Plugin:     "platform",
			ConfigKeys: []string{"module"},
		},

		// platform plugin steps (argo)
		"step.argo_submit": {
			Type:       "step.argo_submit",
			Plugin:     "platform",
			ConfigKeys: []string{"service", "workflow_name", "steps"},
		},
		"step.argo_status": {
			Type:       "step.argo_status",
			Plugin:     "platform",
			ConfigKeys: []string{"service", "workflow_run"},
		},
		"step.argo_logs": {
			Type:       "step.argo_logs",
			Plugin:     "platform",
			ConfigKeys: []string{"service", "workflow_run"},
		},
		"step.argo_delete": {
			Type:       "step.argo_delete",
			Plugin:     "platform",
			ConfigKeys: []string{"service", "workflow_run"},
		},
		"step.argo_list": {
			Type:       "step.argo_list",
			Plugin:     "platform",
			ConfigKeys: []string{"service", "label_selector"},
		},

		// platform plugin steps (DigitalOcean)
		"step.do_deploy": {
			Type:       "step.do_deploy",
			Plugin:     "platform",
			ConfigKeys: []string{"app", "image"},
		},
		"step.do_status": {
			Type:       "step.do_status",
			Plugin:     "platform",
			ConfigKeys: []string{"app"},
		},
		"step.do_logs": {
			Type:       "step.do_logs",
			Plugin:     "platform",
			ConfigKeys: []string{"app"},
		},
		"step.do_scale": {
			Type:       "step.do_scale",
			Plugin:     "platform",
			ConfigKeys: []string{"app", "instances"},
		},
		"step.do_destroy": {
			Type:       "step.do_destroy",
			Plugin:     "platform",
			ConfigKeys: []string{"app"},
		},
	}
}

// KnownTriggerTypes returns all known trigger types.
func KnownTriggerTypes() map[string]bool {
	return map[string]bool{
		"http":           true,
		"event":          true,
		"eventbus":       true,
		"schedule":       true,
		"reconciliation": true,
	}
}
