#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "$0")" && pwd)

targets=(go-qrl go-qrl-clef qrysm-beacon qrysm-validator qrl-genesis-generator)
tag_variables=(GO_QRL_IMAGE_TAG GO_QRL_CLEF_IMAGE_TAG QRYSM_BEACON_IMAGE_TAG QRYSM_VALIDATOR_IMAGE_TAG GENESIS_IMAGE_TAG)
output_keys=(execution-image clef-image consensus-image validator-image genesis-image)
build_types=(bake bake qrysm qrysm bake)
status_variables=(GO_QRL_IMAGE_STATUS GO_QRL_CLEF_IMAGE_STATUS QRYSM_BEACON_IMAGE_STATUS QRYSM_VALIDATOR_IMAGE_STATUS GENESIS_IMAGE_STATUS)

require_build_inputs() {
	: "${REGISTRY_NAMESPACE:?set REGISTRY_NAMESPACE to the registry prefix}"
	: "${GO_QRL_GIT_REPO:?set GO_QRL_GIT_REPO to the go-qrl clone URL}"
	: "${GO_QRL_GIT_COMMIT:?set GO_QRL_GIT_COMMIT to the go-qrl revision}"
	: "${QRYSM_GIT_REPO:?set QRYSM_GIT_REPO to the qrysm clone URL}"
	: "${QRYSM_GIT_COMMIT:?set QRYSM_GIT_COMMIT to the qrysm revision}"
	: "${GENERATOR_GIT_REPO:?set GENERATOR_GIT_REPO to the generator clone URL}"
	: "${GENERATOR_GIT_COMMIT:?set GENERATOR_GIT_COMMIT to the generator revision}"
}

architecture() {
	case "$(uname -m)" in
		aarch64 | arm64) echo arm64 ;;
		x86_64) echo amd64 ;;
		*) echo "unsupported architecture $(uname -m)" >&2; return 1 ;;
	esac
}

recipe_hash() {
	local inputs=("${script_dir}/docker-bake.hcl" "${script_dir}/build.sh")
	if command -v sha256sum >/dev/null; then
		cat "${inputs[@]}" | sha256sum | cut -c1-8
	else
		cat "${inputs[@]}" | shasum -a 256 | cut -c1-8
	fi
}

plan() {
	require_build_inputs
	: "${GITHUB_ENV:?set GITHUB_ENV to the environment file}"
	: "${GITHUB_OUTPUT:?set GITHUB_OUTPUT to the outputs file}"

	local arch image_recipe
	arch=$(architecture)
	image_recipe=$(recipe_hash)

	GO_QRL_IMAGE_TAG="${REGISTRY_NAMESPACE}/go-qrl:src-${GO_QRL_GIT_COMMIT:0:12}-${arch}"
	GO_QRL_CLEF_IMAGE_TAG="${REGISTRY_NAMESPACE}/go-qrl-clef:src-${GO_QRL_GIT_COMMIT:0:12}-${arch}"
	QRYSM_BEACON_IMAGE_TAG="${REGISTRY_NAMESPACE}/qrysm-beacon:src-${QRYSM_GIT_COMMIT:0:12}-r${image_recipe}-${arch}"
	QRYSM_VALIDATOR_IMAGE_TAG="${REGISTRY_NAMESPACE}/qrysm-validator:src-${QRYSM_GIT_COMMIT:0:12}-r${image_recipe}-${arch}"
	GENESIS_IMAGE_TAG="${REGISTRY_NAMESPACE}/qrl-genesis-generator:src-${GENERATOR_GIT_COMMIT:0:12}-q${QRYSM_GIT_COMMIT:0:12}-r${image_recipe}-${arch}"

	{
		printf 'ARCHITECTURE=%s\n' "${arch}"
		printf '%s=%s\n' \
			REGISTRY_NAMESPACE "${REGISTRY_NAMESPACE}" \
			GO_QRL_GIT_REPO "${GO_QRL_GIT_REPO}" \
			GO_QRL_GIT_COMMIT "${GO_QRL_GIT_COMMIT}" \
			QRYSM_GIT_REPO "${QRYSM_GIT_REPO}" \
			QRYSM_GIT_COMMIT "${QRYSM_GIT_COMMIT}" \
			GENERATOR_GIT_REPO "${GENERATOR_GIT_REPO}" \
			GENERATOR_GIT_COMMIT "${GENERATOR_GIT_COMMIT}" \
			GO_QRL_IMAGE_TAG "${GO_QRL_IMAGE_TAG}" \
			GO_QRL_CLEF_IMAGE_TAG "${GO_QRL_CLEF_IMAGE_TAG}" \
			QRYSM_BEACON_IMAGE_TAG "${QRYSM_BEACON_IMAGE_TAG}" \
			QRYSM_VALIDATOR_IMAGE_TAG "${QRYSM_VALIDATOR_IMAGE_TAG}" \
			GENESIS_IMAGE_TAG "${GENESIS_IMAGE_TAG}"
	} >>"${GITHUB_ENV}"

	local missing_bake_targets="" missing_qrysm_targets=""
	local index tag_variable status_variable reference status missing_variable
	for index in "${!targets[@]}"; do
		tag_variable=${tag_variables[index]}
		status_variable=${status_variables[index]}
		reference=${!tag_variable}
		if docker buildx imagetools inspect "${reference}" >/dev/null 2>&1; then
			echo "cache hit: ${reference}"
			status=reused
		else
			echo "cache miss: ${reference}"
			status=built
			case "${build_types[index]}" in
				bake) missing_variable=missing_bake_targets ;;
				qrysm) missing_variable=missing_qrysm_targets ;;
			esac
			if [ -n "${!missing_variable}" ]; then
				printf -v "${missing_variable}" '%s,' "${!missing_variable}"
			fi
			printf -v "${missing_variable}" '%s%s' "${!missing_variable}" "${targets[index]}"
		fi
		printf '%s=%s\n' "${status_variable}" "${status}" >>"${GITHUB_ENV}"
	done
	{
		printf 'bake-targets=%s\n' "${missing_bake_targets}"
		printf 'qrysm-targets=%s\n' "${missing_qrysm_targets}"
	} >>"${GITHUB_OUTPUT}"
}

