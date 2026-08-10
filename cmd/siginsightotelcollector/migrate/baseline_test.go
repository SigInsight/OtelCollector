package migrate

import (
	"testing"

	schemamigrator "github.com/SigInsight/OtelCollector/cmd/siginsightschemamigrator/schema_migrator"
	"github.com/stretchr/testify/require"
)

func TestFormatBaselineStateErrorExplainsDestructiveCutover(t *testing.T) {
	report := schemamigrator.BaselineReport{
		State: schemamigrator.BaselineStateIncompatible,
		Databases: []schemamigrator.BaselineInspection{
			{
				Database:               schemamigrator.SigInsightLogsDB,
				State:                  schemamigrator.BaselineStateIncompatible,
				UnexpectedMigrationIDs: []uint64{2001},
			},
		},
	}

	err := formatBaselineStateError(report)
	require.ErrorContains(t, err, "canonical baseline is incompatible")
	require.ErrorContains(t, err, "cannot adopt an existing telemetry schema")
	require.ErrorContains(t, err, "discard the siginsight_* telemetry databases")
	require.ErrorContains(t, err, "siginsight_logs=incompatible")
}
