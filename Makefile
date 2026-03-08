GO ?= go
HELM ?= helm
KIND ?= kind
KUBECTL ?= kubectl

KIND_CLUSTER_NAME ?= nifi2-platform
NAMESPACE ?= nifi
HELM_RELEASE ?= nifi
LOCALBIN ?= $(PWD)/bin
ENVTEST ?= $(LOCALBIN)/setup-envtest
ENVTEST_K8S_VERSION ?= 1.31.0

.PHONY: fmt test test-unit test-envtest helm-lint run setup-envtest envtest-use kind-up kind-down install-crd helm-install-standalone helm-install-managed apply-managed

fmt:
	$(GO) fmt ./...

test: test-unit test-envtest

test-unit:
	$(GO) test ./api/... ./internal/... ./test/unit/...

test-envtest:
	$(GO) test ./test/envtest/...

helm-lint:
	$(HELM) lint charts/nifi

run:
	$(GO) run ./main.go

$(LOCALBIN):
	mkdir -p $(LOCALBIN)

setup-envtest: | $(LOCALBIN)
	GOBIN=$(LOCALBIN) $(GO) install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

envtest-use: setup-envtest
	@echo "export KUBEBUILDER_ASSETS=$$($(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)"

kind-up:
	$(KIND) create cluster --name $(KIND_CLUSTER_NAME) --config hack/kind-cluster.yaml

kind-down:
	$(KIND) delete cluster --name $(KIND_CLUSTER_NAME)

install-crd:
	$(KUBECTL) apply -f config/crd/bases/platform.nifi.io_nificlusters.yaml

helm-install-standalone:
	$(HELM) upgrade --install $(HELM_RELEASE) charts/nifi --namespace $(NAMESPACE) --create-namespace -f examples/standalone/values.yaml

helm-install-managed:
	$(HELM) upgrade --install $(HELM_RELEASE) charts/nifi --namespace $(NAMESPACE) --create-namespace -f examples/managed/values.yaml

apply-managed:
	$(KUBECTL) apply -f examples/managed/nificluster.yaml

