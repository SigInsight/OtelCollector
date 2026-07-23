package schemamigrator

import (
	"context"
	"errors"
	"fmt"
)

var ErrV1SchemaMismatch = errors.New("v1 schema fingerprint mismatch")

type SchemaFingerprint struct {
	TableCount  uint64
	TableHash   uint64
	ColumnCount uint64
	ColumnHash  uint64
}

type SchemaFingerprintExpectation struct {
	SchemaFingerprint
}

const schemaTableFingerprintQuery = `SELECT
	count(),
	cityHash64(arrayStringConcat(arraySort(groupArray(concat(
		name, '|', engine, '|', partition_key, '|', sorting_key, '|',
		primary_key, '|', sampling_key, '|', create_table_query
	))), '\n'))
FROM system.tables
WHERE database = $1
	AND name NOT IN ('schema_migrations', 'schema_migrations_v2')`

const schemaColumnFingerprintQuery = `SELECT
	count(),
	cityHash64(arrayStringConcat(arraySort(groupArray(concat(
		table, '|', toString(position), '|', name, '|', type, '|',
		default_kind, '|', default_expression, '|', compression_codec
	))), '\n'))
FROM system.columns
WHERE database = $1
	AND table NOT IN ('schema_migrations', 'schema_migrations_v2')`

func (m *MigrationManager) InspectSchemaFingerprint(ctx context.Context, database string) (SchemaFingerprint, error) {
	if !isKnownDatabase(database) {
		return SchemaFingerprint{}, fmt.Errorf("unknown schema database %q", database)
	}

	var fingerprint SchemaFingerprint
	if err := m.conn.QueryRow(ctx, schemaTableFingerprintQuery, database).Scan(
		&fingerprint.TableCount,
		&fingerprint.TableHash,
	); err != nil {
		return SchemaFingerprint{}, fmt.Errorf("fingerprint tables for database %s: %w", database, err)
	}
	if err := m.conn.QueryRow(ctx, schemaColumnFingerprintQuery, database).Scan(
		&fingerprint.ColumnCount,
		&fingerprint.ColumnHash,
	); err != nil {
		return SchemaFingerprint{}, fmt.Errorf("fingerprint columns for database %s: %w", database, err)
	}

	return fingerprint, nil
}

func (m *MigrationManager) VerifyV1BaselineSchema(ctx context.Context, spec BaselineSpec) error {
	expected := v1SchemaFingerprints[spec.Database].SchemaFingerprint
	if expected == (SchemaFingerprint{}) {
		return fmt.Errorf("%w: v1 schema fingerprint is not defined for database %s", ErrV1SchemaMismatch, spec.Database)
	}

	actual, err := m.InspectSchemaFingerprint(ctx, spec.Database)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf(
			"%w: v1 schema mismatch for database %s: got tables=%d/%d columns=%d/%d, want tables=%d/%d columns=%d/%d",
			ErrV1SchemaMismatch, spec.Database,
			actual.TableCount, actual.TableHash, actual.ColumnCount, actual.ColumnHash,
			expected.TableCount, expected.TableHash, expected.ColumnCount, expected.ColumnHash,
		)
	}
	return nil
}
