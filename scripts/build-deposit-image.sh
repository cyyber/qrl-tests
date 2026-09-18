#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(cd -- "${script_dir}/.." && pwd)
source_dir=$(cd -- "${QRYSM_SOURCE_DIR:-${repo_root}/../qrysm}" && pwd)
image=${DEVNET_DEPOSIT_IMAGE:-local/qrysm-deposit:devnet}

case "$(uname -m)" in
	aarch64 | arm64) platform_args=(--platforms=@io_bazel_rules_go//go/toolchain:linux_arm64_cgo) ;;
	x86_64) platform_args=() ;;
	*) echo "unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac

source_epoch=$(git -C "${source_dir}" show -s --format=%ct HEAD)
(
	cd "${source_dir}"
	SOURCE_DATE_EPOCH="${source_epoch}" bazel build \
		//cmd/staking-deposit-cli/deposit:deposit \
		"${platform_args[@]}" --config=release
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
cp "${repo_root}/e2e/internal/depositcli/Dockerfile" "${workdir}/Dockerfile"
docker build -t "${image}" "${workdir}"
