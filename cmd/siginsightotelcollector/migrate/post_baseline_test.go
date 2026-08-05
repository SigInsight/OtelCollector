package migrate

import (
	"testing"

	schemamigrator "github.com/SigInsight/OtelCollector/cmd/siginsightschemamigrator/schema_migrator"
	"github.com/stretchr/testify/require"
)

func TestPrerequisitePostBaselineMigrations(t *testing.T) {
	logsSync := findPostBaselineMigration(t, schemamigrator.PostBaselineSyncMigrations, schemamigrator.SigInsightLogsDB, 2004)
	require.Empty(t, prerequisitePostBaselineMigrations(logsSync, postBaselineSyncPhase),
		"a later sync migration must not be blocked by cleanup migrations from an earlier release")

	logsAsync := findPostBaselineMigration(t, schemamigrator.PostBaselineAsyncMigrations, schemamigrator.SigInsightLogsDB, 2005)
	prerequisites := prerequisitePostBaselineMigrations(logsAsync, postBaselineAsyncPhase)
	ids := make([]uint64, 0, len(prerequisites))
	for _, migration := range prerequisites {
		ids = append(ids, migration.Migration.MigrationID)
	}
	require.ElementsMatch(t, []uint64{2001, 2002, 2003, 2004}, ids)
}

func findPostBaselineMigration(
	t *testing.T,
	migrations []schemamigrator.PostBaselineMigration,
	database string,
	id uint64,
) schemamigrator.PostBaselineMigration {
	t.Helper()
	for _, migration := range migrations {
		if migration.Database == database && migration.Migration.MigrationID == id {
			return migration
		}
	}
	t.Fatalf("post-baseline migration %s/%d not found", database, id)
	return schemamigrator.PostBaselineMigration{}
}
