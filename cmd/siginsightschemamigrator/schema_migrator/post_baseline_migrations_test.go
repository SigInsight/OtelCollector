package schemamigrator

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidatePostBaselineMigrations(t *testing.T) {
	syncMigration := func(database string, id uint64) PostBaselineMigration {
		return PostBaselineMigration{
			Database: database,
			Migration: SchemaMigrationRecord{
				MigrationID: id,
				UpItems: []Operation{AlterTableAddColumn{
					Database: database,
					Table:    "spans",
					Column:   Column{Name: "new_column", Type: ColumnTypeString},
				}},
			},
		}
	}
	asyncMigration := func(database string, id uint64) PostBaselineMigration {
		return PostBaselineMigration{
			Database: database,
			Migration: SchemaMigrationRecord{
				MigrationID: id,
				UpItems: []Operation{AlterTableDropColumn{
					Database: database,
					Table:    "spans",
					Column:   Column{Name: "old_column"},
				}},
			},
		}
	}

	tests := []struct {
		name  string
		sync  []PostBaselineMigration
		async []PostBaselineMigration
		want  string
	}{
		{name: "valid", sync: []PostBaselineMigration{syncMigration(SigInsightLogsDB, 2000)}, async: []PostBaselineMigration{asyncMigration(SigInsightLogsDB, 2001)}},
		{name: "old id", sync: []PostBaselineMigration{syncMigration(SigInsightLogsDB, 1000)}, want: "IDs must be"},
		{name: "unknown database", sync: []PostBaselineMigration{syncMigration("unknown", 2000)}, want: "unknown database"},
		{name: "duplicate across phases", sync: []PostBaselineMigration{syncMigration(SigInsightLogsDB, 2000)}, async: []PostBaselineMigration{asyncMigration(SigInsightLogsDB, 2000)}, want: "duplicate"},
		{name: "out of order", sync: []PostBaselineMigration{syncMigration(SigInsightLogsDB, 2001), syncMigration(SigInsightLogsDB, 2000)}, want: "increasing ID"},
		{name: "empty operations", sync: []PostBaselineMigration{{Database: SigInsightLogsDB, Migration: SchemaMigrationRecord{MigrationID: 2000}}}, want: "no operations"},
		{name: "wrong phase", sync: []PostBaselineMigration{asyncMigration(SigInsightLogsDB, 2000)}, want: "other phase"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePostBaselineMigrations(tc.sync, tc.async)
			if tc.want == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestV1BaselineAllowsRegisteredPostBaselineIDs(t *testing.T) {
	spec := BaselineSpec{
		Database:            SigInsightLogsDB,
		AllowedMigrationIDs: []uint64{2000},
		BaselineMigration:   SchemaMigrationRecord{MigrationID: V1BaselineMigrationID},
	}
	inspection := ClassifyBaselineState(spec, BaselineSnapshot{
		DomainTableCount:   1,
		TrackingTableCount: 1,
		MigrationStatuses: map[uint64]string{
			V1BaselineMigrationID: FinishedStatus,
			2000:                  FinishedStatus,
		},
	})
	require.Equal(t, BaselineStateCompleteFresh, inspection.State)
}
