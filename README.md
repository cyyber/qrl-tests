# QRL Tests

`qrl-tests` provisions pinned Kurtosis networks and runs end-to-end tests across
the QRL execution and consensus stack.

## Run

```bash
go test ./...
make e2e-run
```

The configured client images must already be available to the selected Kurtosis
backend.

`e2e-run` provisions a network, runs the selected E2E lane, writes reports under
`reports/`, and removes the network:

```
reports/
├── run-manifest.json     # provenance and replay metadata for the run
├── summary.json          # spec counts and classified failures
├── summary.md            # Markdown result summary
└── lanes/<lane>/         # Ginkgo reports, output log, and resolved environment manifest
```

For iterative development:

```bash
make network-start
make e2e
make network-stop
```

List registered lanes and suites with:

```bash
go run ./cmd/qrltest list
```

Docker and Kubernetes are supported. Kubernetes requires registry-backed images
and an active Kurtosis gateway.

See [development network configuration](devnet/README.md) and the
[end-to-end suites](e2e/README.md).
