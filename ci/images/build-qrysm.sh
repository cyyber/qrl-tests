#!/usr/bin/env bash
set -euo pipefail

: "${QRYSM_TARGETS:?set QRYSM_TARGETS to the missing Qrysm targets}"
: "${QRYSM_BEACON_IMAGE_TAG:?set QRYSM_BEACON_IMAGE_TAG to the beacon image tag}"
: "${QRYSM_VALIDATOR_IMAGE_TAG:?set QRYSM_VALIDATOR_IMAGE_TAG to the validator image tag}"

source_dir=${QRYSM_SOURCE_DIR:-.build/qrysm}
source_dir=$(cd -- "${source_dir}" && pwd)
source_epoch=$(git -C "${source_dir}" show -s --format=%ct HEAD)

case "$(uname -m)" in
	aarch64 | arm64) arch=arm64 ;;
	x86_64) arch=amd64 ;;
	*) echo "unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac

platform_args=()
if [ "${arch}" = arm64 ]; then
	platform_args=(--platforms=@io_bazel_rules_go//go/toolchain:linux_arm64_cgo)
fi

IFS=',' read -r -a requested_targets <<<"${QRYSM_TARGETS}"
bazel_targets=()
archives=()
image_tags=()
for requested in "${requested_targets[@]}"; do
	case "${requested}" in
	qrysm-beacon)
		target=cmd/beacon-chain
		image_tags+=("${QRYSM_BEACON_IMAGE_TAG}")
		;;
	qrysm-validator)
		target=cmd/validator
		image_tags+=("${QRYSM_VALIDATOR_IMAGE_TAG}")
		;;
	*) echo "unknown Qrysm target: ${requested}" >&2; exit 2 ;;
	esac
	bazel_targets+=("//${target}:oci_image_tarball")
	archives+=("${source_dir}/bazel-bin/${target}/oci_image_tarball/tarball.tar")
done

(
	cd "${source_dir}"
	SOURCE_DATE_EPOCH="${source_epoch}" bazel build \
		"${bazel_targets[@]}" "${platform_args[@]}" --config=release
)

# Both tarballs load under the same upstream name, so each one must be
# retagged before the next load overwrites it.
for index in "${!archives[@]}"; do
	loaded=$(docker load --input "${archives[index]}" | sed -n 's/^Loaded image: //p')
	test -n "${loaded}" || { echo "no image tag in ${archives[index]}" >&2; exit 1; }
	docker tag "${loaded}" "${image_tags[index]}"
	docker push "${image_tags[index]}"
done
