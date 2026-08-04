# Development network

This directory provides a reusable package and CLI for a separately managed,
Kurtosis-backed QRL development network. It supports the local Docker backend
and remote Kubernetes clusters with Kurtosis CLI 1.20.x.

## Run

```bash
make network-start
make network-stop
```

`network-start` uses the configured existing images, runs the pinned qrl-package,
and waits for readiness. It does not build images or run the test suites.

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

DEVNET_BACKEND=kubernetes make e2e E2E_LANE=single
make network-stop
```

Cluster image-pull credentials are managed outside this repository. The
Kubernetes path uses the selected Kurtosis context and the same SDK lifecycle
as Docker.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `DEVNET_BACKEND` | `docker` | Kurtosis backend: `docker` or `kubernetes` |
| `DEVNET_ENCLAVE_NAME` | `go-qrl-devnet` (CLI default) | Kurtosis enclave |
| `DEVNET_EXECUTION_IMAGE` | `local/go-qrl:devnet` | Existing execution image reference |
| `DEVNET_CLEF_IMAGE` | `local/go-qrl-clef:devnet` | Clef image |
| `DEVNET_CONSENSUS_IMAGE` | pinned Qrysm beacon image | Consensus client image |
| `DEVNET_VALIDATOR_IMAGE` | pinned Qrysm validator image | Validator client image |
| `DEVNET_GENESIS_IMAGE` | pinned QRL genesis image | Genesis generator image |
| `DEVNET_PROFILE` | `single` | Built-in network profile |
| `DEVNET_START_TIMEOUT` | `5m` (CLI default) | Network startup budget |
| `DEVNET_PARAMS_FILE` | unset | Complete qrl-package YAML parameters |

`DEVNET_ENCLAVE_NAME` is optional. Without it, every command uses
`go-qrl-devnet`. Set it only to use another enclave name, and use the same value
for each command in that lifecycle:

```bash
DEVNET_ENCLAVE_NAME=my-devnet make network-start
DEVNET_ENCLAVE_NAME=my-devnet make network-stop
```

Kurtosis restricts enclave names to letters, digits, and dashes. Operations using the same name
must run serially. Concurrent networks need different names. Networks testing
different client builds also need different image references.

## Custom parameters

`DEVNET_PARAMS_FILE` replaces the selected built-in profile with a complete
qrl-package YAML argument object. Existing JSON parameter files remain
supported. Exact scalar tokens are substituted:

```text
__DEVNET_EXECUTION_IMAGE__
__DEVNET_CLEF_IMAGE__
__DEVNET_CONSENSUS_IMAGE__
__DEVNET_VALIDATOR_IMAGE__
__DEVNET_GENESIS_IMAGE__
__DEVNET_WALLET_ADDRESS__
```

The first participant's `el_image` must use the execution-image token. The
other image tokens are optional and allow the same parameter file to select
Docker-local or registry images.
`network_params.prefunded_accounts` must contain the wallet token as a key; the
wallet token may also be used as a value, such as `withdrawal_address`.

The checked-in [`network_params.yaml`](network_params.yaml) is a complete
single-participant example using all tokens.

Start the network with the custom parameters:

```bash
DEVNET_PARAMS_FILE=devnet/network_params.yaml make network-start
```

The provisioned E2E runner accepts the same file:

```bash
DEVNET_PARAMS_FILE=devnet/network_params.yaml make e2e-run E2E_LANE=single
```

The controller discovers every execution, consensus, and validator participant
from qrl-package service labels. Consumers select the primary participant with
`Environment.Primary`; multi-node suites use `Environment.Participants`.
The reported GraphQL URL is live only if the profile enables GraphQL on the RPC
port. Readiness requires advancing blocks and a funded development wallet.

## Consumers

Go tooling can import `github.com/cyyber/qrl-tests/devnet` and call
`devnet.NewManager().Inspect(ctx, enclaveName, backend)` to discover the live
execution RPC, GraphQL, WebSocket, and consensus REST endpoints. The separately
maintained [end-to-end suites](../endtoend/README.md) are one consumer.

## Safety

The built-in profile funds a published development address used by readiness
checks and the migrated live suites. Its matching test seed is maintained by
the suite that signs transactions. Never fund or use this account outside
disposable local development networks.

Failed provisioning removes the enclave created by that start attempt. It does
not remove a pre-existing enclave with the requested name.

Parallel networks must use distinct `DEVNET_ENCLAVE_NAME` values and report
directories.
