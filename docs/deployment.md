<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# Deployment

Two images, one chart. The API image carries the UI; the worker only dials the API.
Both are distroless static, non-root (65532), read-only, and satisfy the *restricted*
Pod Security Standard.

| Artifact | Where |
| --- | --- |
| `ghcr.io/lusoris/snatcharr-api` | Go control plane + embedded SPA (HTTP 8080, gRPC 9090) |
| `ghcr.io/lusoris/snatcharr-worker` | Rust snatch-worker (no listener) |
| `oci://ghcr.io/lusoris/charts/snatcharr` | Helm chart |

Every image and the chart are signed with cosign (keyless, GitHub OIDC) and carry a SLSA
build provenance attestation. Each image also has an SPDX SBOM, attached to the GitHub
release. Tags are semver only: there is no `latest`.

Verify an artifact before you deploy it. The script checks that signature and attestation
both come from this repository's release workflow, run on the release tag (it needs `cosign`
and `gh`):

```sh
tools/release/verify.sh ghcr.io/lusoris/snatcharr-api:0.1.5 lusoris/SnatchArr refs/tags/v0.1.5
tools/release/verify.sh ghcr.io/lusoris/snatcharr-worker:0.1.5 lusoris/SnatchArr refs/tags/v0.1.5
tools/release/verify.sh ghcr.io/lusoris/charts/snatcharr:0.1.5 lusoris/SnatchArr refs/tags/v0.1.5
```

It runs these two commands, which also work without a checkout:

```sh
cosign verify ghcr.io/lusoris/charts/snatcharr:0.1.5 \
  --certificate-identity https://github.com/lusoris/SnatchArr/.github/workflows/release.yml@refs/tags/v0.1.5 \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
gh attestation verify oci://ghcr.io/lusoris/charts/snatcharr:0.1.5 --repo lusoris/SnatchArr \
  --signer-workflow lusoris/SnatchArr/.github/workflows/release.yml --source-ref refs/tags/v0.1.5
```

The release workflow (`.github/workflows/release.yml`, job `verify`) runs the same check on
both images and the chart and fails the release when a signature or an attestation is
missing. The chart carries an attestation from 0.1.5 on; the charts up to 0.1.4 are signed
only.

The git ref is the one the release workflow ran on. That is the tag for every release
except 0.1.1, which was rebuilt by hand from `main`: verify it with `refs/heads/main`. A
rebuild started with `gh workflow run release.yml --ref v0.1.5 -f tag=v0.1.5` keeps the tag
as the ref.

## Helm

