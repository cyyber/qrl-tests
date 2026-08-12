variable "REGISTRY_NAMESPACE" {
  default = ""
}

variable "ARCHITECTURE" {
  default = ""
}

variable "GO_QRL_GIT_REPO" {
  default = ""
}

variable "GO_QRL_GIT_COMMIT" {
  default = ""
}

variable "QRYSM_GIT_REPO" {
  default = ""
}

variable "QRYSM_GIT_COMMIT" {
  default = ""
}

variable "GENERATOR_GIT_REPO" {
  default = ""
}

variable "GENERATOR_GIT_COMMIT" {
  default = ""
}

variable "QRYSM_GO_BUILDER_IMAGE" {
  default = "golang:1.26-bookworm@sha256:53eeac89074db483fdf0ab3be1df32bf6e47562263d2d0d6baa7f26acb4957dd"
}

variable "QRYSM_CL_BASE_IMAGE" {
  default = "qrledger/qrysm:beacon-chain-latest@sha256:52b6fbecfe442d0d451e1219652e464d69de8a09edd44d5c54bbbf5ebdb83000"
}

variable "QRYSM_VC_BASE_IMAGE" {
  default = "qrledger/qrysm:validator-latest@sha256:e830b41130a43211803fe3d17eeb0a66cd743f062d5407667ee3531bc5891ede"
}

variable "GENESIS_GO_BUILDER_IMAGE" {
  default = "golang:1.26-bookworm@sha256:53eeac89074db483fdf0ab3be1df32bf6e47562263d2d0d6baa7f26acb4957dd"
}

variable "GENESIS_RUNTIME_BASE_IMAGE" {
  default = "debian:bookworm-slim@sha256:abd67ffcfa541b485a3dff59865ab629aa048a6c613e639d36e7456b0b229241"
}

variable "GO_QRL_IMAGE_TAG" {
  default = ""
}

variable "GO_QRL_CLEF_IMAGE_TAG" {
  default = ""
}

variable "QRYSM_BEACON_IMAGE_TAG" {
  default = ""
}

variable "QRYSM_VALIDATOR_IMAGE_TAG" {
  default = ""
}

variable "GENESIS_IMAGE_TAG" {
  default = ""
}

group "default" {
  targets = [
    "go-qrl",
    "go-qrl-clef",
    "qrysm-beacon",
    "qrysm-validator",
    "qrl-genesis-generator",
  ]
}

target "_go-qrl" {
  context = "${GO_QRL_GIT_REPO}#${GO_QRL_GIT_COMMIT}"
  args = {
    COMMIT = GO_QRL_GIT_COMMIT
  }
}

target "go-qrl" {
  inherits   = ["_go-qrl"]
  dockerfile = "Dockerfile"
  tags       = [GO_QRL_IMAGE_TAG]
  cache-from = ["type=registry,ref=${REGISTRY_NAMESPACE}/go-qrl:buildcache-${ARCHITECTURE}"]
  cache-to   = ["type=registry,ref=${REGISTRY_NAMESPACE}/go-qrl:buildcache-${ARCHITECTURE},mode=max"]
}

target "go-qrl-clef" {
  inherits   = ["_go-qrl"]
  dockerfile = "Dockerfile.alltools"
  tags       = [GO_QRL_CLEF_IMAGE_TAG]
  cache-from = ["type=registry,ref=${REGISTRY_NAMESPACE}/go-qrl-clef:buildcache-${ARCHITECTURE}"]
  cache-to   = ["type=registry,ref=${REGISTRY_NAMESPACE}/go-qrl-clef:buildcache-${ARCHITECTURE},mode=max"]
}

target "_qrysm" {
  context    = "./ci/images/qrysm"
  dockerfile = "Dockerfile"
  args = {
    QRYSM_GO_BUILDER_IMAGE = QRYSM_GO_BUILDER_IMAGE
    QRYSM_CL_BASE_IMAGE     = QRYSM_CL_BASE_IMAGE
    QRYSM_VC_BASE_IMAGE     = QRYSM_VC_BASE_IMAGE
    QRYSM_GIT_REPO          = QRYSM_GIT_REPO
    QRYSM_GIT_COMMIT        = QRYSM_GIT_COMMIT
  }
}

target "qrysm-beacon" {
  inherits   = ["_qrysm"]
  target     = "beacon"
  tags       = [QRYSM_BEACON_IMAGE_TAG]
  cache-from = ["type=registry,ref=${REGISTRY_NAMESPACE}/qrysm-beacon:buildcache-${ARCHITECTURE}"]
  cache-to   = ["type=registry,ref=${REGISTRY_NAMESPACE}/qrysm-beacon:buildcache-${ARCHITECTURE},mode=max"]
}

target "qrysm-validator" {
  inherits   = ["_qrysm"]
  target     = "validator"
  tags       = [QRYSM_VALIDATOR_IMAGE_TAG]
  cache-from = ["type=registry,ref=${REGISTRY_NAMESPACE}/qrysm-validator:buildcache-${ARCHITECTURE}"]
  cache-to   = ["type=registry,ref=${REGISTRY_NAMESPACE}/qrysm-validator:buildcache-${ARCHITECTURE},mode=max"]
}

target "qrl-genesis-generator" {
  context    = "./ci/images/genesis"
  dockerfile = "Dockerfile"
  tags       = [GENESIS_IMAGE_TAG]
  args = {
    GENESIS_GO_BUILDER_IMAGE   = GENESIS_GO_BUILDER_IMAGE
    GENESIS_RUNTIME_BASE_IMAGE = GENESIS_RUNTIME_BASE_IMAGE
    QRYSM_GIT_REPO             = QRYSM_GIT_REPO
    QRYSM_GIT_COMMIT           = QRYSM_GIT_COMMIT
    GENERATOR_GIT_REPO         = GENERATOR_GIT_REPO
    GENERATOR_GIT_COMMIT       = GENERATOR_GIT_COMMIT
  }
  cache-from = ["type=registry,ref=${REGISTRY_NAMESPACE}/qrl-genesis-generator:buildcache-${ARCHITECTURE}"]
  cache-to   = ["type=registry,ref=${REGISTRY_NAMESPACE}/qrl-genesis-generator:buildcache-${ARCHITECTURE},mode=max"]
}
