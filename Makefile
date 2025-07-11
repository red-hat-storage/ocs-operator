LOCALBIN=$(shell pwd)/bin

KUSTOMIZE_VERSION=v4.5.7
KUSTOMIZE=$(LOCALBIN)/kustomize
CONTROLLER_GEN_VERSION=v0.16.1
CONTROLLER_GEN=$(LOCALBIN)/controller-gen


.PHONY: \
	build \
	gen-protobuf \
	clean \
	ocs-operator \
	operator-bundle \
	verify-operator-bundle \
	gen-latest-csv \
	gen-latest-prometheus-rules-yamls \
	verify-latest-csv \
	functest \
	shellcheck-test \
	golangci-lint \
	update-generated \
	ocs-operator-ci \
	unit-test \
	deps-update

deps-update:
	@echo "Running deps-update"
	go mod tidy && go mod vendor
	@echo "Running deps-update on api submodule"
	cd api && go mod tidy && go mod vendor
	@echo "Running deps-update on metrics submodule"
	cd metrics && go mod tidy && go mod vendor
	@echo "Running deps-update on provider/api submodule"
	cd services/provider/api && go mod tidy && go mod vendor

operator-sdk:
	@echo "Ensuring operator-sdk"
	hack/ensure-operator-sdk.sh

build: deps-update generate gen-protobuf

ocs-operator: build
	@echo "Building the ocs-operator image"
	hack/build-operator.sh

ocs-metrics-exporter: build
	@echo "Building the ocs-metrics-exporter image"
	hack/build-metrics-exporter.sh

gen-protobuf:
	@echo "Generating protobuf files for gRPC services"
	hack/gen-protobuf.sh

gen-latest-csv: operator-sdk manifests kustomize
	@echo "Generating latest development CSV version using predefined ENV VARs."
	hack/generate-latest-csv.sh

gen-release-csv: operator-sdk manifests kustomize
	@echo "Generating unified CSV from sourced component-level operators"
	hack/generate-unified-csv.sh

verify-latest-csv: gen-latest-csv
	@echo "Verifying latest CSV"
	hack/verify-latest-csv.sh

verify-operator-bundle: operator-sdk
	@echo "Verifying operator bundle"
	hack/verify-operator-bundle.sh

operator-bundle: gen-latest-csv
	@echo "Building ocs operator bundle"
	hack/build-operator-bundle.sh

clean:
	@echo "cleaning previous outputs"
	hack/clean.sh

build-functest:
	@echo "Building functional tests"
	hack/build-functest.sh

functest: build-functest
	@echo "Running ocs developer functional test suite"
	hack/functest.sh $(ARGS)

shellcheck-test:
	@echo "Testing for shellcheck"
	hack/shellcheck-test.sh

golangci-lint:
	@echo "Running golangci-lint run"
	hack/golangci_lint.sh

# ignoring the functest dir since it requires an active cluster
# use 'make functest' to run just the functional tests
unit-test:
	@echo "Executing unit tests"
	hack/unit-test.sh

ocs-operator-ci: shellcheck-test golangci-lint unit-test verify-deps verify-generated verify-latest-csv verify-operator-bundle

# Generate code
generate: controller-gen
	@echo Updating generated code
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	@echo Updating generated manifests
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd:generateEmbeddedObjectMeta=true,allowDangerousTypes=true paths=./api/... webhook paths="./..." output:crd:artifacts:config=config/crd/bases

verify-deps: deps-update
	@echo "Verifying dependency files"
	hack/verify-dependencies.sh

update-generated: generate manifests

verify-generated: update-generated
	@echo "Verifying generated code and manifests"
	hack/verify-generated.sh

# ARGS is used to pass flags
# `make run ARGS="--zap-devel"` is parsed as
# `go run ./main.go --zap-devel`
run: manifests generate
	go fmt ./...
	go vet ./...
	go run ./main.go $(ARGS)

# find or download controller-gen if necessary
controller-gen:
ifeq ($(wildcard ${CONTROLLER_GEN}),)
	@echo "Installing controller-gen@${CONTROLLER_GEN_VERSION} at ${CONTROLLER_GEN}"
	@GOBIN=$(LOCALBIN) go install -mod=readonly sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)
else ifneq ($(shell ${CONTROLLER_GEN} --version | awk '{print $$2}'), $(CONTROLLER_GEN_VERSION))
	@echo "Installing newer controller-gen@${CONTROLLER_GEN_VERSION} at ${CONTROLLER_GEN}"
	@GOBIN=$(LOCALBIN) go install -mod=readonly sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)
else
	@echo "Using existing controller-gen@${CONTROLLER_GEN_VERSION} at ${CONTROLLER_GEN}"
endif

# find or download kustomize if necessary
kustomize:
ifeq ($(wildcard ${KUSTOMIZE}),)
	@echo "Installing kustomize@${KUSTOMIZE_VERSION} at ${KUSTOMIZE}"
	@curl -s "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"  | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN)
else ifneq ($(shell ${KUSTOMIZE} version | awk -F'[ /]' '/Version/{print $$2}'), $(KUSTOMIZE_VERSION))
	@echo "Installing newer kustomize@${KUSTOMIZE_VERSION} at ${KUSTOMIZE}"
	@rm -f ${KUSTOMIZE}
	@curl -s "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"  | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN)
else
	@echo "Using existing kustomize@${KUSTOMIZE_VERSION} at ${KUSTOMIZE}"
endif
export KUSTOMIZE

install: operator-sdk
	@echo "Installing operators"
	hack/install-ocs-operator.sh
