#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# Backup and restore of a chart install on CloudNativePG with the Barman Cloud plugin, the steps
# of docs/deployment.md ("Backup and restore", gap audit C14-04):
#   1. installs the chart with WAL archiving to an S3 store (RustFS in the namespace), completes
#      the first-run setup and logs in, then takes a base backup with a Backup resource;
#   2. removes the release and its database, keeping the <release>-secrets Secret as the chart
#      does (helm.sh/resource-policy: keep);
#   3. planted: installs the chart again without the recovery bootstrap; its fresh database must
#      be refused as "restored data differs from the backup";
#   4. installs the chart with postgres.cnpg.bootstrap.recovery from the backup and proves every
#      table of the public schema holds the same rows as before (fingerprint.sql);
#   5. turns WAL archiving on again under a new serverName, and the API starts on the restored
#      database with the setup complete and the admin's password from before the backup.
#
# Needs a cluster with the CloudNativePG operator, cert-manager and the Barman Cloud plugin in
# the operator's namespace (CI: the kind cluster of the deploy job; on a workstation only inside a
# VM), helm, kubectl, curl, jq and openssl. RESTORE_API_IMAGE, RESTORE_WORKER_IMAGE,
# RESTORE_NAMESPACE, RESTORE_PORT and RESTORE_KEEP: cnpg_lib.sh.
set -euo pipefail

# The S3 store of the test, by version and digest (the plugin documents RustFS as a store for
# tests); mirror.gcr.io avoids Docker Hub's anonymous pull limit on shared runners.
RUSTFS_IMAGE=mirror.gcr.io/rustfs/rustfs:1.0.1@sha256:1803faef57627e2d9c2e7d89d655d712ddded5389040054987163043fecb6a3c
plugin=barman-cloud.cloudnative-pg.io
store=snatcharr-backups

label=cnpg-backup-restore
prefix=RESTORE
default_ns=snatcharr-restore
# shellcheck source=tools/e2e/cnpg_lib.sh
. "$(dirname "$0")/cnpg_lib.sh"

# object_store runs RustFS with fresh credentials, then barman_store.
object_store() {
    local key secret
    key=$(openssl rand -hex 10)
    secret=$(openssl rand -hex 20)
    kubectl -n "$ns" create secret generic s3-creds \
        --from-literal=ACCESS_KEY_ID="$key" --from-literal=ACCESS_SECRET_KEY="$secret" >/dev/null
    kubectl -n "$ns" apply -f - >/dev/null <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rustfs
spec:
  selector:
    matchLabels: {app: rustfs}
  template:
    metadata:
      labels: {app: rustfs}
    spec:
      containers:
        - name: rustfs
          image: $RUSTFS_IMAGE
          env:
            - name: RUSTFS_ACCESS_KEY
              valueFrom: {secretKeyRef: {name: s3-creds, key: ACCESS_KEY_ID}}
            - name: RUSTFS_SECRET_KEY
              valueFrom: {secretKeyRef: {name: s3-creds, key: ACCESS_SECRET_KEY}}
          ports:
            - containerPort: 9000
          readinessProbe:
            tcpSocket: {port: 9000}
            periodSeconds: 2
          volumeMounts:
            - {name: data, mountPath: /data}
      volumes:
        - {name: data, emptyDir: {}}
---
apiVersion: v1
kind: Service
metadata:
  name: rustfs
spec:
  selector: {app: rustfs}
  ports:
    - port: 9000
EOF
    kubectl -n "$ns" rollout status deploy/rustfs --timeout=180s >/dev/null || fail "RustFS did not start"
    barman_store
}

# barman_store points the ObjectStore of the plugin at RustFS.
barman_store() {
    kubectl -n "$ns" apply -f - >/dev/null <<EOF
apiVersion: barmancloud.cnpg.io/v1
kind: ObjectStore
metadata:
  name: $store
spec:
  configuration:
    destinationPath: s3://backups/
    endpointURL: http://rustfs.$ns.svc:9000
    s3Credentials:
      accessKeyId: {name: s3-creds, key: ACCESS_KEY_ID}
      secretAccessKey: {name: s3-creds, key: ACCESS_SECRET_KEY}
    wal:
      compression: gzip
EOF
}

# install runs the chart with the values file $1, the API and worker scaled to zero.
install() {
    helm install "$release" "$root/deploy/helm/snatcharr" -n "$ns" "${images[@]}" -f "$1" \
        --set api.replicaCount=0 --set worker.replicaCount=0 --wait --timeout 10m >/dev/null ||
        fail "chart install with $(basename "$1") failed"
    wait_healthy 600
}

