package scrapeconfig

import (
	"errors"
	"fmt"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/alecthomas/units"
	commonconfig "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"
	"go.yaml.in/yaml/v2"

	"github.com/SigInsight/OtelCollector/receiver/prometheusreceiver/internal/scrapediscovery"
)

func boolPtr(value bool) *bool { return &value }

var (
	scrapeConfigFilePattern = regexp.MustCompile(`^[^*]*(\*[^/]*)?$`)

	DefaultConfig = Config{GlobalConfig: DefaultGlobalConfig}

	DefaultGlobalConfig = GlobalConfig{
		ScrapeInterval:             model.Duration(time.Minute),
		ScrapeTimeout:              model.Duration(10 * time.Second),
		ScrapeNativeHistograms:     boolPtr(false),
		ExtraScrapeMetrics:         boolPtr(false),
		MetricNameValidationScheme: model.UTF8Validation,
		MetricNameEscapingScheme:   model.AllowUTF8,
	}

	DefaultScrapeConfig = ScrapeConfig{
		MetricsPath:       "/metrics",
		Scheme:            "http",
		HonorTimestamps:   true,
		HTTPClientConfig:  commonconfig.DefaultHTTPClientConfig,
		EnableCompression: true,
	}
)

type Config struct {
	GlobalConfig      GlobalConfig    `yaml:"global"`
	ScrapeConfigFiles []string        `yaml:"scrape_config_files,omitempty"`
	ScrapeConfigs     []*ScrapeConfig `yaml:"scrape_configs,omitempty"`

	loaded bool
}

func LoadScrapeConfig(data []byte, baseDir string) (*Config, error) {
	cfg := &Config{}
	*cfg = DefaultConfig
	if err := yaml.UnmarshalStrict(data, cfg); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg.SetDirectory(baseDir)
	cfg.loaded = true
	return cfg, nil
}

func (c *Config) ContainsScrapeConfigs() bool {
	return c != nil && (len(c.ScrapeConfigs) > 0 || len(c.ScrapeConfigFiles) > 0)
}

func (c *Config) SetDirectory(dir string) {
	c.GlobalConfig.SetDirectory(dir)
	for index, file := range c.ScrapeConfigFiles {
		c.ScrapeConfigFiles[index] = commonconfig.JoinDir(dir, file)
	}
	for _, scrapeConfig := range c.ScrapeConfigs {
		scrapeConfig.SetDirectory(dir)
	}
}

func (c *Config) GetScrapeConfigs() ([]*ScrapeConfig, error) {
	if !c.loaded {
		return nil, errors.New("scrape config cannot be fetched, main config was not validated and loaded correctly")
	}

	configs := append([]*ScrapeConfig(nil), c.ScrapeConfigs...)
	jobNames := make(map[string]string, len(configs))
	for _, scrapeConfig := range configs {
		jobNames[scrapeConfig.JobName] = "main config"
	}

	for _, pattern := range c.ScrapeConfigFiles {
		files, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("error retrieving scrape config files for %q: %w", pattern, err)
		}
		for _, filename := range files {
			content, err := os.ReadFile(filename)
			if err != nil {
				return nil, fileError(filename, err)
			}
			fileConfigs := ScrapeConfigs{}
			if err := yaml.UnmarshalStrict(content, &fileConfigs); err != nil {
				return nil, fileError(filename, err)
			}
			for _, scrapeConfig := range fileConfigs.ScrapeConfigs {
				if err := scrapeConfig.Validate(c.GlobalConfig); err != nil {
					return nil, fileError(filename, err)
				}
				if first, exists := jobNames[scrapeConfig.JobName]; exists {
					return nil, fileError(filename, fmt.Errorf("found multiple scrape configs with job name %q, first found in %s", scrapeConfig.JobName, first))
				}
				jobNames[scrapeConfig.JobName] = filename
				scrapeConfig.SetDirectory(filepath.Dir(filename))
				configs = append(configs, scrapeConfig)
			}
		}
	}
	return configs, nil
}

