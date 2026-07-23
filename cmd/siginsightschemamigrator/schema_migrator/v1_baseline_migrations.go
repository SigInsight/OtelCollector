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
		NonReplicated: SchemaFingerprint{TableCount: 2, TableHash: 9115775075856150932, ColumnCount: 21, ColumnHash: 4487537369260509279},
		Replicated:    SchemaFingerprint{TableCount: 2, TableHash: 14206733227540495132, ColumnCount: 21, ColumnHash: 4487537369260509279},
	},
	SigInsightLogsDB: {
		NonReplicated: SchemaFingerprint{TableCount: 12, TableHash: 14053648442494272017, ColumnCount: 84, ColumnHash: 10876619119462323544},
		Replicated:    SchemaFingerprint{TableCount: 12, TableHash: 15653638739826216655, ColumnCount: 84, ColumnHash: 10876619119462323544},
	},
	SigInsightMetadataDB: {
		NonReplicated: SchemaFingerprint{TableCount: 4, TableHash: 14174565384524331006, ColumnCount: 26, ColumnHash: 3058743734507644696},
		Replicated:    SchemaFingerprint{TableCount: 4, TableHash: 14103122399752458768, ColumnCount: 26, ColumnHash: 3058743734507644696},
	},
	SigInsightMeterDB: {
		NonReplicated: SchemaFingerprint{TableCount: 5, TableHash: 5717364897257198659, ColumnCount: 62, ColumnHash: 12106642581548859426},
		Replicated:    SchemaFingerprint{TableCount: 5, TableHash: 16900608539726510724, ColumnCount: 62, ColumnHash: 12106642581548859426},
	},
	SigInsightMetricsDB: {
		NonReplicated: SchemaFingerprint{TableCount: 31, TableHash: 8955509188233892107, ColumnCount: 330, ColumnHash: 15891760589189666270},
		Replicated:    SchemaFingerprint{TableCount: 31, TableHash: 3479616427438521389, ColumnCount: 330, ColumnHash: 15891760589189666270},
	},
	SigInsightTracesDB: {
		NonReplicated: SchemaFingerprint{TableCount: 34, TableHash: 16604672559652066814, ColumnCount: 406, ColumnHash: 4301296587597110300},
		Replicated:    SchemaFingerprint{TableCount: 34, TableHash: 3709484046233630162, ColumnCount: 406, ColumnHash: 4301296587597110300},
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
		InsertIntoTable{Database: SigInsightMetadataDB, Table: "distributed_column_evolution_metadata", LightWeight: true, Synchronous: true, Columns: []string{"signal", "column_name", "column_type", "field_context", "field_name", "version", "release_time"}, Values: "('logs', 'resources_string', 'Map(LowCardinality(String), Float64)', 'resource', '__all__', 0, 0)"},
		InsertIntoTable{Database: SigInsightMetadataDB, Table: "distributed_column_evolution_metadata", LightWeight: true, Synchronous: true, Columns: []string{"signal", "column_name", "column_type", "field_context", "field_name", "version", "release_time"}, Values: fmt.Sprintf("('logs', 'resource', 'JSON()', 'resource', '__all__', 1, %d)", releaseTime)},
		InsertIntoTable{Database: SigInsightMetadataDB, Table: "distributed_column_evolution_metadata", LightWeight: true, Synchronous: true, Columns: []string{"signal", "column_name", "column_type", "field_context", "field_name", "version", "release_time"}, Values: "('traces', 'resources_string', 'Map(LowCardinality(String), Float64)', 'resource', '__all__', 0, 0)"},
		InsertIntoTable{Database: SigInsightMetadataDB, Table: "distributed_column_evolution_metadata", LightWeight: true, Synchronous: true, Columns: []string{"signal", "column_name", "column_type", "field_context", "field_name", "version", "release_time"}, Values: fmt.Sprintf("('traces', 'resource', 'JSON()', 'resource', '__all__', 1, %d)", releaseTime)},
	}
}
