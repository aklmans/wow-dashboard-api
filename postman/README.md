# Postman/Newman Smoke Tests

This directory contains the black-box API smoke baseline for local and CI
acceptance checks. It exercises the public HTTP API through Newman and does not
replace Go unit, handler, or integration tests.

## Files

- `wow-dashboard-api.postman_collection.json` - Postman collection for health,
  readiness, auth, admin users, system audit events, and owner-scoped projects.
- `env.local.json` - local defaults for `http://localhost:7272` and the seeded
  demo admin account.

## Run Locally

Prepare a disposable local database and run the API first:

```sh
make compose-up
make local-setup
make dev
```

Then run Newman from another terminal:

```sh
make postman-test
```

The target uses `npx --yes newman`, so a global Newman install is not required.
Override connection or credential values when needed:

```sh
make postman-test POSTMAN_BASE_URL=http://localhost:7272 POSTMAN_EMAIL=demo@minimals.cc POSTMAN_PASSWORD='@2Minimal'
```

The collection relies on Newman's cookie jar for refresh-token rotation and also
captures the latest access token in the active Postman environment. Tests do not
print raw tokens.

## Data Notes

The admin flow creates two timestamped projects and archives both before the run
finishes, then reads recent system audit events with an explicit limit. The
authorization checks sign up a timestamped `role=user` account so they can verify
`GET /api/users` and `GET /api/system-events` return `403` with
`Admin role required.` for non-admin users. That user remains in the local
database. Run against a disposable database or use `make local-reset` when you
want a clean local state.
