# QRL Tests

`qrl-tests` provisions a pinned Kurtosis network and runs the QRL execution ABI
suite through public RPC and WebSocket interfaces.

## Run

Point the harness at the go-qrl checkout used to build the execution image and
helper binaries:

```bash
export GO_QRL_SOURCE_DIR=/path/to/go-qrl

make test
make e2e-compile
make e2e-run
```

The runner creates a temporary Go workspace containing this checkout and
`GO_QRL_SOURCE_DIR`. Suite binaries therefore compile against the exact local
go-qrl tree, including uncommitted changes.

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