func (c *Config) Validate() error {
	if c.GlobalConfig.isZero() {
		c.GlobalConfig = DefaultGlobalConfig
	}
	for _, pattern := range c.ScrapeConfigFiles {
		if !scrapeConfigFilePattern.MatchString(pattern) {
			return fmt.Errorf("invalid scrape config file path %q", pattern)
		}
	}
	jobNames := make(map[string]struct{}, len(c.ScrapeConfigs))
	for _, scrapeConfig := range c.ScrapeConfigs {
		if err := scrapeConfig.Validate(c.GlobalConfig); err != nil {
			return err
		}
		if _, exists := jobNames[scrapeConfig.JobName]; exists {
			return fmt.Errorf("found multiple scrape configs with job name %q", scrapeConfig.JobName)
		}
		jobNames[scrapeConfig.JobName] = struct{}{}
	}
	return nil
}

func (c *Config) UnmarshalYAML(unmarshal func(any) error) error {
	*c = DefaultConfig
	type plain Config
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}
	if c.GlobalConfig.isZero() {
		c.GlobalConfig = DefaultGlobalConfig
	}
	return c.Validate()
}

type GlobalConfig struct {
	ScrapeInterval                 model.Duration         `yaml:"scrape_interval,omitempty"`
	ScrapeTimeout                  model.Duration         `yaml:"scrape_timeout,omitempty"`
	ScrapeProtocols                []ScrapeProtocol       `yaml:"scrape_protocols,omitempty"`
	ScrapeFailureLogFile           string                 `yaml:"scrape_failure_log_file,omitempty"`
	ExternalLabels                 labels.Labels          `yaml:"external_labels,omitempty"`
	BodySizeLimit                  units.Base2Bytes       `yaml:"body_size_limit,omitempty"`
	SampleLimit                    uint                   `yaml:"sample_limit,omitempty"`
	TargetLimit                    uint                   `yaml:"target_limit,omitempty"`
	LabelLimit                     uint                   `yaml:"label_limit,omitempty"`
	LabelNameLengthLimit           uint                   `yaml:"label_name_length_limit,omitempty"`
	LabelValueLengthLimit          uint                   `yaml:"label_value_length_limit,omitempty"`
	KeepDroppedTargets             uint                   `yaml:"keep_dropped_targets,omitempty"`
	MetricNameValidationScheme     model.ValidationScheme `yaml:"metric_name_validation_scheme,omitempty"`
	MetricNameEscapingScheme       string                 `yaml:"metric_name_escaping_scheme,omitempty"`
	ScrapeNativeHistograms         *bool                  `yaml:"scrape_native_histograms,omitempty"`
	ConvertClassicHistogramsToNHCB bool                   `yaml:"convert_classic_histograms_to_nhcb,omitempty"`
	AlwaysScrapeClassicHistograms  bool                   `yaml:"always_scrape_classic_histograms,omitempty"`
	ExtraScrapeMetrics             *bool                  `yaml:"extra_scrape_metrics,omitempty"`
}

func (c *GlobalConfig) SetDirectory(dir string) {
	c.ScrapeFailureLogFile = commonconfig.JoinDir(dir, c.ScrapeFailureLogFile)
}

func (c *GlobalConfig) UnmarshalYAML(unmarshal func(any) error) error {
	config := &GlobalConfig{}
	type plain GlobalConfig
	if err := unmarshal((*plain)(config)); err != nil {
		return err
	}
	if config.MetricNameValidationScheme != model.UTF8Validation && config.MetricNameValidationScheme != model.LegacyValidation {
		config.MetricNameValidationScheme = DefaultGlobalConfig.MetricNameValidationScheme
	}
	if err := config.ExternalLabels.Validate(func(label labels.Label) error {
		if !config.MetricNameValidationScheme.IsValidLabelName(label.Name) {
			return fmt.Errorf("%q is not a valid label name", label.Name)
		}
		if !model.LabelValue(label.Value).IsValid() {
			return fmt.Errorf("%q is not a valid label value", label.Value)
		}
		return nil
	}); err != nil {
		return err
	}
	if config.ScrapeInterval == 0 {
		config.ScrapeInterval = DefaultGlobalConfig.ScrapeInterval
	}
	if config.ScrapeTimeout > config.ScrapeInterval {
		return errors.New("global scrape timeout greater than scrape interval")
	}
	if config.ScrapeTimeout == 0 {
		config.ScrapeTimeout = min(DefaultGlobalConfig.ScrapeTimeout, config.ScrapeInterval)
	}
	if config.ScrapeNativeHistograms == nil {
		config.ScrapeNativeHistograms = DefaultGlobalConfig.ScrapeNativeHistograms
	}
	if config.ExtraScrapeMetrics == nil {
		config.ExtraScrapeMetrics = DefaultGlobalConfig.ExtraScrapeMetrics
	}
	if config.ScrapeProtocols != nil {
		if err := validateScrapeProtocols(config.ScrapeProtocols); err != nil {
			return fmt.Errorf("%w for global config", err)
		}
	}
	*c = *config
	return nil
}

