// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package prometheusreceiver // import "github.com/SigInsight/OtelCollector/receiver/prometheusreceiver"

import (
	"errors"
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
	commonconfig "github.com/prometheus/common/config"
	"github.com/prometheus/prometheus/discovery/kubernetes"
	"go.opentelemetry.io/collector/confmap"

	"github.com/SigInsight/OtelCollector/receiver/prometheusreceiver/internal/scrapeconfig"
)

// Config defines the supported scrape-only Prometheus receiver settings.
type Config struct {
	PrometheusConfig   *scrapeconfig.Config `mapstructure:"config"`
	TrimMetricSuffixes bool                 `mapstructure:"trim_metric_suffixes"`

	// For testing only.
	ignoreMetadata bool
	skipOffsetting bool
}

var _ confmap.Unmarshaler = (*Config)(nil)

func (cfg *Config) Unmarshal(componentParser *confmap.Conf) error {
	settings := struct {
		PrometheusConfig   map[string]any `mapstructure:"config"`
		TrimMetricSuffixes bool           `mapstructure:"trim_metric_suffixes"`
	}{}
	if err := componentParser.Unmarshal(&settings); err != nil {
		return err
	}

	yamlOut, err := yaml.MarshalWithOptions(
		settings.PrometheusConfig,
		yaml.CustomMarshaler(func(secret commonconfig.Secret) ([]byte, error) {
			return yaml.Marshal(string(secret))
		}),
	)
	if err != nil {
		return fmt.Errorf("prometheus receiver: failed to marshal config to yaml: %w", err)
	}

	prometheusConfig, err := scrapeconfig.LoadScrapeConfig(yamlOut, "")
	if err != nil {
		return fmt.Errorf("prometheus receiver: failed to load scrape config: %w", err)
	}
	cfg.PrometheusConfig = prometheusConfig
	cfg.TrimMetricSuffixes = settings.TrimMetricSuffixes
	return nil
}

// Validate checks the receiver configuration is valid.
func (cfg *Config) Validate() error {
	if cfg.PrometheusConfig == nil || !cfg.PrometheusConfig.ContainsScrapeConfigs() {
		return errors.New("no Prometheus scrape_configs set")
	}

	scrapeConfigs, err := cfg.PrometheusConfig.GetScrapeConfigs()
	if err != nil {
		return fmt.Errorf("failed to get scrape configs: %w", err)
	}
	for _, scrapeConfig := range scrapeConfigs {
		if scrapeConfig.ScrapeFallbackProtocol == "" {
			scrapeConfig.ScrapeFallbackProtocol = scrapeconfig.PrometheusText0_0_4
		}
		if err := validateHTTPClientConfig(&scrapeConfig.HTTPClientConfig); err != nil {
			return err
		}
		for _, discoveryConfig := range scrapeConfig.ServiceDiscoveryConfigs {
			if kubernetesConfig, ok := discoveryConfig.(*kubernetes.SDConfig); ok {
				if err := validateHTTPClientConfig(&kubernetesConfig.HTTPClientConfig); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func validateHTTPClientConfig(cfg *commonconfig.HTTPClientConfig) error {
	if cfg.Authorization != nil {
		if err := checkFile(cfg.Authorization.CredentialsFile); err != nil {
			return fmt.Errorf("error checking authorization credentials file %q: %w", cfg.Authorization.CredentialsFile, err)
		}
	}

	if err := checkTLSConfig(cfg.TLSConfig); err != nil {
		return err
	}
	return nil
}

func checkFile(fn string) error {
	// Nothing set, nothing to error on.
	if fn == "" {
		return nil
	}
	_, err := os.Stat(fn)
	return err
}

func checkTLSConfig(tlsConfig commonconfig.TLSConfig) error {
	if err := checkFile(tlsConfig.CertFile); err != nil {
		return fmt.Errorf("error checking client cert file %q: %w", tlsConfig.CertFile, err)
	}
	if err := checkFile(tlsConfig.KeyFile); err != nil {
		return fmt.Errorf("error checking client key file %q: %w", tlsConfig.KeyFile, err)
	}
	return nil
}
