module github.com/SigInsight/OtelCollector

go 1.25.12

require (
	github.com/ClickHouse/ch-go v0.66.0
	github.com/ClickHouse/clickhouse-go/v2 v2.36.0
	github.com/DATA-DOG/go-sqlmock v1.5.2
	github.com/Showmax/go-fqdn v1.0.0
	github.com/alecthomas/units v0.0.0-20240927000941-0f3dac36c52b
	github.com/bytedance/sonic v1.14.1
	github.com/cenkalti/backoff/v4 v4.3.0
	github.com/cenkalti/backoff/v5 v5.0.3
	github.com/expr-lang/expr v1.17.7
	github.com/go-redis/redismock/v9 v9.2.0
	github.com/goccy/go-json v0.10.5
	github.com/goccy/go-yaml v1.19.2
	github.com/gogo/protobuf v1.3.2
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da
	github.com/google/uuid v1.6.0
	github.com/gorilla/mux v1.8.1
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674
	github.com/hashicorp/golang-lru v1.0.2
	github.com/hashicorp/golang-lru/v2 v2.0.7
	github.com/jellydator/ttlcache/v3 v3.4.0
	github.com/knadh/koanf v1.5.0
	github.com/lightstep/go-expohisto v1.0.0
	github.com/oklog/ulid v1.3.1
	github.com/open-telemetry/opamp-go v0.22.0
	github.com/open-telemetry/opentelemetry-collector-contrib/extension/healthcheckextension v0.144.0
	github.com/open-telemetry/opentelemetry-collector-contrib/extension/pprofextension v0.144.0
	github.com/open-telemetry/opentelemetry-collector-contrib/pkg/pdatatest v0.152.0
	github.com/open-telemetry/opentelemetry-collector-contrib/pkg/pdatautil v0.152.0
	github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza v0.144.0
	github.com/open-telemetry/opentelemetry-collector-contrib/pkg/translator/prometheus v0.152.0
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/filterprocessor v0.144.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver v0.144.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.23.2
	github.com/prometheus/common v0.67.5
	github.com/prometheus/prometheus v0.311.4-0.20260507094802-91c184a899b8
	github.com/redis/go-redis/v9 v9.17.2
	github.com/segmentio/ksuid v1.0.4
	github.com/shirou/gopsutil/v4 v4.26.4
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.10
	github.com/spf13/viper v1.19.0
	github.com/srikanthccv/ClickHouse-go-mock v0.12.0
	github.com/stretchr/testify v1.11.1
	github.com/tidwall/gjson v1.18.0
	github.com/tilinna/clock v1.1.0
	github.com/vjeantet/grok v1.0.1
	go.opencensus.io v0.24.0
	go.opentelemetry.io/collector/component v1.58.0
	go.opentelemetry.io/collector/component/componentstatus v0.152.0
	go.opentelemetry.io/collector/component/componenttest v0.152.0
	go.opentelemetry.io/collector/config/configgrpc v0.152.0
	go.opentelemetry.io/collector/config/confighttp v0.152.0
	go.opentelemetry.io/collector/config/confignet v1.58.0
	go.opentelemetry.io/collector/config/configoptional v1.58.0
	go.opentelemetry.io/collector/config/configretry v1.58.0
	go.opentelemetry.io/collector/config/configtelemetry v0.152.0
	go.opentelemetry.io/collector/confmap v1.58.0
	go.opentelemetry.io/collector/confmap/provider/envprovider v1.50.0
	go.opentelemetry.io/collector/confmap/provider/fileprovider v1.58.0
	go.opentelemetry.io/collector/confmap/xconfmap v0.152.0
	go.opentelemetry.io/collector/connector v0.152.0
	go.opentelemetry.io/collector/connector/connectortest v0.152.0
	go.opentelemetry.io/collector/consumer v1.58.0
	go.opentelemetry.io/collector/consumer/consumertest v0.152.0
	go.opentelemetry.io/collector/consumer/xconsumer v0.152.0
	go.opentelemetry.io/collector/exporter v1.58.0
	go.opentelemetry.io/collector/exporter/debugexporter v0.144.0
	go.opentelemetry.io/collector/exporter/exporterhelper v0.152.0
	go.opentelemetry.io/collector/exporter/exportertest v0.152.0
	go.opentelemetry.io/collector/exporter/nopexporter v0.128.0
	go.opentelemetry.io/collector/exporter/otlpexporter v0.144.0
	go.opentelemetry.io/collector/extension v1.58.0
	go.opentelemetry.io/collector/extension/xextension v0.152.0
	go.opentelemetry.io/collector/extension/zpagesextension v0.152.0
	go.opentelemetry.io/collector/featuregate v1.58.0
	go.opentelemetry.io/collector/otelcol v0.152.0
	go.opentelemetry.io/collector/otelcol/otelcoltest v0.144.0
	go.opentelemetry.io/collector/pdata v1.58.0
	go.opentelemetry.io/collector/pdata/pprofile v0.152.0
	go.opentelemetry.io/collector/pipeline v1.58.0
	go.opentelemetry.io/collector/processor v1.58.0
	go.opentelemetry.io/collector/processor/batchprocessor v0.152.0
	go.opentelemetry.io/collector/processor/memorylimiterprocessor v0.144.0
	go.opentelemetry.io/collector/processor/processorhelper v0.152.0
	go.opentelemetry.io/collector/processor/processorhelper/xprocessorhelper v0.152.0
	go.opentelemetry.io/collector/processor/processortest v0.152.0
	go.opentelemetry.io/collector/processor/xprocessor v0.152.0
	go.opentelemetry.io/collector/receiver v1.58.0
	go.opentelemetry.io/collector/receiver/nopreceiver v0.128.0
	go.opentelemetry.io/collector/receiver/otlpreceiver v0.152.0
	go.opentelemetry.io/collector/receiver/receiverhelper v0.152.0
	go.opentelemetry.io/collector/receiver/receivertest v0.152.0
	go.opentelemetry.io/collector/semconv v0.128.1-0.20250610090210-188191247685
	go.opentelemetry.io/collector/service v0.152.0
	go.opentelemetry.io/contrib/instrumentation/net/http/httptrace/otelhttptrace v0.67.0
	go.opentelemetry.io/otel/trace v1.43.0
	go.uber.org/atomic v1.11.0
	go.uber.org/goleak v1.3.0
	go.uber.org/multierr v1.11.0
	go.uber.org/zap v1.28.0
	go.uber.org/zap/exp v0.3.0
	go.yaml.in/yaml/v2 v2.4.4
	google.golang.org/grpc v1.81.0
	google.golang.org/protobuf v1.36.11
	gopkg.in/yaml.v2 v2.4.0
)

