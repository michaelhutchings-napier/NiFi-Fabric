# NiFi-Fabric Authz Tooling

`nifi-fabric-authz` is an optional CLI for generating OIDC authorization overlays from Keycloak groups plus a small mapping file.

This is a supported optional in-tree feature for teams that want Git-rendered OIDC authorization overlays without adding controller reconciliation or custom runtime extensions.

This tool is intentionally:

- optional
- GitOps-first
- generator/validator focused
- bundle-oriented rather than policy-heavy

This tool is intentionally not:

- a controller feature
- a CRD or API surface
- a Keycloak operator
- a NiFi policy reconciler
- a runtime sync loop

Current entrypoint:

- `go run ./cmd/nifi-fabric-authz --help`

Commands:

- `render`
- `validate`
- `diff`

Make targets:

- `make authz-render`
- `make authz-render-check`
- `make authz-validate`
- `make authz-diff`

Environment variables for live Keycloak validation:

- `NIFI_FABRIC_AUTHZ_KEYCLOAK_ACCESS_TOKEN`

Example config:

- [examples/oidc-keycloak-authz-config.yaml](../../examples/oidc-keycloak-authz-config.yaml)

Example generated app-chart overlay:

- [examples/oidc-keycloak-authz-values.yaml](../../examples/oidc-keycloak-authz-values.yaml)

Example generated platform-chart overlay:

- [examples/platform-managed-oidc-keycloak-authz-values.yaml](../../examples/platform-managed-oidc-keycloak-authz-values.yaml)

Example path-mode config:

- [examples/oidc-keycloak-path-authz-config.yaml](../../examples/oidc-keycloak-path-authz-config.yaml)

Example path-mode generated app-chart overlay:

- [examples/oidc-keycloak-path-authz-values.yaml](../../examples/oidc-keycloak-path-authz-values.yaml)

Example usage:

```bash
go run ./cmd/nifi-fabric-authz render \
  --config examples/oidc-keycloak-authz-config.yaml \
  --output examples/oidc-keycloak-authz-values.yaml

go run ./cmd/nifi-fabric-authz render \
  --config examples/oidc-keycloak-authz-config.yaml \
  --output examples/oidc-keycloak-authz-values.yaml \
  --check

go run ./cmd/nifi-fabric-authz validate \
  --config examples/oidc-keycloak-authz-config.yaml \
  --helm \
  --base-values examples/managed/values.yaml,examples/oidc-values.yaml

go run ./cmd/nifi-fabric-authz validate \
  --config examples/oidc-keycloak-authz-config.yaml \
  --live

go run ./cmd/nifi-fabric-authz diff \
  --config examples/oidc-keycloak-authz-config.yaml \
  --against examples/oidc-keycloak-authz-values.yaml

make authz-render
make authz-render-check
make authz-validate
make authz-diff
```

Notes:

- `--helm` works best when layered with base OIDC values because this generator intentionally does not own the full OIDC client configuration.
- `--live` uses a bring-your-own access token model in MVP via `NIFI_FABRIC_AUTHZ_KEYCLOAK_ACCESS_TOKEN`.
- live validation fails loudly when the configured `groupValueMode` does not match the claim-value shape actually discoverable from Keycloak.

## Live Keycloak Validation For Operators

`validate --live` expects a bearer token that can read groups from the Keycloak admin API for the target realm.

Practical operator flow:

1. Create or choose a confidential Keycloak client for the authz tool.
2. Enable service accounts for that client.
3. Grant it realm-management permissions needed to read groups for the target realm.
4. Mint an access token out of band.
5. Export it as `NIFI_FABRIC_AUTHZ_KEYCLOAK_ACCESS_TOKEN`.
6. Run `validate --live`.

Example token request using client credentials:

```bash
export KEYCLOAK_BASE_URL="https://keycloak.example.com"
export KEYCLOAK_REALM="nifi"
export KEYCLOAK_CLIENT_ID="nifi-fabric-authz"
export KEYCLOAK_CLIENT_SECRET="replace-me"

export NIFI_FABRIC_AUTHZ_KEYCLOAK_ACCESS_TOKEN="$(
  curl -fsS \
    -d grant_type=client_credentials \
    -d client_id="${KEYCLOAK_CLIENT_ID}" \
    -d client_secret="${KEYCLOAK_CLIENT_SECRET}" \
    "${KEYCLOAK_BASE_URL}/realms/${KEYCLOAK_REALM}/protocol/openid-connect/token" \
  | jq -r '.access_token'
)"
```

