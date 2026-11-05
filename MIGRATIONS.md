# Schema migrations

The M16 schema is a destructive fresh-install boundary. The explicit DDL in
`cmd/siginsightschemamigrator/schema_migrator/canonical_baseline_ddl.go` is the
single baseline source of truth. The `999` record means that this canonical
schema passed fingerprint validation; it is not a version to increment.

The migrator does not adopt pre-M16 histories or rewrite legacy table/column
names. Only a pristine set of `siginsight_*` databases can be initialized.
Existing databases must contain a finished `999` marker and match the runtime
fingerprint, otherwise startup is rejected.

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
   `AlterTableAddColumn`; do not modify the canonical baseline to ship a change
   to an already initialized database.

The runner records `in-progress`, then `finished` or `failed`. A failed or
interrupted migration is deliberately not retried automatically. Resolve the
database state and migration record explicitly before attempting another
release.

`sync up` validates the canonical marker before applying registered migrations.
`sync check` first requires every synchronous migration to be finished and then
validates the final runtime fingerprint. This ordering lets an old canonical
baseline advance to a new registered schema without weakening the startup gate.

## Verification

Add assertions for the new schema to `scripts/test-v1-baseline.sh` and update
the runtime fingerprints only when a registered post-baseline migration changes
the expected schema. Run:

```text
go test -tags=remove_all_sd ./...
make test-migration-integration
```

The integration script must continue to cover fresh single-node creation,
idempotent marker handling, schema drift rejection, failed/missing marker
rejection, canonical writer output, and all materialized-view chains on the
oldest supported ClickHouse version.
