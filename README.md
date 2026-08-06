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
`reports/<lane>/`, and removes the network.

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
