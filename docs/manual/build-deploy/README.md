# Build + Deploy Manual

Reference documentation for `wfctl build`, `wfctl registry`, and the CI pipeline schema introduced in workflow v0.14.0.

## Pages

| # | Title | Use when |
|---|-------|----------|
| [01](./01-ci-build-schema.md) | `ci.build` Schema Reference | Looking up every field in `ci.build` |
| [02](./02-ci-registries-schema.md) | `ci.registries` Schema Reference | Configuring registry credentials + retention |
| [03](./03-ci-deploy-environments.md) | `ci.deploy.environments` | Setting up staging/prod with approval gates |
| [04](./04-builder-plugins.md) | Builder Plugins | Writing a custom builder or understanding built-ins |
| [05](./05-cli-reference.md) | CLI Reference | Every `wfctl` flag and subcommand |
| [06](./06-auth-providers.md) | Auth Providers | Registry login, private plugin repos |
| [07](./07-security-hardening.md) | Security Hardening | SBOM, provenance, Dockerfile auditing |
| [08](./08-local-dev.md) | Local Dev | `wfctl dev up`, per-env overrides, hot reload |
| [09](./09-troubleshooting.md) | Troubleshooting | Common errors and fixes |

## Tutorial

For a step-by-step walkthrough from hello-world to polyglot multi-registry pipeline:
→ [Build + Deploy Pipeline Tutorial](../../tutorials/build-deploy-pipeline.md)
