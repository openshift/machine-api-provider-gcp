DBG ?= 0

ifeq ($(DBG),1)
GOGCFLAGS ?= -gcflags=all="-N -l"
endif

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.32.1

VERSION     ?= $(shell git describe --always --abbrev=7)
REPO_PATH   ?= github.com/openshift/machine-api-provider-gcp
LD_FLAGS    ?= -X $(REPO_PATH)/pkg/version.Raw=$(VERSION) -extldflags -static
BUILD_IMAGE ?= registry.ci.openshift.org/openshift/release:golang-1.22

PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
ENVTEST = go run ${PROJECT_DIR}/vendor/sigs.k8s.io/controller-runtime/tools/setup-envtest

GO111MODULE = on
export GO111MODULE
GOFLAGS ?= -mod=vendor
export GOFLAGS
GOPROXY ?=
export GOPROXY

NO_DOCKER ?= 1

ifeq ($(shell command -v podman > /dev/null 2>&1 ; echo $$? ), 0)
	ENGINE=podman
else ifeq ($(shell command -v docker > /dev/null 2>&1 ; echo $$? ), 0)
	ENGINE=docker
else
	NO_DOCKER=1
endif

USE_DOCKER ?= 0
ifeq ($(USE_DOCKER), 1)
	ENGINE=docker
endif

ifeq ($(NO_DOCKER), 1)
  IMAGE_BUILD_CMD = imagebuilder
else
  DOCKER_CMD :=  $(ENGINE) run --rm -v "$(PWD)":/go/src/github.com/openshift/machine-api-provider-gcp:Z -w /go/src/github.com/openshift/machine-api-provider-gcp $(BUILD_IMAGE)
  IMAGE_BUILD_CMD =  $(ENGINE) build
endif

.PHONY: vendor
vendor:
	go mod tidy
	go mod vendor
	go mod verify

.PHONY: generate
generate: gogen goimports
	./hack/verify-diff.sh

gogen:
	$(DOCKER_CMD) go generate ./pkg/... ./cmd/...

.PHONY: fmt
fmt: ## Go fmt your code
	$(DOCKER_CMD) hack/go-fmt.sh .

.PHONY: goimports
goimports: ## Go fmt your code
	$(DOCKER_CMD) hack/goimports.sh .

.PHONY: vet
vet: ## Apply go vet to all go files
	$(DOCKER_CMD) hack/go-vet.sh ./...

.PHONY: crds-sync
crds-sync: ## Sync crds in install with the ones in the vendored oc/api
	$(DOCKER_CMD) hack/crds-sync.sh .

.PHONY: verify-crds-sync
verify-crds-sync: ## Verify that the crds in install and the ones in vendored oc/api are in sync
	$(DOCKER_CMD) hack/crds-sync.sh . && hack/verify-diff.sh .

.PHONY: test
test: unit ## Run tests

.PHONY: unit
unit: # Run unit test
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path --bin-dir $(PROJECT_DIR)/bin)" ./hack/test.sh

.PHONY: sec
sec: # Run security static analysis
	$(DOCKER_CMD) hack/gosec.sh ./...

.PHONY: build
build: ## build binaries
	$(DOCKER_CMD) go build $(GOGCFLAGS) -o "bin/machine-controller-manager" \
               -ldflags "$(LD_FLAGS)" "$(REPO_PATH)/cmd/manager"
	$(DOCKER_CMD) go build $(GOGCFLAGS) -o "bin/termination-handler" \
               -ldflags "$(LD_FLAGS)" "$(REPO_PATH)/cmd/termination-handler"

.PHONY: test-e2e
test-e2e: ## Run e2e tests
	hack/e2e.sh
