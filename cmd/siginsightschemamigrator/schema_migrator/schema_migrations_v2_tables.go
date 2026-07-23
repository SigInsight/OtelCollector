package schemamigrator

import "time"

type MigrationSchemaMigrationRecord struct {
	MigrationID uint64    `ch:"migration_id"`
	Status      string    `ch:"status"`
	Error       string    `ch:"error"`
	CreatedAt   time.Time `ch:"created_at"`
	UpdatedAt   time.Time `ch:"updated_at"`
}

func migrationTableMigration(id uint64, database string) SchemaMigrationRecord {
	return SchemaMigrationRecord{
		MigrationID: id,
		UpItems: []Operation{
			CreateTableOperation{
				Database: database,
				Table:    "schema_migrations_v2",
				Columns: []Column{
					{Name: "migration_id", Type: ColumnTypeUInt64},
					{Name: "status", Type: ColumnTypeString},
					{Name: "error", Type: ColumnTypeString},
					{Name: "created_at", Type: DateTime64ColumnType{Precision: 9}},
					{Name: "updated_at", Type: DateTime64ColumnType{Precision: 9}},
				},
				Engine: ReplacingMergeTree{MergeTree: MergeTree{
					OrderBy:    "migration_id",
					PrimaryKey: "migration_id",
				}},
			},
		},
	}
}

var V2MigrationTablesLogs = []SchemaMigrationRecord{
	migrationTableMigration(1, SigInsightLogsDB),
}

var V2MigrationTablesTraces = []SchemaMigrationRecord{
	migrationTableMigration(2, SigInsightTracesDB),
}

var V2MigrationTablesMetrics = []SchemaMigrationRecord{
	migrationTableMigration(3, SigInsightMetricsDB),
}

var V2MigrationTablesMetadata = []SchemaMigrationRecord{
	migrationTableMigration(4, SigInsightMetadataDB),
}

var V2MigrationTablesAnalytics = []SchemaMigrationRecord{
	migrationTableMigration(5, SigInsightAnalyticsDB),
}

var V2MigrationTablesMeter = []SchemaMigrationRecord{
	migrationTableMigration(6, SigInsightMeterDB),
}
