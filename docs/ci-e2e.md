# CI E2E orchestration

Two workflows drive the E2E lanes in CI:

- [`e2e-reusable.yml`](../.github/workflows/e2e-reusable.yml) — the single
  authoritative workflow. Manual dispatches, the nightly, and eventually
  go-qrl/qrysm callers all run lanes through it.
- [`nightly.yml`](../.github/workflows/nightly.yml) — resolves the latest
  client revisions, fans the nightly lanes out over the reusable workflow, and
  publishes one combined result.

## The reusable workflow

One call runs one lane: check out qrl-tests, install the pinned Kurtosis CLI,
resolve or build the client images, `make e2e-run`, publish
`reports/summary.md` as the job summary, and upload the whole `reports/` tree
(run manifest, summaries, per-lane Ginkgo reports, diagnostics) as an
artifact. Every run uses a unique enclave name derived from the run id,
attempt, and lane.

Images resolve in this order:

1. An `*-image` input — an immutable reference (digest, or tag plus digest) —
   is used as supplied.
2. For the execution client only: with no image but a `go-qrl-ref`, the
   workflow checks out go-qrl and builds `local/go-qrl:devnet` from the
   repository's own Dockerfile, recording the built revision in the run
   manifest.
3. Anything still unset falls back to the harness defaults
   (`local/*:devnet`), which must already exist on the runner.

The Clef, consensus, validator, and genesis images currently have no
in-workflow build recipe; the nightly falls back to public, digest-pinned
GHCR references (linux/arm64), and repository variables override them with
any immutable reference. Wiring their builds in is a follow-up.

## Repository variables

Set under Settings → Secrets and variables → Actions → Variables; none are
secrets, and none are required — with nothing set the nightly runs on its
public fallbacks.

| Variable | Purpose |
| --- | --- |
| `E2E_RUNNER` | Runner label for lanes (defaults to `ubuntu-24.04-arm`) |
| `E2E_NIGHTLY_LANES` | JSON array of lanes the nightly runs |
| `E2E_CLEF_IMAGE` | Immutable Clef image reference |
| `E2E_CONSENSUS_IMAGE` | Immutable consensus client image reference |
| `E2E_VALIDATOR_IMAGE` | Immutable validator client image reference |
| `E2E_GENESIS_IMAGE` | Immutable genesis generator image reference |
| `E2E_QRL_PACKAGE_REF` | qrl-package locator override |
| `E2E_GO_QRL_REPOSITORY` | go-qrl repository (defaults to `cyyber/go-qrl`) |
| `E2E_QRYSM_REPOSITORY` | qrysm repository (defaults to `cyyber/qrysm`) |

One secret is optional: `DISCORD_E2E_WEBHOOK`, a Discord webhook URL the
nightly posts failures to (run link, resolved revisions, lanes). Store it as
a secret — anyone holding the URL can post to the channel — and leave it
unset to disable the notification.

## Reproducing a run

Every lane's artifact contains `run-manifest.json`: source revisions, exact
image references, the qrl-package locator, the Ginkgo seed per lane, tool
versions, and the GitHub run coordinates. To reproduce, dispatch
`e2e-reusable` with the recorded values — or locally:

```bash
DEVNET_EXECUTION_IMAGE=<from manifest> \
DEVNET_CONSENSUS_IMAGE=<from manifest> \
DEVNET_QRL_PACKAGE_REF=<from manifest> \
make e2e-run
```

Rerunning the nightly from the GitHub UI re-resolves the latest revisions; use
the reusable workflow with manifest values when you need the exact ones.

## Failure handling

- `reports/summary.json` classifies each lane: `bootstrap`,
  `infrastructure`, `timeout`, or `assertion`; unexpected skipped or pending
  specs fail the run.
- Failing lanes dump their network under `reports/diagnostics/<lane>/` before
  the enclave is destroyed (`E2E_DIAGNOSTICS` selects `always`/`never`).
- Cleanup always runs; a cleanup or diagnostics problem is reported alongside
  the test result and never replaces it.
- Enclaves live on the runner, so an abandoned run disappears with it.
