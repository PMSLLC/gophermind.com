# Performance baseline (05-06)

Measured on this development machine (Apple M3 Pro) via
`gophermind-osx/e2e_bench_test.go`'s real benchmarks (`//go:build e2e`),
each against a real `gophermind-server` instance -- no synthetic timing, no
mocks. Re-run these yourself to get a baseline for a different machine or
LLM endpoint; the numbers below are this machine's first recorded baseline,
not a fixed target.

## Message round-trip latency (`GET /healthz`)

```
go test -tags e2e ./gophermind-osx/... -run '^$' -bench 'BenchmarkHealthzLatency_Local'  -benchtime=20x
go test -tags e2e ./gophermind-osx/... -run '^$' -bench 'BenchmarkHealthzLatency_Remote' -benchtime=20x
```

| Path | Latency (ns/op) | Latency (approx) |
|---|---|---|
| Local (spawned subprocess, plain HTTP) | 174,848 | ~0.17 ms |
| Remote (through a real WireGuard tunnel) | 473,321 | ~0.47 ms |

**WG overhead: ~298 microseconds (~2.7x) per request**, isolated from any
LLM inference time by using the cheapest real endpoint (`/healthz`) both
paths share.

## Streaming throughput

```
go test -tags e2e ./gophermind-osx/... -run '^$' -bench 'BenchmarkStreamingThroughput_Local' -benchtime=1x
```

29.40 tokens/sec for one real chat turn ("count from one to fifty").

This number is dominated by the configured LLM's own generation speed, not
by this app's transport -- it is only comparable across runs against the
same model/endpoint, not a fixed absolute target. It exists to catch a
transport-side regression (e.g. a change that adds unnecessary buffering
or serialization between the model and the SSE writer), which would show
up as a throughput drop unexplained by a model change.

## Notes

- The healthz benchmarks are cheap and safe to run with `go test`'s
  default time-based iteration count; the streaming benchmark should be
  run with `-benchtime=1x` (or a small explicit count) since each
  iteration is a real LLM call.
- These benchmarks require `-tags e2e` and a reachable LLM endpoint
  (`GOPHERMIND_LLM_ENDPOINT`, or the shared config's `GOPHERMIND_BASE_URL`)
  the same way `gophermind-osx`'s other E2E tests do.
