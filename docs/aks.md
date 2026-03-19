# AKS Readiness Guide

This guide prepares `NiFi-Fabric` for future AKS evaluation. It is not an AKS validation report.

## Current Status

Proven on kind:

- standalone Helm install
- managed-mode install with the thin controller
- per-pod NiFi health gate
- managed revision rollout
- config drift rollout
- TLS observe-only handling
- restart-required TLS rollout
- hibernation and restore
- focused cert-manager evaluator flow

Prepared for AKS:

- AKS-oriented Helm values overlays in `examples/aks/`
- managed-mode install steps using the existing CRD, controller, and chart
- storage, service, ingress, and TLS assumptions documented here
- controller and chart responsibilities unchanged from the proven kind flow

Not yet validated on AKS:

- runtime behavior on real AKS nodes
- StorageClass behavior beyond the documented assumptions
- ingress or Azure Load Balancer exposure
- cert-manager renewal behavior on AKS
- private registry and image-pull workflows beyond the documented assumptions

## Prerequisites

- an AKS cluster with Kubernetes support for `StatefulSet`, PVCs, Services, and RBAC
- `kubectl`, `helm`, `az`, `curl`, `jq`, and `openssl`
- a registry reachable by AKS nodes for the controller image
- outbound image pull access for `apache/nifi:2.0.0`, or a mirrored equivalent
- either:
  - an external TLS Secret plus auth Secret
  - or a preinstalled cert-manager plus issuer flow compatible with the existing chart contract

## Expected Cluster Features

The current chart and controller assume:

- CoreDNS or equivalent internal cluster DNS
- dynamic `ReadWriteOnce` volume provisioning
- a default or explicit StorageClass suitable for NiFi repository PVCs
- internal pod-to-pod HTTPS reachability on the NiFi cluster ports
- Kubernetes API access from the controller Pod

The controller health gate and lifecycle actions still rely on direct pod DNS names behind the headless Service. That is the same model proven on kind.

## Image Registry And Pull Assumptions

Current assumptions:

- the NiFi chart defaults to `apache/nifi:2.0.0`
- AKS nodes must be able to pull that image directly, or you must override `image.repository` and `image.tag` to a mirrored image
- the controller deployment manifest in `config/manager/manager.yaml` still defaults to the local dev image `nifi-fabric-controller:dev` with `imagePullPolicy: Never`

For AKS evaluation, build and push the controller image to a registry AKS can reach:

```bash
export CONTROLLER_IMAGE=<your-registry>/nifi-fabric-controller:alpha
docker build -t "${CONTROLLER_IMAGE}" .
docker push "${CONTROLLER_IMAGE}"
```

Then patch the manager deployment after applying it:

```bash
kubectl -n nifi-system set image deployment/nifi-fabric-controller-manager manager="${CONTROLLER_IMAGE}"
kubectl -n nifi-system patch deployment nifi-fabric-controller-manager \
  --type='json' \
  -p='[{"op":"replace","path":"/spec/template/spec/containers/0/imagePullPolicy","value":"IfNotPresent"}]'
```

`NiFi-Fabric` now exposes first-class image pull secret values for both the controller and the managed NiFi workload:

- `controller.imagePullSecrets[]`
- `nifi.imagePullSecrets[]`

If AKS nodes cannot pull directly from your registry, set those values in your AKS overlay instead of patching the deployed manifests.

## Storage Assumptions

The chart creates one PVC each for:

- `database_repository`
- `flowfile_repository`
- `content_repository`
- `provenance_repository`

Prepared AKS assumption:

- `managed-csi` is the starting StorageClass for evaluator use

This is reflected in:

- `examples/aks/standalone-values.yaml`
- `examples/aks/managed-values.yaml`

Not yet validated:

- real AKS PVC provisioning timing
- retention semantics across scale-down and cluster lifecycle
- performance characteristics of the default AKS storage profile for NiFi repositories

## Service And Ingress Assumptions

Prepared AKS starting point:

- keep the main NiFi Service as `ClusterIP`
- use `kubectl port-forward` first for initial evaluation
- only add ingress or `LoadBalancer` exposure after the internal cluster is healthy

Why this is the starting point:

- the controller uses direct pod DNS for health and lifecycle checks
- the chart currently derives `nifi.web.proxy.host` from internal Service and pod DNS names
- public or ingress-facing proxy-host behavior has not yet been validated on AKS

If you need an AKS-specific service exposure experiment, start with:

```yaml
service:
  type: LoadBalancer
  annotations:
    service.beta.kubernetes.io/azure-load-balancer-internal: "true"
```

Treat that as prepared guidance only. It is not yet proven by automated or manual AKS runs in this repository.

## DNS And TLS Notes

