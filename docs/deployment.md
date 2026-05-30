# Deployment Runbook

Concrete steps for shipping `wow-dashboard-api` to a real environment. This
document complements [`operations.md`](operations.md) — operations.md is the
release-gate checklist; this is the playbook the operator follows once the
gate is open.

## Image

The image is built and published from an operator workstation (or your
staging/test host), not from GitHub Actions — the multi-arch CI job was
removed to keep PR feedback fast. The Dockerfile is intentionally
minimal:

| Binary           | Purpose                                                  |
| ---------------- | -------------------------------------------------------- |
| `/api`           | Main HTTP server. Default entrypoint. Listens on `:7272`. |
| `/worker`        | River background-job worker. Override entrypoint.        |
| `/river-migrate` | Applies River's queue schema. Idempotent. Run once.      |

The image **does not** ship goose, sqlc, the seed binary, the smoke
harness, or any shell — app-schema migrations and data seeding are deliberately
external steps so a misconfigured container can never silently mutate
production data.

Build and push from the test machine:

```sh
# Single-arch (fast — minutes):
docker build -t ghcr.io/aklmans/wow-dashboard-api:v1.2.3 .
docker push ghcr.io/aklmans/wow-dashboard-api:v1.2.3

# Multi-arch via buildx (slow due to arm64 QEMU emulation — 15–25 min):
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t ghcr.io/aklmans/wow-dashboard-api:v1.2.3 \
  --push .
```

Pin production deployments to a specific `vX.Y.Z` tag so a rolling deploy
is reproducible. Do not point a production ReplicaSet at a moving tag
(`main`, `latest`).

## Required environment

