# Deployment Migration Notes

This document records deployment safety notes for database migrations that need
operator review before they run against shared or production databases.

## Migration Policy

- The API container does not automatically run database migrations.
- Run migrations as a separate deployment step before deploying the API
  container, for example from CI, an init job, or an operator workstation with
  the approved migration tooling.
- Back up the production database before running production migrations.
- Run schema-changing migrations in staging first, using production-like data
  when possible, before approving production rollout.
- Stop the deployment if a migration preflight query reports unsafe data. Do
  not proceed until the data policy for that migration has been reviewed and
  approved.

## 00007 Project Owner Name Unique Index

Migration `migrations/00007_add_projects_owner_name_unique_index.sql` creates
this unique index:

```sql
CREATE UNIQUE INDEX idx_projects_owner_name_unique ON projects (owner_user_id, name);
```

The migration will fail if any existing owner already has more than one project
with the same `name`. This includes archived projects: under the current product
contract, archived projects still reserve their names for the same owner.

### Preflight SQL

Run this SQL before applying migration `00007` in any shared, staging, or
production database:

```sql
SELECT owner_user_id, name, COUNT(*) AS duplicate_count
FROM projects
GROUP BY owner_user_id, name
HAVING COUNT(*) > 1
ORDER BY duplicate_count DESC, owner_user_id, name;
```

Expected result: no rows.

If the query returns rows, do not run the migration yet. Export the affected rows
for manual review, then decide the business-approved remediation.

Optional review export:

```sql
SELECT p.*
FROM projects p
JOIN (
    SELECT owner_user_id, name
    FROM projects
    GROUP BY owner_user_id, name
    HAVING COUNT(*) > 1
) duplicates
    ON duplicates.owner_user_id = p.owner_user_id
   AND duplicates.name = p.name
ORDER BY p.owner_user_id, p.name, p.created_at, p.id;
```

## Duplicate Data Policy

- Do not automatically delete project data.
- Product or business owners must confirm which project should remain under the
  original name before data is changed.
- Acceptable remediation options include:
  - Rename duplicate projects.
  - Archive and merge duplicate projects after product review.
  - Export affected rows for manual review and apply an approved manual fix.
- Remember that archived projects also occupy the `(owner_user_id, name)` pair.
  Renaming or merging must leave each owner with only one project for each name,
  regardless of status.

## Migration Runbook

For local or staging migration runs:

```sh
DATABASE_URL=postgres://... make migrate-up
```

For rollback:

```sh
DATABASE_URL=postgres://... make migrate-down
```

Rollback for `00007` drops `idx_projects_owner_name_unique`, but it does not
restore any data that was manually renamed, archived, or merged before the
migration. Keep a production backup and a record of manual remediation steps.

## Verification After Migration

After migration `00007` succeeds:

- Run the preflight SQL again. It should return no rows.
- Run `make smoke-auth`.
- Run `make postman-test` when the API is running.
- Optionally check readiness directly with `GET /readyz`.

## Agent Notes

- Future unique index migrations must include preflight SQL, duplicate data
  policy, and rollback notes in this document or another durable deployment
  note.
- Do not silently clean up business data inside a migration unless there is
  explicit human approval for that exact cleanup policy.
