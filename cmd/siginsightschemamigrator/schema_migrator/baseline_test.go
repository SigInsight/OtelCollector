package schemamigrator

import (
	"context"
	"testing"

	mockhouse "github.com/srikanthccv/ClickHouse-go-mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestClassifyCanonicalBaselineState(t *testing.T) {
	spec := BaselineSpec{
		Database:            SigInsightLogsDB,
		AllowedMigrationIDs: []uint64{2000},
		BaselineMigration:   SchemaMigrationRecord{MigrationID: CanonicalBaselineMigrationID},
	}
	tests := []struct {
		name       string
		snapshot   BaselineSnapshot
		state      BaselineState
		missing    bool
		nonFinal   []uint64
		unexpected []uint64
	}{
		{name: "pristine", snapshot: BaselineSnapshot{TrackingTableCount: 1, MigrationStatuses: map[uint64]string{}}, state: BaselineStateEmpty},
		{name: "canonical marker", snapshot: BaselineSnapshot{DomainTableCount: 6, TrackingTableCount: 1, MigrationStatuses: map[uint64]string{999: FinishedStatus}}, state: BaselineStateComplete},
		{name: "registered future migration", snapshot: BaselineSnapshot{DomainTableCount: 6, TrackingTableCount: 1, MigrationStatuses: map[uint64]string{999: FinishedStatus, 2000: FinishedStatus}}, state: BaselineStateComplete},
		{name: "legacy tracking table", snapshot: BaselineSnapshot{DomainTableCount: 6, LegacyTrackingTableCount: 1, TrackingTableCount: 1, MigrationStatuses: map[uint64]string{999: FinishedStatus}}, state: BaselineStateIncompatible},
		{name: "unmarked tables", snapshot: BaselineSnapshot{DomainTableCount: 1, TrackingTableCount: 1, MigrationStatuses: map[uint64]string{}}, state: BaselineStateIncompatible, missing: true},
		{name: "failed marker", snapshot: BaselineSnapshot{DomainTableCount: 1, TrackingTableCount: 1, MigrationStatuses: map[uint64]string{999: FailedStatus}}, state: BaselineStateIncompatible, missing: true, nonFinal: []uint64{999}},
		{name: "unknown migration", snapshot: BaselineSnapshot{DomainTableCount: 6, TrackingTableCount: 1, MigrationStatuses: map[uint64]string{999: FinishedStatus, 1000: FinishedStatus}}, state: BaselineStateIncompatible, unexpected: []uint64{1000}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyBaselineState(spec, tc.snapshot)
			assert.Equal(t, tc.state, got.State)
			assert.Equal(t, tc.missing, got.MissingBaselineMarker)
			assert.Equal(t, tc.nonFinal, got.NonFinishedMigrationIDs)
			assert.Equal(t, tc.unexpected, got.UnexpectedMigrationIDs)
		})
	}
}

func TestCanonicalBaselineSpecs(t *testing.T) {
	specs := CanonicalBaselineSpecs()
	require.Len(t, specs, len(Databases))
	for index, spec := range specs {
		assert.Equal(t, Databases[index], spec.Database)
		assert.Equal(t, CanonicalBaselineMigrationID, spec.BaselineMigration.MigrationID)
	}
}

func TestInspectCanonicalBaselineState(t *testing.T) {
	conn, err := mockhouse.NewClickHouseNative(nil)
	require.NoError(t, err)
	conn.ExpectQueryRow(baselineInventoryQuery).WillReturnRow(mockhouse.NewRow(
		[]mockhouse.ColumnType{{Name: "domain_tables", Type: "UInt64"}, {Name: "legacy_tables", Type: "UInt64"}, {Name: "tracking_tables", Type: "UInt64"}},
		[]any{uint64(6), uint64(0), uint64(1)},
	))
	conn.ExpectQuery(baselineMigrationsQuery(SigInsightLogsDB)).WillReturnRows(mockhouse.NewRows(
		[]mockhouse.ColumnType{{Name: "migration_id", Type: "UInt64"}, {Name: "status", Type: "String"}},
		[][]any{{uint64(999), FinishedStatus}},
	))
	manager, err := NewMigrationManager(WithConn(conn), WithLogger(zap.NewNop()))
	require.NoError(t, err)
	got, err := manager.InspectBaselineState(context.Background(), BaselineSpec{
		Database: SigInsightLogsDB, BaselineMigration: SchemaMigrationRecord{MigrationID: CanonicalBaselineMigrationID},
	})
	require.NoError(t, err)
	assert.Equal(t, BaselineStateComplete, got.State)
	require.NoError(t, conn.ExpectationsWereMet())
}