The API refuses to start in `ENV=production` if any of these are missing or
weak. The exact rules are in [`operations.md`](operations.md#configuration-checks).

| Variable                          | Notes |
| --------------------------------- | ----- |
| `ENV=production`                  | Switches on the production-grade validation. |
| `DATABASE_URL`                    | Mandatory in production. Use the provider's required `sslmode`. |
| `JWT_ACCESS_SECRET`               | ≥32 chars, private to the env, no `change-me`/`dev-only`/`example`/`secret` substrings. |
| `JWT_ACCESS_TOKEN_TTL_SECONDS`    | Between 60 and 3600. Default 900 is fine. |
| `CORS_ALLOWED_ORIGINS`            | Exact owned `https://` origins; no `*` wildcards, no loopback. |
| `REFRESH_TOKEN_COOKIE_SECURE`     | Auto-defaults to `true` in production; an explicit `false` blocks startup. |
| `REFRESH_TOKEN_COOKIE_SAMESITE`   | `none` requires `_SECURE=true`. |
| `EMAIL_SMTP_HOST` + friends       | Required in production (refuses to start without `EMAIL_SMTP_HOST`). |
| `OTEL_EXPORTER_OTLP_ENDPOINT`     | Optional. Set to ship traces (no-op when empty). |
| `ENABLE_DOCS`                     | Optional. Defaults to `false` in production (the Swagger UI at `/docs` is hidden); set `true` only to deliberately expose interactive docs. The OpenAPI JSON at `/openapi` is always served. |
| `METRICS_ADDR`                    | Optional but recommended. Set to an internal-only bind (e.g. `127.0.0.1:9090`) to serve `/metrics` off the public API port; point the scraper there. Empty leaves `/metrics` on the API port — restrict it at the ingress in that case. |

Hand the production env-var set to a second engineer for review before
rolling out. See [`operations.md` § Production Ready](operations.md#production-ready)
for the full pre-flight checklist.

## Pre-deploy: app-schema migrations

The API container has no goose binary on purpose, so migrations are a
separate, explicit step that runs **before** the API container receives
traffic. The same release tag must drive both:

```sh
# Tag the operator is rolling out
VERSION=v1.2.3
DATABASE_URL=postgres://...   # production database, not local

# 1. Back up the database (provider tooling, e.g. pg_dump, RDS snapshot).
#    Note who owns the snapshot.

# 2. Check out the API repo at the matching tag and run goose from there.
git fetch --tags
git checkout $VERSION
make migrate-up
```

For schema-changing migrations, run them in staging first against
production-like data. Migrations that add a `UNIQUE` index need a
preflight query — see the
[`00007` runbook](operations.md#migration-00007--projects-ownername-unique-index)
for the template.

## Pre-deploy: River queue schema

River keeps its own schema. The container ships `/river-migrate` for this:

```sh
docker run --rm \
  -e DATABASE_URL=postgres://... \
  ghcr.io/aklmans/wow-dashboard-api:$VERSION \
  /river-migrate
```

It is idempotent and safe to re-run; no preflight needed.

## Deploy: API + worker

The same image runs as either component — flip the entrypoint:

```yaml
# api Deployment / Service
image: ghcr.io/aklmans/wow-dashboard-api:v1.2.3
# uses the default ENTRYPOINT (/api), listens on 7272
livenessProbe:
  httpGet: { path: /healthz, port: 7272 }
readinessProbe:
  httpGet: { path: /readyz, port: 7272 }

# worker Deployment (no Service required)
image: ghcr.io/aklmans/wow-dashboard-api:v1.2.3
command: ["/worker"]
```

Run the API and worker as separate Deployments so HTTP capacity and worker
capacity scale independently. Both honour `SIGINT`/`SIGTERM` with a graceful
drain — the API stops accepting connections and drains in-flight requests
for `HTTP_SHUTDOWN_TIMEOUT_SECONDS` (default 10s); the worker finishes its
in-flight jobs within 30s.

Recommended replica counts:

| Component | Starting point                                      |
| --------- | --------------------------------------------------- |
| `api`     | 2+ (so a rolling deploy never drops to zero healthy). |
| `worker`  | 1 (River persists jobs in Postgres so a single replica is fine until throughput requires more). |

## Post-deploy: smoke

Once the new replicas pass readiness:

```sh
# From an operator workstation, against the deployed base URL.
BASE_URL=https://api.example.com make postman-test
```

The Postman/Newman suite hits every public endpoint. A red run blocks the
rollout from being declared done.

## Rolling updates

Rolling deploys are safe when the new release does not break the database
contract:

- **Backward-compatible migrations** (additive columns, new indexes, new
  tables) → roll the API normally; old and new replicas can coexist.
- **Breaking migrations** (column rename, type change, dropped column) →
  follow the standard expand/contract pattern:
  1. Ship migration N that adds the new column alongside the old.
  2. Roll the API to a version that writes both columns.
  3. Backfill the new column.
  4. Roll the API to a version that reads the new column.
  5. Ship migration N+1 that drops the old column.

Never combine a breaking migration with the API roll that drops support for
the old shape — staged across at least two releases, you can roll back at
any point.

## Rollback

```sh
# Re-deploy the previous tag.
kubectl set image deployment/api  api=ghcr.io/aklmans/wow-dashboard-api:v1.2.2
kubectl set image deployment/worker worker=ghcr.io/aklmans/wow-dashboard-api:v1.2.2

# If the schema also changed and the new shape is incompatible, roll the
# database back using goose (working from the matching git tag):
git checkout v1.2.2
make migrate-down
```

For data-changing rollbacks, restore from the backup taken before
`migrate-up`. The migration `down` files **do not** restore renamed,
archived, or merged data; only the snapshot can.

## Local rehearsal

`compose.prod.yaml` brings the full image-based stack up on a single host
so you can rehearse the runbook without a real cluster:

```sh
export IMAGE_TAG=main          # or a vX.Y.Z tag once one exists
export JWT_ACCESS_SECRET="$(openssl rand -hex 32)"
docker compose -f compose.prod.yaml up -d postgres
DATABASE_URL=postgres://wow_dashboard:wow_dashboard@localhost:5432/wow_dashboard_api?sslmode=disable \
  make migrate-up
docker compose -f compose.prod.yaml up -d
# Wait for /healthz, then:
BASE_URL=http://localhost:7272 make postman-test
docker compose -f compose.prod.yaml down
```

This rehearsal exercises the same image and the same migration sequence
the operator will run against the real environment.
