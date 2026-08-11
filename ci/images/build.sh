#!/usr/bin/env bash
# Build the devnet support images content-addressed by their sources, reusing
# any copy the registry already holds. Tags encode the primary source revision
# plus a hash of every other build input, so a cache hit is exact: the same
# tag can only ever name the same sources, recipe, and bases.
#
# Builds run through buildx with a registry-backed layer cache living beside
# each package as its :buildcache tag, so a cold rebuild reuses every layer
# the sources did not invalidate (module downloads in particular).
#
# Requires docker with a buildx docker-container builder selected
# (docker buildx create --use), crane, a registry login, and:
#   REGISTRY_NAMESPACE  e.g. ghcr.io/cyyber
#   GO_QRL_DIR          go-qrl checkout at the revision under test
#   QRYSM_GIT_REPO        qrysm clone URL
#   QRYSM_GIT_COMMIT      resolved qrysm revision under test
#   GENERATOR_GIT_REPO    genesis generator clone URL
#   GENERATOR_GIT_COMMIT  resolved genesis generator revision
#   GITHUB_OUTPUT         receives <name>-image=<ref@digest> lines
set -euo pipefail

: "${REGISTRY_NAMESPACE:?set REGISTRY_NAMESPACE to the registry prefix}"
: "${GO_QRL_DIR:?set GO_QRL_DIR to a go-qrl checkout}"
: "${QRYSM_GIT_REPO:?set QRYSM_GIT_REPO to the qrysm clone URL}"
: "${QRYSM_GIT_COMMIT:?set QRYSM_GIT_COMMIT to the qrysm revision}"
: "${GENERATOR_GIT_REPO:?set GENERATOR_GIT_REPO to the generator clone URL}"
: "${GENERATOR_GIT_COMMIT:?set GENERATOR_GIT_COMMIT to the generator revision}"
: "${GITHUB_OUTPUT:?set GITHUB_OUTPUT to the outputs file}"

script_dir=$(cd -- "$(dirname -- "$0")" && pwd)
# shellcheck source=sources.env
source "${script_dir}/sources.env"

go_qrl_sha=$(git -C "${GO_QRL_DIR}" rev-parse HEAD)

# Every extra build input beyond the primary source revision, hashed into the
# tag. sources.env is always included: it pins the bases and builders.
recipe_hash() {
	if command -v sha256sum >/dev/null; then
		cat "$@" "${script_dir}/sources.env" | sha256sum | cut -c1-8
	else
		cat "$@" "${script_dir}/sources.env" | shasum -a 256 | cut -c1-8
	fi
}

# ensure <package> <tag> <output-key> <build-function>
# Skips the build when the registry already holds the tag; either way the
# resolved immutable digest reference lands in GITHUB_OUTPUT. Build
# functions read BUILD_CACHE_REF for their layer cache and push on success.
ensure() {
	local reference="${REGISTRY_NAMESPACE}/$1:$2"
	if crane manifest "${reference}" >/dev/null 2>&1; then
		echo "cache hit: ${reference}"
	else
		echo "cache miss, building: ${reference}"
		BUILD_CACHE_REF="${REGISTRY_NAMESPACE}/$1:buildcache" "$4" "${reference}"
	fi
	echo "$3=${REGISTRY_NAMESPACE}/$1@$(crane digest "${reference}")" >>"${GITHUB_OUTPUT}"
}

build_node() {
	docker buildx build --push --provenance=false \
		--cache-from "type=registry,ref=${BUILD_CACHE_REF}" \
		--cache-to "type=registry,ref=${BUILD_CACHE_REF},mode=max" \
		--build-arg "COMMIT=${go_qrl_sha}" \
		-f "${GO_QRL_DIR}/Dockerfile" \
		-t "$1" "${GO_QRL_DIR}"
}

build_clef() {
	docker buildx build --push --provenance=false \
		--cache-from "type=registry,ref=${BUILD_CACHE_REF}" \
		--cache-to "type=registry,ref=${BUILD_CACHE_REF},mode=max" \
		--build-arg "COMMIT=${go_qrl_sha}" \
		-f "${GO_QRL_DIR}/Dockerfile.alltools" \
		-t "$1" "${GO_QRL_DIR}"
}

