# Soak

`qrltest soak` provisions the `soak` profile (four participants, one per
labelled work node) and samples the network for `SOAK_DURATION`. It is not
an E2E lane. Gates live in
[`thresholds.yaml`](thresholds.yaml).

```bash
DEVNET_BACKEND=kubernetes \
DEVNET_ENDPOINT_MODE=cluster \
SOAK_DURATION=15m \
SOAK_ENFORCE=false \
make soak-run
```

`SOAK_ENFORCE=false` records every gate but always reports success, for
threshold calibration. `SOAK_LOAD_PERCENT=0` is an idle baseline.

Against an already-running soak network:

```bash
make soak
```

The command writes `results.json`, `verdict.json`, `samples.jsonl`,
`summary.md`, and `run-manifest.json` at the report root. `verdict.class` is
`passed`, `product`, or `infrastructure`. `results.json` carries
`thresholds_digest`, the qrl-tests commit, the package pin, and image
digests so week-over-week comparison can refuse a changed `thresholds.yaml`
and note any other provenance drift.

While the Job is running it annotates itself with phase (`provisioning`,
`warmup`, `steady`) plus finalized epoch, head slot, txs in the latest
block, and EL RSS. `soak-heartbeat` copies those onto the check run.

`soak-report` downloads the previous `soak-reports-*` artifact (14-day
retention covers the weekly cadence) and runs `qrltest soak-compare`. That
rewrites `summary.md` with per-metric deltas (missed-slot rate, canary p95,
RSS slope, FD/GC, and the other headline numbers) and writes
`comparison.json`. Comparison is skipped when either run is infrastructure
or the thresholds digest changed. Deltas are informational; they do not
fail the run.
