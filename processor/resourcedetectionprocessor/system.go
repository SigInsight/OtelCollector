package resourcedetectionprocessor

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"

	"github.com/Showmax/go-fqdn"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.uber.org/zap"
)

const systemDetectorType = "system"

type systemDetector struct {
	logger *zap.Logger
	cfg    SystemConfig
}

func newSystemDetector(settings processor.Settings, cfg *Config) (detector, error) {
	systemCfg := cfg.System
	if len(systemCfg.HostnameSources) == 0 {
		systemCfg.HostnameSources = []string{"dns", "os"}
	}
	return &systemDetector{logger: settings.Logger, cfg: systemCfg}, nil
}

func (d *systemDetector) Detect(ctx context.Context) (pcommon.Resource, string, error) {
	hostname, err := d.detectHostname()
	if err != nil {
		return pcommon.NewResource(), "", err
	}
	res := pcommon.NewResource()
	attrs := res.Attributes()
	cfg := d.cfg.ResourceAttributes
	putString(attrs, "host.name", hostname, cfg.HostName.Enabled)
	putString(attrs, "os.type", goOSToOSType(runtime.GOOS), cfg.OsType.Enabled)
	putString(attrs, "host.arch", goARCHToHostArch(runtime.GOARCH), cfg.HostArch.Enabled)

	info, err := host.InfoWithContext(ctx)
	if err != nil {
		return pcommon.NewResource(), "", fmt.Errorf("failed getting OS information: %w", err)
	}
	putString(attrs, "os.version", info.PlatformVersion, cfg.OsVersion.Enabled)
	putString(attrs, "os.name", info.Platform, cfg.OsName.Enabled)
	putString(attrs, "os.build.id", detectOSBuildID(info.KernelVersion), cfg.OsBuildID.Enabled)

	if cfg.HostID.Enabled {
		value, hostErr := resource.New(ctx, resource.WithHostID())
		if hostErr == nil {
			putOTelAttribute(attrs, value, string(semconv.HostIDKey), "host.id")
		} else {
			d.logger.Warn("failed to get host ID", zap.Error(hostErr))
		}
	}
	if cfg.OsDescription.Enabled {
		value, osErr := resource.New(ctx, resource.WithOSDescription())
		if osErr != nil {
			return pcommon.NewResource(), "", fmt.Errorf("failed getting OS description: %w", osErr)
		}
		putOTelAttribute(attrs, value, string(semconv.OSDescriptionKey), "os.description")
	}
	if cfg.HostIP.Enabled || cfg.HostMac.Enabled || cfg.HostInterface.Enabled {
		if err := putNetworkAttributes(attrs, cfg); err != nil {
			return pcommon.NewResource(), "", err
		}
	}
	if cpuAttributesEnabled(cfg) {
		cpuInfo, cpuErr := cpu.InfoWithContext(ctx)
		if cpuErr != nil {
			return pcommon.NewResource(), "", fmt.Errorf("failed getting host cpuinfo: %w", cpuErr)
		}
		if len(cpuInfo) > 0 {
			putCPUAttributes(attrs, cfg, cpuInfo[0])
		}
	}
	return res, semconv.SchemaURL, nil
}

func (d *systemDetector) detectHostname() (string, error) {
	var lastErr error
	for _, source := range d.cfg.HostnameSources {
		var value string
		switch source {
		case "os":
			value, lastErr = os.Hostname()
		case "dns":
			value, lastErr = fqdn.FqdnHostname()
		case "cname":
			var hostname string
			hostname, lastErr = os.Hostname()
			if lastErr == nil {
				value, lastErr = net.LookupCNAME(hostname)
				value = strings.TrimRight(value, ".")
			}
		case "lookup":
			value, lastErr = reverseLookupHostname()
		}
		if lastErr == nil {
			return value, nil
		}
		d.logger.Debug(lastErr.Error())
	}
	return "", errors.New("all hostname sources failed to get hostname")
}

func reverseLookupHostname() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", err
	}
	addresses, err := net.LookupHost(hostname)
	if err != nil {
		return "", err
	}
	for _, address := range addresses {
		names, lookupErr := net.LookupAddr(address)
		if lookupErr == nil && len(names) > 0 {
			return strings.TrimRight(names[0], "."), nil
		}
		err = lookupErr
	}
	return "", err
}

