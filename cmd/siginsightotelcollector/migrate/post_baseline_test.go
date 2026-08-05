package migrate

import (
	"testing"

	schemamigrator "github.com/SigInsight/OtelCollector/cmd/siginsightschemamigrator/schema_migrator"
	"github.com/stretchr/testify/require"
)

func TestPrerequisitePostBaselineMigrations(t *testing.T) {
	require.Empty(t, schemamigrator.PostBaselineSyncMigrations)
	require.Empty(t, schemamigrator.PostBaselineAsyncMigrations)
}
