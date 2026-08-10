package schemamigrator

import (
	"context"
	"errors"
	"fmt"
)

var ErrCanonicalSchemaMismatch = errors.New("canonical runtime schema fingerprint mismatch")

type SchemaFingerprint struct {
	TableCount  uint64
	TableHash   uint64
	ColumnCount uint64
	ColumnHash  uint64
}

type CanonicalSchemaFingerprint struct {
	SchemaFingerprint
	IndexCount uint64
	IndexHash  uint64
}

const emptyFingerprintHash uint64 = 11160318154034397263

var canonicalSchemaFingerprints = map[string]CanonicalSchemaFingerprint{
	SigInsightAnalyticsDB: {SchemaFingerprint: SchemaFingerprint{TableCount: 1, TableHash: 11591339041108278501, ColumnCount: 11, ColumnHash: 3278462431271298120}, IndexHash: emptyFingerprintHash},
	SigInsightLogsDB:      {SchemaFingerprint: SchemaFingerprint{TableCount: 6, TableHash: 11909639802394019030, ColumnCount: 43, ColumnHash: 6345803967174251801}, IndexCount: 18, IndexHash: 4705711304205954561},
	SigInsightMetadataDB:  {SchemaFingerprint: SchemaFingerprint{TableHash: emptyFingerprintHash, ColumnHash: emptyFingerprintHash}, IndexHash: emptyFingerprintHash},
	SigInsightMeterDB:     {SchemaFingerprint: SchemaFingerprint{TableCount: 3, TableHash: 14168883310890703930, ColumnCount: 38, ColumnHash: 16828873058290010379}, IndexHash: emptyFingerprintHash},
	SigInsightMetricsDB:   {SchemaFingerprint: SchemaFingerprint{TableCount: 15, TableHash: 3541604166905384461, ColumnCount: 174, ColumnHash: 8325196865590917439}, IndexHash: emptyFingerprintHash},
	SigInsightTracesDB:    {SchemaFingerprint: SchemaFingerprint{TableCount: 15, TableHash: 6154575477337266297, ColumnCount: 131, ColumnHash: 14213257424511927958}, IndexCount: 28, IndexHash: 813116833925738708},
}

// Runtime fingerprints exclude table TTL/storage settings and the two mutable
// log retention defaults. Those are deliberately changed by the retention API.
// Materialized-view definitions and all stable table/column/index contracts
// remain part of the fingerprint.
const canonicalTableFingerprintQuery = `SELECT
	count(),
	cityHash64(arrayStringConcat(arraySort(groupArray(concat(
		name, '|', engine, '|', partition_key, '|', sorting_key, '|',
		primary_key, '|', sampling_key, '|',
		if(engine = 'MaterializedView', create_table_query, '')
	))), '\n'))
FROM system.tables
WHERE database = $1
	AND name NOT IN ('schema_migrations', 'schema_migrations_v2')`

const canonicalColumnFingerprintQuery = `SELECT
	count(),
	cityHash64(arrayStringConcat(arraySort(groupArray(concat(
		table, '|', toString(position), '|', name, '|', type, '|',
		default_kind, '|',
		if(name IN ('_retention_days', '_retention_days_cold'), '', default_expression), '|',
		compression_codec
	))), '\n'))
FROM system.columns
WHERE database = $1
	AND table NOT IN ('schema_migrations', 'schema_migrations_v2')`

const canonicalIndexFingerprintQuery = `SELECT
	count(),
	cityHash64(arrayStringConcat(arraySort(groupArray(concat(
		table, '|', name, '|', type_full, '|', expr, '|', toString(granularity)
	))), '\n'))
FROM system.data_skipping_indices
WHERE database = $1`

func (m *MigrationManager) InspectCanonicalSchemaFingerprint(ctx context.Context, database string) (CanonicalSchemaFingerprint, error) {
	if !isKnownDatabase(database) {
		return CanonicalSchemaFingerprint{}, fmt.Errorf("unknown schema database %q", database)
	}

	var exists uint64
	if err := m.conn.QueryRow(ctx, "SELECT count() FROM system.databases WHERE name = $1", database).Scan(&exists); err != nil {
		return CanonicalSchemaFingerprint{}, fmt.Errorf("check canonical database %s: %w", database, err)
	}
	if exists != 1 {
		return CanonicalSchemaFingerprint{}, fmt.Errorf("%w: database %s does not exist", ErrCanonicalSchemaMismatch, database)
	}

	var fingerprint CanonicalSchemaFingerprint
	if err := m.conn.QueryRow(ctx, canonicalTableFingerprintQuery, database).Scan(&fingerprint.TableCount, &fingerprint.TableHash); err != nil {
		return CanonicalSchemaFingerprint{}, fmt.Errorf("fingerprint canonical tables for database %s: %w", database, err)
	}
	if err := m.conn.QueryRow(ctx, canonicalColumnFingerprintQuery, database).Scan(&fingerprint.ColumnCount, &fingerprint.ColumnHash); err != nil {
		return CanonicalSchemaFingerprint{}, fmt.Errorf("fingerprint canonical columns for database %s: %w", database, err)
	}
	if err := m.conn.QueryRow(ctx, canonicalIndexFingerprintQuery, database).Scan(&fingerprint.IndexCount, &fingerprint.IndexHash); err != nil {
		return CanonicalSchemaFingerprint{}, fmt.Errorf("fingerprint canonical indexes for database %s: %w", database, err)
	}
	return fingerprint, nil
}

func (m *MigrationManager) VerifyCanonicalSchema(ctx context.Context, database string) error {
	expected, ok := canonicalSchemaFingerprints[database]
	if !ok {
		return fmt.Errorf("%w: fingerprint is not defined for database %s", ErrCanonicalSchemaMismatch, database)
	}
	actual, err := m.InspectCanonicalSchemaFingerprint(ctx, database)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("%w for %s: got tables=%d/%d columns=%d/%d indexes=%d/%d, want tables=%d/%d columns=%d/%d indexes=%d/%d",
			ErrCanonicalSchemaMismatch, database,
			actual.TableCount, actual.TableHash, actual.ColumnCount, actual.ColumnHash, actual.IndexCount, actual.IndexHash,
			expected.TableCount, expected.TableHash, expected.ColumnCount, expected.ColumnHash, expected.IndexCount, expected.IndexHash,
		)
	}
	return nil
}
