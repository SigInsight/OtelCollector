# Schema migrations

The v1 schema is a frozen compatibility boundary. The static DDL in
`cmd/siginsightschemamigrator/schema_migrator/v1_baseline_ddl.go` and the
`V1BaselineSpecs` history must not be edited after the baseline is released.
The `999` record means that the exact v1 schema was created or verified; it is
not a version to increment.

## Adding a migration

1. Add one `PostBaselineMigration` to the appropriate registry in
   `cmd/siginsightschemamigrator/schema_migrator/post_baseline_migrations.go`:
   `PostBaselineSyncMigrations` for metadata-only, idempotent, lightweight
   operations, or `PostBaselineAsyncMigrations` for mutations and other long
   running work.
2. Choose a new ID greater than or equal to `2000`. IDs are scoped to a
   database, must increase within each registry, and must never be reused.
3. Keep all operations in one migration in the same phase. If a change needs a
   synchronous DDL step and an asynchronous mutation, use two IDs and make the
   synchronous ID lower. The runner will not execute a migration whose earlier
   registered IDs are unfinished.
4. Use the existing `Operation` types. Prefer idempotent operations such as
   `AlterTableAddColumn`; do not add a second copy of the v1 DDL to the frozen
   baseline file.

The runner records `in-progress`, then `finished` or `failed`. A failed or
interrupted migration is deliberately not retried automatically. Resolve the
database state and migration record explicitly before attempting another
release.

## Verification

Add assertions for the new schema to `scripts/test-v1-baseline.sh` and update
its current-schema fingerprints when the expected post-baseline schema changes.
Do not change the frozen v1 fingerprints used internally to adopt a completed
v1.0.4 database. Run:

```text
go test -tags=remove_all_sd ./...
make test-migration-integration
```

The integration script must continue to cover v1.0.4 adoption, fresh
non-replicated and replicated creation, and rejection of failed migration
states.