require (
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/alecthomas/participle/v2 v2.1.4 // indirect
	github.com/antchfx/xmlquery v1.5.0 // indirect
	github.com/antchfx/xpath v1.3.6 // indirect
	github.com/aws/aws-sdk-go-v2 v1.41.7 // indirect
	github.com/aws/aws-sdk-go-v2/config v1.32.17 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.19.16 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.42.1 // indirect
	github.com/bytedance/gopkg v0.1.3 // indirect
	github.com/bytedance/sonic/loader v0.3.0 // indirect
	github.com/cloudwego/base64x v0.1.6 // indirect
	github.com/dennwc/varint v1.0.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/ebitengine/purego v0.10.0 // indirect
	github.com/elastic/go-grok v0.3.1 // indirect
	github.com/elastic/lunes v0.2.0 // indirect
	github.com/emicklei/go-restful/v3 v3.13.0 // indirect
	github.com/foxboron/go-tpm-keyfiles v0.0.0-20251226215517-609e4778396f // indirect
	github.com/fxamacker/cbor/v2 v2.9.1 // indirect
	github.com/go-faster/city v1.0.1 // indirect
	github.com/go-faster/errors v0.7.1 // indirect
	github.com/go-openapi/jsonpointer v0.22.5 // indirect
	github.com/go-openapi/jsonreference v0.21.5 // indirect
	github.com/go-openapi/swag v0.25.5 // indirect
	github.com/go-openapi/swag/cmdutils v0.25.5 // indirect
	github.com/go-openapi/swag/conv v0.25.5 // indirect
	github.com/go-openapi/swag/fileutils v0.25.5 // indirect
	github.com/go-openapi/swag/jsonname v0.25.5 // indirect
	github.com/go-openapi/swag/jsonutils v0.25.5 // indirect
	github.com/go-openapi/swag/loading v0.25.5 // indirect
	github.com/go-openapi/swag/mangling v0.25.5 // indirect
	github.com/go-openapi/swag/netutils v0.25.5 // indirect
	github.com/go-openapi/swag/stringutils v0.25.5 // indirect
	github.com/go-openapi/swag/typeutils v0.25.5 // indirect
	github.com/go-openapi/swag/yamlutils v0.25.5 // indirect
	github.com/go-openapi/testify/enable/yaml/v2 v2.4.1 // indirect
	github.com/go-openapi/testify/v2 v2.4.1 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/gobwas/glob v0.2.3 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/gnostic-models v0.7.1 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/googleapis/gax-go/v2 v2.21.0 // indirect
	github.com/grafana/regexp v0.0.0-20250905093917-f7b3be9d1853 // indirect
	github.com/hashicorp/go-version v1.9.0 // indirect
	github.com/iancoleman/strcase v0.3.0 // indirect
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/jpillora/backoff v1.0.0 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/knadh/koanf/v2 v2.3.4 // indirect
	github.com/leodido/go-syslog/v4 v4.3.0 // indirect
	github.com/magefile/mage v1.15.0 // indirect
	github.com/michel-laterman/proxy-connect-dialer-go v0.1.0 // indirect
	github.com/mitchellh/mapstructure v1.5.1-0.20231216201459-8508981c8b6c // indirect
	github.com/moby/moby/api v1.54.2 // indirect
	github.com/moby/moby/client v0.4.1 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/mwitkow/go-conntrack v0.0.0-20190716064945-2f068394615f // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/internal/common v0.152.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/internal/coreinternal v0.152.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/internal/filter v0.144.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/internal/gopsutilenv v0.144.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/internal/healthcheck v0.144.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/pkg/experimentalmetricmetadata v0.144.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl v0.144.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/pkg/status v0.144.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/pkg/winperfcounters v0.144.0 // indirect
	github.com/pelletier/go-toml v1.9.5 // indirect
	github.com/prometheus/otlptranslator v1.0.0 // indirect
	github.com/sagikazarmark/locafero v0.4.0 // indirect
	github.com/sagikazarmark/slog-shim v0.1.0 // indirect
	github.com/segmentio/asm v1.2.0 // indirect
	github.com/sourcegraph/conc v0.3.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/twmb/murmur3 v1.1.8 // indirect
	github.com/ua-parser/uap-go v0.0.0-20240611065828-3a4781585db6 // indirect
	github.com/valyala/fastjson v1.6.7 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/zeebo/xxh3 v1.0.2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/collector v0.152.0 // indirect
	go.opentelemetry.io/collector/client v1.58.0 // indirect
	go.opentelemetry.io/collector/config/configauth v1.58.0 // indirect
	go.opentelemetry.io/collector/config/configcompression v1.58.0 // indirect
	go.opentelemetry.io/collector/config/configmiddleware v1.58.0 // indirect
	go.opentelemetry.io/collector/config/configopaque v1.58.0 // indirect
	go.opentelemetry.io/collector/config/configtls v1.58.0 // indirect
	go.opentelemetry.io/collector/confmap/provider/httpprovider v1.50.0 // indirect
	go.opentelemetry.io/collector/confmap/provider/yamlprovider v1.50.0 // indirect
	go.opentelemetry.io/collector/connector/xconnector v0.152.0 // indirect
	go.opentelemetry.io/collector/consumer/consumererror v0.152.0 // indirect
	go.opentelemetry.io/collector/consumer/consumererror/xconsumererror v0.144.0 // indirect
	go.opentelemetry.io/collector/exporter/exporterhelper/xexporterhelper v0.144.0 // indirect
	go.opentelemetry.io/collector/exporter/xexporter v0.152.0 // indirect
	go.opentelemetry.io/collector/extension/extensionauth v1.58.0 // indirect
	go.opentelemetry.io/collector/extension/extensioncapabilities v0.152.0 // indirect
	go.opentelemetry.io/collector/extension/extensionmiddleware v0.152.0 // indirect
	go.opentelemetry.io/collector/extension/extensiontest v0.152.0 // indirect
	go.opentelemetry.io/collector/filter v0.144.0 // indirect
	go.opentelemetry.io/collector/internal/componentalias v0.152.0 // indirect
	go.opentelemetry.io/collector/internal/fanoutconsumer v0.152.0 // indirect
	go.opentelemetry.io/collector/internal/memorylimiter v0.144.0 // indirect
	go.opentelemetry.io/collector/internal/sharedcomponent v0.152.0 // indirect
	go.opentelemetry.io/collector/internal/telemetry v0.152.0 // indirect
	go.opentelemetry.io/collector/pdata/testdata v0.152.0 // indirect
	go.opentelemetry.io/collector/pdata/xpdata v0.152.0 // indirect
	go.opentelemetry.io/collector/pipeline/xpipeline v0.152.0 // indirect
	go.opentelemetry.io/collector/receiver/xreceiver v0.152.0 // indirect
	go.opentelemetry.io/collector/scraper v0.144.0 // indirect
	go.opentelemetry.io/collector/scraper/scraperhelper v0.144.0 // indirect
	go.opentelemetry.io/collector/service/hostcapabilities v0.152.0 // indirect
	go.opentelemetry.io/contrib/bridges/otelzap v0.18.0 // indirect
	go.opentelemetry.io/contrib/otelconf v0.23.0 // indirect
	go.opentelemetry.io/contrib/propagators/b3 v1.43.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc v0.19.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp v0.19.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc v1.43.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp v1.43.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.43.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.43.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.43.0 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdoutlog v0.19.0 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdoutmetric v1.43.0 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.43.0 // indirect
	go.opentelemetry.io/otel/log v0.19.0 // indirect
	go.opentelemetry.io/otel/sdk/log v0.19.0 // indirect
	go.opentelemetry.io/proto/otlp v1.10.0 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/arch v0.4.0 // indirect
	golang.org/x/exp v0.0.0-20260312153236-7ab1446f8b90 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/term v0.45.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	google.golang.org/api v0.274.0 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.13.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	k8s.io/api v0.35.4 // indirect
	k8s.io/apimachinery v0.35.4 // indirect
	k8s.io/client-go v0.35.4 // indirect
	k8s.io/klog/v2 v2.140.0 // indirect
	k8s.io/kube-openapi v0.0.0-20260330154417-16be699c7b31 // indirect
	k8s.io/utils v0.0.0-20260319190234-28399d86e0b5 // indirect
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.3.2 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)

