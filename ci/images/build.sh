#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "$0")" && pwd)

targets=(go-qrl go-qrl-clef qrysm-beacon qrysm-validator qrl-genesis-generator)
tag_variables=(GO_QRL_IMAGE_TAG GO_QRL_CLEF_IMAGE_TAG QRYSM_BEACON_IMAGE_TAG QRYSM_VALIDATOR_IMAGE_TAG GENESIS_IMAGE_TAG)
output_keys=(execution-image clef-image consensus-image validator-image genesis-image)

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
	local inputs=("$@" "${script_dir}/docker-bake.hcl" "${script_dir}/build.sh")
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

	local arch qrysm_recipe genesis_recipe
	arch=$(architecture)
	qrysm_recipe=$(recipe_hash "${script_dir}/qrysm/Dockerfile")
	genesis_recipe=$(recipe_hash "${script_dir}/genesis/Dockerfile")

	GO_QRL_IMAGE_TAG="${REGISTRY_NAMESPACE}/go-qrl:src-${GO_QRL_GIT_COMMIT:0:12}-${arch}"
	GO_QRL_CLEF_IMAGE_TAG="${REGISTRY_NAMESPACE}/go-qrl-clef:src-${GO_QRL_GIT_COMMIT:0:12}-${arch}"
	QRYSM_BEACON_IMAGE_TAG="${REGISTRY_NAMESPACE}/qrysm-beacon:src-${QRYSM_GIT_COMMIT:0:12}-r${qrysm_recipe}-${arch}"
	QRYSM_VALIDATOR_IMAGE_TAG="${REGISTRY_NAMESPACE}/qrysm-validator:src-${QRYSM_GIT_COMMIT:0:12}-r${qrysm_recipe}-${arch}"
	GENESIS_IMAGE_TAG="${REGISTRY_NAMESPACE}/qrl-genesis-generator:src-${GENERATOR_GIT_COMMIT:0:12}-q${QRYSM_GIT_COMMIT:0:12}-r${genesis_recipe}-${arch}"

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

	local missing_targets="" index tag_variable reference
	for index in "${!targets[@]}"; do
		tag_variable=${tag_variables[index]}
		reference=${!tag_variable}
		if docker buildx imagetools inspect "${reference}" >/dev/null 2>&1; then
			echo "cache hit: ${reference}"
		else
			echo "cache miss: ${reference}"
			if [ -n "${missing_targets}" ]; then
				missing_targets+=,
			fi
			missing_targets+=${targets[index]}
		fi
	done
	echo "targets=${missing_targets}" >>"${GITHUB_OUTPUT}"
}

collect() {
	: "${GITHUB_OUTPUT:?set GITHUB_OUTPUT to the outputs file}"
	local metadata=${BAKE_METADATA:-}
	local index target tag_variable reference digest repository
	for index in "${!targets[@]}"; do
		target=${targets[index]}
		tag_variable=${tag_variables[index]}
		reference=${!tag_variable}
		digest=""
		if [ -n "${metadata}" ]; then
			digest=$(jq -er --arg target "${target}" '.[$target]["containerimage.digest"] // empty' <<<"${metadata}" 2>/dev/null || true)
		fi
		if [ -z "${digest}" ]; then
			digest=$(docker buildx imagetools inspect "${reference}" --format '{{.Manifest.Digest}}')
		fi
		repository=${reference%:*}
		echo "${output_keys[index]}=${repository}@${digest}" >>"${GITHUB_OUTPUT}"
	done
}

case "${1:-}" in
	plan) plan ;;
	collect) collect ;;
	*) echo "usage: $0 <plan|collect>" >&2; exit 2 ;;
esac
