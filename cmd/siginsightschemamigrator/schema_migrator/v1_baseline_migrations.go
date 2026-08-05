package schemamigrator

// V1BaselineMigrationID is a boundary marker, not the next migration ID.
// Post-v1 schema changes must run after this baseline instead of modifying it.
const V1BaselineMigrationID uint64 = 999

var v1SchemaFingerprints = map[string]SchemaFingerprintExpectation{
	SigInsightAnalyticsDB: {
		SchemaFingerprint: SchemaFingerprint{TableCount: 1, TableHash: 12242196818159068904, ColumnCount: 11, ColumnHash: 9151060719887308879},
	},
	SigInsightLogsDB: {
		SchemaFingerprint: SchemaFingerprint{TableCount: 6, TableHash: 4907456606029717637, ColumnCount: 43, ColumnHash: 6450537337900337272},
	},
	SigInsightMetadataDB: {
		SchemaFingerprint: SchemaFingerprint{TableCount: 0, TableHash: 11160318154034397263, ColumnCount: 0, ColumnHash: 11160318154034397263},
	},
	SigInsightMeterDB: {
		SchemaFingerprint: SchemaFingerprint{TableCount: 3, TableHash: 4794694533261956589, ColumnCount: 38, ColumnHash: 16828873058290010379},
	},
	SigInsightMetricsDB: {
		SchemaFingerprint: SchemaFingerprint{TableCount: 15, TableHash: 13305681492122153582, ColumnCount: 174, ColumnHash: 8325196865590917439},
	},
	SigInsightTracesDB: {
		SchemaFingerprint: SchemaFingerprint{TableCount: 15, TableHash: 9151180053864431952, ColumnCount: 131, ColumnHash: 14213257424511927958},
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
	return canonicalBaselineOperations(database)
}
