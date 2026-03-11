GO ?= go
HELM ?= helm
KIND ?= kind
KUBECTL ?= kubectl

KIND_CLUSTER_NAME ?= nifi-fabric
NAMESPACE ?= nifi
HELM_RELEASE ?= nifi
LOCALBIN ?= $(PWD)/bin
ENVTEST ?= $(LOCALBIN)/setup-envtest
ENVTEST_K8S_VERSION ?= 1.31.0
CONTROLLER_IMAGE ?= nifi-fabric-controller:dev
NIFI_IMAGE ?= apache/nifi:2.0.0

.PHONY: fmt test test-unit test-envtest helm-lint run setup-envtest envtest-use kind-up kind-down kind-secrets kind-health kind-config-drift kind-tls-drift kind-tls-config-drift kind-tls-restart-e2e kind-hibernate kind-restore kind-alpha-e2e kind-e2e-rollout kind-e2e-config-drift kind-e2e-tls kind-e2e-hibernate kind-bootstrap-cert-manager kind-cert-manager-secrets kind-cert-manager-e2e kind-cert-manager-e2e-reuse kind-cert-manager-fast-e2e kind-cert-manager-fast-e2e-reuse kind-cert-manager-nifi-2-8-e2e kind-cert-manager-nifi-2-8-e2e-reuse kind-cert-manager-nifi-2-8-fast-e2e kind-cert-manager-nifi-2-8-fast-e2e-reuse kind-auth-oidc-e2e kind-auth-oidc-e2e-reuse kind-auth-oidc-fast-e2e kind-auth-oidc-fast-e2e-reuse kind-auth-ldap-e2e kind-auth-ldap-e2e-reuse kind-auth-ldap-fast-e2e kind-auth-ldap-fast-e2e-reuse kind-nifi-2-8-e2e kind-nifi-2-8-e2e-reuse kind-nifi-2-8-fast-e2e kind-nifi-2-8-fast-e2e-reuse kind-flow-registry-gitlab-e2e kind-flow-registry-gitlab-e2e-reuse kind-flow-registry-gitlab-fast-e2e kind-flow-registry-gitlab-fast-e2e-reuse docker-build-controller kind-load-controller kind-load-nifi-image deploy-controller undeploy-controller install-crd helm-install-standalone helm-install-managed apply-managed install-standalone install-managed install-managed-cert-manager

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

kind-load-nifi-image:
	bash hack/load-kind-image.sh $(KIND_CLUSTER_NAME) $(NIFI_IMAGE)

kind-secrets:
	bash hack/create-kind-secrets.sh $(NAMESPACE) $(HELM_RELEASE) nifi-tls nifi-auth

kind-health:
	bash hack/check-nifi-health.sh --namespace $(NAMESPACE) --statefulset $(HELM_RELEASE) --auth-secret nifi-auth

kind-config-drift:
	bash hack/trigger-config-drift.sh --namespace $(NAMESPACE) --configmap $(HELM_RELEASE)-config

kind-tls-drift:
	bash hack/trigger-tls-drift.sh --namespace $(NAMESPACE) --secret nifi-tls

kind-tls-config-drift:
	$(HELM) upgrade --install $(HELM_RELEASE) charts/nifi --namespace $(NAMESPACE) -f examples/managed/values.yaml --reuse-values --set tls.mountPath=/opt/nifi/tls-alt

kind-tls-restart-e2e:
	bash hack/kind-tls-restart-e2e.sh

kind-hibernate:
	$(KUBECTL) -n $(NAMESPACE) patch nificluster $(HELM_RELEASE) --type merge -p '{"spec":{"desiredState":"Hibernated"}}'

kind-restore:
	$(KUBECTL) -n $(NAMESPACE) patch nificluster $(HELM_RELEASE) --type merge -p '{"spec":{"desiredState":"Running"}}'

kind-alpha-e2e:
	bash hack/kind-alpha-e2e.sh

kind-e2e-rollout:
	bash hack/kind-alpha-e2e.sh --phase rollout

kind-e2e-config-drift:
	bash hack/kind-alpha-e2e.sh --phase config-drift

kind-e2e-tls:
	bash hack/kind-alpha-e2e.sh --phase tls

kind-e2e-hibernate:
	bash hack/kind-alpha-e2e.sh --phase hibernate

kind-bootstrap-cert-manager:
	bash hack/kind-bootstrap-cert-manager.sh

kind-cert-manager-secrets:
	bash hack/create-kind-cert-manager-secrets.sh $(NAMESPACE) nifi-auth nifi-tls-params

kind-cert-manager-e2e:
	bash hack/kind-cert-manager-e2e.sh

kind-cert-manager-e2e-reuse:
	SKIP_KIND_BOOTSTRAP=true bash hack/kind-cert-manager-e2e.sh

kind-cert-manager-fast-e2e:
	FAST_PROFILE=true bash hack/kind-cert-manager-e2e.sh

kind-cert-manager-fast-e2e-reuse:
	FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-cert-manager-e2e.sh