build_qrysm() {
	: "${QRYSM_TARGETS:?set QRYSM_TARGETS to the missing Qrysm targets}"
	: "${QRYSM_GIT_COMMIT:?set QRYSM_GIT_COMMIT to the Qrysm revision}"
	: "${QRYSM_BEACON_IMAGE_TAG:?set QRYSM_BEACON_IMAGE_TAG to the beacon image tag}"
	: "${QRYSM_VALIDATOR_IMAGE_TAG:?set QRYSM_VALIDATOR_IMAGE_TAG to the validator image tag}"

	local source_dir=${QRYSM_SOURCE_DIR:-.build/qrysm}
	source_dir=$(cd -- "${source_dir}" && pwd)
	test "$(git -C "${source_dir}" rev-parse HEAD)" = "${QRYSM_GIT_COMMIT}"

	local source_epoch arch requested target
	local -a requested_targets=() bazel_targets=() archives=() image_tags=() platform_args=()
	source_epoch=$(git -C "${source_dir}" show -s --format=%ct HEAD)
	arch=$(architecture)
	if [ "${arch}" = arm64 ]; then
		platform_args=(--platforms=@io_bazel_rules_go//go/toolchain:linux_arm64_cgo)
	fi

	IFS=',' read -r -a requested_targets <<<"${QRYSM_TARGETS}"
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
			*) echo "unknown Qrysm target: ${requested}" >&2; return 2 ;;
		esac
		bazel_targets+=("//${target}:oci_image_tarball")
		archives+=("${source_dir}/bazel-bin/${target}/oci_image_tarball/tarball.tar")
	done

	(
		cd "${source_dir}"
		SOURCE_DATE_EPOCH="${source_epoch}" bazel build \
			"${bazel_targets[@]}" "${platform_args[@]}" --config=release
	)

	local index
	for index in "${!archives[@]}"; do
		docker load --input "${archives[index]}"
		docker tag index.docker.io/qrledger/qrysm:latest "${image_tags[index]}"
		docker push "${image_tags[index]}"
	done
}

collect() {
	: "${GITHUB_OUTPUT:?set GITHUB_OUTPUT to the outputs file}"
	local metadata=${BAKE_METADATA:-}
	local index target tag_variable status_variable reference digest repository immutable status
	if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
		{
			echo "### Nightly images"
			echo
			echo "| Image | Result | Immutable reference |"
			echo "| --- | --- | --- |"
		} >>"${GITHUB_STEP_SUMMARY}"
	fi
	for index in "${!targets[@]}"; do
		target=${targets[index]}
		tag_variable=${tag_variables[index]}
		status_variable=${status_variables[index]}
		reference=${!tag_variable}
		digest=""
		if [ -n "${metadata}" ]; then
			digest=$(jq -er --arg target "${target}" '.[$target]["containerimage.digest"] // empty' <<<"${metadata}" 2>/dev/null || true)
		fi
		if [ -z "${digest}" ]; then
			digest=$(docker buildx imagetools inspect "${reference}" --format '{{.Manifest.Digest}}')
		fi
		repository=${reference%:*}
		immutable=${repository}@${digest}
		echo "${output_keys[index]}=${immutable}" >>"${GITHUB_OUTPUT}"
		if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
			status=$(printenv "${status_variable}" || printf resolved)
			printf "| \`%s\` | %s | \`%s\` |\n" "${target}" "${status}" "${immutable}" >>"${GITHUB_STEP_SUMMARY}"
		fi
	done
}

case "${1:-}" in
	plan) plan ;;
	build-qrysm) build_qrysm ;;
	collect) collect ;;
	*) echo "usage: $0 <plan|build-qrysm|collect>" >&2; exit 2 ;;
esac
