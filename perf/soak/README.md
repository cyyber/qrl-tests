# Soak

`qrltest soak` provisions the `soak` profile (four participants, one per
labelled work node) and samples the network for `SOAK_DURATION`. It is not
an E2E lane. Gates live in
[`internal/soak/thresholds.yaml`](../../internal/soak/thresholds.yaml).

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

The command writes `results.json`, `verdict.json`, `samples.jsonl`, and
`summary.md` at the report root. `verdict.class` is `passed`, `product`, or
`infrastructure`.
