.PHONY: e2e-compile network-preflight network-start network-stop e2e e2e-run e2e-all

DEVNET_BACKEND ?= docker
DEVNET_EXECUTION_IMAGE ?= local/go-qrl:devnet
DEVNET_CLEF_IMAGE ?= local/go-qrl-clef:devnet
DEVNET_CONSENSUS_IMAGE ?=
DEVNET_VALIDATOR_IMAGE ?=
DEVNET_GENESIS_IMAGE ?=
DEVNET_ENCLAVE_NAME ?= go-qrl-devnet
DEVNET_PROFILE ?= single
DEVNET_START_TIMEOUT ?= 5m
DEVNET_PARAMS_FILE := $(if $(strip $(DEVNET_PARAMS_FILE)),$(abspath $(DEVNET_PARAMS_FILE)))
E2E_LANE ?= single
E2E_SUITE ?=
E2E_REPORT_DIR ?= reports
E2E_MAX_PARALLEL ?= 1
E2E_SUITE_ARGS := $(foreach suite,$(E2E_SUITE),--suite "$(suite)")

export DEVNET_BACKEND DEVNET_EXECUTION_IMAGE DEVNET_CLEF_IMAGE
export DEVNET_CONSENSUS_IMAGE DEVNET_VALIDATOR_IMAGE DEVNET_GENESIS_IMAGE
export DEVNET_ENCLAVE_NAME DEVNET_PROFILE DEVNET_START_TIMEOUT DEVNET_PARAMS_FILE
export E2E_REPORT_DIR E2E_MAX_PARALLEL

e2e-compile:
	go test -tags=e2e -run '^$$' ./endtoend/...

network-preflight:
	@case "$(DEVNET_BACKEND)" in \
		docker) docker info >/dev/null 2>&1 || { echo "Docker is required and its daemon must be running" >&2; exit 1; } ;; \
		kubernetes) case "$(DEVNET_EXECUTION_IMAGE) $(DEVNET_CLEF_IMAGE) $(DEVNET_CONSENSUS_IMAGE) $(DEVNET_VALIDATOR_IMAGE) $(DEVNET_GENESIS_IMAGE)" in *local/*) echo "Kubernetes requires registry image references; override all local DEVNET_*_IMAGE values" >&2; exit 1 ;; esac ;; \
		*) echo "DEVNET_BACKEND must be docker or kubernetes" >&2; exit 2 ;; \
	esac
	@kurtosis version 2>/dev/null | grep -Eq '^CLI Version:[[:space:]]+1\.20\.' || { \
		echo "Kurtosis CLI 1.20.x is required (https://docs.kurtosis.com/upgrade)" >&2; \
		exit 1; \
	}
	kurtosis engine start

network-start: network-preflight
	go run ./cmd/qrltest network start

network-stop:
	go run ./cmd/qrltest network stop

e2e:
	go run ./cmd/qrltest test $(E2E_SUITE_ARGS) "$(E2E_LANE)"

e2e-run: network-preflight
	go run ./cmd/qrltest run $(E2E_SUITE_ARGS) "$(E2E_LANE)"

e2e-all: network-preflight
	go run ./cmd/qrltest run-all