func (c *GlobalConfig) isZero() bool {
	return c.ScrapeInterval == 0 && c.ScrapeTimeout == 0 && c.ScrapeProtocols == nil &&
		c.ScrapeFailureLogFile == "" && c.ExternalLabels.IsEmpty() && c.BodySizeLimit == 0 &&
		c.SampleLimit == 0 && c.TargetLimit == 0 && c.LabelLimit == 0 &&
		c.LabelNameLengthLimit == 0 && c.LabelValueLengthLimit == 0 && c.KeepDroppedTargets == 0 &&
		c.MetricNameValidationScheme == model.UnsetValidation && c.MetricNameEscapingScheme == "" &&
		c.ScrapeNativeHistograms == nil && !c.ConvertClassicHistogramsToNHCB &&
		!c.AlwaysScrapeClassicHistograms && c.ExtraScrapeMetrics == nil
}

type ScrapeProtocol string

var (
	PrometheusProto      ScrapeProtocol = "PrometheusProto"
	PrometheusText0_0_4  ScrapeProtocol = "PrometheusText0.0.4"
	PrometheusText1_0_0  ScrapeProtocol = "PrometheusText1.0.0"
	OpenMetricsText0_0_1 ScrapeProtocol = "OpenMetricsText0.0.1"
	OpenMetricsText1_0_0 ScrapeProtocol = "OpenMetricsText1.0.0"

	ScrapeProtocolsHeaders = map[ScrapeProtocol]string{
		PrometheusProto:      "application/vnd.google.protobuf;proto=io.prometheus.client.MetricFamily;encoding=delimited",
		PrometheusText0_0_4:  "text/plain;version=0.0.4",
		PrometheusText1_0_0:  "text/plain;version=1.0.0",
		OpenMetricsText0_0_1: "application/openmetrics-text;version=0.0.1",
		OpenMetricsText1_0_0: "application/openmetrics-text;version=1.0.0",
	}
	DefaultScrapeProtocols = []ScrapeProtocol{
		OpenMetricsText1_0_0, OpenMetricsText0_0_1, PrometheusText1_0_0, PrometheusText0_0_4,
	}
	DefaultProtoFirstScrapeProtocols = []ScrapeProtocol{
		PrometheusProto, OpenMetricsText1_0_0, OpenMetricsText0_0_1, PrometheusText1_0_0, PrometheusText0_0_4,
	}
)

func (protocol ScrapeProtocol) Validate() error {
	if _, exists := ScrapeProtocolsHeaders[protocol]; exists {
		return nil
	}
	supported := make([]string, 0, len(ScrapeProtocolsHeaders))
	for candidate := range ScrapeProtocolsHeaders {
		supported = append(supported, string(candidate))
	}
	sort.Strings(supported)
	return fmt.Errorf("unknown scrape protocol %v, supported: %v", protocol, supported)
}

func (protocol ScrapeProtocol) HeaderMediaType() string {
	header, exists := ScrapeProtocolsHeaders[protocol]
	if !exists {
		return ""
	}
	mediaType, _, err := mime.ParseMediaType(header)
	if err != nil {
		return ""
	}
	return mediaType
}

