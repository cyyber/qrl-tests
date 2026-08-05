# QRL Tests

`qrl-tests` provisions pinned Kurtosis networks and runs end-to-end test suites
across the QRL execution and consensus stack.

## Run

```bash
go test ./...
go test -tags=e2e -run '^$' ./endtoend/...
make e2e-run
```

The configured client images must already be available to the selected Kurtosis
backend. Suite binaries compile against the client dependencies pinned in
`go.mod`.

`e2e-run` provisions the single-participant network, runs the ABI suite, and
removes the network.

For iterative work, keep a network running:

```bash
make network-start
make e2e
make network-stop
```

List the registered lane and suite with
`go run ./cmd/qrltest list`. Reports are written under `reports/<lane>/`.

## Kubernetes

The same runner supports Kurtosis on Docker and Kubernetes. Kubernetes runs
must use registry-backed images and an active Kurtosis gateway:

```bash
DEVNET_BACKEND=kubernetes \
DEVNET_EXECUTION_IMAGE=registry.example/go-qrl:test \
DEVNET_CLEF_IMAGE=registry.example/go-qrl-clef:test \
make e2e-run
```

Use distinct `DEVNET_ENCLAVE_NAME` and `E2E_REPORT_DIR` values for concurrent
networks.

See [development network configuration](devnet/README.md) and the
[ABI suite](endtoend/suites/execution/abi/README.md).
