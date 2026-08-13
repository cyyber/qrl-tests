variable "REGISTRY_NAMESPACE" { default = "" }
variable "ARCHITECTURE" { default = "" }
variable "GO_QRL_GIT_REPO" { default = "" }
variable "GO_QRL_GIT_COMMIT" { default = "" }
variable "GO_QRL_IMAGE_TAG" { default = "" }
variable "GO_QRL_CLEF_IMAGE_TAG" { default = "" }

group "default" {
  targets = [
    "go-qrl",
    "go-qrl-clef",
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