kind-cert-manager-nifi-2-8-e2e:
	NIFI_IMAGE=apache/nifi:2.8.0 VERSION_VALUES_FILE=examples/nifi-2.8.0-values.yaml KIND_CLUSTER_NAME=nifi-fabric-cert-manager-nifi-2-8 bash hack/kind-cert-manager-e2e.sh

kind-cert-manager-nifi-2-8-e2e-reuse:
	NIFI_IMAGE=apache/nifi:2.8.0 VERSION_VALUES_FILE=examples/nifi-2.8.0-values.yaml KIND_CLUSTER_NAME=nifi-fabric-cert-manager-nifi-2-8 SKIP_KIND_BOOTSTRAP=true bash hack/kind-cert-manager-e2e.sh

kind-cert-manager-nifi-2-8-fast-e2e:
	NIFI_IMAGE=apache/nifi:2.8.0 VERSION_VALUES_FILE=examples/nifi-2.8.0-values.yaml KIND_CLUSTER_NAME=nifi-fabric-cert-manager-nifi-2-8 FAST_PROFILE=true bash hack/kind-cert-manager-e2e.sh

kind-cert-manager-nifi-2-8-fast-e2e-reuse:
	NIFI_IMAGE=apache/nifi:2.8.0 VERSION_VALUES_FILE=examples/nifi-2.8.0-values.yaml KIND_CLUSTER_NAME=nifi-fabric-cert-manager-nifi-2-8 FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-cert-manager-e2e.sh

kind-auth-oidc-e2e:
	bash hack/kind-auth-oidc-e2e.sh

kind-auth-oidc-e2e-reuse:
	SKIP_KIND_BOOTSTRAP=true bash hack/kind-auth-oidc-e2e.sh

kind-auth-oidc-fast-e2e:
	FAST_PROFILE=true bash hack/kind-auth-oidc-e2e.sh

kind-auth-oidc-fast-e2e-reuse:
	FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-auth-oidc-e2e.sh

kind-auth-ldap-e2e:
	bash hack/kind-auth-ldap-e2e.sh

kind-auth-ldap-e2e-reuse:
	SKIP_KIND_BOOTSTRAP=true bash hack/kind-auth-ldap-e2e.sh

kind-auth-ldap-fast-e2e:
	FAST_PROFILE=true bash hack/kind-auth-ldap-e2e.sh

kind-auth-ldap-fast-e2e-reuse:
	FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-auth-ldap-e2e.sh

kind-nifi-2-8-e2e:
	bash hack/kind-nifi-2-8-e2e.sh

kind-nifi-2-8-e2e-reuse:
	SKIP_KIND_BOOTSTRAP=true bash hack/kind-nifi-2-8-e2e.sh

kind-nifi-2-8-fast-e2e:
	FAST_PROFILE=true bash hack/kind-nifi-2-8-e2e.sh

kind-nifi-2-8-fast-e2e-reuse:
	FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-nifi-2-8-e2e.sh

kind-flow-registry-gitlab-e2e:
	bash hack/kind-flow-registry-gitlab-e2e.sh

kind-flow-registry-gitlab-e2e-reuse:
	SKIP_KIND_BOOTSTRAP=true bash hack/kind-flow-registry-gitlab-e2e.sh

kind-flow-registry-gitlab-fast-e2e:
	FAST_PROFILE=true bash hack/kind-flow-registry-gitlab-e2e.sh

kind-flow-registry-gitlab-fast-e2e-reuse:
	FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-flow-registry-gitlab-e2e.sh

docker-build-controller:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -o bin/manager ./main.go
	docker build -t $(CONTROLLER_IMAGE) .

kind-load-controller:
	$(KIND) load docker-image $(CONTROLLER_IMAGE) --name $(KIND_CLUSTER_NAME)

deploy-controller:
	$(KUBECTL) apply -f config/manager/namespace.yaml
	$(KUBECTL) apply -f config/rbac/service_account.yaml -f config/rbac/role.yaml -f config/rbac/role_binding.yaml
	$(KUBECTL) apply -f config/manager/manager.yaml

undeploy-controller:
	-$(KUBECTL) delete -f config/manager/manager.yaml
	-$(KUBECTL) delete -f config/rbac/role_binding.yaml -f config/rbac/role.yaml -f config/rbac/service_account.yaml
	-$(KUBECTL) delete -f config/manager/namespace.yaml

install-crd:
	$(KUBECTL) apply -f config/crd/bases/platform.nifi.io_nificlusters.yaml

helm-install-standalone:
	$(HELM) upgrade --install $(HELM_RELEASE) charts/nifi --namespace $(NAMESPACE) --create-namespace -f examples/standalone/values.yaml

helm-install-managed:
	$(HELM) upgrade --install $(HELM_RELEASE) charts/nifi --namespace $(NAMESPACE) --create-namespace -f examples/managed/values.yaml

apply-managed:
	$(KUBECTL) apply -f examples/managed/nificluster.yaml

install-standalone:
	bash hack/install-standalone.sh

install-managed:
	bash hack/install-managed.sh

install-managed-cert-manager:
	bash hack/install-managed-cert-manager.sh
