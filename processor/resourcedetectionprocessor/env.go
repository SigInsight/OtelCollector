package resourcedetectionprocessor

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/processor"
)

const (
	envDetectorType       = "env"
	envResourceAttributes = "OTEL_RESOURCE_ATTRIBUTES"
	deprecatedEnvResource = "OTEL_RESOURCE"
)

type envDetector struct{}

func newEnvDetector(processor.Settings, *Config) (detector, error) {
	return &envDetector{}, nil
}

func (*envDetector) Detect(context.Context) (pcommon.Resource, string, error) {
	res := pcommon.NewResource()
	labels := strings.TrimSpace(os.Getenv(envResourceAttributes))
	if labels == "" {
		labels = strings.TrimSpace(os.Getenv(deprecatedEnvResource))
		if labels == "" {
			return res, "", nil
		}
	}
	if err := initializeAttributeMap(res.Attributes(), labels); err != nil {
		res.Attributes().Clear()
		return res, "", err
	}
	return res, "", nil
}

var labelRegex = regexp.MustCompile(`\s*([[:ascii:]]{1,256}?)\s*=\s*([[:ascii:]]{0,256}?)\s*(?:,|$)`)

func initializeAttributeMap(attributes pcommon.Map, value string) error {
	matches := labelRegex.FindAllStringSubmatchIndex(value, -1)
	if len(matches) == 0 {
		return fmt.Errorf("invalid resource format: %q", value)
	}

	previous := 0
	for _, match := range matches {
		if previous != match[0] {
			return fmt.Errorf("invalid resource format, invalid text: %q", value[previous:match[0]])
		}
		key := value[match[2]:match[3]]
		decoded, err := url.QueryUnescape(value[match[4]:match[5]])
		if err != nil {
			return fmt.Errorf("invalid resource format in attribute: %q, err: %w", value[match[0]:match[1]], err)
		}
		attributes.PutStr(key, decoded)
		previous = match[1]
	}
	if previous != len(value) {
		return fmt.Errorf("invalid resource format, invalid text: %q", value[previous:])
	}
	return nil
}
