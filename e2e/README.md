# End-to-end suites

The suites run against a [development network](../devnet/README.md). The
runner passes a generated manifest containing the participant
endpoints; suites do not provision infrastructure.

## Lanes

| Lane | Profile | Coverage |
| --- | --- | --- |
| `execution` | `single` | Execution client: contract ABI, gqrl console, and transaction pool |
| `consensus-staking-automated` | `single` | Staking through contract deposits and the keymanager API |
| `consensus-staking-operator` | `single` | Staking through the deposit and validator CLIs |
| `consensus-remote-signer` | `remote-signer` | Validator duties signed by go-remotesigner: its keys and chain, finality, its metrics, refused slashable requests, a key removed and added back through the remote keys API, and a refused exit; needs locally built images |
| `consensus-remote-signer-mtls` | `remote-signer-mtls` | The same, with the validator client presenting a client certificate as well as the token; needs locally built images and a qrl-package that issues the certificate |
| `consensus-remote-signer-exits` | `remote-signer-exits` | The same, with go-remotesigner signing voluntary exits: the last spec exits a validator through it; needs locally built images |
| `resilience-network-restart` | `anchored` | Recovery from a full outage: proposing and finalizing after every node stops at once |
| `sync` | `single` | A new full-mode node syncs from genesis, then catches up after a stop |

Run one lane with a fresh network:

```bash
make e2e-run
```

Run a lane against an existing matching network:

```bash
make network-start
make e2e
make network-stop
```

`resilience-network-restart` stops and restarts every service, so it runs only
on a network the runner provisions (`make e2e-run` or `run-all`), without a
parameters file.

## Remote signer lane

`consensus-remote-signer` runs a validator client that holds no keys: every
duty is signed by go-remotesigner, which loads the validator client's
keystores, over TLS with a bearer token. The suite checks that the signer
serves exactly the validator client's keys and is bound to its chain, that the
validator client has no keystore, that the chain finalizes, that the signer's
metrics show every duty type signed, a RANDAO reveal for every block and no
refusal or fault, that it refuses a double vote and a double proposal sent to
it directly, and that the chain keeps finalizing afterwards.

Then it removes one validator's key through the validator client's remote
keys API (`/qrl/v1/remotekeys`), after checking that the API lists exactly
the signer's keys: the key file loses the key, the signer gets every
attestation of the next epoch but that key's, and its vote is missing on
chain. It adds the key back with a url of its own, which qrysm must ignore:
the key file has the key again, the signer gets every attestation of the
next epoch, the key's vote is on chain, and the signer gets no request for
that url. Last, it runs qrysm's exit command, `validator accounts
voluntary-exit`, next to the validator client with its remote signer flags
and its chain config, for another validator. The signer refuses it with 403
`ARTIFACT_TYPE_DISABLED`, as it refuses every exit unless it runs with
`--voluntary-exit-signing-enabled`, and the command fails, naming that
option, and submits nothing. `consensus-remote-signer-exits` runs the same
suite on the `remote-signer-exits` profile, whose signer runs with the
option: the beacon node accepts the exit, and the validator is
`active_exiting`. An exit cannot be undone, so it comes last.

When a spec fails, the signer's metrics, healthcheck and capabilities, and
the validator client's remote signer metrics, are saved under
`reports/lanes/<lane>/remote-signer/<spec>/`.

Its images are not published yet, and the pinned qrl-package does not run
go-remotesigner, so the nightly does not run the lane, `run-all` leaves it
out, and `qrltest list` marks it; the
[remote signer nightly](#remote-signer-nightly) builds the images and runs it.
Build the images from local checkouts, the validator client and beacon node
from one qrysm commit with the remote signer client, its remote keys API and
its exits through the signer:

```bash
REMOTESIGNER_SOURCE_DIR=../go-remotesigner make remote-signer-image
QRYSM_SOURCE_DIR=../qrysm QRYSM_REF=<commit> make qrysm-images
```

The scripts build the working tree of the checkout, or the commit
`REMOTESIGNER_REF` or `QRYSM_REF` names. The signer image is built with
go-remotesigner's own Dockerfile and needs `go-licenses` v2; the qrysm images
are static `go build`s, without Bazel.

Then start the network from a qrl-package checkout that runs go-remotesigner
and serves qrysm's keymanager API on the validator client's gRPC gateway,
with the parameters of the `remote-signer` profile, and run the lane against
it. The network gets an enclave of its own, which `kurtosis enclave add`
creates and refuses when the name is taken, and its parameters go to the
ignored `reports/` directory:

```bash
mkdir -p reports
go run ./cmd/qrltest network params --profile remote-signer \
	--consensus-image local/qrysm:remote-signer-beacon-chain \
	--validator-image local/qrysm:remote-signer-validator \
	> reports/remote-signer.json
