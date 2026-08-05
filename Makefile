.PHONY: lint network-preflight network-start network-stop e2e e2e-run

GOLANGCI_LINT_VERSION ?= v2.12.2
DEVNET_BACKEND ?= docker
E2E_LANE ?= single
E2E_SUITE_ARGS := $(foreach suite,$(E2E_SUITE),--suite "$(suite)")

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run

network-preflight:
	@case "$(DEVNET_BACKEND)" in \
		docker) docker info >/dev/null 2>&1 || { echo "Docker is required and its daemon must be running" >&2; exit 1; } ;; \
		kubernetes) ;; \
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
