// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package prometheusreceiver // import "github.com/SigInsight/OtelCollector/receiver/prometheusreceiver"

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"time"
	"unsafe"

	"github.com/prometheus/client_golang/prometheus"
	commonconfig "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componentstatus"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
	"go.uber.org/zap"
	"go.uber.org/zap/exp/zapslog"

	"github.com/SigInsight/OtelCollector/receiver/prometheusreceiver/internal"
	"github.com/SigInsight/OtelCollector/receiver/prometheusreceiver/internal/metadata"
	"github.com/SigInsight/OtelCollector/receiver/prometheusreceiver/internal/scrape"
	scrapelogging "github.com/SigInsight/OtelCollector/receiver/prometheusreceiver/internal/scrape/logging"
	"github.com/SigInsight/OtelCollector/receiver/prometheusreceiver/internal/scrapeconfig"
	"github.com/SigInsight/OtelCollector/receiver/prometheusreceiver/internal/scrapediscovery"
)

const (
	defaultGCInterval = 2 * time.Minute
	gcIntervalDelta   = 1 * time.Minute
)

// pReceiver is the type that provides Prometheus scraper/receiver functionality.
type pReceiver struct {
	cfg            *Config
	consumer       consumer.Metrics
	cancelFunc     context.CancelFunc
	configLoaded   chan struct{}
	loadConfigOnce sync.Once

	settings          receiver.Settings
	scrapeManager     *scrape.Manager
	discoveryManager  *scrapediscovery.Manager
	registry          *prometheus.Registry
	registerer        prometheus.Registerer
	unregisterMetrics func()
}

// New creates a new prometheus.Receiver reference.
func newPrometheusReceiver(set receiver.Settings, cfg *Config, next consumer.Metrics) *pReceiver {
	registry := prometheus.NewRegistry()
	registerer := prometheus.WrapRegistererWith(
		prometheus.Labels{"receiver": set.ID.String()},
		registry)
	pr := &pReceiver{
		cfg:          cfg,
		consumer:     next,
		settings:     set,
		configLoaded: make(chan struct{}),
		registerer:   registerer,
		registry:     registry,
	}
	return pr
}

// Start is the method that starts Prometheus scraping. It
// is controlled by having previously defined a Configuration using perhaps New.
func (r *pReceiver) Start(ctx context.Context, host component.Host) error {
	return r.start(ctx, host, prometheusComponentTestOptions{})
}

func (r *pReceiver) start(ctx context.Context, host component.Host, opts prometheusComponentTestOptions) error {
	discoveryCtx, cancel := context.WithCancel(ctx)
	r.cancelFunc = cancel

	logger := slog.New(zapslog.NewHandler(r.settings.Logger.Core()))

	err := r.initPrometheusComponents(discoveryCtx, logger, host, opts)
	if err != nil {
		r.settings.Logger.Error("Failed to initPrometheusComponents Prometheus components", zap.Error(err))
		return err
	}

	r.loadConfigOnce.Do(func() {
		close(r.configLoaded)
	})

	return nil
}

