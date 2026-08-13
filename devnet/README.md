# Development network

This directory provides a reusable package for a Kurtosis-backed QRL
development network, driven by the `qrltest` CLI in
[`cmd/qrltest`](../cmd/qrltest). It supports Docker and Kubernetes backends
with Kurtosis CLI 1.20.x.

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

In another terminal, start the network with a parameters file containing image
references available to the cluster:

```bash
DEVNET_BACKEND=kubernetes \
DEVNET_PARAMS_FILE=/path/to/network_params.yaml \
make network-start

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
| `DEVNET_CONSENSUS_IMAGE` | `local/qrysm-beacon:devnet` | Consensus client image reference |
| `DEVNET_VALIDATOR_IMAGE` | `local/qrysm-validator:devnet` | Validator client image reference |
| `DEVNET_GENESIS_IMAGE` | `local/qrl-genesis-generator:devnet` | Genesis generator image reference |
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

Image references may name a tag, a digest, or both
(`registry.example/go-qrl:v1@sha256:<64 hex>`); malformed references fail the
start before any enclave is created. The qrl-package revision is pinned in code
(`devnet.PackageLocator`) and is not configurable.

## Custom parameters

`DEVNET_PARAMS_FILE` replaces the selected built-in profile with a complete
qrl-package YAML argument object. The file is used unchanged, including its
image references and development wallet address. Existing JSON parameter files
remain supported. Image flags and `DEVNET_PROFILE` are ignored when the file is
set.

The checked-in [`network_params.yaml`](network_params.yaml) is a complete
single-participant example. Kubernetes configurations must reference images the
cluster can resolve — registry-backed, or preloaded onto the nodes (for
example with `kind load docker-image`) — instead of the Docker-local image
defaults. Custom files
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
