#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "$0")" && pwd)

image_inventory=(
	'go-qrl|GO_QRL_IMAGE_TAG|execution-image|bake|GO_QRL_IMAGE_STATUS'
	'go-qrl-clef|GO_QRL_CLEF_IMAGE_TAG|clef-image|bake|GO_QRL_CLEF_IMAGE_STATUS'
	'qrysm-beacon|QRYSM_BEACON_IMAGE_TAG|consensus-image|qrysm|QRYSM_BEACON_IMAGE_STATUS'
	'qrysm-validator|QRYSM_VALIDATOR_IMAGE_TAG|validator-image|qrysm|QRYSM_VALIDATOR_IMAGE_STATUS'
	'qrl-genesis-generator|GENESIS_IMAGE_TAG|genesis-image|bake|GENESIS_IMAGE_STATUS'
)

require_build_inputs() {
	if [ -z "${QRL_TESTS_GIT_COMMIT:-}" ]; then
		QRL_TESTS_GIT_COMMIT=$(git -C "$(dirname "${BASH_SOURCE[0]}")/../.." rev-parse HEAD)
	fi
	: "${REGISTRY_NAMESPACE:?set REGISTRY_NAMESPACE to the registry prefix}"
	: "${QRL_TESTS_GIT_COMMIT:?set QRL_TESTS_GIT_COMMIT to the qrl-tests revision}"
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

recipe_revision() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -c1-8
	else
		shasum -a 256 "$1" | cut -c1-8
	fi
}

plan() {
	require_build_inputs
	: "${GITHUB_ENV:?set GITHUB_ENV to the environment file}"
	: "${GITHUB_OUTPUT:?set GITHUB_OUTPUT to the outputs file}"

	local arch qrysm_recipe_revision genesis_recipe_revision
	arch=$(architecture)
	qrysm_recipe_revision=$(recipe_revision "${script_dir}/build-qrysm.sh")
	genesis_recipe_revision=$(recipe_revision "${script_dir}/qrl-genesis-generator.hcl")

	GO_QRL_IMAGE_TAG="${REGISTRY_NAMESPACE}/go-qrl:src-${GO_QRL_GIT_COMMIT:0:12}-${arch}"
	GO_QRL_CLEF_IMAGE_TAG="${REGISTRY_NAMESPACE}/go-qrl-clef:src-${GO_QRL_GIT_COMMIT:0:12}-tests-${QRL_TESTS_GIT_COMMIT}-${arch}"
	QRYSM_BEACON_IMAGE_TAG="${REGISTRY_NAMESPACE}/qrysm-beacon:src-${QRYSM_GIT_COMMIT:0:12}-r${qrysm_recipe_revision}-${arch}"
	QRYSM_VALIDATOR_IMAGE_TAG="${REGISTRY_NAMESPACE}/qrysm-validator:src-${QRYSM_GIT_COMMIT:0:12}-r${qrysm_recipe_revision}-${arch}"
	GENESIS_IMAGE_TAG="${REGISTRY_NAMESPACE}/qrl-genesis-generator:src-${GENERATOR_GIT_COMMIT:0:12}-q${QRYSM_GIT_COMMIT:0:12}-r${genesis_recipe_revision}-${arch}"

	{
		printf 'ARCHITECTURE=%s\n' "${arch}"
		printf '%s=%s\n' \
			REGISTRY_NAMESPACE "${REGISTRY_NAMESPACE}" \
			QRL_TESTS_GIT_COMMIT "${QRL_TESTS_GIT_COMMIT}" \
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

	local -a missing_bake_targets=() missing_qrysm_targets=()
	local image target tag_variable build_type status_variable reference status
	for image in "${image_inventory[@]}"; do
		IFS='|' read -r target tag_variable _ build_type status_variable <<<"${image}"
		reference=${!tag_variable}
		if docker buildx imagetools inspect "${reference}" >/dev/null 2>&1; then
			echo "cache hit: ${reference}"
			status=reused
		else
			echo "cache miss: ${reference}"
			status=built
			case "${build_type}" in
				bake) missing_bake_targets+=("${target}") ;;
				qrysm) missing_qrysm_targets+=("${target}") ;;
				*) echo "unknown build type: ${build_type}" >&2; return 2 ;;
			esac
		fi
		printf '%s=%s\n' "${status_variable}" "${status}" >>"${GITHUB_ENV}"
	done
	local IFS=,
	{
		printf 'bake-targets=%s\n' "${missing_bake_targets[*]-}"
		printf 'qrysm-targets=%s\n' "${missing_qrysm_targets[*]-}"
	} >>"${GITHUB_OUTPUT}"
}

collect() {
	: "${GITHUB_OUTPUT:?set GITHUB_OUTPUT to the outputs file}"
	local metadata=${BAKE_METADATA:-}
	local image target tag_variable output_key status_variable reference digest repository immutable status
	if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
		{
			echo "### Nightly images"
			echo
			echo "| Image | Result | Immutable reference |"
			echo "| --- | --- | --- |"
		} >>"${GITHUB_STEP_SUMMARY}"
	fi
	for image in "${image_inventory[@]}"; do
		IFS='|' read -r target tag_variable output_key _ status_variable <<<"${image}"
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
		echo "${output_key}=${immutable}" >>"${GITHUB_OUTPUT}"
		if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
			status=$(printenv "${status_variable}" || printf resolved)
			printf "| \`%s\` | %s | \`%s\` |\n" "${target}" "${status}" "${immutable}" >>"${GITHUB_STEP_SUMMARY}"
		fi
	done
}

case "${1:-}" in
	plan) plan ;;
	build-qrysm) exec "${script_dir}/build-qrysm.sh" ;;
	collect) collect ;;
	*) echo "usage: $0 <plan|build-qrysm|collect>" >&2; exit 2 ;;
esac