func ToEscapingScheme(scheme string, validationScheme model.ValidationScheme) (model.EscapingScheme, error) {
	if scheme == "" {
		switch validationScheme {
		case model.UTF8Validation:
			return model.NoEscaping, nil
		case model.LegacyValidation:
			return model.UnderscoreEscaping, nil
		case model.UnsetValidation:
			return model.NoEscaping, fmt.Errorf("ValidationScheme is unset: %s", validationScheme)
		default:
			panic(fmt.Errorf("unhandled validation scheme: %s", validationScheme))
		}
	}
	return model.ToEscapingScheme(scheme)
}

func CheckTargetAddress(address model.LabelValue) error {
	if strings.Contains(string(address), "/") {
		return fmt.Errorf("%q is not a valid hostname", address)
	}
	return nil
}

func validateScrapeProtocols(protocols []ScrapeProtocol) error {
	if len(protocols) == 0 {
		return errors.New("scrape_protocols cannot be empty")
	}
	seen := make(map[string]struct{}, len(protocols))
	for _, protocol := range protocols {
		key := strings.ToLower(string(protocol))
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicated protocol in scrape_protocols, got %v", protocols)
		}
		if err := protocol.Validate(); err != nil {
			return fmt.Errorf("scrape_protocols: %w", err)
		}
		seen[key] = struct{}{}
	}
	return nil
}

type ScrapeConfigs struct {
	ScrapeConfigs []*ScrapeConfig `yaml:"scrape_configs,omitempty"`
}

type ScrapeConfig struct {
	JobName                        string                 `yaml:"job_name"`
	HonorLabels                    bool                   `yaml:"honor_labels,omitempty"`
	HonorTimestamps                bool                   `yaml:"honor_timestamps"`
	TrackTimestampsStaleness       bool                   `yaml:"track_timestamps_staleness"`
	Params                         url.Values             `yaml:"params,omitempty"`
	ScrapeInterval                 model.Duration         `yaml:"scrape_interval,omitempty"`
	ScrapeTimeout                  model.Duration         `yaml:"scrape_timeout,omitempty"`
	ScrapeProtocols                []ScrapeProtocol       `yaml:"scrape_protocols,omitempty"`
	ScrapeFallbackProtocol         ScrapeProtocol         `yaml:"fallback_scrape_protocol,omitempty"`
	ScrapeNativeHistograms         *bool                  `yaml:"scrape_native_histograms,omitempty"`
	AlwaysScrapeClassicHistograms  *bool                  `yaml:"always_scrape_classic_histograms,omitempty"`
	ConvertClassicHistogramsToNHCB *bool                  `yaml:"convert_classic_histograms_to_nhcb,omitempty"`
	ScrapeFailureLogFile           string                 `yaml:"scrape_failure_log_file,omitempty"`
	MetricsPath                    string                 `yaml:"metrics_path,omitempty"`
	Scheme                         string                 `yaml:"scheme,omitempty"`
	EnableCompression              bool                   `yaml:"enable_compression"`
	BodySizeLimit                  units.Base2Bytes       `yaml:"body_size_limit,omitempty"`
	SampleLimit                    uint                   `yaml:"sample_limit,omitempty"`
	TargetLimit                    uint                   `yaml:"target_limit,omitempty"`
	LabelLimit                     uint                   `yaml:"label_limit,omitempty"`
	LabelNameLengthLimit           uint                   `yaml:"label_name_length_limit,omitempty"`
	LabelValueLengthLimit          uint                   `yaml:"label_value_length_limit,omitempty"`
	NativeHistogramBucketLimit     uint                   `yaml:"native_histogram_bucket_limit,omitempty"`
	NativeHistogramMinBucketFactor float64                `yaml:"native_histogram_min_bucket_factor,omitempty"`
	KeepDroppedTargets             uint                   `yaml:"keep_dropped_targets,omitempty"`
	MetricNameValidationScheme     model.ValidationScheme `yaml:"metric_name_validation_scheme,omitempty"`
	MetricNameEscapingScheme       string                 `yaml:"metric_name_escaping_scheme,omitempty"`
	ExtraScrapeMetrics             *bool                  `yaml:"extra_scrape_metrics,omitempty"`

	ServiceDiscoveryConfigs scrapediscovery.Configs       `yaml:"-"`
	HTTPClientConfig        commonconfig.HTTPClientConfig `yaml:",inline"`
	RelabelConfigs          []*relabel.Config             `yaml:"relabel_configs,omitempty"`
	MetricRelabelConfigs    []*relabel.Config             `yaml:"metric_relabel_configs,omitempty"`
}

