package collectorsimulator

import (
	"context"
	"testing"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/filterprocessor"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/otelcol"
	"go.opentelemetry.io/collector/pdata/plog"
)

type ProcessorConfig struct {
	Name   string
	Config map[string]interface{}
}

func TestLogsProcessingSimulation(t *testing.T) {
	require := require.New(t)

	inputLogs := []plog.Logs{
		makeTestPlog("test log 1", map[string]string{
			"method": "GET",
		}),
		makeTestPlog("test log 2", map[string]string{
			"method": "POST",
		}),
	}

	// Use filterprocessor to drop logs with method=POST, keeping only GET.
	// This verifies the simulator can run a logs pipeline through a processor.
	testFilterConf, err := yaml.Parser().Unmarshal([]byte(`
    error_mode: ignore
    logs:
      log_record:
        - 'attributes["method"] == "POST"'
    `))
	require.Nil(err, "could not unmarshal test filter config")
	testProcessor := ProcessorConfig{
		Name:   "filter/test",
		Config: testFilterConf,
	}

	processorFactories, err := otelcol.MakeFactoryMap(
		filterprocessor.NewFactory(),
	)
	require.Nil(err, "could not create processors factory map")

	configGenerator := makeTestConfigGenerator(
		[]ProcessorConfig{testProcessor},
	)
	outputLogs, collectorErrs, err := SimulateLogsProcessing(
		context.Background(),
		processorFactories,
		configGenerator,
		inputLogs,
		300*time.Millisecond,
	)
	require.Nil(err)
	require.Equal(0, len(collectorErrs))

	// Only the GET log should pass through the filter; POST is dropped.
	require.Equal(1, len(outputLogs))
	rl := outputLogs[0].ResourceLogs().At(0)
	sl := rl.ScopeLogs().At(0)
	record := sl.LogRecords().At(0)
	method, exists := record.Attributes().Get("method")
	require.True(exists)
	require.Equal("GET", method.Str())
}

func makeTestPlog(body string, attrsStr map[string]string) plog.Logs {
	pl := plog.NewLogs()
	rl := pl.ResourceLogs().AppendEmpty()

	scopeLog := rl.ScopeLogs().AppendEmpty()
	slRecord := scopeLog.LogRecords().AppendEmpty()
	slRecord.Body().SetStr(body)
	slAttribs := slRecord.Attributes()
	for k, v := range attrsStr {
		slAttribs.PutStr(k, v)
	}

	return pl
}

func makeTestConfigGenerator(
	processorConfigs []ProcessorConfig,
) ConfigGenerator {
	return func(baseConf []byte) ([]byte, error) {
		conf, err := yaml.Parser().Unmarshal([]byte(baseConf))
		if err != nil {
			return nil, err
		}

		processors := map[string]interface{}{}
		if conf["processors"] != nil {
			processors = conf["processors"].(map[string]interface{})
		}
		logsProcessors := []string{}
		svc := conf["service"].(map[string]interface{})
		svcPipelines := svc["pipelines"].(map[string]interface{})
		svcLogsPipeline := svcPipelines["logs"].(map[string]interface{})
		if svcLogsPipeline["processors"] != nil {
			logsProcessors = svcLogsPipeline["processors"].([]string)
		}

		for _, processorConf := range processorConfigs {
			processors[processorConf.Name] = processorConf.Config
			logsProcessors = append(logsProcessors, processorConf.Name)
		}

		conf["processors"] = processors
		svcLogsPipeline["processors"] = logsProcessors

		confYaml, err := yaml.Parser().Marshal(conf)
		if err != nil {
			return nil, err
		}

		return confYaml, nil
	}
}
