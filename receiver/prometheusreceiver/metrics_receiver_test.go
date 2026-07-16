package prometheusreceiver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver/receivertest"

	"github.com/SigInsight/OtelCollector/receiver/prometheusreceiver/internal/metadata"
	"github.com/SigInsight/OtelCollector/receiver/prometheusreceiver/internal/scrapeconfig"
)

type signalMetricsConsumer struct {
	metrics chan pmetric.Metrics
}

func (c *signalMetricsConsumer) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{}
}

func (c *signalMetricsConsumer) ConsumeMetrics(_ context.Context, metrics pmetric.Metrics) error {
	metricsCopy := pmetric.NewMetrics()
	metrics.CopyTo(metricsCopy)
	select {
	case c.metrics <- metricsCopy:
	default:
	}
	return nil
}

func TestStaticScrapeEndToEnd(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, err := fmt.Fprintln(writer, "# TYPE test_temperature_celsius gauge\ntest_temperature_celsius{room=\"lab\",sensor=\"north\"} 21.5")
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	prometheusConfig, err := scrapeconfig.LoadScrapeConfig(fmt.Appendf(nil, `
global:
  scrape_interval: 20ms
  scrape_timeout: 10ms
scrape_configs:
  - job_name: static-e2e
    static_configs:
      - targets: [%q]
`, serverURL.Host), t.TempDir())
	require.NoError(t, err)

	metricsConsumer := &signalMetricsConsumer{metrics: make(chan pmetric.Metrics, 1)}
	cfg := &Config{PrometheusConfig: prometheusConfig, ignoreMetadata: true, skipOffsetting: true}
	receiver := newPrometheusReceiver(receivertest.NewNopSettings(metadata.Type), cfg, metricsConsumer)
	require.NoError(t, receiver.start(context.Background(), componenttest.NewNopHost(), prometheusComponentTestOptions{
		discovery: prometheusDiscoveryTestOptions{updatert: 10 * time.Millisecond},
		scrape:    prometheusScrapeTestOptions{discoveryReloadInterval: 10 * time.Millisecond},
	}))
	t.Cleanup(func() {
		require.NoError(t, receiver.Shutdown(context.Background()))
	})

	var metrics pmetric.Metrics
	select {
	case metrics = <-metricsConsumer.metrics:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for scraped metrics")
	}

	metric, found := findMetric(metrics, "test_temperature_celsius")
	require.True(t, found, "received metrics: %s", metricNames(metrics))
	require.Equal(t, pmetric.MetricTypeGauge, metric.Type())
	require.Equal(t, 21.5, metric.Gauge().DataPoints().At(0).DoubleValue())
	attributes := metric.Gauge().DataPoints().At(0).Attributes()
	room, ok := attributes.Get("room")
	require.True(t, ok)
	require.Equal(t, "lab", room.Str())
	sensor, ok := attributes.Get("sensor")
	require.True(t, ok)
	require.Equal(t, "north", sensor.Str())
}

func TestGCInterval(t *testing.T) {
	tests := map[string]struct {
		global    time.Duration
		intervals []time.Duration
		want      time.Duration
	}{
		"minimum":        {global: 15 * time.Second, want: defaultGCInterval},
		"global longest": {global: 3 * time.Minute, intervals: []time.Duration{time.Minute}, want: 4 * time.Minute},
		"job longest":    {global: time.Minute, intervals: []time.Duration{30 * time.Second, 4 * time.Minute}, want: 5 * time.Minute},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := &scrapeconfig.Config{GlobalConfig: scrapeconfig.GlobalConfig{ScrapeInterval: model.Duration(test.global)}}
			for index, interval := range test.intervals {
				cfg.ScrapeConfigs = append(cfg.ScrapeConfigs, &scrapeconfig.ScrapeConfig{
					JobName:        fmt.Sprintf("job-%d", index),
					ScrapeInterval: model.Duration(interval),
				})
			}
			require.Equal(t, test.want, gcInterval(cfg))
		})
	}
}

func findMetric(metrics pmetric.Metrics, name string) (pmetric.Metric, bool) {
	resourceMetrics := metrics.ResourceMetrics()
	for resourceIndex := 0; resourceIndex < resourceMetrics.Len(); resourceIndex++ {
		scopeMetrics := resourceMetrics.At(resourceIndex).ScopeMetrics()
		for scopeIndex := 0; scopeIndex < scopeMetrics.Len(); scopeIndex++ {
			metricSlice := scopeMetrics.At(scopeIndex).Metrics()
			for metricIndex := 0; metricIndex < metricSlice.Len(); metricIndex++ {
				metric := metricSlice.At(metricIndex)
				if metric.Name() == name {
					return metric, true
				}
			}
		}
	}
	return pmetric.NewMetric(), false
}

func metricNames(metrics pmetric.Metrics) string {
	var names []string
	resourceMetrics := metrics.ResourceMetrics()
	for resourceIndex := 0; resourceIndex < resourceMetrics.Len(); resourceIndex++ {
		scopeMetrics := resourceMetrics.At(resourceIndex).ScopeMetrics()
		for scopeIndex := 0; scopeIndex < scopeMetrics.Len(); scopeIndex++ {
			metricSlice := scopeMetrics.At(scopeIndex).Metrics()
			for metricIndex := 0; metricIndex < metricSlice.Len(); metricIndex++ {
				names = append(names, metricSlice.At(metricIndex).Name())
			}
		}
	}
	return strings.Join(names, ", ")
}
