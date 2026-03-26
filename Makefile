GO ?= go
HELM ?= helm
KIND ?= kind
KUBECTL ?= kubectl
CONTROLLER_NAMESPACE ?= nifi-system
CONTROLLER_DEPLOYMENT ?= nifi-controller-manager
MANAGED ?= auto
AUTH_SECRET ?= nifi-auth
TLS_SECRET ?= nifi-tls
TLS_PARAMS_SECRET ?=
CERTIFICATE ?=
STATEFULSET_NAME ?= $(HELM_RELEASE)
CLUSTER_NAME ?= $(HELM_RELEASE)
SERVICE_NAME ?= $(HELM_RELEASE)

KIND_CLUSTER_NAME ?= nifi-fabric
NAMESPACE ?= nifi
HELM_RELEASE ?= nifi
LOCALBIN ?= $(PWD)/bin
ENVTEST ?= $(LOCALBIN)/setup-envtest
ENVTEST_K8S_VERSION ?= 1.31.0
CONTROLLER_IMAGE ?= nifi-fabric-controller:dev
NIFI_IMAGE ?= apache/nifi:2.0.0

.PHONY: fmt test test-unit test-envtest helm-lint run setup-envtest envtest-use first-day-check kind-up kind-down kind-secrets kind-health kind-config-drift kind-tls-drift kind-tls-config-drift kind-tls-restart-e2e kind-hibernate kind-restore kind-alpha-e2e kind-e2e-rollout kind-e2e-config-drift kind-e2e-tls kind-e2e-hibernate kind-bootstrap-cert-manager kind-cert-manager-secrets kind-cert-manager-e2e kind-cert-manager-e2e-reuse kind-cert-manager-fast-e2e kind-cert-manager-fast-e2e-reuse kind-cert-manager-nifi-2-8-e2e kind-cert-manager-nifi-2-8-e2e-reuse kind-cert-manager-nifi-2-8-fast-e2e kind-cert-manager-nifi-2-8-fast-e2e-reuse kind-platform-managed-e2e kind-platform-managed-e2e-reuse kind-platform-managed-fast-e2e kind-platform-managed-fast-e2e-reuse kind-platform-managed-restore-fast-e2e kind-platform-managed-restore-fast-e2e-reuse kind-parameter-contexts-runtime-fast-e2e kind-parameter-contexts-runtime-fast-e2e-reuse kind-platform-managed-versioned-flow-import-fast-e2e kind-platform-managed-versioned-flow-import-fast-e2e-reuse kind-platform-managed-versioned-flow-import-nifi-registry-fast-e2e kind-platform-managed-versioned-flow-import-nifi-registry-fast-e2e-reuse kind-platform-managed-cert-manager-e2e kind-platform-managed-cert-manager-e2e-reuse kind-platform-managed-cert-manager-fast-e2e kind-platform-managed-cert-manager-fast-e2e-reuse kind-platform-managed-trust-manager-fast-e2e kind-platform-managed-trust-manager-fast-e2e-reuse kind-metrics-fast-e2e kind-metrics-fast-e2e-reuse kind-metrics-native-api-fast-e2e kind-metrics-native-api-fast-e2e-reuse kind-metrics-native-api-trust-manager-fast-e2e kind-metrics-native-api-trust-manager-fast-e2e-reuse kind-metrics-exporter-fast-e2e kind-metrics-exporter-fast-e2e-reuse kind-metrics-exporter-trust-manager-fast-e2e kind-metrics-exporter-trust-manager-fast-e2e-reuse kind-metrics-site-to-site-fast-e2e kind-metrics-site-to-site-fast-e2e-reuse kind-site-to-site-status-fast-e2e kind-site-to-site-status-fast-e2e-reuse kind-site-to-site-provenance-fast-e2e kind-site-to-site-provenance-fast-e2e-reuse kind-keda-scale-up-fast-e2e kind-keda-scale-up-fast-e2e-reuse kind-keda-scale-down-fast-e2e kind-keda-scale-down-fast-e2e-reuse kind-linkerd-e2e kind-linkerd-e2e-reuse kind-linkerd-fast-e2e kind-linkerd-fast-e2e-reuse kind-istio-ambient-e2e kind-istio-ambient-e2e-reuse kind-istio-ambient-fast-e2e kind-istio-ambient-fast-e2e-reuse kind-auth-oidc-e2e kind-auth-oidc-e2e-reuse kind-auth-oidc-fast-e2e kind-auth-oidc-fast-e2e-reuse kind-auth-oidc-ingress-fast-e2e kind-auth-oidc-ingress-fast-e2e-reuse kind-auth-oidc-initial-admin-group-fast-e2e kind-auth-oidc-initial-admin-group-fast-e2e-reuse kind-auth-oidc-nifi-2-8-fast-e2e kind-auth-oidc-nifi-2-8-fast-e2e-reuse kind-auth-ldap-e2e kind-auth-ldap-e2e-reuse kind-auth-ldap-fast-e2e kind-auth-ldap-fast-e2e-reuse kind-nifi-2-8-e2e kind-nifi-2-8-e2e-reuse kind-nifi-2-8-fast-e2e kind-nifi-2-8-fast-e2e-reuse kind-nifi-compatibility-fast-e2e kind-nifi-compatibility-fast-e2e-reuse kind-autoscaling-scale-up-fast-e2e kind-autoscaling-scale-up-fast-e2e-reuse kind-autoscaling-scale-down-fast-e2e kind-autoscaling-scale-down-fast-e2e-reuse kind-autoscaling-churn-fast-e2e kind-autoscaling-churn-fast-e2e-reuse kind-flow-registry-gitlab-e2e kind-flow-registry-gitlab-e2e-reuse kind-flow-registry-gitlab-fast-e2e kind-flow-registry-gitlab-fast-e2e-reuse kind-flow-registry-github-fast-e2e kind-flow-registry-github-fast-e2e-reuse kind-flow-registry-github-workflow-fast-e2e kind-flow-registry-github-workflow-fast-e2e-reuse kind-versioned-flow-selection-fast-e2e kind-versioned-flow-selection-fast-e2e-reuse kind-flow-registry-bitbucket-fast-e2e kind-flow-registry-bitbucket-fast-e2e-reuse openshift-platform-managed-proof openshift-platform-managed-route-proof package-platform-chart render-platform-managed-bundle render-platform-managed-cert-manager-bundle render-platform-standalone-bundle docker-build-controller kind-load-controller kind-load-nifi-image deploy-controller undeploy-controller install-crd helm-install-standalone helm-install-managed apply-managed install-standalone install-managed install-managed-cert-manager

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

