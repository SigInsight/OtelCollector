package components

import (
	"github.com/SigInsight/OtelCollector/connectors/signozmeterconnector"
	"github.com/SigInsight/OtelCollector/exporter/clickhouselogsexporter"
	"github.com/SigInsight/OtelCollector/exporter/clickhousetracesexporter"
	"github.com/SigInsight/OtelCollector/exporter/metadataexporter"
	"github.com/SigInsight/OtelCollector/exporter/signozclickhousemeter"
	"github.com/SigInsight/OtelCollector/exporter/signozclickhousemetrics"
	_ "github.com/SigInsight/OtelCollector/pkg/parser/grok"
	"github.com/SigInsight/OtelCollector/processor/resourcedetectionprocessor"
	"github.com/SigInsight/OtelCollector/processor/signozlogspipelineprocessor"
	"github.com/SigInsight/OtelCollector/processor/signozspanmetricsprocessor"
	"github.com/SigInsight/OtelCollector/processor/signoztailsampler"
	"github.com/SigInsight/OtelCollector/receiver/clickhousesystemtablesreceiver"
	"github.com/SigInsight/OtelCollector/receiver/prometheusreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/healthcheckextension"
	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/pprofextension"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/filterprocessor"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8seventsreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/kubeletstatsreceiver"
	"go.opentelemetry.io/collector/connector"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/debugexporter"
	"go.opentelemetry.io/collector/exporter/nopexporter"
	"go.opentelemetry.io/collector/extension"
	"go.opentelemetry.io/collector/extension/zpagesextension"
	"go.opentelemetry.io/collector/otelcol"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/batchprocessor"
	"go.opentelemetry.io/collector/processor/memorylimiterprocessor"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/receiver/nopreceiver"
	"go.opentelemetry.io/collector/receiver/otlpreceiver"
	"go.opentelemetry.io/collector/service/telemetry/otelconftelemetry"
	"go.uber.org/multierr"
)

func Components() (otelcol.Factories, error) {
	extensions := []extension.Factory{}

	receivers := []receiver.Factory{
		clickhousesystemtablesreceiver.NewFactory(),
		hostmetricsreceiver.NewFactory(),
		k8sclusterreceiver.NewFactory(),
		k8seventsreceiver.NewFactory(),
		kubeletstatsreceiver.NewFactory(),
		prometheusreceiver.NewFactory(),
	}

	exporters := []exporter.Factory{
		clickhouselogsexporter.NewFactory(),
		signozclickhousemetrics.NewFactory(),
		clickhousetracesexporter.NewFactory(),
		debugexporter.NewFactory(),
		metadataexporter.NewFactory(),
		nopexporter.NewFactory(),
		signozclickhousemeter.NewFactory(),
	}

	processors := []processor.Factory{
		filterprocessor.NewFactory(),
		resourcedetectionprocessor.NewFactory(),
		signozspanmetricsprocessor.NewFactory(),
		signoztailsampler.NewFactory(),
		signozlogspipelineprocessor.NewFactory(),
	}

	connectors := []connector.Factory{
		signozmeterconnector.NewFactory(),
	}

	factories, err := CoreComponents(
		extensions,
		receivers,
		processors,
		exporters,
		connectors,
	)
	if err != nil {
		return otelcol.Factories{}, err
	}

	return factories, err
}

func CoreComponents(
	extensions []extension.Factory,
	receivers []receiver.Factory,
	processors []processor.Factory,
	exporters []exporter.Factory,
	connectors []connector.Factory,
) (
	otelcol.Factories,
	error,
) {
	var errs []error

	extensions = append(
		extensions,
		healthcheckextension.NewFactory(),
		pprofextension.NewFactory(),
		zpagesextension.NewFactory(),
	)
	extensionsMap, err := otelcol.MakeFactoryMap(extensions...)
	if err != nil {
		errs = append(errs, err)
	}

	receivers = append(
		receivers,
		otlpreceiver.NewFactory(),
		nopreceiver.NewFactory(),
	)
	receiversMap, err := otelcol.MakeFactoryMap(receivers...)
	if err != nil {
		errs = append(errs, err)
	}

	exportersMap, err := otelcol.MakeFactoryMap(exporters...)
	if err != nil {
		errs = append(errs, err)
	}

	processors = append(
		processors,
		batchprocessor.NewFactory(),
		memorylimiterprocessor.NewFactory(),
	)

	processorsMap, err := otelcol.MakeFactoryMap(processors...)
	if err != nil {
		errs = append(errs, err)
	}

	connectorsMap, err := otelcol.MakeFactoryMap(connectors...)
	if err != nil {
		errs = append(errs, err)
	}

	factories := otelcol.Factories{
		Extensions: extensionsMap,
		Receivers:  receiversMap,
		Processors: processorsMap,
		Exporters:  exportersMap,
		Connectors: connectorsMap,
		Telemetry:  otelconftelemetry.NewFactory(),
	}

	return factories, multierr.Combine(errs...)
}