func (c *ScrapeConfig) SetDirectory(dir string) {
	c.ServiceDiscoveryConfigs.SetDirectory(dir)
	c.HTTPClientConfig.SetDirectory(dir)
	c.ScrapeFailureLogFile = commonconfig.JoinDir(dir, c.ScrapeFailureLogFile)
}

func (c *ScrapeConfig) UnmarshalYAML(unmarshal func(any) error) error {
	*c = DefaultScrapeConfig
	if err := scrapediscovery.UnmarshalYAMLWithInlineConfigs(c, unmarshal); err != nil {
		return err
	}
	if c.JobName == "" {
		return errors.New("job_name is empty")
	}
	if err := c.HTTPClientConfig.Validate(); err != nil {
		return err
	}
	if len(c.RelabelConfigs) == 0 {
		if err := checkStaticTargets(c.ServiceDiscoveryConfigs); err != nil {
			return err
		}
	}
	for _, relabelConfig := range c.RelabelConfigs {
		if relabelConfig == nil {
			return errors.New("empty or null target relabeling rule in scrape config")
		}
	}
	for _, relabelConfig := range c.MetricRelabelConfigs {
		if relabelConfig == nil {
			return errors.New("empty or null metric relabeling rule in scrape config")
		}
	}
	return nil
}

func (c *ScrapeConfig) Validate(global GlobalConfig) error {
	if c == nil {
		return errors.New("empty or null scrape config section")
	}
	if c.ScrapeInterval == 0 {
		c.ScrapeInterval = global.ScrapeInterval
	}
	if c.ScrapeTimeout > c.ScrapeInterval {
		return fmt.Errorf("scrape timeout greater than scrape interval for scrape config with job name %q", c.JobName)
	}
	if c.ScrapeTimeout == 0 {
		c.ScrapeTimeout = min(global.ScrapeTimeout, c.ScrapeInterval)
	}
	if c.BodySizeLimit == 0 {
		c.BodySizeLimit = global.BodySizeLimit
	}
	if c.SampleLimit == 0 {
		c.SampleLimit = global.SampleLimit
	}
	if c.TargetLimit == 0 {
		c.TargetLimit = global.TargetLimit
	}
	if c.LabelLimit == 0 {
		c.LabelLimit = global.LabelLimit
	}
	if c.LabelNameLengthLimit == 0 {
		c.LabelNameLengthLimit = global.LabelNameLengthLimit
	}
	if c.LabelValueLengthLimit == 0 {
		c.LabelValueLengthLimit = global.LabelValueLengthLimit
	}
	if c.KeepDroppedTargets == 0 {
		c.KeepDroppedTargets = global.KeepDroppedTargets
	}
	if c.ScrapeFailureLogFile == "" {
		c.ScrapeFailureLogFile = global.ScrapeFailureLogFile
	}
	if c.ScrapeNativeHistograms == nil {
		c.ScrapeNativeHistograms = global.ScrapeNativeHistograms
	}
	if c.ExtraScrapeMetrics == nil {
		c.ExtraScrapeMetrics = global.ExtraScrapeMetrics
	}
	if c.ScrapeProtocols == nil {
		if global.ScrapeProtocols != nil {
			c.ScrapeProtocols = global.ScrapeProtocols
		} else if c.ScrapeNativeHistogramsEnabled() {
			c.ScrapeProtocols = DefaultProtoFirstScrapeProtocols
		} else {
			c.ScrapeProtocols = DefaultScrapeProtocols
		}
	}
	if err := validateScrapeProtocols(c.ScrapeProtocols); err != nil {
		return fmt.Errorf("%w for scrape config with job name %q", err, c.JobName)
	}
	if c.ScrapeFallbackProtocol != "" {
		if err := c.ScrapeFallbackProtocol.Validate(); err != nil {
			return fmt.Errorf("invalid fallback_scrape_protocol for scrape config with job name %q: %w", c.JobName, err)
		}
	}
	if global.MetricNameValidationScheme != model.LegacyValidation && global.MetricNameValidationScheme != model.UTF8Validation {
		return errors.New("global name validation method must be set")
	}
	localValidationUnset := c.MetricNameValidationScheme == model.UnsetValidation
	if localValidationUnset {
		c.MetricNameValidationScheme = global.MetricNameValidationScheme
	} else if c.MetricNameValidationScheme != model.LegacyValidation && c.MetricNameValidationScheme != model.UTF8Validation {
		return fmt.Errorf("unknown scrape config name validation method %q", c.MetricNameValidationScheme)
	}
	globalEscaping := global.MetricNameEscapingScheme
	if globalEscaping == "" {
		if global.MetricNameValidationScheme == model.LegacyValidation {
			globalEscaping = model.EscapeUnderscores
		} else {
			globalEscaping = model.AllowUTF8
		}
	}
	if c.MetricNameEscapingScheme == "" {
		if localValidationUnset {
			c.MetricNameEscapingScheme = globalEscaping
		} else if c.MetricNameValidationScheme == model.LegacyValidation {
			c.MetricNameEscapingScheme = model.EscapeUnderscores
		} else {
			c.MetricNameEscapingScheme = model.AllowUTF8
		}
	}
	switch c.MetricNameEscapingScheme {
	case model.AllowUTF8:
		if c.MetricNameValidationScheme != model.UTF8Validation {
			return errors.New("utf8 metric names requested but validation scheme is not set to UTF8")
		}
	case model.EscapeUnderscores, model.EscapeDots, model.EscapeValues:
	default:
		return fmt.Errorf("unknown scrape config name escaping method %q", c.MetricNameEscapingScheme)
	}
	if c.ConvertClassicHistogramsToNHCB == nil {
		value := global.ConvertClassicHistogramsToNHCB
		c.ConvertClassicHistogramsToNHCB = &value
	}
	if c.AlwaysScrapeClassicHistograms == nil {
		value := global.AlwaysScrapeClassicHistograms
		c.AlwaysScrapeClassicHistograms = &value
	}
	for _, relabelConfig := range c.RelabelConfigs {
		if err := relabelConfig.Validate(c.MetricNameValidationScheme); err != nil {
			return err
		}
	}
	for _, relabelConfig := range c.MetricRelabelConfigs {
		if err := relabelConfig.Validate(c.MetricNameValidationScheme); err != nil {
			return err
		}
	}
	return nil
}

