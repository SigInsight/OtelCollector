package schemamigrator

import (
	"fmt"
	"slices"
)

// PostBaselineMigrationIDStart separates new migrations from every ID accepted
// by the frozen v1 baseline. IDs are scoped to a database and must never be
// reused after a migration has shipped.
const PostBaselineMigrationIDStart uint64 = 2000

// PostBaselineMigration associates a migration record with its database.
type PostBaselineMigration struct {
	Database  string
	Migration SchemaMigrationRecord
}

// PostBaselineSyncMigrations is the extension point for schema changes that are
// safe to complete during the release upgrade. Append migrations here when all
// operations are non-mutations, idempotent, and lightweight (or ForceMigrate).
//
// Example:
//
//	PostBaselineMigration{
//		Database: SigInsightLogsDB,
//		Migration: SchemaMigrationRecord{
//			MigrationID: 2000,
//			UpItems: []Operation{AlterTableAddColumn{...}},
//		},
//	}
var PostBaselineSyncMigrations = []PostBaselineMigration{}

// PostBaselineAsyncMigrations is the extension point for mutations and other
// work that must run after the synchronous release upgrade. Do not mix sync and
// async operations in one migration; use consecutive IDs and make the sync
// migration the lower ID when both are introduced in the same release.
var PostBaselineAsyncMigrations = []PostBaselineMigration{}

// ValidatePostBaselineMigrations catches registry mistakes before any schema
// operation runs. It is intentionally strict because migration failures are not
// recovered automatically.
func ValidatePostBaselineMigrations() error {
	return validatePostBaselineMigrations(PostBaselineSyncMigrations, PostBaselineAsyncMigrations)
}

func validatePostBaselineMigrations(syncMigrations, asyncMigrations []PostBaselineMigration) error {
	seen := make(map[string]map[uint64]struct{}, len(Databases))
	lastSync := make(map[string]uint64, len(Databases))
	lastAsync := make(map[string]uint64, len(Databases))

	validate := func(phase string, migrations []PostBaselineMigration, last map[string]uint64) error {
		for index, item := range migrations {
			migration := item.Migration
			if !isKnownDatabase(item.Database) {
				return fmt.Errorf("post-baseline %s migration %d has unknown database %q", phase, migration.MigrationID, item.Database)
			}
			if migration.MigrationID < PostBaselineMigrationIDStart {
				return fmt.Errorf("post-baseline %s migration for %s has ID %d; IDs must be >= %d", phase, item.Database, migration.MigrationID, PostBaselineMigrationIDStart)
			}
			if len(migration.UpItems) == 0 {
				return fmt.Errorf("post-baseline %s migration %s/%d has no operations", phase, item.Database, migration.MigrationID)
			}
			if len(migration.DownItems) != 0 {
				return fmt.Errorf("post-baseline %s migration %s/%d has down operations, but rollback is not supported", phase, item.Database, migration.MigrationID)
			}
			if previous := last[item.Database]; previous != 0 && migration.MigrationID <= previous {
				return fmt.Errorf("post-baseline %s migrations for %s are not in increasing ID order at index %d", phase, item.Database, index)
			}
			last[item.Database] = migration.MigrationID

			if seen[item.Database] == nil {
				seen[item.Database] = make(map[uint64]struct{})
			}
			if _, exists := seen[item.Database][migration.MigrationID]; exists {
				return fmt.Errorf("duplicate post-baseline migration ID %s/%d", item.Database, migration.MigrationID)
			}
			seen[item.Database][migration.MigrationID] = struct{}{}

			for operationIndex, operation := range migration.UpItems {
				if operation == nil {
					return fmt.Errorf("post-baseline %s migration %s/%d operation %d is nil", phase, item.Database, migration.MigrationID, operationIndex)
				}
				operationIsSync := isSyncOperation(operation)
				if (phase == "sync") != operationIsSync {
					return fmt.Errorf("post-baseline %s migration %s/%d operation %d belongs to the other phase", phase, item.Database, migration.MigrationID, operationIndex)
				}
			}
		}
		return nil
	}

	if err := validate("sync", syncMigrations, lastSync); err != nil {
		return err
	}
	return validate("async", asyncMigrations, lastAsync)
}

func isSyncOperation(operation Operation) bool {
	return operation.ForceMigrate() ||
		(!operation.IsMutation() && operation.IsIdempotent() && operation.IsLightweight())
}

func allPostBaselineMigrations() []PostBaselineMigration {
	migrations := make([]PostBaselineMigration, 0, len(PostBaselineSyncMigrations)+len(PostBaselineAsyncMigrations))
	migrations = append(migrations, PostBaselineSyncMigrations...)
	migrations = append(migrations, PostBaselineAsyncMigrations...)
	slices.SortFunc(migrations, func(a, b PostBaselineMigration) int {
		if a.Database != b.Database {
			return slices.Index(Databases, a.Database) - slices.Index(Databases, b.Database)
		}
		if a.Migration.MigrationID < b.Migration.MigrationID {
			return -1
		}
		if a.Migration.MigrationID > b.Migration.MigrationID {
			return 1
		}
		return 0
	})
	return migrations
}

func postBaselineMigrationIDs(database string) []uint64 {
	var ids []uint64
	for _, item := range allPostBaselineMigrations() {
		if item.Database == database {
			ids = append(ids, item.Migration.MigrationID)
		}
	}
	return ids
}