first-day-check:
	bash hack/first-day-check.sh \
		--namespace $(NAMESPACE) \
		--release $(HELM_RELEASE) \
		--statefulset $(STATEFULSET_NAME) \
		--cluster-name $(CLUSTER_NAME) \
		--managed $(MANAGED) \
		--controller-namespace $(CONTROLLER_NAMESPACE) \
		--controller-deployment $(CONTROLLER_DEPLOYMENT) \
		--service $(SERVICE_NAME) \
		--auth-secret $(AUTH_SECRET) \
		--tls-secret $(TLS_SECRET) \
		$(if $(TLS_PARAMS_SECRET),--tls-params-secret $(TLS_PARAMS_SECRET),) \
		$(if $(CERTIFICATE),--certificate $(CERTIFICATE),)

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

kind-platform-managed-e2e:
	KIND_CLUSTER_NAME=nifi-fabric-platform-managed bash hack/kind-platform-managed-e2e.sh

kind-platform-managed-e2e-reuse:
	KIND_CLUSTER_NAME=nifi-fabric-platform-managed SKIP_KIND_BOOTSTRAP=true bash hack/kind-platform-managed-e2e.sh

kind-platform-managed-fast-e2e:
	KIND_CLUSTER_NAME=nifi-fabric-platform-managed FAST_PROFILE=true bash hack/kind-platform-managed-e2e.sh

kind-platform-managed-fast-e2e-reuse:
	KIND_CLUSTER_NAME=nifi-fabric-platform-managed FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-platform-managed-e2e.sh

kind-platform-managed-restore-fast-e2e:
	KIND_CLUSTER_NAME=nifi-fabric-platform-restore FAST_PROFILE=true bash hack/kind-platform-managed-restore-e2e.sh

kind-platform-managed-restore-fast-e2e-reuse:
	KIND_CLUSTER_NAME=nifi-fabric-platform-restore FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-platform-managed-restore-e2e.sh

kind-parameter-contexts-runtime-fast-e2e:
	KIND_CLUSTER_NAME=nifi-fabric-parameter-contexts FAST_PROFILE=true bash hack/kind-parameter-contexts-runtime-e2e.sh

