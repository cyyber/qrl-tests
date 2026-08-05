# Development network

This directory provides a reusable package and CLI for a separately managed,
Kurtosis-backed QRL development network. It supports Docker and Kubernetes
backends with Kurtosis CLI 1.20.x.

## Run

```bash
make network-start
make network-stop
```

`network-start` uses the configured existing images, runs the pinned qrl-package,
and waits for readiness. It does not build images or run the test suites.

## Kubernetes

For Kubernetes, select the Kurtosis cluster and run its gateway:

```bash
kurtosis cluster set <cluster>
kurtosis gateway
```

In another terminal, start the network using images available to the cluster:

```bash
DEVNET_EXECUTION_IMAGE=registry.example/go-qrl:test \
DEVNET_CLEF_IMAGE=registry.example/go-qrl-clef:test \
DEVNET_BACKEND=kubernetes make network-start

DEVNET_BACKEND=kubernetes make e2e
make network-stop
```

The configured images and image-pull credentials must be available to the
cluster. The commands use the currently selected Kurtosis context.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `DEVNET_BACKEND` | `docker` | Kurtosis backend: `docker` or `kubernetes` |
| `DEVNET_ENCLAVE_NAME` | `go-qrl-devnet` | Kurtosis enclave |
| `DEVNET_EXECUTION_IMAGE` | `local/go-qrl:devnet` | Execution client image reference |
| `DEVNET_CLEF_IMAGE` | `local/go-qrl-clef:devnet` | Clef signer image reference |
| `DEVNET_CONSENSUS_IMAGE` | pinned Qrysm beacon image | Consensus client image reference |
| `DEVNET_VALIDATOR_IMAGE` | pinned Qrysm validator image | Validator client image reference |
| `DEVNET_GENESIS_IMAGE` | pinned QRL genesis image | Genesis generator image reference |
| `DEVNET_PROFILE` | `single` | Built-in profile used by `network-start` |
| `DEVNET_START_TIMEOUT` | `5m` | Network startup budget |
| `DEVNET_PARAMS_FILE` | unset | Complete qrl-package YAML parameters |

`e2e-run` derives its network profile from `E2E_LANE` and does not use
`DEVNET_PROFILE`.

`DEVNET_ENCLAVE_NAME` is optional. Without it, every command uses
`go-qrl-devnet`. Set it only to use another enclave name, and use the same value
for each command in that lifecycle:

```bash
DEVNET_ENCLAVE_NAME=my-devnet make network-start
DEVNET_ENCLAVE_NAME=my-devnet make network-stop
```

Kurtosis restricts enclave names to letters, digits, and dashes. Operations
using the same name must run serially. Concurrent networks need different names.
Networks testing different client builds also need different image references.

## Custom parameters

`DEVNET_PARAMS_FILE` replaces the selected built-in profile with a complete
qrl-package YAML argument object. The file is used unchanged, including its
image references and development wallet address. Existing JSON parameter files
remain supported. Image flags and `DEVNET_PROFILE` are ignored when the file is
set.

The checked-in [`network_params.yaml`](network_params.yaml) is a complete
single-participant example. Kubernetes configurations must use registry-backed
images instead of the Docker-local execution and Clef defaults. Custom files
must pre-fund the checked-in development wallet used by readiness checks and
the E2E suites.

Start the network with the custom parameters:

```bash
DEVNET_PARAMS_FILE=devnet/network_params.yaml make network-start
```

The provisioned E2E runner accepts the same file:

```bash
DEVNET_PARAMS_FILE=devnet/network_params.yaml make e2e-run
```

## Go API

Go code can import `github.com/cyyber/qrl-tests/devnet` and call
`devnet.NewManager().Inspect(ctx, enclaveName, backend)` to discover the live
execution RPC, GraphQL, WebSocket, and consensus REST endpoints.

Inspection discovers every execution, consensus, and validator participant from
qrl-package service labels. Use `Environment.Primary` for the primary
participant and `Environment.Participants` for multi-node suites. The GraphQL
URL is available only when the selected profile enables GraphQL. Readiness
requires advancing blocks and a funded development wallet.

## Safety

The built-in profile funds a published development address used by readiness
checks and the migrated live suites. Its matching test seed is maintained by
the suite that signs transactions. Never fund or use this account outside
disposable local development networks.

Failed provisioning removes the enclave created by that start attempt. It does
not remove a pre-existing enclave with the requested name.
