# Workflow Capability Catalog

- Generated: 2026-06-11T07:40:01Z
- Workflow version: dev
- Taxonomy: 2026-06-11
- Hidden uncategorized rows: 1775

| Capability               | Name                   | Category       | Providers                                                                                                                                                                                 |
|---                       |---                     |---             |---                                                                                                                                                                                        |
| auth.authn               | Authentication         | auth           | auth (released), workflow-plugin-auth (local-only)                                                                                                                                        |
| ci.generation            | CI Generation          | ci             | workflow-plugin-ci-generator (local-only)                                                                                                                                                 |
| communication.messaging  | Customer Messaging     | communication  | workflow-plugin-twilio (local-only)                                                                                                                                                       |
| migrations.schema        | Schema Migrations      | data           | workflow-plugin-data-engineering (local-only)                                                                                                                                             |
| docs.api                 | API Documentation      | docs           | observability (released)                                                                                                                                                                  |
| featureflags.flags       | Feature Flags          | featureflags   | feature-flags (released)                                                                                                                                                                  |
| http.routing             | HTTP Routing           | http           | http (released)                                                                                                                                                                           |
| iac.dns                  | DNS Infrastructure     | iac            | workflow-plugin-digitalocean (local-only), workflow-plugin-infra (local-only)                                                                                                             |
| iac.provider             | IaC Provider           | iac            | workflow-plugin-aws (local-only), workflow-plugin-azure (local-only), workflow-plugin-digitalocean (local-only), workflow-plugin-gcp (local-only), workflow-plugin-hover (local-only)     |
| iac.state-backend        | IaC State Backend      | iac            | workflow-plugin-aws (local-only), workflow-plugin-azure (local-only), workflow-plugin-digitalocean (local-only), workflow-plugin-gcp (local-only), workflow-plugin-platform (local-only)  |
| messaging.broker         | Message Broker         | messaging      | messaging (released), workflow-plugin-eventbus (local-only)                                                                                                                               |
| observability.tracing    | Tracing                | observability  | observability (released)                                                                                                                                                                  |
| payments.processing      | Payments               | payments       | workflow-plugin-payments (local-only)                                                                                                                                                     |
| platform.digitalocean    | DigitalOcean Provider  | platform       | workflow-plugin-digitalocean (local-only)                                                                                                                                                 |
| platform.github          | GitHub Provider        | platform       | workflow-plugin-github (local-only), workflow-plugin-gitlab (local-only)                                                                                                                  |
| secrets.management       | Secrets Management     | secrets        | secrets (released)                                                                                                                                                                        |
| storage.database         | Database Storage       | storage        | pipeline-steps (released), storage (released)                                                                                                                                             |
| storage.object           | Object Storage         | storage        | storage (released), workflow-plugin-aws (local-only), workflow-plugin-gcp (local-only)                                                                                                    |
| tenancy.scope            | Tenant Scope           | tenancy        | workflow-plugin-data-engineering (local-only)                                                                                                                                             |