Then run:

```bash
go run ./cmd/nifi-fabric-authz validate \
  --config examples/oidc-keycloak-authz-config.yaml \
  --live
```

Or with the repo target:

```bash
NIFI_FABRIC_AUTHZ_KEYCLOAK_ACCESS_TOKEN="${NIFI_FABRIC_AUTHZ_KEYCLOAK_ACCESS_TOKEN}" \
make authz-validate
```

If your Keycloak administrators prefer not to expose client secrets broadly, they can mint and hand you a short-lived access token instead. The CLI only needs the bearer token for the live validation call.

## Token Shape Example

The most important live-validation check is whether the group values NiFi will see actually match the values you seed into NiFi-Fabric.

If Keycloak emits simple names in the token:

```json
{
  "groups": [
    "nifi-platform-admins",
    "nifi-flow-operators"
  ]
}
```

Then your config should use `groupValueMode: name` and the bindings should use those exact values:

```yaml
source:
  groupsClaim: groups
  groupValueMode: name

authz:
  initialAdminGroup: nifi-platform-admins

bindings:
- keycloakGroup: nifi-platform-admins
  bundles: [admin]
- keycloakGroup: nifi-flow-operators
  bundles: [viewer, editor, flowVersionManager]
```

If Keycloak emits full paths in the token:

```json
{
  "groups": [
    "/platform/nifi-platform-admins",
    "/platform/nifi-flow-operators"
  ]
}
```

Then your config must use `groupValueMode: path` and the bindings must use those exact path values:

```yaml
source:
  groupsClaim: groups
  groupValueMode: path

authz:
  initialAdminGroup: /platform/nifi-platform-admins

bindings:
- keycloakGroup: /platform/nifi-platform-admins
  bundles: [admin]
- keycloakGroup: /platform/nifi-flow-operators
  bundles: [viewer, editor, flowVersionManager]
```

If those do not line up, `validate --live` will fail with an explicit claim-shape mismatch instead of a generic “group missing” error.

## Runtime Behavior After Deployment

This tool generates the seeded NiFi group catalog and bundle bindings. It does not add runtime reconciliation.

Operationally, that means:

- new users can be added in Keycloak after NiFi is already running without restarting NiFi
- those users can log in immediately as long as they are added to Keycloak groups that already exist in the generated NiFi authz overlay
- users whose group membership changes after they are already logged in should re-login so NiFi sees fresh claims
- new Keycloak group names still require a regenerated overlay and rollout because NiFi-Fabric must seed those groups and bundle bindings explicitly

## Troubleshooting

Most real failures fall into one of these two cases.

Wrong group claim shape:

- Keycloak emits `groups` as one value shape, but the overlay was generated with the other
- common mismatch: Keycloak emits `/platform/nifi-flow-operators` while the overlay uses `nifi-flow-operators`
- fix the Keycloak mapper or switch the config between `groupValueMode: name` and `groupValueMode: path`
- re-run `validate --live` before regenerating and deploying the overlay

User added to a new group that NiFi was never seeded with:

- the user exists and login works, but NiFi still denies access
- this happens when the Keycloak group name is new and is not present in `authz.applicationGroups` plus the bundle bindings
- adding the user alone is not enough in that case
- update the mapping file, regenerate the overlay, commit it, and roll it out

## Compose With Base Values

The generator owns the OIDC authorization overlay, not the full OIDC client configuration. In practice, compose the generated output with the existing base values for your install surface.

App chart example:

```bash
helm template nifi charts/nifi \
  -f examples/managed/values.yaml \
  -f examples/oidc-values.yaml \
  -f examples/oidc-keycloak-authz-values.yaml
```

Platform chart example:

```bash
helm template nifi charts/nifi-platform \
  -f examples/platform-managed-values.yaml \
  -f examples/platform-managed-oidc-keycloak-authz-values.yaml
```