build_beacon() {
	docker buildx build --push --provenance=false \
		--cache-from "type=registry,ref=${BUILD_CACHE_REF}" \
		--cache-to "type=registry,ref=${BUILD_CACHE_REF},mode=max" \
		--target beacon \
		--build-arg "QRYSM_GO_BUILDER_IMAGE=${QRYSM_GO_BUILDER_IMAGE}" \
		--build-arg "QRYSM_CL_BASE_IMAGE=${QRYSM_CL_BASE_IMAGE}" \
		--build-arg "QRYSM_VC_BASE_IMAGE=${QRYSM_VC_BASE_IMAGE}" \
		--build-arg "QRYSM_GIT_REPO=${QRYSM_GIT_REPO}" \
		--build-arg "QRYSM_GIT_COMMIT=${QRYSM_GIT_COMMIT}" \
		-f "${script_dir}/qrysm/Dockerfile" \
		-t "$1" "${script_dir}/qrysm"
}

build_validator() {
	docker buildx build --push --provenance=false \
		--cache-from "type=registry,ref=${BUILD_CACHE_REF}" \
		--cache-to "type=registry,ref=${BUILD_CACHE_REF},mode=max" \
		--target validator \
		--build-arg "QRYSM_GO_BUILDER_IMAGE=${QRYSM_GO_BUILDER_IMAGE}" \
		--build-arg "QRYSM_CL_BASE_IMAGE=${QRYSM_CL_BASE_IMAGE}" \
		--build-arg "QRYSM_VC_BASE_IMAGE=${QRYSM_VC_BASE_IMAGE}" \
		--build-arg "QRYSM_GIT_REPO=${QRYSM_GIT_REPO}" \
		--build-arg "QRYSM_GIT_COMMIT=${QRYSM_GIT_COMMIT}" \
		-f "${script_dir}/qrysm/Dockerfile" \
		-t "$1" "${script_dir}/qrysm"
}

build_genesis() {
	docker buildx build --push --provenance=false \
		--cache-from "type=registry,ref=${BUILD_CACHE_REF}" \
		--cache-to "type=registry,ref=${BUILD_CACHE_REF},mode=max" \
		--build-arg "GENESIS_GO_BUILDER_IMAGE=${GENESIS_GO_BUILDER_IMAGE}" \
		--build-arg "GENESIS_BASE_IMAGE=${GENESIS_BASE_IMAGE}" \
		--build-arg "QRYSM_GIT_REPO=${QRYSM_GIT_REPO}" \
		--build-arg "QRYSM_GIT_COMMIT=${QRYSM_GIT_COMMIT}" \
		--build-arg "GENERATOR_GIT_REPO=${GENERATOR_GIT_REPO}" \
		--build-arg "GENERATOR_GIT_COMMIT=${GENERATOR_GIT_COMMIT}" \
		-f "${script_dir}/genesis/Dockerfile" \
		-t "$1" "${script_dir}/genesis"
}

qrysm_recipe=$(recipe_hash "${script_dir}/qrysm/Dockerfile")
genesis_recipe=$(recipe_hash \
	"${script_dir}/genesis/Dockerfile" \
	"${script_dir}/genesis/vm64_genesis_gqrl.py" \
	"${script_dir}/genesis/qrysm-deposit-runtime/main.go")

ensure go-qrl "src-${go_qrl_sha:0:12}" execution-image build_node
ensure go-qrl-clef "src-${go_qrl_sha:0:12}" clef-image build_clef
ensure qrysm-beacon "src-${QRYSM_GIT_COMMIT:0:12}-r${qrysm_recipe}" consensus-image build_beacon
ensure qrysm-validator "src-${QRYSM_GIT_COMMIT:0:12}-r${qrysm_recipe}" validator-image build_validator
# The generator image embeds qrysm tooling, so its identity spans both
# source revisions.
ensure qrl-genesis-generator "src-${GENERATOR_GIT_COMMIT:0:12}-q${QRYSM_GIT_COMMIT:0:12}-r${genesis_recipe}" genesis-image build_genesis
