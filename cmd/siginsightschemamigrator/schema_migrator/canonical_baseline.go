package schemamigrator

// CanonicalBaselineMigrationID identifies the destructive M16 fresh-install
// schema. Future changes use the post-baseline registry starting at ID 2000.
const CanonicalBaselineMigrationID uint64 = 999

// BaselineSpec describes the canonical baseline and the post-baseline IDs that
// may coexist with its marker. Pre-M16 histories are intentionally absent.
type BaselineSpec struct {
	Database            string
	AllowedMigrationIDs []uint64
	BaselineMigration   SchemaMigrationRecord
}

// CanonicalBaselineSpecs returns one explicit fresh-install baseline per
// telemetry database.
func CanonicalBaselineSpecs() []BaselineSpec {
	specs := make([]BaselineSpec, 0, len(Databases))
	for _, database := range Databases {
		specs = append(specs, BaselineSpec{
			Database:            database,
			AllowedMigrationIDs: postBaselineMigrationIDs(database),
			BaselineMigration: SchemaMigrationRecord{
				MigrationID: CanonicalBaselineMigrationID,
				UpItems:     canonicalBaselineDDLOperations[database],
			},
		})
	}
	return specs
}