kind-parameter-contexts-runtime-fast-e2e-reuse:
	KIND_CLUSTER_NAME=nifi-fabric-parameter-contexts FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-parameter-contexts-runtime-e2e.sh

kind-platform-managed-versioned-flow-import-fast-e2e:
	KIND_CLUSTER_NAME=nifi-fabric-platform-versioned-flow-import FAST_PROFILE=true bash hack/kind-platform-managed-versioned-flow-import-e2e.sh

kind-platform-managed-versioned-flow-import-fast-e2e-reuse:
	KIND_CLUSTER_NAME=nifi-fabric-platform-versioned-flow-import FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-platform-managed-versioned-flow-import-e2e.sh

kind-platform-managed-versioned-flow-import-nifi-registry-fast-e2e:
	KIND_CLUSTER_NAME=nifi-fabric-flow-import-registry FAST_PROFILE=true bash hack/kind-platform-managed-versioned-flow-import-nifi-registry-e2e.sh

kind-platform-managed-versioned-flow-import-nifi-registry-fast-e2e-reuse:
	KIND_CLUSTER_NAME=nifi-fabric-flow-import-registry FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-platform-managed-versioned-flow-import-nifi-registry-e2e.sh

kind-platform-managed-cert-manager-e2e:
	KIND_CLUSTER_NAME=nifi-fabric-platform-managed-cert-manager bash hack/kind-platform-managed-cert-manager-e2e.sh

kind-platform-managed-cert-manager-e2e-reuse:
	KIND_CLUSTER_NAME=nifi-fabric-platform-managed-cert-manager SKIP_KIND_BOOTSTRAP=true bash hack/kind-platform-managed-cert-manager-e2e.sh

kind-platform-managed-cert-manager-fast-e2e:
	KIND_CLUSTER_NAME=nifi-fabric-platform-managed-cert-manager FAST_PROFILE=true bash hack/kind-platform-managed-cert-manager-e2e.sh

kind-platform-managed-cert-manager-fast-e2e-reuse:
	KIND_CLUSTER_NAME=nifi-fabric-platform-managed-cert-manager FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-platform-managed-cert-manager-e2e.sh

kind-platform-managed-trust-manager-fast-e2e:
	KIND_CLUSTER_NAME=nifi-fabric-platform-managed-trust-manager FAST_PROFILE=true bash hack/kind-platform-managed-trust-manager-e2e.sh

kind-platform-managed-trust-manager-fast-e2e-reuse:
	KIND_CLUSTER_NAME=nifi-fabric-platform-managed-trust-manager FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-platform-managed-trust-manager-e2e.sh

kind-metrics-fast-e2e:
	FAST_PROFILE=true bash hack/kind-metrics-matrix-e2e.sh

kind-metrics-fast-e2e-reuse:
	FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-metrics-matrix-e2e.sh

kind-metrics-native-api-fast-e2e:
	KIND_CLUSTER_NAME=nifi-fabric-metrics-native-api FAST_PROFILE=true bash hack/kind-metrics-native-api-e2e.sh

kind-metrics-native-api-fast-e2e-reuse:
	KIND_CLUSTER_NAME=nifi-fabric-metrics-native-api FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-metrics-native-api-e2e.sh

kind-metrics-native-api-trust-manager-fast-e2e:
	KIND_CLUSTER_NAME=nifi-fabric-metrics-native-api-trust-manager FAST_PROFILE=true TRUST_MANAGER_ENABLED=true bash hack/kind-metrics-native-api-e2e.sh

kind-metrics-native-api-trust-manager-fast-e2e-reuse:
	KIND_CLUSTER_NAME=nifi-fabric-metrics-native-api-trust-manager FAST_PROFILE=true TRUST_MANAGER_ENABLED=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-metrics-native-api-e2e.sh

kind-metrics-exporter-fast-e2e:
	KIND_CLUSTER_NAME=nifi-fabric-metrics-exporter FAST_PROFILE=true bash hack/kind-metrics-exporter-e2e.sh

kind-metrics-exporter-fast-e2e-reuse:
	KIND_CLUSTER_NAME=nifi-fabric-metrics-exporter FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-metrics-exporter-e2e.sh

kind-metrics-exporter-trust-manager-fast-e2e:
	KIND_CLUSTER_NAME=nifi-fabric-metrics-exporter-trust-manager FAST_PROFILE=true TRUST_MANAGER_ENABLED=true bash hack/kind-metrics-exporter-e2e.sh

