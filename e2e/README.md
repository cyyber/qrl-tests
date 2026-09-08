# End-to-end suites

The suites run against a [development network](../devnet/README.md). The
runner passes a generated manifest containing the participant
endpoints; suites do not provision infrastructure.

## Lanes

| Lane | Profile | Coverage |
| --- | --- | --- |
| `execution` | `single` | Execution ABI calls, events, errors, and WebSocket filters |

Run one lane with a fresh network:

```bash
make e2e-run
```

Run a lane against an existing matching network:

```bash
make network-start
make e2e
make network-stop
```

The Ginkgo runner writes its JSON report, output log and resolved environment
manifest under `reports/lanes/<lane>/`, next to the run manifest and result
summaries at the report root. Every run records its Ginkgo seed in
`reports/run-manifest.json`; unexpected skipped or pending specs fail the run.
Inspect the registered lane and suites with `go run ./cmd/qrltest list`.

The multi-hour soak is a separate command (`qrltest soak`), documented in
[`perf/soak/README.md`](../perf/soak/README.md).

Files that register or execute live scenarios use the `e2e` build tag.
Deterministic fixture, encoding, and helper tests remain untagged so the default
`go test ./...` run continues to validate them without a network.

Build test inputs inside the suite when the building is the behavior under
test; shared client helpers and fixtures belong under `internal/`.
