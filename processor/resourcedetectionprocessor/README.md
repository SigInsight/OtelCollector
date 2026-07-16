# Resource Detection Processor

This is a controlled local fork of the OpenTelemetry resource detection processor. It keeps the `resourcedetection` component type and supports traces, metrics, logs, and profiles.

Only the following detectors are registered:

- `env`: reads `OTEL_RESOURCE_ATTRIBUTES` and the deprecated `OTEL_RESOURCE` fallback.
- `system`: detects host and operating-system resource attributes.

Cloud and platform detectors are intentionally unavailable. A configuration containing an unsupported detector fails with `invalid detector key` instead of silently ignoring it.