The current design keeps the same TLS rules on AKS that are already proven on kind:

- stable Secret names
- stable mount paths
- stable keystore and truststore paths
- stable password refs
- NiFi autoreload first for content-only TLS changes
- controller escalation only when the existing TLS policy requires a restart

### External Secret Mode

`tls.mode=externalSecret` remains the baseline AKS path.

Before install, create:

- `Secret/nifi-tls`
- `Secret/nifi-auth`

The TLS Secret still needs to satisfy the chart contract:

- `ca.crt`
- `keystore.p12`
- `truststore.p12`
- keystore and truststore password keys
- `sensitivePropsKey` unless you supply a separate secret reference

### Cert-Manager Mode

`tls.mode=certManager` is prepared for AKS, not validated there.

Requirements stay the same as on kind:

- cert-manager is installed separately from the NiFi chart
- the issuer writes a stable Secret name
- the Secret includes `ca.crt`
- PKCS12 password refs remain stable
- `nifi.sensitive.props.key` is still provided by a stable Secret or explicit value

You can compose:

- `examples/aks/managed-values.yaml`
- `examples/cert-manager-values.yaml`

That gives you the same chart-managed `Certificate` behavior already proven on kind, but it should still be treated as AKS-prepared only until a real cluster run is captured.

## AKS-Oriented Example Values

Starting overlays:

- `examples/aks/standalone-values.yaml`
- `examples/aks/managed-values.yaml`

They currently assume:

- `managed-csi` storage
- internal `ClusterIP` service exposure
- the same NiFi image tag proven on kind

## Managed-Mode Install Steps

Once an AKS cluster is available, start with the managed path because it exercises the full hybrid model.

1. Get cluster credentials.

```bash
az aks get-credentials --resource-group <rg> --name <cluster> --overwrite-existing
kubectl config use-context <aks-context>
```

2. Create namespaces.

```bash
kubectl create namespace nifi --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace nifi-system --dry-run=client -o yaml | kubectl apply -f -
```

3. Prepare TLS and auth prerequisites.

External Secret mode:

```bash
kubectl -n nifi apply -f <your-nifi-tls-secret.yaml>
kubectl -n nifi apply -f <your-nifi-auth-secret.yaml>
```

Cert-manager mode:

- install cert-manager separately
- create or choose an issuer compatible with the current PKCS12 + `ca.crt` contract
- create the password and sensitive-properties Secret expected by `examples/cert-manager-values.yaml`

4. Install the CRD.

```bash
kubectl apply -f config/crd/bases/platform.nifi.io_nificlusters.yaml
```

5. Deploy the controller from an AKS-reachable image.

```bash
kubectl apply -f config/rbac/service_account.yaml
kubectl apply -f config/rbac/role.yaml
kubectl apply -f config/rbac/role_binding.yaml
kubectl apply -f config/manager/manager.yaml
kubectl -n nifi-system set image deployment/nifi-fabric-controller-manager manager="${CONTROLLER_IMAGE}"
kubectl -n nifi-system patch deployment nifi-fabric-controller-manager \
  --type='json' \
  -p='[{"op":"replace","path":"/spec/template/spec/containers/0/imagePullPolicy","value":"IfNotPresent"}]'
kubectl -n nifi-system rollout status deployment/nifi-fabric-controller-manager --timeout=5m
```

6. Install the chart with the AKS managed overlay.

External Secret mode:

```bash
helm upgrade --install nifi charts/nifi \
  -n nifi \
  -f examples/aks/managed-values.yaml
```

Cert-manager mode:

```bash
helm upgrade --install nifi charts/nifi \
  -n nifi \
  -f examples/aks/managed-values.yaml \
  -f examples/cert-manager-values.yaml
```

7. Apply the `NiFiCluster`.

```bash
kubectl apply -f examples/managed/nificluster.yaml
```

8. Verify the current health gate.

```bash
bash hack/check-nifi-health.sh --namespace nifi --statefulset nifi --auth-secret nifi-auth
```

9. Start with internal access before adding AKS ingress or public service exposure.

```bash
kubectl -n nifi port-forward service/nifi 8443:8443
```

## First Things To Validate On Real AKS

The first real AKS evaluation should answer:

- do the four repository PVCs provision and remain stable on the chosen StorageClass
- does the direct pod-DNS health gate behave the same way on AKS networking
- does managed `OnDelete` rollout sequencing behave the same way under AKS StatefulSet timing
- does hibernation preserve PVCs as expected
- do ingress or load-balancer exposure patterns require additional proxy-host chart settings
- does cert-manager renewal preserve the current autoreload-first behavior without unexpected restarts

Until those answers come from a real AKS run, treat this document as readiness guidance only.
