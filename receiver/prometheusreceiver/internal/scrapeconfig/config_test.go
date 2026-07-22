package scrapeconfig

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestLoadStaticDefaultsAndInheritance(t *testing.T) {
	cfg, err := LoadScrapeConfig([]byte(`
global:
  scrape_interval: 30s
  scrape_timeout: 7s
  sample_limit: 42
  external_labels:
    cluster: test
scrape_configs:
  - job_name: static
    basic_auth:
      username: user
      password: pass
    static_configs:
      - targets: [localhost:9090]
`), t.TempDir())
	require.NoError(t, err)
	require.True(t, cfg.ContainsScrapeConfigs())
	require.Len(t, cfg.ScrapeConfigs, 1)
	scrape := cfg.ScrapeConfigs[0]
	require.Equal(t, model.Duration(30*time.Second), scrape.ScrapeInterval)
	require.Equal(t, model.Duration(7*time.Second), scrape.ScrapeTimeout)
	require.Equal(t, uint(42), scrape.SampleLimit)
	require.Equal(t, "/metrics", scrape.MetricsPath)
	require.Equal(t, "http", scrape.Scheme)
	require.True(t, scrape.HonorTimestamps)
	require.True(t, scrape.EnableCompression)
	require.Equal(t, DefaultScrapeProtocols, scrape.ScrapeProtocols)
}

func TestHTTPAuthenticationAndTLS(t *testing.T) {
	tests := map[string]string{
		"basic auth": `basic_auth:
      username: user
      password: pass`,
		"authorization": `authorization:
      credentials: token`,
		"oauth2": `oauth2:
      client_id: client
      client_secret: secret
      token_url: https://example.com/token`,
		"tls": `tls_config:
      insecure_skip_verify: true`,
	}
	for name, httpConfig := range tests {
		t.Run(name, func(t *testing.T) {
			data := []byte("scrape_configs:\n  - job_name: auth\n    " + httpConfig + "\n")
			_, err := LoadScrapeConfig(data, t.TempDir())
			require.NoError(t, err)
		})
	}
}

func TestAllowedDiscoveryConfigs(t *testing.T) {
	tests := map[string]string{
		"file": `file_sd_configs:
      - files: [targets/*.json]`,
		"http": `http_sd_configs:
      - url: https://example.com/targets
        oauth2:
          client_id: client
          client_secret: secret
          token_url: https://example.com/token`,
	}
	for name, discoveryConfig := range tests {
		t.Run(name, func(t *testing.T) {
			data := []byte("scrape_configs:\n  - job_name: " + name + "\n    " + discoveryConfig + "\n")
			cfg, err := LoadScrapeConfig(data, t.TempDir())
			require.NoError(t, err)
			require.Len(t, cfg.ScrapeConfigs[0].ServiceDiscoveryConfigs, 1)
		})
	}
}

func TestStrictUnknownFields(t *testing.T) {
	for name, data := range map[string]string{
		"remote write":         "remote_write:\n  - url: https://example.com\n",
		"aws discovery":        "scrape_configs:\n  - job_name: aws\n    ec2_sd_configs:\n      - region: us-east-1\n",
		"kubernetes discovery": "scrape_configs:\n  - job_name: kubernetes\n    kubernetes_sd_configs:\n      - role: pod\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadScrapeConfig([]byte(data), t.TempDir())
			require.Error(t, err)
			require.Contains(t, err.Error(), "field")
		})
	}
}

func TestDuplicateJobAndInvalidTarget(t *testing.T) {
	for name, data := range map[string]string{
		"duplicate job": `scrape_configs:
  - job_name: duplicate
  - job_name: duplicate
`,
		"invalid target": `scrape_configs:
  - job_name: invalid
    static_configs:
      - targets: [http://localhost:9090/metrics]
`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadScrapeConfig([]byte(data), t.TempDir())
			require.Error(t, err)
		})
	}
}

func TestScrapeConfigFilesReload(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "jobs.yml")
	require.NoError(t, os.WriteFile(filename, []byte(`scrape_configs:
  - job_name: first
    static_configs:
      - targets: [localhost:9090]
`), 0o600))

	cfg, err := LoadScrapeConfig([]byte("scrape_config_files: [jobs*.yml]\n"), dir)
	require.NoError(t, err)
	configs, err := cfg.GetScrapeConfigs()
	require.NoError(t, err)
	require.Equal(t, "first", configs[0].JobName)

	require.NoError(t, os.WriteFile(filename, []byte(`scrape_configs:
  - job_name: reloaded
    static_configs:
      - targets: [localhost:9091]
`), 0o600))
	configs, err = cfg.GetScrapeConfigs()
	require.NoError(t, err)
	require.Equal(t, "reloaded", configs[0].JobName)
}

func TestInvalidScrapeConfigFilePattern(t *testing.T) {
	_, err := LoadScrapeConfig([]byte("scrape_config_files: ['jobs/*/*.yml']\n"), t.TempDir())
	require.ErrorContains(t, err, "invalid scrape config file path")
}
