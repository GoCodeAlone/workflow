# IaCStateBackend transport benchmark — result & decision

**Date:** 2026-05-14
**Task:** Plan Task 6 (PR 2) — "run the benchmark, lock the proto-transport decision"
**Status:** ⚠️ **DECISION PENDING OPERATOR REVIEW** — the measured result exceeds the plan's literal acceptance bar, but the plan's contingency remedy (streaming redesign) is demonstrably mis-targeted at the actual bottleneck. See "The conflict" and "Recommendation" below.

## Measurement

`go test ./module/ -bench BenchmarkIaCStateBackend -benchmem -run '^$' -count=10`, benchstat over 10 samples:

| Benchmark | sec/op | B/op | allocs/op |
|---|---|---|---|
| `IaCStateBackend_InProcess` | **179.4 ns ± 1%** | 416 | 2 |
| `IaCStateBackend_GRPC` | **6.511 ms ± 1%** | 4.934 MiB ± 7% | 6.851 k |

Each sample is one full `Lock → GetState → SaveState → Unlock` cycle against a ~1 MB synthetic `IaCState` (a 1024-entry `Outputs` map of 1 KiB string values). `_InProcess` calls the `memory` backend directly; `_GRPC` routes every call over a real in-memory `bufconn` gRPC boundary using the new `IaCStateBackend` service.

**Added latency (gRPC over in-process): ≈ 6.51 ms p50 per cycle.**

## The acceptance bar (plan Task 6, Step 3)

> "unary transport is accepted if the gRPC path's p50 added latency for the full 4-call cycle is **< 5 ms** over the in-process baseline. … If the bar is NOT met: do NOT proceed. The proto needs a streaming redesign for `GetState`/`SaveState` — revise Task 4's proto, regenerate, re-run this task."

**6.51 ms > 5 ms — the literal bar is not met.**

## Root-cause analysis — what the 6.51 ms actually is

Per cycle, for a 1 MB state:
- `Lock` / `Unlock`: trivial (a `resource_id` string each) — negligible.
- `GetState`: server-side `json.Marshal` of the ~1 MB `Outputs` map → proto-marshal the resulting `[]byte` → `bufconn` copy → client proto-unmarshal.
- `SaveState`: client sends a pre-built proto message (its JSON was marshalled **once**, outside the benchmark loop — so client-side JSON is amortised and *not* in the hot path) → `bufconn` copy → server proto-unmarshal → server-side `json.Unmarshal` of the ~1 MB `OutputsJson`.

The 4.9 MiB/op + 6.8 k allocs/op profile is dominated by **one `json.Marshal` + one `json.Unmarshal` of a ~1 MB `map[string]any` per cycle**. That cost is **inherent to the `bytes <name>_json` wire format** the `iac.proto:6-10` hard invariant mandates (no `google.protobuf.Struct`) — it is a *serialization-CPU* cost, not a gRPC *transport* cost.

**Why streaming (the plan's contingency remedy) does not fix this:** chunked-stream `GetState`/`SaveState` addresses two things — (a) the gRPC 4 MB default message-size cap, and (b) peak memory from buffering one large message. The benchmark hits *neither*: the 1 MB payload is well under the 4 MB cap, and 4.9 MiB/op peak is not memory-pressure. Streaming would still `json.Marshal`/`json.Unmarshal` the same 1 MB — just in pieces. **The streaming redesign would do significant work (new proto shape, new converters, new plugin-serve pattern, re-review of Task 4) and not move the measured number.**

## The conflict

- **Literal plan reading:** 6.51 ms > 5 ms → "do NOT proceed; streaming redesign."
- **Technical reality:** (1) streaming does not reduce the JSON-serialization CPU that *is* the 6.51 ms; (2) the design's own stated rationale for the bar (Architecture §1) is *"sub-5 ms per cycle is negligible against real cloud-provider API latency (hundreds of ms)"* — and **6.51 ms is also negligible against hundreds of ms**. A real `azure_blob` backend's `GetState`/`SaveState` is an Azure Blob GET/PUT of the state object — tens to hundreds of ms of network I/O — which dwarfs 6.51 ms of local serialization. (3) 1 MB is a deliberately stressful synthetic size; typical IaC state is far smaller and the per-cycle cost scales with state size.

The "5 ms" figure was a **pre-measurement estimate**; the post-measurement reality is "6.51 ms for a worst-case 1 MB state, dominated by a serialization cost streaming cannot remove."

## Recommendation

**Retain unary `IaCStateBackend`.** Do not do the streaming redesign — it is mis-targeted at the actual bottleneck and would not change the result. The unary proto from Task 4 stands.

Rationale, in order:
1. Streaming does not address the measured cost (JSON CPU, not transport buffering / message cap).
2. 6.51 ms remains negligible against the real cloud-provider backend I/O latency that the design's own bar-rationale invokes — the same logic that justified "< 5 ms" justifies "< ~10 ms for a stress-test payload."
3. If the operator wants the literal bar honored, the *correct* remedy is a **serialization-format** change (e.g. a more compact binary state encoding instead of `json` inside the `bytes` field), **not** a transport-shape change — and that is a separate design question, not Task 4-redo work.

## What this means for the locked plan

- Task 6 is **not** being skipped or dropped — the benchmark was run, analysed, and recorded (this file). No task is added or removed; no PR is collapsed. The scope lock is intact.
- Task 6's Step 3 contingency branch ("streaming redesign") rested on the unstated assumption that *if the bar is exceeded, streaming is the fix*. The measurement falsifies that assumption. This is a **finding within Task 6**, recorded in Task 6's own artifact — the place adversarial-review designates for recorded overrides.
- **This deviation from the literal 5 ms threshold is surfaced to the operator, not silently absorbed.** If the operator confirms the recommendation, stamp this file `Status: Unary LOCKED (operator-confirmed deviation from the 5 ms estimate-bar; see Root-cause analysis)` and PR 2 / Phase A proceed unchanged. If the operator wants the bar honored literally, the follow-up is a serialization-format spike, not a streaming redesign.

## Raw data

```
IaCStateBackend_InProcess-10   179.4n ± 1%      416.0 B ± 0%     2.000 allocs ± 0%
IaCStateBackend_GRPC-10        6.511m ± 1%    4.934Mi B ± 7%    6.851k allocs ± 0%
```
(10 samples each; full per-sample output in the Task 6 run log.)
