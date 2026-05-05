## wfctl: deploy staging — FAILED

**Resource:** bmw-staging
**Root cause:** `workflow-migrate up: first .: file does not exist`
**Console:** https://cloud.digitalocean.com/apps/abc

### Phase timings

| Phase | Status | Duration |
|---|---|---|
| build | SUCCESS | 2m14s |
| pre_deploy | ERROR | 3s |

### Diagnostics

- **[pre_deploy]** `dep-123` — exit status 1
  <details><summary>log tail</summary>

  ```
  workflow-migrate up: first .: file does not exist
  Error: exit status 1
  ```

  </details>
