package schemamigrator

import (
	"context"
	"testing"

	mockhouse "github.com/srikanthccv/ClickHouse-go-mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestClassifyBaselineState(t *testing.T) {
	spec := BaselineSpec{
		Database:             SigInsightLogsDB,
		BaseMigrationIDs:     []uint64{1, 2},
		RequiredMigrationIDs: []uint64{1000, 1001},
	}

	tests := []struct {
		name     string
		snapshot BaselineSnapshot
		state    BaselineState
		missing  []uint64
		nonFinal []uint64
		ahead    []uint64
	}{
		{
			name: "empty",
			snapshot: BaselineSnapshot{
				TrackingTableCount: 1,
				MigrationStatuses:  map[uint64]string{},
			},
			state: BaselineStateEmpty,
		},
		{
			name: "complete fresh",
			snapshot: BaselineSnapshot{
				DomainTableCount:   1,
				TrackingTableCount: 1,
				MigrationStatuses: map[uint64]string{
					1: FinishedStatus, 2: FinishedStatus,
					1000: FinishedStatus, 1001: FinishedStatus,
				},
			},
			state: BaselineStateCompleteFresh,
		},
		{
			name: "complete legacy",
			snapshot: BaselineSnapshot{
				DomainTableCount:   1,
				LegacyTableExists:  true,
				TrackingTableCount: 1,
				MigrationStatuses: map[uint64]string{
					1000: FinishedStatus, 1001: FinishedStatus,
				},
			},
			state: BaselineStateCompleteLegacy,
		},
		{
			name: "missing migration",
			snapshot: BaselineSnapshot{
				DomainTableCount:   1,
				TrackingTableCount: 1,
				MigrationStatuses: map[uint64]string{
					1: FinishedStatus, 2: FinishedStatus,
					1000: FinishedStatus,
				},
			},
			state:   BaselineStatePartial,
			missing: []uint64{1001},
		},
		{
			name: "failed migration",
			snapshot: BaselineSnapshot{
				DomainTableCount:   1,
				TrackingTableCount: 1,
				MigrationStatuses: map[uint64]string{
					1: FinishedStatus, 2: FinishedStatus,
					1000: FinishedStatus, 1001: FailedStatus,
				},
			},
			state:    BaselineStatePartial,
			nonFinal: []uint64{1001},
		},
		{
			name: "incomplete tracking tables",
			snapshot: BaselineSnapshot{
				DomainTableCount:   1,
				TrackingTableCount: 0,
				MigrationStatuses: map[uint64]string{
					1: FinishedStatus, 2: FinishedStatus,
					1000: FinishedStatus, 1001: FinishedStatus,
				},
			},
			state: BaselineStatePartial,
		},
		{
			name: "database ahead of baseline",
			snapshot: BaselineSnapshot{
				DomainTableCount:   1,
				TrackingTableCount: 1,
				MigrationStatuses: map[uint64]string{
					1: FinishedStatus, 2: FinishedStatus,
					1000: FinishedStatus, 1001: FinishedStatus,
					3000: FinishedStatus,
				},
			},
			state: BaselineStateAhead,
			ahead: []uint64{3000},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyBaselineState(spec, tc.snapshot)
			assert.Equal(t, tc.state, got.State)
			assert.Equal(t, tc.missing, got.MissingMigrationIDs)
			assert.Equal(t, tc.nonFinal, got.NonFinishedMigrationIDs)
			assert.Equal(t, tc.ahead, got.UnexpectedMigrationIDs)
		})
	}
}

func TestV1BaselineSpecs(t *testing.T) {
	specs := V1BaselineSpecs()
	require.Len(t, specs, len(Databases))
	assert.Equal(t, Databases, baselineDatabases(specs))
}

func TestInspectBaselineState(t *testing.T) {
	conn, err := mockhouse.NewClickHouseNative(nil)
	require.NoError(t, err)

	inventoryRow := mockhouse.NewRow(
		[]mockhouse.ColumnType{
			{Name: "domain_tables", Type: "UInt64"},
			{Name: "legacy_tables", Type: "UInt64"},
			{Name: "tracking_tables", Type: "UInt64"},
		},
		[]any{uint64(1), uint64(0), uint64(1)},
	)
	conn.ExpectQueryRow(baselineInventoryQuery).WillReturnRow(inventoryRow)

	migrationRows := mockhouse.NewRows(
		[]mockhouse.ColumnType{
			{Name: "migration_id", Type: "UInt64"},
			{Name: "status", Type: "String"},
		},
		[][]any{
			{uint64(1), FinishedStatus},
			{uint64(1000), FinishedStatus},
		},
	)
	conn.ExpectQuery(baselineMigrationsQuery(SigInsightLogsDB)).WillReturnRows(migrationRows)

	manager, err := NewMigrationManager(
		WithConn(conn),
		WithLogger(zap.NewNop()),
	)
	require.NoError(t, err)

	got, err := manager.InspectBaselineState(context.Background(), BaselineSpec{
		Database:             SigInsightLogsDB,
		BaseMigrationIDs:     []uint64{1},
		RequiredMigrationIDs: []uint64{1000},
	})
	require.NoError(t, err)
	assert.Equal(t, BaselineStateCompleteFresh, got.State)
	require.NoError(t, conn.ExpectationsWereMet())
}

func baselineDatabases(specs []BaselineSpec) []string {
	databases := make([]string, 0, len(specs))
	for _, spec := range specs {
		databases = append(databases, spec.Database)
	}
	return databases
}
