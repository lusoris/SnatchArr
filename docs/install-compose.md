<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# Install with Docker Compose

One Docker host runs Postgres, the SnatchArr API (which serves the web UI) and one snatch
worker. The bundle lives in `deploy/homelab/`: `compose.yaml` and `env.example`. For
Kubernetes use the Helm chart instead, see [Deployment](deployment.md).

You need Docker Engine with the Compose plugin (`docker compose version`) and `openssl` to
generate the secrets.

## Install

1. Copy `deploy/homelab/compose.yaml` and `deploy/homelab/env.example` into an empty
   directory on the host, for example `/opt/snatcharr`. Take both files from the release tag
   you install, so the image pins match that release.
2. Create `.env` from the example and fill in every secret, each one generated with
   `openssl rand -hex 32`:

   ```sh
   cp env.example .env
   chmod 600 .env
   ```

   Compose refuses to start while a required secret is empty and names the missing one.
3. Set how the UI is reached. `SNATCHARR_BIND=127.0.0.1` publishes it on this host only;
   use `0.0.0.0` to reach it from the LAN. Set `SNATCHARR_PUBLIC_URL` to the address users
   open. On plain HTTP set `SNATCHARR_SESSION_SECURE=false`, since the browser drops secure
   cookies there; behind an HTTPS reverse proxy keep it `true`.
4. Start the stack and wait until every service with a health check reports healthy:

   ```sh
   docker compose up -d --wait
   docker compose ps
   ```

5. Open `SNATCHARR_PUBLIC_URL`. The first visit lands on the setup screen: create the admin
   account and enter `SNATCHARR_SETUP_SECRET` from `.env`. Then add your *arr instances.

All three containers run with a read-only root file system, no Linux capabilities and
`no-new-privileges`. Only the UI port is published; the worker reaches the API over the
compose network on gRPC port 9090.

## Health checks

Postgres reports its health through `pg_isready`, and the API starts only once Postgres is
healthy. The API and worker images are distroless: they carry no shell and no `curl`, so a
health check has to be a command of the binary itself. Both binaries answer a `healthcheck`
subcommand (`/snatcharr-api healthcheck` asks `/readyz`, `/snatch-worker healthcheck` asks
`/livez`; exit 0 = healthy). The images pinned in `compose.yaml` (0.1.9) predate that
subcommand, so the bundle gets API and worker health checks with the first release that ships
it; Renovate moves the pins to that release.

`make e2e-compose-smoke` starts this bundle with images built from the checkout, adds both
health checks, and checks that every service turns healthy, that `/setup` serves the UI and
that `/api/v1/auth/setup/status` reports a fresh install. CI runs it on every pull request.

## Upgrade

Read the release notes first, then take a backup (below). Replace `compose.yaml` with the one
from the new release tag, compare `env.example` for new variables, and restart:

```sh
docker compose pull
docker compose up -d --wait
```

The API applies its database migrations on start. Do not skip the backup: migrations only
move forward, and a downgrade needs the backup taken before the upgrade.

A new Postgres major (the `postgres` image moving from 18 to 19) does not start on a data
volume of the previous major. Dump the database with the old image, move the volume aside,
start the new image on an empty volume and restore, as below.

## Backup and restore

Back up two things together: the database and `.env`. Without `SNATCHARR_CRYPTO_KEY` the *arr
API keys stored in the database cannot be decrypted.

```sh
docker compose exec -T postgres pg_dump -U snatcharr --format=custom snatcharr > snatcharr.dump
```

To restore, stop the API and worker so nothing writes, restore into the running Postgres, and
start them again:

```sh
docker compose stop api worker
docker compose exec -T postgres pg_restore -U snatcharr --clean --if-exists --no-owner \
  --exit-on-error --dbname=snatcharr < snatcharr.dump
docker compose up -d --wait
```

## Remove

`docker compose down` stops the stack and keeps the data volume. `docker compose down
--volumes` also deletes the database.