kind-metrics-exporter-trust-manager-fast-e2e-reuse:
	KIND_CLUSTER_NAME=nifi-fabric-metrics-exporter-trust-manager FAST_PROFILE=true TRUST_MANAGER_ENABLED=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-metrics-exporter-e2e.sh

kind-metrics-site-to-site-fast-e2e:
	KIND_CLUSTER_NAME=nifi-fabric-metrics-site-to-site FAST_PROFILE=true bash hack/kind-metrics-site-to-site-e2e.sh

kind-metrics-site-to-site-fast-e2e-reuse:
	KIND_CLUSTER_NAME=nifi-fabric-metrics-site-to-site FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-metrics-site-to-site-e2e.sh

kind-site-to-site-status-fast-e2e:
	KIND_CLUSTER_NAME=nifi-fabric-site-to-site-status FAST_PROFILE=true bash hack/kind-site-to-site-status-e2e.sh

kind-site-to-site-status-fast-e2e-reuse:
	KIND_CLUSTER_NAME=nifi-fabric-site-to-site-status FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-site-to-site-status-e2e.sh

kind-site-to-site-provenance-fast-e2e:
	KIND_CLUSTER_NAME=nifi-fabric-site-to-site-provenance FAST_PROFILE=true bash hack/kind-site-to-site-provenance-e2e.sh

kind-site-to-site-provenance-fast-e2e-reuse:
	KIND_CLUSTER_NAME=nifi-fabric-site-to-site-provenance FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-site-to-site-provenance-e2e.sh

kind-keda-scale-up-fast-e2e:
	NIFI_IMAGE=apache/nifi:2.8.0 VERSION_VALUES_FILE=examples/nifi-2.8.0-values.yaml KIND_CLUSTER_NAME=nifi-fabric-keda-scale-up FAST_PROFILE=true bash hack/kind-keda-scale-up-e2e.sh

kind-keda-scale-up-fast-e2e-reuse:
	NIFI_IMAGE=apache/nifi:2.8.0 VERSION_VALUES_FILE=examples/nifi-2.8.0-values.yaml KIND_CLUSTER_NAME=nifi-fabric-keda-scale-up FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-keda-scale-up-e2e.sh

kind-keda-scale-down-fast-e2e:
	NIFI_IMAGE=apache/nifi:2.8.0 VERSION_VALUES_FILE=examples/nifi-2.8.0-values.yaml KIND_CLUSTER_NAME=nifi-fabric-keda-scale-down FAST_PROFILE=true bash hack/kind-keda-scale-down-e2e.sh

kind-keda-scale-down-fast-e2e-reuse:
	NIFI_IMAGE=apache/nifi:2.8.0 VERSION_VALUES_FILE=examples/nifi-2.8.0-values.yaml KIND_CLUSTER_NAME=nifi-fabric-keda-scale-down FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-keda-scale-down-e2e.sh

kind-linkerd-e2e:
	KIND_CLUSTER_NAME=nifi-fabric-linkerd bash hack/kind-linkerd-e2e.sh

kind-linkerd-e2e-reuse:
	KIND_CLUSTER_NAME=nifi-fabric-linkerd SKIP_KIND_BOOTSTRAP=true bash hack/kind-linkerd-e2e.sh

kind-linkerd-fast-e2e:
	KIND_CLUSTER_NAME=nifi-fabric-linkerd FAST_PROFILE=true bash hack/kind-linkerd-e2e.sh

kind-linkerd-fast-e2e-reuse:
	KIND_CLUSTER_NAME=nifi-fabric-linkerd FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-linkerd-e2e.sh

kind-istio-ambient-e2e:
	KIND_CLUSTER_NAME=nifi-fabric-istio-ambient bash hack/kind-istio-ambient-e2e.sh

kind-istio-ambient-e2e-reuse:
	KIND_CLUSTER_NAME=nifi-fabric-istio-ambient SKIP_KIND_BOOTSTRAP=true bash hack/kind-istio-ambient-e2e.sh

kind-istio-ambient-fast-e2e:
	KIND_CLUSTER_NAME=nifi-fabric-istio-ambient FAST_PROFILE=true bash hack/kind-istio-ambient-e2e.sh

kind-istio-ambient-fast-e2e-reuse:
	KIND_CLUSTER_NAME=nifi-fabric-istio-ambient FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-istio-ambient-e2e.sh

kind-istio-e2e:
	KIND_CLUSTER_NAME=nifi-fabric-istio bash hack/kind-istio-e2e.sh