kurtosis enclave add --name remote-signer-devnet &&
	kurtosis run --enclave remote-signer-devnet ../qrl-package \
		--args-file reports/remote-signer.json
DEVNET_ENCLAVE_NAME=remote-signer-devnet \
E2E_LANE=consensus-remote-signer make e2e
DEVNET_ENCLAVE_NAME=remote-signer-devnet make network-stop
```

`consensus-remote-signer-exits` runs the same way on a network of its own,
with `--profile remote-signer-exits` and
`E2E_LANE=consensus-remote-signer-exits`.

To record the commits the images were built from in
`reports/run-manifest.json`, set `E2E_SOURCE_REMOTESIGNER` and
`E2E_SOURCE_QRYSM` for `make e2e`. Once the pinned qrl-package runs
go-remotesigner, `make e2e-run` with `DEVNET_CONSENSUS_IMAGE` and
`DEVNET_VALIDATOR_IMAGE` naming the qrysm images provisions the network
itself.

`consensus-remote-signer-mtls` runs the same suite on the
`remote-signer-mtls` profile, whose signer runs with
`--http-auth-mode=token+mtls`: the validator client presents a client
certificate as well as the token, the suite presents the validator client's
certificate too, and it checks that the signer answers no request without
one, not even `/upcheck`. Its parameters come from `qrltest network params
--profile remote-signer-mtls`, which set the participant field
`remote_signer_auth_mode: token+mtls`; no qrl-package has the field yet. A
qrl-package that has it must, for a go-remotesigner participant:

- issue a client certificate (`extendedKeyUsage=clientAuth`) from the CA that
  issued the signer's certificate, and give it and its key (`0600`) to the
  validator client as `/remote-signer/client-cert.pem` and
  `/remote-signer/client-key.pem`;
- give the signer that CA, and run it with `--http-auth-mode=token+mtls` and
  `--tls-client-ca-file` naming it;
- run the validator client with
  `--validators-external-signer-tls-client-cert-file` and
  `--validators-external-signer-tls-client-key-file` naming the two files;
- present the certificate in its own readiness check of the signer, since
  every route needs it.

### Remote signer nightly

[`remote-signer-nightly.yml`](../.github/workflows/remote-signer-nightly.yml)
runs the remote-signer lanes every night and on demand. It builds the signer
image from go-remotesigner and the beacon node and validator client images
from qrysm with the scripts above, at pinned commits, and takes the execution
client and genesis generator images from the nightly's registry, building
them when they are missing. It starts each lane's network from a qrl-package
checkout and runs the lane with `make e2e`; `pinned` as the qrl-package
revision runs `make e2e-run` on the qrl-package qrl-tests pins instead, once
that runs go-remotesigner. The revisions are inputs of a manual run, then
repository variables, then the pinned defaults:

| Source | Input | Variable | Default |
| --- | --- | --- | --- |
| go-remotesigner | `go-remotesigner-repository`, `go-remotesigner-ref` | `E2E_REMOTE_SIGNER_GO_REMOTESIGNER_REPOSITORY`, `E2E_REMOTE_SIGNER_GO_REMOTESIGNER_REF` | `rgeraldes24/go-remotesigner` at `8447c2d29add5f52b5157038bc6b74bd0ecef36b` |
| qrysm | `qrysm-repository`, `qrysm-ref` | `E2E_REMOTE_SIGNER_QRYSM_REPOSITORY`, `E2E_REMOTE_SIGNER_QRYSM_REF` | `rgeraldes24/qrysm` at `d026800cb2337ac1c7564968730e0587f5bc1161` |
| qrl-package | `qrl-package-repository`, `qrl-package-ref` | `E2E_REMOTE_SIGNER_QRL_PACKAGE_REPOSITORY`, `E2E_REMOTE_SIGNER_QRL_PACKAGE_REF` | `rgeraldes24/qrl-package` at `f68c0523e2b46f8afeaed4943361afed2f075df3` |
| go-qrl | | the nightly's `E2E_GO_QRL_REPOSITORY`, `E2E_REMOTE_SIGNER_GO_QRL_REF` | `cyyber/go-qrl` at `main` |
| qrl-genesis-generator | | the nightly's `E2E_GENERATOR_REPOSITORY`, `E2E_REMOTE_SIGNER_GENERATOR_REF` | `cyyber/qrl-genesis-generator` at `main` |
| the generator's qrysm | | the nightly's `E2E_QRYSM_REPOSITORY`, `E2E_REMOTE_SIGNER_GENERATOR_QRYSM_REF` | `cyyber/qrysm` at `main` |

A ref names a branch, a tag or a commit by its full 40-character SHA, which
is all a remote resolves; every job runs the commit it resolved to. The
workflow's own variables all start with `E2E_REMOTE_SIGNER_`; one misspelt is
not an error, it leaves the default in force.

go-qrl and the genesis generator, and the qrysm the generator is built with,
follow main by default, as in the nightly. A run on the pinned signer, qrysm
and qrl-package can still break, or change, with their main branches; pin
them with the three variables to rule that out.

Each lane uploads its reports, with `reports/sources.md` naming every source
revision: the run manifest has neither the qrl-package of a network the job
starts itself nor the qrysm the genesis generator was built with, and its
`qrysm` is the beacon node's and validator client's. On a failure the reports
also hold the services' logs, and the signer's metrics, healthcheck and
capabilities for each failed spec. When a job fails, is cancelled or times
out they also hold a dump of the enclave without its files artifacts, which
hold the keys and the token, and the signer's metrics, so that a network that
did not start is diagnosed too.

The lanes are the JSON list of the `lanes` input, then of the
`E2E_REMOTE_SIGNER_LANES` variable, then `["consensus-remote-signer"]`: mTLS
is not covered until a qrl-package issues client certificates and the
variable names `consensus-remote-signer-mtls` too. Before it builds any image,
each lane job checks that the qrl-package takes every participant field the
lane's profile sets, so a lane whose qrl-package lacks
`remote_signer_auth_mode` fails in seconds.

GitHub runs a scheduled workflow, and offers a manual run of one, only once
the workflow is on the default branch: the nightly runs, and so three
consecutive green ones, can only start after it is merged.

The Ginkgo runner writes its JSON report, output log and resolved environment
manifest under `reports/lanes/<lane>/`, next to the run manifest and result
summaries at the report root. Every run records its Ginkgo seed in
`reports/run-manifest.json`; unexpected skipped or pending specs fail the run.
Inspect the registered lane and suites with `go run ./cmd/qrltest list`.

Files that register or execute live scenarios use the `e2e` build tag.
Deterministic fixture, encoding, and helper tests remain untagged so the default
`go test ./...` run continues to validate them without a network.

Build test inputs inside the suite when the building is the behavior under
test; shared client helpers and fixtures belong under `internal/`.
