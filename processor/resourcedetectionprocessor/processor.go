package resourcedetectionprocessor

import (
	"context"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pprofile"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type resourceDetectionProcessor struct {
	provider        *resourceProvider
	override        bool
	refreshInterval time.Duration
}

func (p *resourceDetectionProcessor) Start(ctx context.Context, _ component.Host) error {
	if err := p.provider.refresh(ctx); err != nil {
		return err
	}
	p.provider.startRefreshing(p.refreshInterval)
	return nil
}

func (p *resourceDetectionProcessor) Shutdown(context.Context) error {
	p.provider.stopRefreshing()
	return nil
}

func (p *resourceDetectionProcessor) processTraces(_ context.Context, data ptrace.Traces) (ptrace.Traces, error) {
	res, schemaURL, _ := p.provider.get()
	for index := 0; index < data.ResourceSpans().Len(); index++ {
		item := data.ResourceSpans().At(index)
		item.SetSchemaUrl(mergeSchemaURL(item.SchemaUrl(), schemaURL))
		mergeResource(item.Resource(), res, p.override)
	}
	return data, nil
}

func (p *resourceDetectionProcessor) processMetrics(_ context.Context, data pmetric.Metrics) (pmetric.Metrics, error) {
	res, schemaURL, _ := p.provider.get()
	for index := 0; index < data.ResourceMetrics().Len(); index++ {
		item := data.ResourceMetrics().At(index)
		item.SetSchemaUrl(mergeSchemaURL(item.SchemaUrl(), schemaURL))
		mergeResource(item.Resource(), res, p.override)
	}
	return data, nil
}

func (p *resourceDetectionProcessor) processLogs(_ context.Context, data plog.Logs) (plog.Logs, error) {
	res, schemaURL, _ := p.provider.get()
	for index := 0; index < data.ResourceLogs().Len(); index++ {
		item := data.ResourceLogs().At(index)
		item.SetSchemaUrl(mergeSchemaURL(item.SchemaUrl(), schemaURL))
		mergeResource(item.Resource(), res, p.override)
	}
	return data, nil
}

func (p *resourceDetectionProcessor) processProfiles(_ context.Context, data pprofile.Profiles) (pprofile.Profiles, error) {
	res, schemaURL, _ := p.provider.get()
	for index := 0; index < data.ResourceProfiles().Len(); index++ {
		item := data.ResourceProfiles().At(index)
		item.SetSchemaUrl(mergeSchemaURL(item.SchemaUrl(), schemaURL))
		mergeResource(item.Resource(), res, p.override)
	}
	return data, nil
}