func putString(attrs pcommon.Map, key, value string, enabled bool) {
	if enabled {
		attrs.PutStr(key, value)
	}
}

func putOTelAttribute(attrs pcommon.Map, res *resource.Resource, source, target string) {
	iterator := res.Iter()
	for iterator.Next() {
		attr := iterator.Attribute()
		if attr.Key == attribute.Key(source) {
			attrs.PutStr(target, attr.Value.Emit())
			return
		}
	}
}

func putNetworkAttributes(attrs pcommon.Map, cfg ResourceAttributesConfig) error {
	interfaces, err := net.Interfaces()
	if err != nil {
		return err
	}
	var ips, macs, names []any
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		names = append(names, iface.Name)
		macs = append(macs, strings.ToUpper(strings.ReplaceAll(iface.HardwareAddr.String(), ":", "-")))
		addresses, addressErr := iface.Addrs()
		if addressErr != nil {
			return fmt.Errorf("failed to get addresses for interface %v: %w", iface, addressErr)
		}
		for _, address := range addresses {
			ip, _, parseErr := net.ParseCIDR(address.String())
			if parseErr != nil {
				return fmt.Errorf("failed to parse address %q from interface %v: %w", address, iface, parseErr)
			}
			if !ip.IsLoopback() {
				ips = append(ips, ip.String())
			}
		}
	}
	if cfg.HostIP.Enabled {
		if err := attrs.PutEmptySlice("host.ip").FromRaw(ips); err != nil {
			return err
		}
	}
	if cfg.HostMac.Enabled {
		if err := attrs.PutEmptySlice("host.mac").FromRaw(macs); err != nil {
			return err
		}
	}
	if cfg.HostInterface.Enabled {
		if err := attrs.PutEmptySlice("host.interface").FromRaw(names); err != nil {
			return err
		}
	}
	return nil
}

func cpuAttributesEnabled(cfg ResourceAttributesConfig) bool {
	return cfg.HostCPUCacheL2Size.Enabled || cfg.HostCPUFamily.Enabled || cfg.HostCPUModelID.Enabled ||
		cfg.HostCPUModelName.Enabled || cfg.HostCPUStepping.Enabled || cfg.HostCPUVendorID.Enabled
}

func putCPUAttributes(attrs pcommon.Map, cfg ResourceAttributesConfig, info cpu.InfoStat) {
	putString(attrs, "host.cpu.vendor.id", info.VendorID, cfg.HostCPUVendorID.Enabled)
	putString(attrs, "host.cpu.family", info.Family, cfg.HostCPUFamily.Enabled)
	putString(attrs, "host.cpu.model.id", info.Model, cfg.HostCPUModelID.Enabled && info.Model != "")
	putString(attrs, "host.cpu.model.name", info.ModelName, cfg.HostCPUModelName.Enabled)
	putString(attrs, "host.cpu.stepping", fmt.Sprintf("%d", info.Stepping), cfg.HostCPUStepping.Enabled)
	if cfg.HostCPUCacheL2Size.Enabled {
		attrs.PutInt("host.cpu.cache.l2.size", int64(info.CacheSize))
	}
}

func detectOSBuildID(fallback string) string {
	switch runtime.GOOS {
	case "darwin":
		data, err := os.ReadFile("/System/Library/CoreServices/SystemVersion.plist")
		if err == nil {
			if value := parsePlistValue(data, "ProductBuildVersion"); value != "" {
				return value
			}
		}
	case "linux":
		data, err := os.ReadFile("/etc/os-release")
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "BUILD_ID=") {
					return strings.Trim(strings.TrimPrefix(line, "BUILD_ID="), `"`)
				}
			}
		}
	}
	return fallback
}

func parsePlistValue(data []byte, key string) string {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var current string
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		if start, ok := token.(xml.StartElement); ok {
			if start.Name.Local == "key" {
				_ = decoder.DecodeElement(&current, &start)
			}
			if start.Name.Local == "string" && current == key {
				var value string
				_ = decoder.DecodeElement(&value, &start)
				return value
			}
		}
	}
}

func goOSToOSType(value string) string {
	if value == "dragonfly" {
		return "dragonflybsd"
	}
	if value == "zos" {
		return "z_os"
	}
	return value
}

func goARCHToHostArch(value string) string {
	switch value {
	case "arm":
		return "arm32"
	case "ppc64le":
		return "ppc64"
	case "386":
		return "x86"
	default:
		return value
	}
}
