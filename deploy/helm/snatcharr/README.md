<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# snatcharr Helm chart

Deploys the SnatchArr control plane (Go API with the embedded UI) and the Rust
snatch-worker, with a CloudNativePG Postgres cluster or an external DSN.

```sh
helm install snatcharr oci://ghcr.io/lusoris/charts/snatcharr \
  --namespace snatcharr --create-namespace \
  --set config.publicUrl=https://snatcharr.example.com \
  --set api.ingress.enabled=true \
  --set 'api.ingress.hosts[0].host=snatcharr.example.com' \
  --set 'api.ingress.hosts[0].paths[0].path=/' \
  --set 'api.ingress.hosts[0].paths[0].pathType=Prefix'
```

Values are documented inline in [values.yaml](values.yaml); the full guide lives at
<https://lusoris.github.io/SnatchArr/deployment/>.
