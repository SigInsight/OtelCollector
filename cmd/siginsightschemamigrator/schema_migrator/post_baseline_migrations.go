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
var PostBaselineAsyncMigrations = []PostBaselineMigration{
	{
		Database: SigInsightTracesDB,
		Migration: SchemaMigrationRecord{
			MigrationID: 2000,
			UpItems: dropTables(SigInsightTracesDB,
				"distributed_dependency_graph_minutes_v2",
				"distributed_signoz_error_index_v2",
				"distributed_signoz_index_v2",
				"distributed_signoz_index_v3",
				"distributed_signoz_spans",
				"distributed_span_attributes",
				"distributed_span_attributes_keys",
				"distributed_tag_attributes_v2",
				"distributed_top_level_operations",
				"distributed_trace_summary",
				"distributed_traces_v3_resource",
				"distributed_usage",
				"distributed_usage_explorer",
				"span_attributes",
			),
		},
	},
	// These trace objects belong to the pre-v3 read path. The current trace
	// exporter and every supported reader use span_index_v3 and its active MVs.
	{
		Database: SigInsightTracesDB,
		Migration: SchemaMigrationRecord{
			MigrationID: 2001,
			UpItems: dropTables(SigInsightTracesDB,
				"usage_explorer_mv",
				"usage_explorer",
				"durationSort",
				"span_index_v2",
				"legacy_spans",
			),
		},
	},
	{
		Database: SigInsightMetricsDB,
		Migration: SchemaMigrationRecord{
			MigrationID: 2000,
			UpItems: dropTables(SigInsightMetricsDB,
				"distributed_exp_hist",
				"distributed_metadata",
				"distributed_samples_v2",
				"distributed_samples_v4",
				"distributed_samples_v4_agg_30m",
				"distributed_samples_v4_agg_5m",
				"distributed_time_series_v2",
				"distributed_time_series_v4",
				"distributed_time_series_v4_1day",
				"distributed_time_series_v4_1week",
				"distributed_time_series_v4_6hrs",
				"distributed_updated_metadata",
				"distributed_usage",
				"samples_v2",
				"time_series_v2",
			),
		},
	},
	// Metric metadata is derived from collected time series. The former
	// operator-managed override table is intentionally discarded.
	{
		Database: SigInsightMetricsDB,
		Migration: SchemaMigrationRecord{
			MigrationID: 2001,
			UpItems:     dropTables(SigInsightMetricsDB, "updated_metadata"),
		},
	},
	// Ingestion timestamps were never part of the query, merge, or TTL
	// contract. Event time remains unix_milli.
	{
		Database: SigInsightMetricsDB,
		Migration: SchemaMigrationRecord{
			MigrationID: 2002,
			UpItems: []Operation{
				AlterTableDropColumn{Database: SigInsightMetricsDB, Table: "samples_v4", Column: Column{Name: "inserted_at_unix_milli"}},
				AlterTableDropColumn{Database: SigInsightMetricsDB, Table: "time_series_v4", Column: Column{Name: "inserted_at_unix_milli"}},
			},
		},
	},
	// Logs migration 2001 was previously used to add body_v2, body_promoted,
	// and their JSON-path metadata. Keep the ID registered so databases that
	// completed the legacy migration remain valid; 2002 performs the cleanup.
	{
		Database: SigInsightLogsDB,
		Migration: SchemaMigrationRecord{
			MigrationID: 2001,
			UpItems: []Operation{
				DropTableOperation{Database: SigInsightMetadataDB, Table: "distributed_json_path_types"},
			},
		},
	},
	{
		Database: SigInsightLogsDB,
		Migration: SchemaMigrationRecord{
			MigrationID: 2002,
			UpItems: []Operation{
				AlterTableDropIndex{Database: SigInsightLogsDB, Table: "logs_v2", Index: Index{Name: "body_v2_string_ngram_idx"}},
				AlterTableDropIndex{Database: SigInsightLogsDB, Table: "logs_v2", Index: Index{Name: "body_v2_string_token_idx"}},
				AlterTableDropIndex{Database: SigInsightLogsDB, Table: "logs_v2", Index: Index{Name: "body_v2_paths_ngram_idx"}},
				AlterTableDropIndex{Database: SigInsightLogsDB, Table: "logs_v2", Index: Index{Name: "body_v2_paths_token_idx"}},
				AlterTableDropColumn{Database: SigInsightLogsDB, Table: "logs_v2", Column: Column{Name: "body_promoted"}},
				AlterTableDropColumn{Database: SigInsightLogsDB, Table: "logs_v2", Column: Column{Name: "body_v2"}},
				DropTableOperation{Database: SigInsightMetadataDB, Table: "distributed_json_path_types"},
				DropTableOperation{Database: SigInsightMetadataDB, Table: "json_path_types"},
			},
		},
	},
	{
		Database: SigInsightLogsDB,
		Migration: SchemaMigrationRecord{
			MigrationID: 2003,
			UpItems: dropTables(SigInsightLogsDB,
				"distributed_logs_attribute_keys",
				"distributed_logs_resource_keys",
				"distributed_logs_v2",
				"distributed_logs_v2_resource",
				"distributed_tag_attributes_v2",
				"distributed_usage",
			),
		},
	},
	{
		Database: SigInsightMetadataDB,
		Migration: SchemaMigrationRecord{
			MigrationID: 2000,
			UpItems: dropTables(SigInsightMetadataDB,
				"distributed_attributes_metadata",
				"distributed_column_evolution_metadata",
				"column_evolution_metadata",
			),
		},
	},
	// Related-value autocomplete is no longer a supported product capability.
	// Signal-specific key/value tables remain owned by their signal exporters.
	{
		Database: SigInsightMetadataDB,
		Migration: SchemaMigrationRecord{
			MigrationID: 2001,
			UpItems:     dropTables(SigInsightMetadataDB, "attributes_metadata"),
		},
	},
	{
		Database: SigInsightAnalyticsDB,
		Migration: SchemaMigrationRecord{
			MigrationID: 2000,
			UpItems: dropTables(SigInsightAnalyticsDB,
				"distributed_rule_state_history_v0",
			),
		},
	},
	{
		Database: SigInsightMeterDB,
		Migration: SchemaMigrationRecord{
			MigrationID: 2000,
			UpItems: dropTables(SigInsightMeterDB,
				"distributed_samples",
				"distributed_samples_agg_1d",
			),
		},
	},
}

func dropTables(database string, tables ...string) []Operation {
	operations := make([]Operation, 0, len(tables))
	for _, table := range tables {
		operations = append(operations, DropTableOperation{Database: database, Table: table})
	}
	return operations
}

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
