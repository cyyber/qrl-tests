# Soak lane

The `soak` lane provisions the `soak` profile (four participants, one per
labelled work node) and samples the network for `SOAK_DURATION`. Gates live in
[`internal/soak/thresholds.yaml`](../../../internal/soak/thresholds.yaml).

```bash
DEVNET_BACKEND=kubernetes \
DEVNET_ENDPOINT_MODE=cluster \
SOAK_DURATION=15m \
SOAK_ENFORCE=false \
make soak-run
```

`SOAK_ENFORCE=false` records every gate but always reports success, for
threshold calibration. `SOAK_LOAD_PERCENT=0` is an idle baseline.

On failure the suite writes `results.json` and `verdict.json` next to the
Ginkgo report. `verdict.class` is `product` or `infrastructure`.
