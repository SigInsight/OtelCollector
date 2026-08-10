package schemamigrator

import (
	"context"
	"testing"

	mockhouse "github.com/srikanthccv/ClickHouse-go-mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func expectCanonicalFingerprint(
	t *testing.T,
	conn mockhouse.ClickConnMockCommon,
	database string,
	fingerprint CanonicalSchemaFingerprint,
) {
	t.Helper()
	conn.ExpectQueryRow("SELECT count() FROM system.databases WHERE name = $1").WillReturnRow(mockhouse.NewRow(
		[]mockhouse.ColumnType{{Name: "count()", Type: "UInt64"}},
		[]any{uint64(1)},
	))
	conn.ExpectQueryRow(canonicalTableFingerprintQuery).WillReturnRow(mockhouse.NewRow(
		[]mockhouse.ColumnType{{Name: "count()", Type: "UInt64"}, {Name: "hash", Type: "UInt64"}},
		[]any{fingerprint.TableCount, fingerprint.TableHash},
	))
	conn.ExpectQueryRow(canonicalColumnFingerprintQuery).WillReturnRow(mockhouse.NewRow(
		[]mockhouse.ColumnType{{Name: "count()", Type: "UInt64"}, {Name: "hash", Type: "UInt64"}},
		[]any{fingerprint.ColumnCount, fingerprint.ColumnHash},
	))
	conn.ExpectQueryRow(canonicalIndexFingerprintQuery).WillReturnRow(mockhouse.NewRow(
		[]mockhouse.ColumnType{{Name: "count()", Type: "UInt64"}, {Name: "hash", Type: "UInt64"}},
		[]any{fingerprint.IndexCount, fingerprint.IndexHash},
	))
}

func TestVerifyCanonicalSchema(t *testing.T) {
	conn, err := mockhouse.NewClickHouseNative(nil)
	require.NoError(t, err)
	expected := canonicalSchemaFingerprints[SigInsightLogsDB]
	expectCanonicalFingerprint(t, conn, SigInsightLogsDB, expected)

	manager, err := NewMigrationManager(WithConn(conn), WithLogger(zap.NewNop()))
	require.NoError(t, err)
	require.NoError(t, manager.VerifyCanonicalSchema(context.Background(), SigInsightLogsDB))
	require.NoError(t, conn.ExpectationsWereMet())
}

func TestVerifyCanonicalSchemaRejectsDrift(t *testing.T) {
	conn, err := mockhouse.NewClickHouseNative(nil)
	require.NoError(t, err)
	drifted := canonicalSchemaFingerprints[SigInsightLogsDB]
	drifted.ColumnHash++
	expectCanonicalFingerprint(t, conn, SigInsightLogsDB, drifted)

	manager, err := NewMigrationManager(WithConn(conn), WithLogger(zap.NewNop()))
	require.NoError(t, err)
	err = manager.VerifyCanonicalSchema(context.Background(), SigInsightLogsDB)
	require.ErrorIs(t, err, ErrCanonicalSchemaMismatch)
	require.ErrorContains(t, err, "siginsight_logs")
	require.NoError(t, conn.ExpectationsWereMet())
}
