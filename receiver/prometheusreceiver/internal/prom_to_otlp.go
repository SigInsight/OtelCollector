// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "github.com/SigInsight/OtelCollector/receiver/prometheusreceiver/internal"

import (
	"net"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"go.opentelemetry.io/collector/pdata/pcommon"
	conventions "go.opentelemetry.io/otel/semconv/v1.40.0"
)

// isDiscernibleHost checks if a host can be used as a value for the 'host.name' key.
// localhost-like hosts and unspecified (0.0.0.0) hosts are not discernible.
func isDiscernibleHost(host string) bool {
	ip := net.ParseIP(host)
	if ip != nil {
		// An IP is discernible if
		//  - it's not local (e.g. belongs to 127.0.0.0/8 or ::1/128) and
		//  - it's not unspecified (e.g. the 0.0.0.0 address).
		return !ip.IsLoopback() && !ip.IsUnspecified()
	}

	if host == "localhost" {
		return false
	}

	// not an IP, not 'localhost', assume it is discernible.
	return true
}

// CreateResource creates the resource data added to OTLP payloads.
func CreateResource(job, instance string, serviceDiscoveryLabels labels.Labels) pcommon.Resource {
	host, port, err := net.SplitHostPort(instance)
	if err != nil {
		host = instance
	}
	resource := pcommon.NewResource()
	attrs := resource.Attributes()
	attrs.PutStr(string(conventions.ServiceNameKey), job)
	if isDiscernibleHost(host) {
		attrs.PutStr(string(conventions.ServerAddressKey), host)
	}
	attrs.PutStr(string(conventions.ServiceInstanceIDKey), instance)
	attrs.PutStr(string(conventions.ServerPortKey), port)
	attrs.PutStr(string(conventions.URLSchemeKey), serviceDiscoveryLabels.Get(model.SchemeLabel))

	return resource
}