kind-istio-e2e-reuse:
	KIND_CLUSTER_NAME=nifi-fabric-istio SKIP_KIND_BOOTSTRAP=true bash hack/kind-istio-e2e.sh

kind-istio-fast-e2e:
	KIND_CLUSTER_NAME=nifi-fabric-istio FAST_PROFILE=true bash hack/kind-istio-e2e.sh

kind-istio-fast-e2e-reuse:
	KIND_CLUSTER_NAME=nifi-fabric-istio FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-istio-e2e.sh

kind-auth-oidc-e2e:
	bash hack/kind-auth-oidc-e2e.sh

kind-auth-oidc-e2e-reuse:
	SKIP_KIND_BOOTSTRAP=true bash hack/kind-auth-oidc-e2e.sh

kind-auth-oidc-fast-e2e:
	FAST_PROFILE=true bash hack/kind-auth-oidc-e2e.sh

kind-auth-oidc-fast-e2e-reuse:
	FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-auth-oidc-e2e.sh

kind-auth-oidc-ingress-fast-e2e:
	FAST_PROFILE=true OIDC_EXTERNAL_INGRESS=true KIND_CLUSTER_NAME=nifi-fabric-auth-oidc-ingress bash hack/kind-auth-oidc-e2e.sh

kind-auth-oidc-ingress-fast-e2e-reuse:
	FAST_PROFILE=true OIDC_EXTERNAL_INGRESS=true KIND_CLUSTER_NAME=nifi-fabric-auth-oidc-ingress SKIP_KIND_BOOTSTRAP=true bash hack/kind-auth-oidc-e2e.sh

kind-auth-oidc-initial-admin-group-fast-e2e:
	FAST_PROFILE=true OIDC_INITIAL_ADMIN_GROUP=true KIND_CLUSTER_NAME=nifi-fabric-auth-oidc-initial-admin-group bash hack/kind-auth-oidc-e2e.sh

kind-auth-oidc-initial-admin-group-fast-e2e-reuse:
	FAST_PROFILE=true OIDC_INITIAL_ADMIN_GROUP=true KIND_CLUSTER_NAME=nifi-fabric-auth-oidc-initial-admin-group SKIP_KIND_BOOTSTRAP=true bash hack/kind-auth-oidc-e2e.sh

kind-auth-oidc-nifi-2-8-fast-e2e:
	NIFI_IMAGE=apache/nifi:2.8.0 VERSION_VALUES_FILE=examples/nifi-2.8.0-values.yaml KIND_CLUSTER_NAME=nifi-fabric-auth-oidc-nifi-2-8 FAST_PROFILE=true bash hack/kind-auth-oidc-e2e.sh

kind-auth-oidc-nifi-2-8-fast-e2e-reuse:
	NIFI_IMAGE=apache/nifi:2.8.0 VERSION_VALUES_FILE=examples/nifi-2.8.0-values.yaml KIND_CLUSTER_NAME=nifi-fabric-auth-oidc-nifi-2-8 FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-auth-oidc-e2e.sh

kind-auth-ldap-e2e:
	bash hack/kind-auth-ldap-e2e.sh

openshift-platform-managed-proof:
	bash hack/openshift-platform-managed-proof.sh

openshift-platform-managed-route-proof:
	ROUTE_PROOF=true bash hack/openshift-platform-managed-proof.sh

openshift-platform-managed-oidc-proof:
	ROUTE_PROOF=true AUTH_PROOF_MODE=oidc bash hack/openshift-platform-managed-proof.sh

openshift-platform-managed-ldap-proof:
	ROUTE_PROOF=true AUTH_PROOF_MODE=ldap bash hack/openshift-platform-managed-proof.sh

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

kind-nifi-compatibility-fast-e2e:
	bash hack/kind-nifi-version-sweep.sh

kind-nifi-compatibility-fast-e2e-reuse:
	COMPATIBILITY_VERSIONS="2.0.0 2.1.0 2.2.0 2.3.0 2.4.0 2.5.0 2.6.0 2.7.0 2.8.0" bash hack/kind-nifi-version-sweep.sh

kind-autoscaling-scale-up-fast-e2e:
	NIFI_IMAGE=apache/nifi:2.8.0 VERSION_VALUES_FILE=examples/nifi-2.8.0-values.yaml KIND_CLUSTER_NAME=nifi-fabric-autoscaling-scale-up FAST_PROFILE=true bash hack/kind-autoscaling-scale-up-e2e.sh

