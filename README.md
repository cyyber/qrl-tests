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
├── run-manifest.json         # provenance and replay metadata for the run
├── summary.json              # spec counts and classified failures
├── summary.md                # Markdown result summary
└── lanes/<lane>/
    ├── report.json           # Ginkgo test report
    ├── output.log            # test command output
    ├── manifest.json         # lane, profile, and network environment
    └── diagnostics/          # created only when the lane fails
        ├── diagnostics.json   # capture status and errors
        ├── inspect.txt        # Kurtosis enclave inspection
        └── services/
            └── <service>.log  # log for each discovered service
```

Diagnostics are collected for post-creation start failures and for test or
harness failures after a network is acquired. Runner-owned networks are
diagnosed before cleanup, while attached networks are left running. For
cleanup-only failures, diagnostics are collected after the failed cleanup
attempt. Diagnostic and cleanup errors do not replace the original failure.

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
