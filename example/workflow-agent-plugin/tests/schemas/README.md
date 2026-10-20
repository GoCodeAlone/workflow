# Schema provenance

Unmodified Agent Plugins 1.0.0 schemas fetched 2026-10-19:

- [plugin.schema.json](https://agent-plugins.org/schemas/1.0.0/plugin.schema.json)
- [mcp.schema.json](https://agent-plugins.org/schemas/1.0.0/mcp.schema.json)

Upstream: [agentplugins/agent-plugins-spec](https://github.com/agentplugins/agent-plugins-spec),
observed main commit `ff8ab5e392cc87bd88d87c060815a87490e51003`.
Schemas/software are Apache-2.0 under the upstream
[licensing policy](https://github.com/agentplugins/agent-plugins-spec/blob/ff8ab5e392cc87bd88d87c060815a87490e51003/LICENSE.md).
The included [Apache-2.0.txt](Apache-2.0.txt) applies to these schemas;
the surrounding package is MIT. No schema changes were made.

Tests use the vendored copies, so they need no network access. They validate the
portable core; the `com.openai` extension is opaque to those schemas and does not
establish desktop behavior.
