#!/usr/bin/env bash
# Builds the qrysm beacon-chain and validator images of the remote-signer
# lane from one qrysm commit, without Bazel: a static go build of each client
# for linux on the Docker daemon's architecture, as the go-remotesigner image
# is built, in the image scripts/qrysm.Dockerfile describes. The validator
# client must have the remote signer client (--validators-external-signer-*).
#
# QRYSM_SOURCE_DIR        the checkout (default: ../qrysm)
# QRYSM_REF               a commit to build instead of the checkout's working
#                         tree; it is exported with git archive, so the
#                         checkout and its worktrees stay as they are
# DEVNET_CONSENSUS_IMAGE  the beacon-chain tag
#                         (default: local/qrysm:remote-signer-beacon-chain)
# DEVNET_VALIDATOR_IMAGE  the validator tag
#                         (default: local/qrysm:remote-signer-validator)
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(cd -- "${script_dir}/.." && pwd)
source_dir=$(cd -- "${QRYSM_SOURCE_DIR:-${repo_root}/../qrysm}" && pwd)
beacon_image=${DEVNET_CONSENSUS_IMAGE:-local/qrysm:remote-signer-beacon-chain}
validator_image=${DEVNET_VALIDATOR_IMAGE:-local/qrysm:remote-signer-validator}
# The daemon's architecture, which Docker names as Go does; Go's own only when
# the daemon cannot be asked.
arch=$(docker version --format '{{.Server.Arch}}' 2>/dev/null) || arch=
arch=${arch:-$(go env GOARCH)}

workdir=$(mktemp -d)
trap 'rm -rf "${workdir}"' EXIT

build_dir=${source_dir}
if [ -n "${QRYSM_REF:-}" ]; then
	commit=$(git -C "${source_dir}" rev-parse --verify "${QRYSM_REF}^{commit}")
	build_dir=${workdir}/src
	mkdir "${build_dir}"
	git -C "${source_dir}" archive --format=tar "${commit}" | tar -x -C "${build_dir}"
else
	commit=$(git -C "${source_dir}" rev-parse HEAD)
	if [ -n "$(git -C "${source_dir}" status --porcelain --untracked-files=no)" ]; then
		commit=${commit}-dirty
	fi
fi

mkdir "${workdir}/context"
(
	cd "${build_dir}"
	CGO_ENABLED=0 GOOS=linux GOARCH="${arch}" go build -trimpath \
		-ldflags "-s -w -X github.com/theQRL/qrysm/runtime/version.gitCommit=${commit}" \
		-o "${workdir}/context/" ./cmd/beacon-chain ./cmd/validator
)

for target in beacon-chain validator; do
	case "${target}" in
		beacon-chain) image=${beacon_image} ;;
		validator) image=${validator_image} ;;
	esac
	docker build --platform "linux/${arch}" --target "${target}" \
		--label "org.opencontainers.image.revision=${commit}" \
		-f "${script_dir}/qrysm.Dockerfile" -t "${image}" "${workdir}/context"
	echo "built ${image} from qrysm ${commit}"
done
