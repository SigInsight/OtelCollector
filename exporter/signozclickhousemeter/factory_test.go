package signozclickhousemeter

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateDefaultConfig(t *testing.T) {
	factory := NewFactory()
	cfg, ok := factory.CreateDefaultConfig().(*Config)
	require.True(t, ok)
	require.Equal(t, "siginsight_meter", cfg.Database)
	require.Equal(t, "meter_points", cfg.SamplesTable)
}
