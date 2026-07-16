package resourcedetectionprocessor

import (
	"fmt"
	"time"

	"go.opentelemetry.io/collector/config/confighttp"
)

type Config struct {
	Detectors               []string     `mapstructure:"detectors"`
	Override                bool         `mapstructure:"override"`
	System                  SystemConfig `mapstructure:"system"`
	confighttp.ClientConfig `mapstructure:",squash"`
	RefreshInterval         time.Duration `mapstructure:"refresh_interval"`
}

type SystemConfig struct {
	HostnameSources    []string                 `mapstructure:"hostname_sources"`
	ResourceAttributes ResourceAttributesConfig `mapstructure:"resource_attributes"`
}

type ResourceAttributeConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

type ResourceAttributesConfig struct {
	HostArch           ResourceAttributeConfig `mapstructure:"host.arch"`
	HostCPUCacheL2Size ResourceAttributeConfig `mapstructure:"host.cpu.cache.l2.size"`
	HostCPUFamily      ResourceAttributeConfig `mapstructure:"host.cpu.family"`
	HostCPUModelID     ResourceAttributeConfig `mapstructure:"host.cpu.model.id"`
	HostCPUModelName   ResourceAttributeConfig `mapstructure:"host.cpu.model.name"`
	HostCPUStepping    ResourceAttributeConfig `mapstructure:"host.cpu.stepping"`
	HostCPUVendorID    ResourceAttributeConfig `mapstructure:"host.cpu.vendor.id"`
	HostID             ResourceAttributeConfig `mapstructure:"host.id"`
	HostInterface      ResourceAttributeConfig `mapstructure:"host.interface"`
	HostIP             ResourceAttributeConfig `mapstructure:"host.ip"`
	HostMac            ResourceAttributeConfig `mapstructure:"host.mac"`
	HostName           ResourceAttributeConfig `mapstructure:"host.name"`
	OsBuildID          ResourceAttributeConfig `mapstructure:"os.build.id"`
	OsDescription      ResourceAttributeConfig `mapstructure:"os.description"`
	OsName             ResourceAttributeConfig `mapstructure:"os.name"`
	OsType             ResourceAttributeConfig `mapstructure:"os.type"`
	OsVersion          ResourceAttributeConfig `mapstructure:"os.version"`
}

func defaultResourceAttributesConfig() ResourceAttributesConfig {
	return ResourceAttributesConfig{
		HostName: ResourceAttributeConfig{Enabled: true},
		OsType:   ResourceAttributeConfig{Enabled: true},
	}
}

func (cfg *Config) Validate() error {
	validSources := map[string]struct{}{"dns": {}, "os": {}, "cname": {}, "lookup": {}}
	for _, source := range cfg.System.HostnameSources {
		if _, ok := validSources[source]; !ok {
			return fmt.Errorf("hostname_sources contains invalid value: %q", source)
		}
	}
	return nil
}