kind-autoscaling-scale-up-fast-e2e-reuse:
	NIFI_IMAGE=apache/nifi:2.8.0 VERSION_VALUES_FILE=examples/nifi-2.8.0-values.yaml KIND_CLUSTER_NAME=nifi-fabric-autoscaling-scale-up FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-autoscaling-scale-up-e2e.sh

kind-autoscaling-scale-down-fast-e2e:
	NIFI_IMAGE=apache/nifi:2.8.0 VERSION_VALUES_FILE=examples/nifi-2.8.0-values.yaml KIND_CLUSTER_NAME=nifi-fabric-autoscaling-scale-down FAST_PROFILE=true bash hack/kind-autoscaling-scale-down-e2e.sh

kind-autoscaling-scale-down-fast-e2e-reuse:
	NIFI_IMAGE=apache/nifi:2.8.0 VERSION_VALUES_FILE=examples/nifi-2.8.0-values.yaml KIND_CLUSTER_NAME=nifi-fabric-autoscaling-scale-down FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-autoscaling-scale-down-e2e.sh

kind-autoscaling-churn-fast-e2e:
	NIFI_IMAGE=apache/nifi:2.8.0 VERSION_VALUES_FILE=examples/nifi-2.8.0-values.yaml KIND_CLUSTER_NAME=nifi-fabric-autoscaling-churn FAST_PROFILE=true AUTOSCALING_CHURN_MODE=true bash hack/kind-autoscaling-scale-down-e2e.sh

kind-autoscaling-churn-fast-e2e-reuse:
	NIFI_IMAGE=apache/nifi:2.8.0 VERSION_VALUES_FILE=examples/nifi-2.8.0-values.yaml KIND_CLUSTER_NAME=nifi-fabric-autoscaling-churn FAST_PROFILE=true AUTOSCALING_CHURN_MODE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-autoscaling-scale-down-e2e.sh

kind-flow-registry-gitlab-e2e:
	bash hack/kind-flow-registry-gitlab-e2e.sh

kind-flow-registry-gitlab-e2e-reuse:
	SKIP_KIND_BOOTSTRAP=true bash hack/kind-flow-registry-gitlab-e2e.sh

kind-flow-registry-gitlab-fast-e2e:
	FAST_PROFILE=true bash hack/kind-flow-registry-gitlab-e2e.sh

kind-flow-registry-gitlab-fast-e2e-reuse:
	FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-flow-registry-gitlab-e2e.sh

kind-flow-registry-github-fast-e2e:
	FAST_PROFILE=true bash hack/kind-flow-registry-github-e2e.sh

kind-flow-registry-github-fast-e2e-reuse:
	FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-flow-registry-github-e2e.sh

kind-flow-registry-github-workflow-fast-e2e:
	FAST_PROFILE=true WORKFLOW_PROOF=true bash hack/kind-flow-registry-github-e2e.sh

kind-flow-registry-github-workflow-fast-e2e-reuse:
	FAST_PROFILE=true WORKFLOW_PROOF=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-flow-registry-github-e2e.sh

kind-versioned-flow-selection-fast-e2e:
	FAST_PROFILE=true FLOW_SELECTION_PROOF=true bash hack/kind-flow-registry-github-e2e.sh

kind-versioned-flow-selection-fast-e2e-reuse:
	FAST_PROFILE=true FLOW_SELECTION_PROOF=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-flow-registry-github-e2e.sh

kind-flow-registry-bitbucket-fast-e2e:
	FAST_PROFILE=true bash hack/kind-flow-registry-bitbucket-e2e.sh

kind-flow-registry-bitbucket-fast-e2e-reuse:
	FAST_PROFILE=true SKIP_KIND_BOOTSTRAP=true bash hack/kind-flow-registry-bitbucket-e2e.sh

package-platform-chart:
	bash hack/package-platform-chart.sh

render-platform-managed-bundle:
	bash hack/render-platform-bundle.sh --profile managed --output dist/nifi-platform-managed-bundle.yaml

render-platform-managed-cert-manager-bundle:
	bash hack/render-platform-bundle.sh --profile managed-cert-manager --output dist/nifi-platform-managed-cert-manager-bundle.yaml

render-platform-standalone-bundle:
	bash hack/render-platform-bundle.sh --profile standalone --output dist/nifi-platform-standalone-bundle.yaml

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
