# Self-contained genesis generator image built entirely from the resolved
# sources: a Go stage builds the qrysm tooling, a venv stage builds the
# Python environment with its compilers, and the runtime stage keeps only
# what the entrypoint executes — no published generator image is involved.
ARG GENESIS_GO_BUILDER_IMAGE=scratch
ARG GENESIS_RUNTIME_BASE_IMAGE=scratch

FROM ${GENESIS_GO_BUILDER_IMAGE} AS qrysm-builder

ARG QRYSM_GIT_REPO
ARG QRYSM_GIT_COMMIT
ARG GENERATOR_GIT_REPO
ARG GENERATOR_GIT_COMMIT

RUN test -n "${QRYSM_GIT_REPO}" && test -n "${QRYSM_GIT_COMMIT}" && \
    test -n "${GENERATOR_GIT_REPO}" && test -n "${GENERATOR_GIT_COMMIT}" && \
    printf '%s\n' "${QRYSM_GIT_COMMIT}" | grep -Eq '^[0-9a-f]{40}$' && \
    printf '%s\n' "${GENERATOR_GIT_COMMIT}" | grep -Eq '^[0-9a-f]{40}$' && \
    git init /src/qrysm && \
    git -C /src/qrysm remote add origin "${QRYSM_GIT_REPO}" && \
    git -C /src/qrysm fetch --depth 1 origin "${QRYSM_GIT_COMMIT}" && \
    git -C /src/qrysm checkout --detach FETCH_HEAD && \
    test "$(git -C /src/qrysm rev-parse HEAD)" = "${QRYSM_GIT_COMMIT}" && \
    git init /src/generator && \
    git -C /src/generator remote add origin "${GENERATOR_GIT_REPO}" && \
    git -C /src/generator fetch --depth 1 origin "${GENERATOR_GIT_COMMIT}" && \
    git -C /src/generator checkout --detach FETCH_HEAD && \
    test "$(git -C /src/generator rev-parse HEAD)" = "${GENERATOR_GIT_COMMIT}" && \
    cd /src/qrysm && \
    GOTOOLCHAIN=local go install -mod=readonly \
        ./cmd/qrysmctl \
        ./cmd/staking-deposit-cli/deposit

# The virtual environment is built with the compiler toolchain in a stage
# that is thrown away; only the finished venv reaches the runtime, whose
# identical base keeps the interpreter paths valid.
FROM ${GENESIS_RUNTIME_BASE_IMAGE} AS venv-builder

RUN apt-get update && \
    apt-get install --no-install-recommends -y \
        build-essential ca-certificates python3 python3-dev python3-pip python3-venv && \
    rm -rf /var/lib/apt/lists/*

COPY --from=qrysm-builder /src/generator/apps /apps
RUN cd /apps/el-gen && \
    python3 -m venv .venv && \
    .venv/bin/pip install --no-cache-dir -r requirements.txt

FROM ${GENESIS_RUNTIME_BASE_IMAGE}

ARG QRYSM_GIT_REPO
ARG QRYSM_GIT_COMMIT
ARG GENERATOR_GIT_REPO
ARG GENERATOR_GIT_COMMIT
ARG GENESIS_RUNTIME_BASE_IMAGE

# Exactly what entrypoint.sh and the generator scripts execute: python for
# the generators, envsubst (gettext-base) for templating, jq for JSON, and
# openssl for the JWT secret.
RUN apt-get update && \
    apt-get install --no-install-recommends -y \
        ca-certificates gettext-base jq openssl python3 python3-venv && \
    rm -rf /var/lib/apt/lists/*

# The Kurtosis package invokes ./entrypoint.sh relative to this directory.
WORKDIR /work

COPY --from=venv-builder /apps /apps
COPY --from=qrysm-builder /src/generator/defaults /defaults
COPY --from=qrysm-builder /src/generator/config-example /config
COPY --from=qrysm-builder /src/generator/entrypoint.sh /work/entrypoint.sh
COPY --from=qrysm-builder /go/bin/qrysmctl /usr/local/bin/qrysmctl
COPY --from=qrysm-builder /go/bin/deposit /usr/local/bin/deposit

# Exec smoke tests plus the 64-byte markers the devnet depends on: a
# generator revision without the VM64-native genesis layout fails the build
# here, not the network bootstrap.
RUN /apps/el-gen/.venv/bin/pip check && \
    /apps/el-gen/.venv/bin/python3 --version && \
    grep -q 'to_bytes(length=64' /apps/el-gen/genesis_gqrl.py && \
    grep -q '"baseFeePerGas"' /apps/el-gen/genesis_gqrl.py && \
    qrysmctl --help >/dev/null && \
    deposit --help >/dev/null

ENTRYPOINT ["/work/entrypoint.sh"]

LABEL org.opencontainers.image.source="${GENERATOR_GIT_REPO}" \
      org.opencontainers.image.revision="${GENERATOR_GIT_COMMIT}" \
      org.opencontainers.image.base.name="${GENESIS_RUNTIME_BASE_IMAGE}" \
      io.theqrl.vm64.generator.source="${GENERATOR_GIT_REPO}" \
      io.theqrl.vm64.generator.revision="${GENERATOR_GIT_COMMIT}" \
      io.theqrl.vm64.qrysm.source="${QRYSM_GIT_REPO}" \
      io.theqrl.vm64.qrysm.revision="${QRYSM_GIT_COMMIT}" \
      io.theqrl.vm64.genesis-generator="true"
