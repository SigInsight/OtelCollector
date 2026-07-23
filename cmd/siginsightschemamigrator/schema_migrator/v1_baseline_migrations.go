package schemamigrator

import (
	"fmt"
	"time"
)

// V1BaselineMigrationID is a boundary marker, not the next migration ID.
// Post-v1 schema changes must run after this baseline instead of modifying it.
const V1BaselineMigrationID uint64 = 999

var v1SchemaFingerprints = map[string]SchemaFingerprintExpectation{
	SigInsightAnalyticsDB: {
		SchemaFingerprint: SchemaFingerprint{TableCount: 1, TableHash: 573356622719098860, ColumnCount: 11, ColumnHash: 4111144204702223109},
	},
	SigInsightLogsDB: {
		SchemaFingerprint: SchemaFingerprint{TableCount: 6, TableHash: 10013745453448773421, ColumnCount: 43, ColumnHash: 15143235747694373747},
	},
	SigInsightMetadataDB: {
		SchemaFingerprint: SchemaFingerprint{TableCount: 2, TableHash: 9471719162152962200, ColumnCount: 13, ColumnHash: 11073036235476819325},
	},
	SigInsightMeterDB: {
		SchemaFingerprint: SchemaFingerprint{TableCount: 3, TableHash: 5731232586497943347, ColumnCount: 38, ColumnHash: 8743824752276981965},
	},
	SigInsightMetricsDB: {
		SchemaFingerprint: SchemaFingerprint{TableCount: 18, TableHash: 4727395258706994518, ColumnCount: 196, ColumnHash: 954080453982615115},
	},
	SigInsightTracesDB: {
		SchemaFingerprint: SchemaFingerprint{TableCount: 21, TableHash: 15141153314076288482, ColumnCount: 235, ColumnHash: 12795843324237009066},
	},
}

func newV1BaselineSpec(database string, base, required []uint64) BaselineSpec {
	return BaselineSpec{
		Database:             database,
		BaseMigrationIDs:     base,
		RequiredMigrationIDs: required,
		AllowedMigrationIDs:  postBaselineMigrationIDs(database),
		BaselineMigration: SchemaMigrationRecord{
			MigrationID: V1BaselineMigrationID,
			UpItems:     v1BaselineOperations(database),
		},
	}
}

func v1BaselineOperations(database string) []Operation {
	operations := append([]Operation(nil), v1BaselineDDLOperations[database]...)
	if database == SigInsightMetadataDB {
		// These rows are part of the v1 schema contract even though fingerprints
		// only cover system.tables and system.columns.
		operations = append(operations, v1MetadataSeedOperations()...)
	}
	return operations
}

func v1MetadataSeedOperations() []Operation {
	releaseTime := time.Now().UnixNano()
	return []Operation{
		InsertIntoTable{Database: SigInsightMetadataDB, Table: "column_evolution_metadata", LightWeight: true, Synchronous: true, Columns: []string{"signal", "column_name", "column_type", "field_context", "field_name", "version", "release_time"}, Values: "('logs', 'resources_string', 'Map(LowCardinality(String), Float64)', 'resource', '__all__', 0, 0)"},
		InsertIntoTable{Database: SigInsightMetadataDB, Table: "column_evolution_metadata", LightWeight: true, Synchronous: true, Columns: []string{"signal", "column_name", "column_type", "field_context", "field_name", "version", "release_time"}, Values: fmt.Sprintf("('logs', 'resource', 'JSON()', 'resource', '__all__', 1, %d)", releaseTime)},
		InsertIntoTable{Database: SigInsightMetadataDB, Table: "column_evolution_metadata", LightWeight: true, Synchronous: true, Columns: []string{"signal", "column_name", "column_type", "field_context", "field_name", "version", "release_time"}, Values: "('traces', 'resources_string', 'Map(LowCardinality(String), Float64)', 'resource', '__all__', 0, 0)"},
		InsertIntoTable{Database: SigInsightMetadataDB, Table: "column_evolution_metadata", LightWeight: true, Synchronous: true, Columns: []string{"signal", "column_name", "column_type", "field_context", "field_name", "version", "release_time"}, Values: fmt.Sprintf("('traces', 'resource', 'JSON()', 'resource', '__all__', 1, %d)", releaseTime)},
	}
}
