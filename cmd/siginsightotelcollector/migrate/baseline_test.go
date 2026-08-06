package migrate

import (
	"testing"

	schemamigrator "github.com/SigInsight/OtelCollector/cmd/siginsightschemamigrator/schema_migrator"
	"github.com/stretchr/testify/require"
)

func TestFormatBaselineStateErrorExplainsDestructiveCutover(t *testing.T) {
	report := schemamigrator.BaselineReport{
		State: schemamigrator.BaselineStateAhead,
		Databases: []schemamigrator.BaselineInspection{
			{
				Database:               schemamigrator.SigInsightLogsDB,
				State:                  schemamigrator.BaselineStateAhead,
				UnexpectedMigrationIDs: []uint64{2001},
			},
		},
	}

	err := formatBaselineStateError(report)
	require.ErrorContains(t, err, "v1 baseline is ahead")
	require.ErrorContains(t, err, "cannot adopt an existing telemetry schema")
	require.ErrorContains(t, err, "discard the siginsight_* telemetry databases")
	require.ErrorContains(t, err, "siginsight_logs=ahead")
}
