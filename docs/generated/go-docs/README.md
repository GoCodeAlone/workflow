# Generated Go API Docs

This directory contains generated Go API documentation for Workflow-owned
packages. The files are generated from real Go packages and are intended for
`gocodealone-website` ingestion.

Regenerate from the workflow repository root with:

```sh
GOWORK=off go run ./cmd/wfctl docs generate \
  --source . \
  --out docs/generated/go-docs \
  --module github.com/GoCodeAlone/workflow \
  --version local \
  --packages capability,capability/inventory,plugin,plugin/sdk,plugin/external/sdk,config,manifest
```

`index.json` and `versions.json` currently contain the same metadata for
compatibility. Public website generation should prefer released tags; local
generation is for review and preview.
