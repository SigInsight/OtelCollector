package components

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/confmap/provider/envprovider"
	"go.opentelemetry.io/collector/confmap/provider/fileprovider"
	"go.opentelemetry.io/collector/otelcol"
)

// TestDefaultConfigsLoad guards against the failure mode where a module is
// removed from the factory registry but a shipped default/example config still
// references it — i.e. the collector binary builds and unit-tests pass, but
// a fresh `docker run` or any boot off these configs fatals on startup with
// "'receivers'/'exporters' unknown type: ...".
//
// `otelcol.ConfigProvider.Get` performs the exact same factory-type lookup
// the collector does at boot time, so it fails identically here when a
// config references something `Components()` no longer registers.
func TestDefaultConfigsLoad(t *testing.T) {
	cases := []string{
		"../config/collector.yaml",
		"../config/collector.local.yaml",
		"../example/example-config.yaml",
	}

	factories, err := Components()
	require.NoError(t, err)

	for _, rel := range cases {
		t.Run(rel, func(t *testing.T) {
			abs, err := filepath.Abs(rel)
			require.NoError(t, err)

			provider, err := otelcol.NewConfigProvider(otelcol.ConfigProviderSettings{
				ResolverSettings: confmap.ResolverSettings{
					URIs: []string{abs},
					ProviderFactories: []confmap.ProviderFactory{
						fileprovider.NewFactory(),
						envprovider.NewFactory(),
					},
				},
			})
			require.NoError(t, err)

			cfg, err := provider.Get(context.Background(), factories)
			require.NoError(t, err, "config %s failed to parse against registered factories", rel)
			require.NoError(t, cfg.Validate(), "config %s failed validation", rel)
		})
	}
}