Requirements: Kubernetes 1.29+, and the
[CloudNativePG](https://cloudnative-pg.io/) operator unless you bring your own Postgres.

```sh
helm install snatcharr oci://ghcr.io/lusoris/charts/snatcharr \
  --namespace snatcharr --create-namespace \
  --set config.publicUrl=https://snatcharr.example.com \
  --set api.ingress.enabled=true \
  --set 'api.ingress.hosts[0].host=snatcharr.example.com' \
  --set 'api.ingress.hosts[0].paths[0].path=/' \
  --set 'api.ingress.hosts[0].paths[0].pathType=Prefix' \
  --set networkPolicy.enabled=true
```

The first visit runs the setup wizard. `helm status` prints the port-forward command when
no Ingress or HTTPRoute is enabled.

### What the chart renders

| Resource | Notes |
| --- | --- |
| Deployment `-api` | probes on `/startupz`, `/livez`, `/readyz` (readiness: Postgres and the leader gate, which is down while the election fails or a periodic loop failed to start); `/tmp` is a memory-backed emptyDir; `replicaCount` may exceed 1, periodic work runs on the elected replica |
| Deployment `-worker` | dials `<release>-api:9090`; liveness probe on `/livez` (port `worker.httpPort`, 8081), which fails once no API call succeeded for `worker.staleSecs` (180 s); `/metrics` there counts failed API calls per call in `snatcharr_worker_api_failures_total`; optional HPA 1..4 on CPU |
| Service `-api` | `http` 8080, `grpc` 9090 (h2c) |
| Ingress or HTTPRoute | `api.ingress.*` or `api.httpRoute.*` |
| CNPG `Cluster` `-db` | `postgres.cnpg.*`; the API reads the DSN from the operator's `-db-app` Secret |
| Secret `-secrets` | crypto key, CSRF secret, API-key HMAC secret, worker token |
| NetworkPolicy | worker may only reach the API, `networkPolicy.arrCidrs` and `networkPolicy.arrPeers` (both empty allow no *arr egress, never all of it); gRPC only from the worker |
| ServiceMonitor | `serviceMonitor.enabled` scrapes `/metrics` on the API |
| PodDisruptionBudget | per component, opt-in |

### Metrics

The API serves these on `/metrics` next to the framework's own (`api/internal/metrics`). The
loops run on the elected replica only, so only its series move. The chart ships no alert
rules; the cluster that scrapes the API owns them.

| Series | Meaning |
| --- | --- |
| `snatcharr_loop_failures_total{loop}` | ticks of a leader loop that returned an error; `loop` is `snatch_planner`, `retention` or `seerr_sync`. The series exists at 0 from the first tick on |
| `snatcharr_loop_last_success_timestamp_seconds{loop}` | Unix time of the loop's last tick without error; absent until one succeeded |
| `snatcharr_loop_interval_seconds{loop}` | the loop's interval, to compare the last success against |
| `snatcharr_planner_instance_failures_total{instance}` | planner passes that skipped the instance because planning it failed, for example on a schedule the API cannot read |
| `snatcharr_runs_queued_total` | runs the planner queued; exists at 0 |
| `snatcharr_runs_leased_total{kind}` | runs a worker leased, by snatch kind (`missing`, `cutoff`, ...) |
| `snatcharr_runs_finished_total{outcome}` | runs that ended: `done`, `failed` or `cancelled` as the worker reported, or `lease_exhausted` when the API failed a run whose lease expired too often; each exists at 0 |
| `snatcharr_worker_events_total{type}` | events the workers reported, by SSE event name (`search_failed`, `instance_unreachable`, ...); a type this release does not define counts as `unknown`. Each defined type exists at 0 |
| `snatcharr_worker_events_unconfirmed_total` | events workers emitted for a finished run that `ReportEvents` never confirmed; each such run also records an `events_unconfirmed` warning. Exists at 0 |
| `snatcharr_budget_requests_total{result}` | budget requests of workers: `granted` (all asked for), `partial`, `denied` (cap spent) or `error`; each exists at 0 |

A tick that a replica cuts short because it stopped leading counts as neither. The run,
event and budget series move on whichever replica answers the worker, so an alert sums
them over replicas.

### NetworkPolicy and in-cluster *arr apps

`networkPolicy.arrCidrs` allows egress by IP range. That reaches *arr apps outside the
cluster, but some CNIs never apply CIDR rules to traffic between pods they manage: Cilium
states that "CIDR rules do not apply to traffic where both sides of the connection are
either managed by Cilium" (its layer 3 policy docs). If your *arr apps, Seerr, Cleanuparr
or download clients run as pods in the same cluster, select them in `networkPolicy.arrPeers`:

```yaml
networkPolicy:
  enabled: true
  arrPeers:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: media
```

An entry without `ports` allows every port of the pods it selects. To allow one port per
app, give the entry a `ports` list; the chart then renders an egress rule of its own for it:

```yaml
networkPolicy:
  enabled: true
  arrCidrs: []
  arrPeers:
    - podSelector:
        matchLabels:
          app.kubernetes.io/name: lidarr
      ports:
        - port: 8686
          protocol: TCP
```

A `podSelector` without a `namespaceSelector` selects pods in the release namespace only.
Each `ports` entry needs a `port` (number or port name) and may set `protocol` (TCP by
default) and `endPort`. `arrPeers` accepts `podSelector`, `namespaceSelector` and `ports`
only; IP ranges belong in `arrCidrs`, which always excludes the cloud metadata address and
allows every port. A rule for one port of an IP range belongs in `networkPolicy.extraEgress`.

### Secrets

The chart needs four secrets. Leave them empty and Helm generates them once, reusing the
existing Secret on upgrades (`helm.sh/resource-policy: keep`). Tools that render without
cluster access, such as ArgoCD, cannot reuse them, so create the Secret first:

```sh
kubectl -n snatcharr create secret generic snatcharr-secrets \
  --from-literal=crypto-key="$(openssl rand -hex 32)" \
  --from-literal=csrf-secret="$(openssl rand -hex 32)" \
  --from-literal=apikey-secret="$(openssl rand -base64 36)" \
  --from-literal=worker-token="$(openssl rand -base64 36)"
```

and set `secrets.existingSecret=snatcharr-secrets`. The crypto key encrypts every *arr
API key and download-client password at rest; losing it means re-entering them.

### Postgres

`postgres.cnpg.enabled=true` (default) renders a CloudNativePG `Cluster` with
`postgres.cnpg.instances` replicas and `postgres.cnpg.storage.size` per instance;
`postgres.cnpg.extraSpec` is merged verbatim for backups, affinity or parameters.
For an external database set `postgres.cnpg.enabled=false` and point
`postgres.external.dsnSecretRef` at a Secret holding the DSN. Only the API connects to
Postgres.

`sslmode=require` in the DSN encrypts the connection without checking the server
certificate. To verify it against the cluster CA (`verify-full`), mount the CA and point
the API at it. The settings override the DSN, and the CA file is re-read for every new
connection, so a rotated CA applies without a restart. With CloudNativePG the CA lives in
the `<cluster>-ca` Secret; mount only its `ca.crt`, never `ca.key`:

```yaml
config:
  extraEnv:
    - name: APP_DB_SSL_MODE
      value: verify-full
    - name: APP_DB_SSL_ROOTCERT
      value: /etc/snatcharr/db-ca/ca.crt
api:
  extraVolumes:
    - name: db-ca
      secret:
        secretName: pg-cluster-ca
        items:
          - key: ca.crt
            path: ca.crt
  extraVolumeMounts:
    - name: db-ca
      mountPath: /etc/snatcharr/db-ca
      readOnly: true
```

`verify-full` also checks the host name, so the DSN must name a host on the server
certificate, such as CloudNativePG's `<cluster>-rw.<namespace>.svc`.

The schema migrations that run at start use the same settings: a migrator that inherits
`APP_DB_DSN` also takes the `APP_DB_SSL_*` values and, when set, the password from the
`db.password_file` config key. A path in `APP_DB_SSL_*` must not contain a space. Before 0.1.3
the migrator ignored these settings and the API crash-looped on
`x509: certificate signed by unknown authority`; on 0.1.2 and older, add `sslrootcert=<path>` to
the DSN itself. An explicit `APP_DB_MIGRATE_DSN` is used exactly as given: it carries its own
password and TLS parameters. `TestMigratorConnection` in `api/internal/app` checks both rules
against a real server.

### Configarr

Already running [Configarr](https://github.com/raydak-labs/configarr)? Mount the same
files and SnatchArr imports every instance from them (linked mode):

```yaml
configarr:
  enabled: true
  existingConfigMap: configarr-config   # key config.yml
  existingSecret: configarr-secrets     # key secrets.yml (optional)
```

## ArgoCD

`deploy/argocd/` holds an `AppProject` and an `Application` that track `main` with
automated prune and self-heal, server-side apply and retries. Override values under
`spec.source.helm.valuesObject`; keep `secrets.existingSecret` set, see above.

## Local cluster

`make kind-up` builds both images, starts an incus VM (`tools/incus-kind-vm.sh`, 6 CPUs
and 12 GiB by default), creates a kind cluster inside it, installs CloudNativePG and
applies `deploy/kustomize/kind` (restricted namespace, NetworkPolicy on, throwaway
secrets). kind never runs in host Docker: its kubelet changes kernel settings such as
`vm.overcommit_memory` that are not namespaced, so on the host it would change the
workstation's kernel (#61).

The host reaches the cluster through a dedicated kubeconfig:

```bash
export KUBECONFIG=$PWD/.kind/snatcharr.kubeconfig
kubectl -n snatcharr port-forward svc/snatcharr-api 8080:8080
```

`make kind-down` deletes the VM, its profile and the kubeconfig. The VM needs incus with
KVM on the host; `SNATCHARR_KIND_VM_CPUS` and `SNATCHARR_KIND_VM_MEMORY` size it. For plain
local development `make dev` starts Postgres with Docker Compose and runs the three
processes on the host.
