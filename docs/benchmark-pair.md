# Same-runner benchmark pair

This candidate replaces a restored benchmark-result cache with fresh measurements
of the exact pull-request base and GitHub synthetic merge in the existing
`Benchmark` / `Performance Benchmarks` job. It does not measure the bare PR head.
The push-only cache producer and write-capable PR comment step are removed.
Results remain available in the job summary and `benchmark-results` artifact.

## Collection contract

- Two checkouts use full event SHAs and `persist-credentials: false`. The collector
  binds base, head, merge, trees, and ordered merge parents before building UI
  assets, then verifies source cleanliness and immutable identities again.
- The sides have separate UI, module, build, and output directories. Measurement
  outputs are never restored from caches. Each side records its own lockfiles,
  module graph, module verification, compiler, flags, OS, architecture, CPU, and
  raw Go command stdout/stderr with hashes and phase receipts, plus raw UI
  build/install stdout/stderr.
- Both sides use the existing six-sample Go benchmark invocation. Explicit phase,
  step, and job timeouts fail closed. Missing data, changed source, diagnostics,
  differing CPU/compiler/flags/benchmark roots, or differing serializer modes do
  not become performance passes.
- Serializer selection is established from the selected Go files of both Sonic
  packages, bound to the reviewed `v1.15.4` module and checksum identity. A changed
  dependency identity or local replacement requires a separate source review.
  Absence of a warning is not proof of native mode. Equal fallback modes are
  recorded as fallback, never described as native.
- The existing strict AWK raw-data/report validator and regression thresholds
  remain unchanged. `pair.json` means only that collection inputs are complete;
  it is not a performance verdict. Raw diagnostics and partial phase receipts
  remain useful when a later step fails.

Same-job collection provides hardware comparability, not containment against
malicious source code. This lane retains the reviewed-source trust boundary and
read-only repository/package permissions. It adds no secrets, persistent
credentials, runner class, namespace, service, or reporting write permission.

## Verification and adoption

The bounded local check is:

```sh
PYTHONDONTWRITEBYTECODE=1 python3 scripts/test-benchmark-pair.py
```

It uses synthetic fixtures and mocked workload launches, plus the unchanged AWK
validator on synthetic data. It does not build the repository, run benchmarks,
establish native CI readiness, or replace the full test suite.

The helper is executable authority. Review its complete bytes and derive its
exact hash together with the workflow's canonical context, action nodes, and
shell statements using the existing policy parser. A raw YAML SHA256 is not the
canonical context SHA256. No trust manifest or protected checker/harness changes
are part of this candidate.

Adoption requires the normal separate lifecycle: first a trust-only stage that
retains active authority, then workflow/helper adoption with trust unchanged,
then promotion. A checker or harness change, if subsequently needed, requires its
own implementation lifecycle before workflow adoption. Refresh product PRs only
after adoption so checks apply to their actual heads.

The trust-only staging PR still runs the old benchmark workflow. Its native
bootstrap requirements must be established independently; a green main push run
does not prove that the old cross-runner PR comparison can pass. This candidate
does not supply a waiver, fallback success, or protection bypass.
