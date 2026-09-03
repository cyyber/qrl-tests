# syntax=docker/dockerfile:1
# The harness as an image, for lanes that run as in-cluster Kubernetes Jobs.
# qrltest shells out to `go tool ginkgo` against this checkout, so the image
# is the Go toolchain plus the source with its module and build caches warm.
FROM golang:1.26-bookworm

ARG KURTOSIS_VERSION=1.20.0
ARG KUBECTL_VERSION=v1.33.4

RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates curl jq \
	&& rm -rf /var/lib/apt/lists/*

# Same release artifacts and checksums the GitHub-hosted lanes install.
RUN set -eu; \
	case "$(dpkg --print-architecture)" in \
		arm64) arch=arm64; kurtosis_sha256=8c5f50adbb27ca2d0693b9ac8643a148d55240cfdbc5b36fa8eef4b02325af16 ;; \
		amd64) arch=amd64; kurtosis_sha256=3866e805a83a63d3d0b177f54c3fb556066b8c0e12f6442a0579bb7deaa1d5ae ;; \
		*) echo "unsupported architecture" >&2; exit 1 ;; \
	esac; \
	curl -fsSL --retry 3 --retry-connrefused -o /tmp/kurtosis.tar.gz \
		"https://github.com/kurtosis-tech/kurtosis-cli-release-artifacts/releases/download/${KURTOSIS_VERSION}/kurtosis-cli_${KURTOSIS_VERSION}_linux_${arch}.tar.gz"; \
	echo "${kurtosis_sha256}  /tmp/kurtosis.tar.gz" | sha256sum -c -; \
	tar -xzf /tmp/kurtosis.tar.gz -C /usr/local/bin kurtosis; \
	rm /tmp/kurtosis.tar.gz; \
	curl -fsSL --retry 3 --retry-connrefused -o /usr/local/bin/kubectl \
		"https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${arch}/kubectl"; \
	curl -fsSL --retry 3 --retry-connrefused -o /tmp/kubectl.sha256 \
		"https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${arch}/kubectl.sha256"; \
	echo "$(cat /tmp/kubectl.sha256)  /usr/local/bin/kubectl" | sha256sum -c -; \
	chmod 0755 /usr/local/bin/kubectl; \
	rm /tmp/kubectl.sha256

# Unprivileged runtime user; Kurtosis and kubectl keep their state under HOME.
RUN useradd --create-home --uid 10001 --shell /bin/bash runner \
	&& mkdir -p /src /reports /var/lib/dbus \
	&& printf '%s\n' "$(printf qrl-tests-runner | sha256sum | cut -c1-32)" >/etc/machine-id \
	&& cp /etc/machine-id /var/lib/dbus/machine-id \
	&& chown runner:runner /src /reports

USER runner
ENV HOME=/home/runner \
	GOPATH=/home/runner/go \
	GOCACHE=/home/runner/.cache/go-build \
	GOFLAGS=-buildvcs=false \
	QRL_TESTS_SOURCE_DIR=/src \
	E2E_REPORT_DIR=/reports \
	KURTOSIS_VERSION=${KURTOSIS_VERSION}

WORKDIR /src

COPY --chown=runner:runner go.mod go.sum ./
RUN go mod download

COPY --chown=runner:runner . .

# Compile the harness and every suite once so a Job spends its start budget
# on the network, not on the Go compiler.
RUN go build -o "${HOME}/go/bin/qrltest" ./cmd/qrltest
RUN go test -tags=e2e -run '^$' ./e2e/...
RUN go tool ginkgo version

ENV PATH=/home/runner/go/bin:${PATH}

ENTRYPOINT ["/src/ci/images/runner/entrypoint.sh"]
