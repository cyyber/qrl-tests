# The qrysm client images scripts/build-qrysm-images.sh builds: one static
# binary from the build context, at the path and as the entrypoint of
# qrysm's own images, run as root, since qrl-package gives the clients
# root-owned files.
FROM debian:bookworm-slim AS base
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
	&& rm -rf /var/lib/apt/lists/*

FROM base AS beacon-chain
COPY beacon-chain /beacon-chain
ENTRYPOINT ["/beacon-chain"]

FROM base AS validator
COPY validator /validator
ENTRYPOINT ["/validator"]
