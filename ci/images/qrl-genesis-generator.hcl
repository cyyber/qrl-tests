variable "QRYSM_GIT_REPO" { default = "" }
variable "QRYSM_GIT_COMMIT" { default = "" }
variable "GENERATOR_GIT_REPO" { default = "" }
variable "GENERATOR_GIT_COMMIT" { default = "" }
variable "GENESIS_IMAGE_TAG" { default = "" }

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
