# QRL Tests

`qrl-tests` provisions pinned Kurtosis networks and runs end-to-end test suites
across the QRL execution and consensus stack.

## Run

```bash
make test
make e2e-compile
make e2e-run
```

The configured client images must already be available to the selected Kurtosis
backend. To compile the suites against a local go-qrl checkout, set
`GO_QRL_SOURCE_DIR=/path/to/go-qrl`; otherwise the module dependency is used.

`e2e-run` provisions the single-participant network, runs the ABI suite, and
removes the network.

Image builds are separate, explicit operations for local development:

```bash
GO_QRL_SOURCE_DIR=/path/to/go-qrl make network-image clef-image
```

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
