// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package prometheusreceiver

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/confmap"
)

func TestConfigUnmarshalAllowedDiscoveryConfigs(t *testing.T) {
	tests := map[string]map[string]any{
		"static": {
			"static_configs": []any{map[string]any{"targets": []any{"localhost:9090"}}},
		},
		"file": {
			"file_sd_configs": []any{map[string]any{"files": []any{"targets/*.json"}}},
		},
		"http": {
			"http_sd_configs": []any{map[string]any{"url": "https://example.com/targets"}},
		},
		"kubernetes": {
			"kubernetes_sd_configs": []any{map[string]any{"role": "pod", "kubeconfig_file": "kubeconfig"}},
		},
	}

	for name, discoveryConfig := range tests {
		t.Run(name, func(t *testing.T) {
			scrapeConfig := map[string]any{"job_name": name}
			for key, value := range discoveryConfig {
				scrapeConfig[key] = value
			}
			componentConfig := confmap.NewFromStringMap(map[string]any{
				"config": map[string]any{
					"scrape_configs": []any{scrapeConfig},
				},
			})

			cfg := createDefaultConfig().(*Config)
			require.NoError(t, componentConfig.Unmarshal(cfg))
			require.Len(t, cfg.PrometheusConfig.ScrapeConfigs, 1)
			require.Len(t, cfg.PrometheusConfig.ScrapeConfigs[0].ServiceDiscoveryConfigs, 1)
		})
	}
}

func TestConfigUnmarshalRejectsRemovedReceiverSettings(t *testing.T) {
	for _, field := range []string{"target_allocator", "api_server"} {
		t.Run(field, func(t *testing.T) {
			componentConfig := confmap.NewFromStringMap(map[string]any{
				"config": map[string]any{
					"scrape_configs": []any{map[string]any{"job_name": "valid"}},
				},
				field: map[string]any{"enabled": true},
			})

			cfg := createDefaultConfig().(*Config)
			err := componentConfig.Unmarshal(cfg)
			require.Error(t, err)
			require.Contains(t, err.Error(), field)
		})
	}
}

func TestConfigUnmarshalRejectsUnsupportedPrometheusConfig(t *testing.T) {
	tests := map[string]map[string]any{
		"remote_write": {
			"remote_write": []any{map[string]any{"url": "https://example.com/write"}},
		},
		"ec2_sd_configs": {
			"scrape_configs": []any{map[string]any{
				"job_name":       "ec2",
				"ec2_sd_configs": []any{map[string]any{"region": "us-east-1"}},
			}},
		},
	}

	for name, prometheusConfig := range tests {
		t.Run(name, func(t *testing.T) {
			componentConfig := confmap.NewFromStringMap(map[string]any{"config": prometheusConfig})
			cfg := createDefaultConfig().(*Config)

			err := componentConfig.Unmarshal(cfg)
			require.Error(t, err)
			require.Contains(t, err.Error(), name)
		})
	}
}
