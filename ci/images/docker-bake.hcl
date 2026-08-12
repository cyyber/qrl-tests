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

variable "GO_QRL_IMAGE_TAG" {
  default = ""
}

variable "GO_QRL_CLEF_IMAGE_TAG" {
  default = ""
}

variable "GENESIS_IMAGE_TAG" {
  default = ""
}

group "default" {
  targets = [
    "go-qrl",
    "go-qrl-clef",
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

target "qrl-genesis-generator" {
  context    = "${GENERATOR_GIT_REPO}#${GENERATOR_GIT_COMMIT}"
  tags       = [GENESIS_IMAGE_TAG]
  args = {
    QRYSM_GIT_REPO = QRYSM_GIT_REPO
    QRYSM_GIT_REF  = QRYSM_GIT_COMMIT
  }
  cache-from = ["type=registry,ref=${REGISTRY_NAMESPACE}/qrl-genesis-generator:buildcache-${ARCHITECTURE}"]
  cache-to   = ["type=registry,ref=${REGISTRY_NAMESPACE}/qrl-genesis-generator:buildcache-${ARCHITECTURE},mode=max"]
}