# uninstall removes the release and waits until the database and its volumes are gone.
uninstall() {
    local i left
    helm uninstall "$release" -n "$ns" --wait --timeout 5m >/dev/null || fail "helm uninstall failed"
    for ((i = 0; i < 60; i++)); do
        left=$(kubectl -n "$ns" get pod,pvc -l "cnpg.io/cluster=$cluster" -o name)
        [ -z "$left" ] && break
        sleep 3
    done
    [ -z "$left" ] || fail "the database is not gone after the uninstall: $left"
    kubectl -n "$ns" get secret "$release-secrets" >/dev/null || fail "the chart Secret did not survive the uninstall"
}

# archiving waits until the operator reports WAL archiving working.
archiving() {
    kubectl -n "$ns" wait "cluster/$cluster" --for=condition=ContinuousArchiving=True --timeout=300s >/dev/null ||
        fail "WAL archiving did not start"
}

cat >"$work/archive.yaml" <<EOF
postgres:
  cnpg:
    storage: {size: 1Gi}
    extraSpec:
      plugins:
        - name: $plugin
          isWALArchiver: true
          parameters: {barmanObjectName: $store}
EOF
cat >"$work/fresh.yaml" <<EOF
postgres:
  cnpg:
    storage: {size: 1Gi}
EOF
cat >"$work/restore.yaml" <<EOF
postgres:
  cnpg:
    storage: {size: 1Gi}
    bootstrap:
      recovery:
        source: backup
        database: snatcharr
        owner: snatcharr
    extraSpec:
      externalClusters:
        - name: backup
          plugin:
            name: $plugin
            parameters: {barmanObjectName: $store, serverName: $cluster}
EOF
cat >"$work/rearchive.yaml" <<EOF
postgres:
  cnpg:
    extraSpec:
      plugins:
        - name: $plugin
          isWALArchiver: true
          parameters: {barmanObjectName: $store, serverName: $cluster-restored}
EOF

# 1. An install that archives to the store, a completed setup, then a base backup.
kubectl create namespace "$ns" >/dev/null
object_store
install "$work/archive.yaml"
archiving
scale_app 1
port_forward
setup_admin
scale_app 0
fingerprint >"$work/before.txt"
grep -q '^users 1 ' "$work/before.txt" || fail "no user row before the backup"
kubectl -n "$ns" apply -f - >/dev/null <<EOF
apiVersion: postgresql.cnpg.io/v1
kind: Backup
metadata:
  name: before-loss
spec:
  cluster: {name: $cluster}
  method: plugin
  pluginConfiguration: {name: $plugin}
EOF
for ((i = 0; i < 60; i++)); do
    phase=$(kubectl -n "$ns" get backup before-loss -o jsonpath='{.status.phase}')
    case "$phase" in
    completed) break ;;
    failed) fail "backup failed: $(kubectl -n "$ns" get backup before-loss -o jsonpath='{.status.error}')" ;;
    esac
    sleep 5
done
[ "$phase" = completed ] || fail "backup is '$phase' after 300 s, want completed"
echo "ok   base backup completed with $(wc -l <"$work/before.txt") tables and WAL archiving on"

# 2. The database is lost; the chart Secret stays.
uninstall
echo "ok   release and database removed, $release-secrets kept"

# 3. Planted: an install without the recovery bootstrap starts an empty database.
install "$work/fresh.yaml"
if fingerprint >"$work/planted.txt" && diff -q "$work/before.txt" "$work/planted.txt" >/dev/null; then
    fail "planted: a fresh database matched the backup"
fi
echo "ok   planted install without recovery refused: restored data differs from the backup"
uninstall

# 4. The documented restore.
install "$work/restore.yaml"
fingerprint >"$work/after.txt"
diff -u "$work/before.txt" "$work/after.txt" >&2 || fail "restored data differs from the backup"
echo "ok   recovery from the backup: all $(wc -l <"$work/after.txt") tables read back unchanged"

# 5. Archiving again under a new serverName, then the API on the restored database.
helm upgrade "$release" "$root/deploy/helm/snatcharr" -n "$ns" --reuse-values -f "$work/rearchive.yaml" \
    --wait --timeout 10m >/dev/null || fail "turning WAL archiving on again failed"
wait_healthy 600
archiving
echo "ok   WAL archiving on again as $cluster-restored"
check_app
echo "ok   API on the restored database: setup complete, admin logs in"
echo "chart CloudNativePG backup and restore passed"
