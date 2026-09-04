variable "REGISTRY_NAMESPACE" { default = "" }
variable "ARCHITECTURE" { default = "" }
variable "GO_QRL_GIT_REPO" { default = "" }
variable "GO_QRL_GIT_COMMIT" { default = "" }
variable "GO_QRL_IMAGE_TAG" { default = "" }
variable "GO_QRL_CLEF_IMAGE_TAG" { default = "" }
variable "QRYSM_GIT_REPO" { default = "" }
variable "QRYSM_GIT_COMMIT" { default = "" }
variable "GENERATOR_GIT_REPO" { default = "" }
variable "GENERATOR_GIT_COMMIT" { default = "" }
variable "GENESIS_IMAGE_TAG" { default = "" }
variable "TX_SPAMMER_GIT_REPO" { default = "" }
variable "TX_SPAMMER_GIT_COMMIT" { default = "" }
variable "TX_SPAMMER_IMAGE_TAG" { default = "" }
variable "METRICS_EXPORTER_GIT_REPO" { default = "" }
variable "METRICS_EXPORTER_GIT_COMMIT" { default = "" }
variable "METRICS_EXPORTER_IMAGE_TAG" { default = "" }
variable "KURTOSIS_VERSION" { default = "1.20.0" }
variable "RUNNER_IMAGE_TAG" { default = "" }

group "default" {
  targets = [
    "go-qrl",
    "go-qrl-clef",
    "qrl-genesis-generator",
    "qrl-tx-spammer",
    "qrl-metrics-exporter",
    "qrl-tests-runner",
  ]
}

function "buildcache" {
  params = [name]
  result = "type=registry,ref=${REGISTRY_NAMESPACE}/${name}:buildcache-${ARCHITECTURE}"
}

target "_go-qrl" {
  context = "${GO_QRL_GIT_REPO}#${GO_QRL_GIT_COMMIT}"
  args = {
    COMMIT = GO_QRL_GIT_COMMIT
  }
}

target "go-qrl" {
  inherits   = ["_go-qrl"]
  tags       = [GO_QRL_IMAGE_TAG]
  cache-from = [buildcache("go-qrl")]
  cache-to   = ["${buildcache("go-qrl")},mode=max"]
}

target "go-qrl-clef" {
  inherits   = ["_go-qrl"]
  dockerfile = "Dockerfile.alltools"
  tags       = [GO_QRL_CLEF_IMAGE_TAG]
  cache-from = [buildcache("go-qrl-clef")]
  cache-to   = ["${buildcache("go-qrl-clef")},mode=max"]
}

target "qrl-genesis-generator" {
  context = "${GENERATOR_GIT_REPO}#${GENERATOR_GIT_COMMIT}"
  tags    = [GENESIS_IMAGE_TAG]
  args = {
    QRYSM_GIT_REPO = QRYSM_GIT_REPO
    QRYSM_GIT_REF  = QRYSM_GIT_COMMIT
  }
  cache-from = [buildcache("qrl-genesis-generator")]
  cache-to   = ["${buildcache("qrl-genesis-generator")},mode=max"]
}

# Load generator for the soak. Its Dockerfile stamps the version from
# git, so the git directory is kept in the remote build context.
target "qrl-tx-spammer" {
  context = "${TX_SPAMMER_GIT_REPO}#${TX_SPAMMER_GIT_COMMIT}"
  tags    = [TX_SPAMMER_IMAGE_TAG]
  args = {
    BUILDKIT_CONTEXT_KEEP_GIT_DIR = 1
  }
  cache-from = [buildcache("qrl-tx-spammer")]
  cache-to   = ["${buildcache("qrl-tx-spammer")},mode=max"]
}

# Prometheus exporter for execution and consensus client metrics that the
# clients do not expose natively.
target "qrl-metrics-exporter" {
  context    = "${METRICS_EXPORTER_GIT_REPO}#${METRICS_EXPORTER_GIT_COMMIT}"
  tags       = [METRICS_EXPORTER_IMAGE_TAG]
  cache-from = [buildcache("qrl-metrics-exporter")]
  cache-to   = ["${buildcache("qrl-metrics-exporter")},mode=max"]
}

# The harness itself, for lanes that run as in-cluster Jobs: this checkout
# plus the Go toolchain, Kurtosis CLI and kubectl.
target "qrl-tests-runner" {
  context    = "."
  dockerfile = "ci/images/runner/Dockerfile"
  tags       = [RUNNER_IMAGE_TAG]
  args = {
    KURTOSIS_VERSION = KURTOSIS_VERSION
  }
  cache-from = [buildcache("qrl-tests-runner")]
  cache-to   = ["${buildcache("qrl-tests-runner")},mode=max"]
}
