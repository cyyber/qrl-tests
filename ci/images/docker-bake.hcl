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

group "default" {
  targets = [
    "go-qrl",
    "go-qrl-clef",
    "qrl-genesis-generator",
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
