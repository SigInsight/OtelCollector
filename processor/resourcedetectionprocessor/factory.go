package resourcedetectionprocessor

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/consumer/xconsumer"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/processorhelper"
	"go.opentelemetry.io/collector/processor/processorhelper/xprocessorhelper"
	"go.opentelemetry.io/collector/processor/xprocessor"
)

var componentType = component.MustNewType("resourcedetection")
var capabilities = consumer.Capabilities{MutatesData: true}

type factory struct {
	providerFactory *providerFactory
	providers       map[component.ID]*resourceProvider
	lock            sync.Mutex
}

func NewFactory() processor.Factory {
	createdFactory := &factory{
		providerFactory: &providerFactory{detectors: map[string]detectorFactory{
			envDetectorType:    newEnvDetector,
			systemDetectorType: newSystemDetector,
		}},
		providers: map[component.ID]*resourceProvider{},
	}
	return xprocessor.NewFactory(
		componentType,
		createDefaultConfig,
		xprocessor.WithTraces(createdFactory.createTraces, component.StabilityLevelBeta),
		xprocessor.WithMetrics(createdFactory.createMetrics, component.StabilityLevelBeta),
		xprocessor.WithLogs(createdFactory.createLogs, component.StabilityLevelBeta),
		xprocessor.WithProfiles(createdFactory.createProfiles, component.StabilityLevelDevelopment),
	)
}

func createDefaultConfig() component.Config {
	client := confighttp.NewDefaultClientConfig()
	client.Timeout = 5 * time.Second
	return &Config{
		Detectors:    []string{envDetectorType},
		Override:     true,
		System:       SystemConfig{ResourceAttributes: defaultResourceAttributesConfig()},
		ClientConfig: client,
	}
}

func (f *factory) createProcessor(settings processor.Settings, cfg component.Config) (*resourceDetectionProcessor, error) {
	config := cfg.(*Config)
	provider, err := f.getProvider(settings, config)
	if err != nil {
		return nil, err
	}
	return &resourceDetectionProcessor{provider: provider, override: config.Override, refreshInterval: config.RefreshInterval}, nil
}

func (f *factory) getProvider(settings processor.Settings, cfg *Config) (*resourceProvider, error) {
	f.lock.Lock()
	defer f.lock.Unlock()
	if provider, ok := f.providers[settings.ID]; ok {
		return provider, nil
	}
	provider, err := f.providerFactory.create(settings, cfg.Timeout, cfg)
	if err != nil {
		return nil, err
	}
	f.providers[settings.ID] = provider
	return provider, nil
}

func (f *factory) createTraces(ctx context.Context, settings processor.Settings, cfg component.Config, next consumer.Traces) (processor.Traces, error) {
	createdProcessor, err := f.createProcessor(settings, cfg)
	if err != nil {
		return nil, err
	}
	return processorhelper.NewTraces(ctx, settings, cfg, next, createdProcessor.processTraces,
		processorhelper.WithCapabilities(capabilities), processorhelper.WithStart(createdProcessor.Start), processorhelper.WithShutdown(createdProcessor.Shutdown))
}

func (f *factory) createMetrics(ctx context.Context, settings processor.Settings, cfg component.Config, next consumer.Metrics) (processor.Metrics, error) {
	createdProcessor, err := f.createProcessor(settings, cfg)
	if err != nil {
		return nil, err
	}
	return processorhelper.NewMetrics(ctx, settings, cfg, next, createdProcessor.processMetrics,
		processorhelper.WithCapabilities(capabilities), processorhelper.WithStart(createdProcessor.Start), processorhelper.WithShutdown(createdProcessor.Shutdown))
}

func (f *factory) createLogs(ctx context.Context, settings processor.Settings, cfg component.Config, next consumer.Logs) (processor.Logs, error) {
	createdProcessor, err := f.createProcessor(settings, cfg)
	if err != nil {
		return nil, err
	}
	return processorhelper.NewLogs(ctx, settings, cfg, next, createdProcessor.processLogs,
		processorhelper.WithCapabilities(capabilities), processorhelper.WithStart(createdProcessor.Start), processorhelper.WithShutdown(createdProcessor.Shutdown))
}

func (f *factory) createProfiles(ctx context.Context, settings processor.Settings, cfg component.Config, next xconsumer.Profiles) (xprocessor.Profiles, error) {
	createdProcessor, err := f.createProcessor(settings, cfg)
	if err != nil {
		return nil, err
	}
	return xprocessorhelper.NewProfiles(ctx, settings, cfg, next, createdProcessor.processProfiles,
		xprocessorhelper.WithCapabilities(capabilities), xprocessorhelper.WithStart(createdProcessor.Start), xprocessorhelper.WithShutdown(createdProcessor.Shutdown))
}
