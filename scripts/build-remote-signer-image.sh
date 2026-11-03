#!/usr/bin/env bash
# Builds a go-remotesigner image for the remote-signer profiles from a
# go-remotesigner checkout, with the checkout's own Dockerfile
# (deploy/docker/Dockerfile, through its `make image` target): by default the
# consensus-only flavour, tagged DEVNET_REMOTE_SIGNER_IMAGE, the image the
# consensus profiles run; with REMOTESIGNER_FLAVOUR=remotesigner the full
# flavour, which has execution mode too, tagged
# DEVNET_REMOTE_SIGNER_EXECUTION_IMAGE, the image the remote-signer-execution
# profile runs.
#
# REMOTESIGNER_FLAVOUR     remotesigner-consensus (default) or remotesigner
# REMOTESIGNER_SOURCE_DIR  the checkout (default: ../go-remotesigner)
# REMOTESIGNER_REF         a commit to build instead of the checkout's working
#                          tree; it is exported with git archive, so the
#                          checkout and its worktrees stay as they are
# GO_LICENSES              go-licenses v2, which the build needs for the
#                          image's licence files (default: the one on PATH)
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(cd -- "${script_dir}/.." && pwd)
source_dir=$(cd -- "${REMOTESIGNER_SOURCE_DIR:-${repo_root}/../go-remotesigner}" && pwd)
flavour=${REMOTESIGNER_FLAVOUR:-remotesigner-consensus}
case "${flavour}" in
remotesigner-consensus) image=${DEVNET_REMOTE_SIGNER_IMAGE:-local/go-remotesigner:devnet} ;;
remotesigner) image=${DEVNET_REMOTE_SIGNER_EXECUTION_IMAGE:-local/go-remotesigner:devnet-full} ;;
*)
	echo "REMOTESIGNER_FLAVOUR must be remotesigner-consensus or remotesigner, not ${flavour}" >&2
	exit 1
	;;
esac

go_licenses=${GO_LICENSES:-$(command -v go-licenses || true)}
test -n "${go_licenses}" || {
	echo "go-licenses is required: go install github.com/google/go-licenses/v2@v2.0.1, or set GO_LICENSES" >&2
	exit 1
}

workdir=$(mktemp -d)
trap 'rm -rf "${workdir}"' EXIT

make_args=(image "FLAVOUR=${flavour}" "IMAGE=${image}" "GO_LICENSES=${go_licenses}")
build_dir=${source_dir}
if [ -n "${REMOTESIGNER_REF:-}" ]; then
	commit=$(git -C "${source_dir}" rev-parse --verify "${REMOTESIGNER_REF}^{commit}")
	build_dir=${workdir}/src
	mkdir "${build_dir}"
	git -C "${source_dir}" archive --format=tar "${commit}" | tar -x -C "${build_dir}"
	# The export has no repository to describe it.
	make_args+=(
		"VERSION=$(git -C "${source_dir}" describe --tags --always "${commit}" | sed 's/^v//')"
		"COMMIT=${commit}"
		"CREATED=$(git -C "${source_dir}" log -1 --format=%cI "${commit}")"
	)
fi

test -f "${build_dir}/deploy/docker/Dockerfile" || {
	echo "${REMOTESIGNER_REF:-${source_dir}} has no go-remotesigner deploy/docker/Dockerfile" >&2
	exit 1
}
make -C "${build_dir}" "${make_args[@]}"
echo "built ${image}"
