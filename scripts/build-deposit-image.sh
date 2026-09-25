#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(cd -- "${script_dir}/.." && pwd)
source_dir=$(cd -- "${QRYSM_SOURCE_DIR:-${repo_root}/../qrysm}" && pwd)
image=${DEVNET_DEPOSIT_IMAGE:-local/qrysm-deposit:devnet}

# The image is Linux, so build for Linux on the host's architecture even on macOS.
case "$(uname -m)" in
	aarch64 | arm64) arch=arm64 ;;
	x86_64 | amd64) arch=amd64 ;;
	*) echo "unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac

source_epoch=$(git -C "${source_dir}" show -s --format=%ct HEAD)
(
	cd "${source_dir}"
	SOURCE_DATE_EPOCH="${source_epoch}" bazel build \
		//cmd/staking-deposit-cli/deposit:deposit \
		--platforms="@io_bazel_rules_go//go/toolchain:linux_${arch}_cgo" --config=release
)

binary=""
for candidate in \
	"${source_dir}/bazel-bin/cmd/staking-deposit-cli/deposit/deposit_/deposit" \
	"${source_dir}/bazel-bin/cmd/staking-deposit-cli/deposit/deposit"; do
	if [ -f "${candidate}" ]; then
		binary=${candidate}
		break
	fi
done
test -n "${binary}" || {
	echo "deposit binary not found under ${source_dir}/bazel-bin/cmd/staking-deposit-cli/deposit" >&2
	exit 1
}

workdir=$(mktemp -d)
trap 'rm -rf "${workdir}"' EXIT
cp "${binary}" "${workdir}/deposit"
cp "${repo_root}/e2e/internal/sidecar/deposit/Dockerfile" "${workdir}/Dockerfile"
docker build -t "${image}" "${workdir}"