require (
	github.com/andybalholm/brotli v1.2.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/go-kit/log v0.2.1
	github.com/go-logfmt/logfmt v0.6.1 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.28.0 // indirect
	github.com/hashicorp/hcl v1.0.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/json-iterator/go v1.1.13-0.20220915233716-71ac16282d12 // indirect
	github.com/klauspost/compress v1.18.6
	github.com/leodido/ragel-machinery v0.0.0-20190525184631-5f46317e436b // indirect
	github.com/lufia/plan9stats v0.0.0-20251013123823-9fd1530e3ec3 // indirect
	github.com/magiconair/properties v1.8.10 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/paulmach/orb v0.11.1 // indirect
	github.com/pelletier/go-toml/v2 v2.2.2 // indirect
	github.com/pierrec/lz4/v4 v4.1.26 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/power-devops/perfstat v0.0.0-20240221224432-82ca36839d55 // indirect
	github.com/prometheus/client_model v0.6.2
	github.com/prometheus/procfs v0.20.1 // indirect
	github.com/rs/cors v1.11.1 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/tklauser/go-sysconf v0.3.16 // indirect
	github.com/tklauser/numcpus v0.11.0 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	github.com/zeebo/assert v1.3.1
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.68.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.68.0
	go.opentelemetry.io/contrib/zpages v0.68.0 // indirect
	go.opentelemetry.io/otel v1.43.0
	go.opentelemetry.io/otel/exporters/prometheus v0.65.0 // indirect
	go.opentelemetry.io/otel/metric v1.43.0
	go.opentelemetry.io/otel/sdk v1.43.0
	go.opentelemetry.io/otel/sdk/metric v1.43.0
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sync v0.22.0
	golang.org/x/sys v0.47.0 // indirect
	gonum.org/v1/gonum v0.17.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260406210006-6f92a3bedf2d // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260406210006-6f92a3bedf2d // indirect
	gopkg.in/ini.v1 v1.67.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/ClickHouse/ch-go v0.66.0 => github.com/SigNoz/ch-go v0.66.0-dd-sketch
	github.com/ClickHouse/clickhouse-go/v2 v2.36.0 => github.com/SigNoz/clickhouse-go/v2 v2.36.0-dd-sketch
	github.com/segmentio/ksuid => github.com/signoz/ksuid v1.0.4
	github.com/vjeantet/grok => github.com/signoz/grok v1.0.3

	// using 0.23.0 as there is an issue with 0.24.0 stats that results in
	// an error
	// panic: interface conversion: interface {} is nil, not func(*tag.Map, []stats.Measurement, map[string]interface {})
	go.opencensus.io => go.opencensus.io v0.23.0
)

exclude github.com/StackExchange/wmi v1.2.0
