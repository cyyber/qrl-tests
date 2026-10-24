# Soak

`qrltest soak` provisions the `soak` profile (four participants, Prometheus
and Grafana, the metrics exporter and, under load, `tx_spammer`) on
Kurtosis's Docker backend and samples the network for `SOAK_DURATION`. It is
not an E2E lane. Gates live in [`thresholds.yaml`](thresholds.yaml).

```bash
SOAK_DURATION=15m \
SOAK_ENFORCE=false \
make soak-run
```

`SOAK_ENFORCE=false` records every gate but always reports success, for
threshold calibration. `SOAK_LOAD_PERCENT=0` is an idle baseline; the
default, 30, has the spammer fill 30% of each block's gas.
`SOAK_PARTICIPANTS` provisions fewer participants on a small host.

Gates whose verdict is a slope or a rate over time (RSS, heap, goroutine,
working-set and file-descriptor slopes, GC-pause slope, GC rate) report
`n/a` until the steady window spans `memory.min_window` (1 h): a few
minutes of growth extrapolated to an hour is noise, not a leak. Short
calibration runs therefore only judge the chain, peer, RPC, consensus,
canary and headroom gates.

Per-container working set and CPU come from the Docker Engine stats API,
computed as cAdvisor does (usage minus inactive page cache). The containers
run without memory limits, so the working-set headroom gate is not judged;
the working-set slopes are.

Against an already-running soak network:

```bash
make soak
```

The command writes `results.json`, `verdict.json`, `samples.jsonl`,
`summary.md`, and `run-manifest.json` at the report root, and logs one
progress line per sample (phase, finalized epoch, head slot, transactions
per slot, EL RSS). `verdict.class` is `passed` or `product`; a run that
writes no verdict never got a network to judge and counts as
infrastructure. `results.json` carries `thresholds_digest`, the qrl-tests
commit, the package pin, and image digests so week-over-week comparison can
refuse a changed `thresholds.yaml` and note any other provenance drift.

## CI

[`soak.yml`](../../.github/workflows/soak.yml) runs every Sunday at 03:00
UTC, and on `workflow_dispatch`, as one job on the GitHub-hosted runner the
E2E lanes use. It bakes the images, runs `make soak-run`, downloads the
previous `soak-reports-*` artifact (14-day retention covers the weekly
cadence) and runs `qrltest soak-compare`, then publishes `summary.md` as the
job summary and `reports/` as an artifact. The hosted runner's 6 h job limit
caps the window at 5 h.

`soak-compare` rewrites `summary.md` with per-metric deltas (missed-slot
rate, canary p95, RSS slope, FD/GC, and the other headline numbers) and
writes `comparison.json`. Comparison is skipped when the thresholds digest
changed. Deltas are informational; they do not fail the run.

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