func (c *ScrapeConfig) MarshalYAML() (any, error) {
	return scrapediscovery.MarshalYAMLWithInlineConfigs(c)
}

func (c *ScrapeConfig) ScrapeNativeHistogramsEnabled() bool {
	return c.ScrapeNativeHistograms != nil && *c.ScrapeNativeHistograms
}

func (c *ScrapeConfig) ConvertClassicHistogramsToNHCBEnabled() bool {
	return c.ConvertClassicHistogramsToNHCB != nil && *c.ConvertClassicHistogramsToNHCB
}

func (c *ScrapeConfig) AlwaysScrapeClassicHistogramsEnabled() bool {
	return c.AlwaysScrapeClassicHistograms != nil && *c.AlwaysScrapeClassicHistograms
}

func (c *ScrapeConfig) ExtraScrapeMetricsEnabled() bool {
	return c.ExtraScrapeMetrics != nil && *c.ExtraScrapeMetrics
}

func checkStaticTargets(configs scrapediscovery.Configs) error {
	for _, discoveryConfig := range configs {
		staticConfig, ok := discoveryConfig.(scrapediscovery.StaticConfig)
		if !ok {
			continue
		}
		for _, group := range staticConfig {
			for _, target := range group.Targets {
				address := target[model.AddressLabel]
				if strings.Contains(string(address), "/") {
					return fmt.Errorf("%q is not a valid hostname", address)
				}
			}
		}
	}
	return nil
}

func fileError(filename string, err error) error {
	absolute, absoluteErr := filepath.Abs(filename)
	if absoluteErr == nil {
		filename = absolute
	}
	return fmt.Errorf("%q: %w", filename, err)
}