func (r *pReceiver) initPrometheusComponents(
	ctx context.Context, logger *slog.Logger, host component.Host,
	opts prometheusComponentTestOptions,
) error {
	// Register the metrics needed by service discovery mechanisms.
	sdMetrics, err := scrapediscovery.CreateAndRegisterSDMetrics(r.registerer)
	if err != nil {
		return fmt.Errorf("failed to register service discovery metrics: %w", err)
	}

	var discoveryManagerTestOptions []scrapediscovery.ManagerOption
	if opts.discovery.updatert > 0 {
		discoveryManagerTestOptions = append(discoveryManagerTestOptions, scrapediscovery.Updatert(opts.discovery.updatert))
	}
	r.discoveryManager = scrapediscovery.NewManager(ctx, logger, r.registerer, sdMetrics, discoveryManagerTestOptions...)
	if r.discoveryManager == nil {
		// NewManager can sometimes return nil if it encountered an error, but
		// the error message is logged separately.
		return errors.New("failed to create discovery manager")
	}

	go func() {
		r.settings.Logger.Info("Starting discovery manager")
		if err = r.discoveryManager.Run(); err != nil && !errors.Is(err, context.Canceled) {
			r.settings.Logger.Error("Discovery manager failed", zap.Error(err))
			componentstatus.ReportStatus(host, componentstatus.NewFatalErrorEvent(err))
		}
	}()

	store, err := internal.NewAppendable(
		r.consumer,
		r.settings,
		!r.cfg.ignoreMetadata,
		r.cfg.PrometheusConfig.GlobalConfig.ExternalLabels,
		r.cfg.TrimMetricSuffixes,
	)
	if err != nil {
		return err
	}

	scrapeOpts := r.initScrapeOptions(opts.scrape)

	// for testing only
	if r.cfg.skipOffsetting {
		optsValue := reflect.ValueOf(scrapeOpts).Elem()
		field := optsValue.FieldByName("skipJitterOffsetting")
		reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).
			Elem().
			Set(reflect.ValueOf(true))
	}

	scrapeManager, err := scrape.NewManager(scrapeOpts, logger, scrapelogging.NewJSONFileLogger, nil, store, r.registerer)
	if err != nil {
		return err
	}
	r.scrapeManager = scrapeManager
	if err := r.scrapeManager.ApplyConfig(r.cfg.PrometheusConfig); err != nil {
		return fmt.Errorf("failed to apply scrape config: %w", err)
	}
	scrapeConfigs, err := r.cfg.PrometheusConfig.GetScrapeConfigs()
	if err != nil {
		return fmt.Errorf("failed to get scrape configs: %w", err)
	}
	discoveryConfigs := make(map[string]scrapediscovery.Configs, len(scrapeConfigs))
	for _, scrapeConfig := range scrapeConfigs {
		discoveryConfigs[scrapeConfig.JobName] = scrapeConfig.ServiceDiscoveryConfigs
	}
	if err := r.discoveryManager.ApplyConfig(discoveryConfigs); err != nil {
		return fmt.Errorf("failed to apply service discovery config: %w", err)
	}

	r.unregisterMetrics = func() {
		r.discoveryManager.UnregisterMetrics()
		r.scrapeManager.UnregisterMetrics()
	}

	go func() {
		// The scrape manager needs to wait for the configuration to be loaded before beginning
		<-r.configLoaded
		r.settings.Logger.Info("Starting scrape manager")
		if err := r.scrapeManager.Run(r.discoveryManager.SyncCh()); err != nil {
			r.settings.Logger.Error("Scrape manager failed", zap.Error(err))
			componentstatus.ReportStatus(host, componentstatus.NewFatalErrorEvent(err))
		}
	}()

	return nil
}

func (r *pReceiver) initScrapeOptions(o prometheusScrapeTestOptions) *scrape.Options {
	opts := &scrape.Options{
		DiscoveryReloadInterval: model.Duration(o.discoveryReloadInterval),
		PassMetadataInContext:   true,
		HTTPClientOptions: []commonconfig.HTTPClientOption{
			commonconfig.WithUserAgent(r.settings.BuildInfo.Command + "/" + r.settings.BuildInfo.Version),
		},
		EnableStartTimestampZeroIngestion: metadata.ReceiverPrometheusreceiverEnableCreatedTimestampZeroIngestionFeatureGate.IsEnabled(),
	}

	return opts
}

// gcInterval returns the longest scrape interval used by a scrape config,
// plus a delta to prevent race conditions.
// This ensures jobs are not garbage collected between scrapes.
func gcInterval(cfg *scrapeconfig.Config) time.Duration {
	gcInterval := max(time.Duration(cfg.GlobalConfig.ScrapeInterval)+gcIntervalDelta, defaultGCInterval)
	for _, scrapeConfig := range cfg.ScrapeConfigs {
		if time.Duration(scrapeConfig.ScrapeInterval)+gcIntervalDelta > gcInterval {
			gcInterval = time.Duration(scrapeConfig.ScrapeInterval) + gcIntervalDelta
		}
	}
	return gcInterval
}

// Shutdown stops and cancels the underlying Prometheus scrapers.
func (r *pReceiver) Shutdown(ctx context.Context) error {
	if r.cancelFunc != nil {
		r.cancelFunc()
	}
	if r.scrapeManager != nil {
		r.scrapeManager.Stop()
	}
	if r.unregisterMetrics != nil {
		r.unregisterMetrics()
	}
	return nil
}

type prometheusComponentTestOptions struct {
	discovery prometheusDiscoveryTestOptions
	scrape    prometheusScrapeTestOptions
}

type prometheusDiscoveryTestOptions struct {
	// updatert is the interval for updating targets.
	//
	// If zero, the default (5s) from Prometheus is used.
	// This option is primarily for testing.
	updatert time.Duration
}

type prometheusScrapeTestOptions struct {
	// discoveryReloadInterval is the interval for reloading
	// scrape configurations.
	//
	// If zero, the default (5s) from Prometheus is used.
	// This option is primarily for testing.
	discoveryReloadInterval time.Duration
}
