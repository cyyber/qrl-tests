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

Gates whose verdict is a slope or a rate over time (RSS, heap, goroutine,
working-set and file-descriptor slopes, GC-pause slope, GC rate) report
`n/a` until the steady window spans `memory.min_window` (1 h): a few
minutes of growth extrapolated to an hour is noise, not a leak. Short
calibration runs therefore only judge the chain, peer, RPC, consensus,
canary and headroom gates.

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
block, and EL RSS. An ephemeral self-hosted runner on the core pool holds
the `watch` job so the soak Actions run stays in progress without GitHub-
hosted minutes. `watch` copies those annotations into the job log and the
`soak-cluster` check. `ACTIONS_RUNNER_PAT` (repo Administration: read/write) is
required so submit can register that runner.

`soak-report` downloads the previous `soak-reports-*` artifact (14-day
retention covers the weekly cadence) and runs `qrltest soak-compare`. That
rewrites `summary.md` with per-metric deltas (missed-slot rate, canary p95,
RSS slope, FD/GC, and the other headline numbers) and writes
`comparison.json`. Comparison is skipped when either run is infrastructure
or the thresholds digest changed. Deltas are informational; they do not
fail the run.

Each row carries its own unit (memory and working-set slopes in MB/h,
file descriptors in /h, GC pause in ms/h, GC rate in GC/h, rates in
percent, latencies in seconds). A change is a percentage when the
previous value is above the metric's noise floor (8 MB/h, 10 FDs/h,
5 ms/h, 600 GC/h, 0.5 percentage points, 1 s, 0.5 blocks/min, one epoch or
sample); below it the absolute difference is shown instead, so a 0.45 ms/h
baseline never reads as "+18000%". A row is labelled `worse` only when the
change exceeds both that floor and 10% in the harmful direction. A gate
that was `n/a` in either run shows `n/a` instead of a number, and steady
windows are compared to whole seconds with a tolerance of 30 s or 5%.
