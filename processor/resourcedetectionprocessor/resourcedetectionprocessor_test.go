package resourcedetectionprocessor

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/processor/processortest"
)

func TestDefaultConfig(t *testing.T) {
	cfg := createDefaultConfig().(*Config)

	require.Equal(t, []string{"env"}, cfg.Detectors)
	require.True(t, cfg.Override)
	require.Equal(t, 5*time.Second, cfg.Timeout)
	require.True(t, cfg.System.ResourceAttributes.HostName.Enabled)
	require.True(t, cfg.System.ResourceAttributes.OsType.Enabled)
}

func TestEnvDetector(t *testing.T) {
	t.Setenv(envResourceAttributes, "service.name=api%20server,service.version=1.2.3")
	t.Setenv(deprecatedEnvResource, "ignored=value")

	res, schemaURL, err := (&envDetector{}).Detect(context.Background())
	require.NoError(t, err)
	require.Empty(t, schemaURL)
	require.Equal(t, map[string]any{
		"service.name":    "api server",
		"service.version": "1.2.3",
	}, res.Attributes().AsRaw())
}

func TestEnvDetectorRejectsEntireInvalidValue(t *testing.T) {
	t.Setenv(envResourceAttributes, "valid=value,broken")

	res, _, err := (&envDetector{}).Detect(context.Background())
	require.Error(t, err)
	require.Empty(t, res.Attributes().AsRaw())
}

func TestCloudDetectorIsRejected(t *testing.T) {
	createdFactory := NewFactory()
	cfg := createdFactory.CreateDefaultConfig().(*Config)
	cfg.Detectors = []string{"ec2"}
	settings := processortest.NewNopSettings(componentType)

	_, err := createdFactory.CreateTraces(context.Background(), settings, cfg, nil)
	require.ErrorContains(t, err, "invalid detector key: ec2")
}

func TestMergeResourceOverride(t *testing.T) {
	existing := pcommon.NewResource()
	existing.Attributes().PutStr("service.name", "existing")
	detected := pcommon.NewResource()
	detected.Attributes().PutStr("service.name", "detected")
	detected.Attributes().PutStr("host.name", "host")

	mergeResource(existing, detected, false)
	require.Equal(t, "existing", attributeString(t, existing, "service.name"))
	require.Equal(t, "host", attributeString(t, existing, "host.name"))

	mergeResource(existing, detected, true)
	require.Equal(t, "detected", attributeString(t, existing, "service.name"))
}

func TestSystemDetectorDefaultAttributes(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.System.HostnameSources = []string{"os"}
	created, err := newSystemDetector(processortest.NewNopSettings(componentType), cfg)
	require.NoError(t, err)

	res, schemaURL, err := created.Detect(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, schemaURL)
	require.NotEmpty(t, attributeString(t, res, "host.name"))
	require.Equal(t, goOSToOSType(runtime.GOOS), attributeString(t, res, "os.type"))
	require.Len(t, res.Attributes().AsRaw(), 2)
}

func TestConfigRejectsUnknownHostnameSource(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.System.HostnameSources = []string{"metadata"}
	require.ErrorContains(t, cfg.Validate(), "invalid value")
}

func attributeString(t *testing.T, res pcommon.Resource, key string) string {
	t.Helper()
	value, ok := res.Attributes().Get(key)
	require.True(t, ok)
	return value.Str()
}
