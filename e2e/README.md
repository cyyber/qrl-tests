# End-to-end suites

The suites run against a [development network](../devnet/README.md). The
runner passes a generated manifest containing the participant
endpoints; suites do not provision infrastructure.

## Lanes

| Lane | Profile | Coverage |
| --- | --- | --- |
| `execution` | `single` | Execution client: contract ABI, gqrl console, and transaction pool |
| `consensus-staking-automated` | `single` | Staking through contract deposits and the keymanager API |
| `consensus-staking-operator` | `single` | Staking through the deposit and validator CLIs |
| `consensus-penalties` | `spare-validator` | Penalties while a validator's client is down, and rewards once it is back |
| `consensus-slashing-double-vote` | `duplicate-validator` | Slashing for a double vote by a key running on two nodes at once, through the validator's exit |
| `consensus-slashing-double-proposal` | `double-proposal` | Slashing for a double proposal by a key running on two nodes at once |
| `consensus-slashing-evidence` | `duplicate-validator` | Slashing on evidence the suite signs itself: a proposer slashing submitted through the beacon API, a surround vote a slasher detects through gossip, and a double vote by a validator that has exited but cannot withdraw yet |
| `resilience-network-restart` | `anchored` | Recovery from a full outage: proposing and finalizing after every node stops at once |
| `sync` | `single` | A new full-mode node syncs from genesis or from a checkpoint, then catches up after a stop |

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

`resilience-network-restart` stops and restarts every service, so it runs only
on a network the runner provisions (`make e2e-run` or `run-all`), without a
parameters file.

The Ginkgo runner writes its JSON report, output log and resolved environment
manifest under `reports/lanes/<lane>/`, next to the run manifest and result
summaries at the report root. Every run records its Ginkgo seed in
`reports/run-manifest.json`; unexpected skipped or pending specs fail the run.
Inspect the registered lane and suites with `go run ./cmd/qrltest list`.

Files that register or execute live scenarios use the `e2e` build tag.
Deterministic fixture, encoding, and helper tests remain untagged so the default
`go test ./...` run continues to validate them without a network.

Build test inputs inside the suite when the building is the behavior under
test; shared client helpers and fixtures belong under `internal/`.
