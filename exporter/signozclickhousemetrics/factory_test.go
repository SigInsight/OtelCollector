package signozclickhousemetrics

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateDefaultConfig(t *testing.T) {
	factory := NewFactory()
	cfg, ok := factory.CreateDefaultConfig().(*Config)
	require.True(t, ok)
	require.Equal(t, "siginsight_metrics", cfg.Database)
	require.Equal(t, "metric_points", cfg.SamplesTable)
	require.Equal(t, "metric_series", cfg.TimeSeriesTable)
	require.Equal(t, "exp_hist", cfg.ExpHistTable)
	require.Equal(t, "metadata", cfg.MetadataTable)
}
