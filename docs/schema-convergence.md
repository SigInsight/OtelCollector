# SigInsight ClickHouse Schema 收敛：Collector 审核结论

状态：M16 Complete（direct canonical baseline 与跨仓库协作验证已通过）
日期：2026-08-05
ClickHouse：`clickhouse/clickhouse-server:25.5.6`

完整对象矩阵、列 allowlist 和实施顺序见 SigInsight：

`docs/architecture/lightweight-query-engine/schema-convergence.md`

## 约束

- 允许直接丢弃旧 ClickHouse 数据，不双写、不回填。
- 不降低当前 Logs、Traces、Metrics、Meter、Trace Detail、Service Map、Exceptions、基本 Alert、字段补全和 Retention 能力。
- 新业务表名和列名不使用 `_v2`、`_v3`；migration ID 和 schema fingerprint 表达版本。
- Collector writer、MVs、SigInsight reader/Catalog 必须在同一协作里程碑切换。
- 每项实现必须补 migration、exporter 和 ClickHouse 25.5.6 真实协作测试。

## 首批 Collector 清理

### 可删除对象

| 对象 | Collector 侧原因 | 同步项 |
| --- | --- | --- |
| `durationSort` | baseline-only，无 writer | bootstrap/DDL 测试 |
| `span_index_v2` | 当前 trace exporter 只写 v3 | bootstrap rename、历史兼容测试 |
| `legacy_spans` | baseline-only，无 writer | bootstrap rename |
| `usage_explorer`、`usage_explorer_mv` | MV 从不再写入的 `span_index_v2` 读取，目标表为空 | baseline、SigInsight Retention fixture |
| `attributes_metadata` | 当前前端不请求 related values | 删除 `metadataexporter` |

`metadataexporter` 的唯一持久化输出是 `siginsight_metadata.attributes_metadata`。Logs/Traces 的 field keys/value tables 由各自 exporter 写入，Metrics metadata 由 metrics exporter 写入，因此删除 metadata exporter 不等于删除字段补全。

### 可删除列

- `samples_v4.inserted_at_unix_milli`；
- `time_series_v4.inserted_at_unix_milli`。

两列不被最新 SigInsight 读取，也不参与 `ORDER BY`、TTL 或 ReplacingMergeTree version。
它们不进入 M16 的 `metric_points`、`metric_series` DDL 或 schema fingerprint。

### S1 迁移后不再存在

- `span_attributes`、`samples_v2`、`time_series_v2`；
- 所有 `distributed_*`；
- `column_evolution_metadata`、`updated_metadata`；
- `body_v2`、`body_promoted`、JSON path metadata 及相关索引。

冻结的历史 DDL 仅作为生成输入，不再作为可安装 schema。M16 新安装会按原依赖顺序
直接生成最终对象，随后以最终 fingerprint 写入 `999` marker；没有 post-baseline
create/drop/cleanup 链，也不接受旧数据升级。

## S1 实施结果

| Migration | 变更 |
| --- | --- |
| `siginsight_traces/2001` | 删除 `usage_explorer_mv`、`usage_explorer`、`durationSort`、`span_index_v2`、`legacy_spans` |
| `siginsight_metadata/2001` | 删除 `attributes_metadata` |
| `siginsight_metrics/2002` | 删除 `samples_v4`、`time_series_v4` 的 `inserted_at_unix_milli` |

- 删除完整 `metadataexporter` 组件及缓存实现，净减少约 2,800 行代码；
- 从默认、示例和轻量查询集成配置中删除 exporter 与 pipeline 引用；
- Metrics writer 的 samples/time-series INSERT 契约同步去除 ingestion timestamp；
- bootstrap 不再迁移已废弃的 v2/legacy trace 名称；仍保留当前 v3/error 表的名称兼容；
- `go mod tidy` 移除了 metadata exporter 独占的 Redis/cache 等依赖；
- baseline 分类以已完成的 `999` marker 作为冻结指纹曾验证成功的凭据，允许登记过的
  post-baseline migration 删除全部业务表；缺失、失败或未知 migration 仍拒绝自动恢复。

`exp_hist.inserted_at_unix_milli` 未在 S1 删除，因为 exponential histogram 仍是第二批
能力候选，不能把其写入契约与普通 Gauge/Sum/Histogram 一并改变。

## M16 direct canonical schema

S2 不使用 `RENAME TABLE`。ClickHouse 25.5.6 实测表明，重命名 source/target
不会重写 Materialized View 的定义；后续向新表名写入时，旧 MV 链不会被触发。
因此 baseline 从冻结 DDL 派生最终 canonical 表和重写后的 MV，并在第一次 `sync up`
直接创建它们。Collector/SigInsight 在停服发布窗口一起切换；`async up` 在 M16 没有
cleanup 工作。旧数据不复制、不回填，也没有双写或运行时 fallback。

Trace `spans` 同时删除所有 camelCase `ALIAS`，并将 `resource_string_service$$name`
等物化快速路径改为 `service_name`、`http_route`、`db_system`、`rpc_method`
及对应的 `*_present`。重写覆盖 `spans` 的 skip index 和所有 Trace MV 的
source 引用；旧 SQL 不再有 alias 回退通道。

| Database | 旧对象 | Canonical 对象 |
| --- | --- | --- |
| Analytics | `rule_state_history_v0` | `rule_state_history` |
| Logs | `logs_v2`、`logs_v2_resource`、`tag_attributes_v2` | `logs`、`resource_sets`、`field_values` |
| Traces | `span_index_v3`、`traces_v3_resource`、`tag_attributes_v2`、`error_index_v2` | `spans`、`resource_sets`、`field_values`、`exceptions` |
| Traces derived | `top_level_operations`、`root_operations`、`sub_root_operations`、`trace_summary_mv` | `operations`、`root_operations_mv`、`sub_root_operations_mv`、`trace_summary_from_spans_mv` |
| Service Map | `dependency_graph_minutes_v2` 及三个 `_v2` MV | `service_edges` 及三个 `service_edges_*_mv` |
| Metrics points | `samples_v4`、`samples_v4_agg_5m/30m` 及 MV | `metric_points`、`metric_rollup_5m/30m` 及 MV |
| Metrics series | `time_series_v4`、`time_series_v4_6hrs/1day/1week` 及 MV | `metric_series`、`metric_series_6h/1d/1w` 及 MV |
| Meter | `samples`、`samples_agg_1d` 及 MV | `meter_points`、`meter_rollup_1d` 及 MV |

### M16 migration marker

每个 telemetry database 只记录 canonical baseline marker `999`。未来 schema 变更从
`2000` 起登记于 post-baseline registry；M16 的 sync/async registry 均为空。

`999` 不是永久跳过 schema 校验的凭据。Collector 启动前的 `migrate sync check` 会重新
计算 runtime-stable fingerprint：表引擎、partition/sorting/primary key、物化视图定义、
列顺序/类型/codec 和 data-skipping index 必须完全匹配。表 TTL、storage policy 以及日志
`_retention_days*` 的默认表达式由 Retention API 合法修改，因此不进入运行时 fingerprint。
连接失败会重试，确定性的 schema drift 会立即失败。

## 必须保留的生产写入链

| 能力 | Source | Target/consumer |
| --- | --- | --- |
| Trace 查询 | trace exporter | `spans` |
| Trace resource filter | trace exporter | `resource_sets` |
| Trace field autocomplete | trace exporter | `span_attributes_keys`、`field_values` |
| Services operations | `spans` | `root_operations_mv`、`sub_root_operations_mv` -> `operations` |
| Trace Detail summary | `spans` | `trace_summary_from_spans_mv` -> `trace_summary` |
| Service Map | `spans` | 3 个 `service_edges_*_mv` -> `service_edges` |
| Exceptions | trace exporter | `exceptions` |
| Logs 查询/resource filter | logs exporter | `logs`、`resource_sets` |
| Logs autocomplete | logs exporter | keys tables、`field_values` |
| Metrics | metrics exporter | `metric_points`、`metric_series`、rollups、`metadata` |
| Meter | meter exporter | `meter_points`、`meter_rollup_1d_mv` -> `meter_rollup_1d` |

`root_operations_mv` 和 `sub_root_operations_mv` 是当前生产 MV。删除其中任一项都会使 Services operation 列表随时间失真。

## 第二批候选

- `exp_hist`：只有在配置关闭 exponential histogram、前端不再展示该类型、SigInsight 明确拒绝该 capability 后才能删除。
- Trace/Logs `resource` JSON：旧 field mapper 仍使用，必须先统一到 typed/resource maps。
- Metrics `env`：当前虽无直接查询，但来自 `deployment.environment` 且属于主键；先证明 fingerprint/resource identity 已覆盖语义。
- 多级 metric rollup：必须由真实长时间范围 workload 决定，不能只用小规模测试数据判断。
- Trace `links`：Trace Detail 正在读取，不能用 `parent_span_id` 直接替代 OTel Span Links。

## Canonical writer 目标

| Signal | Canonical 主对象 | 关键写入契约 |
| --- | --- | --- |
| Logs | `siginsight_logs.logs` | timestamp/id/body/severity、trace identity、typed attrs/resource、scope、retention |
| Traces | `siginsight_traces.spans` | trace/span/parent identity、duration/status、events/links、typed maps、materialized fast paths |
| Metrics | `siginsight_metrics.metric_points`、`metric_series` | Gauge/Sum/Regular Histogram、series identity、attrs/resource/scope、metadata |
| Meter | `siginsight_meter.meter_points` | identity、temporality、labels、timestamp、value |

辅助对象使用无版本名称，例如 `resource_sets`、`field_values`、`operations`、`trace_summary`、`service_edges`、`exceptions`、`metric_rollup_5m`。`_mv` 只描述 Materialized View 类型，可以使用。

## 发布顺序

1. 停止 SigInsight 和 Collector，丢弃原有 `siginsight_*` telemetry databases。
2. 执行 `bootstrap`、`sync up`、`async up`，从空实例直接创建最终 schema 及 MV 链。
3. 部署同一发布单元中的 Collector v2.0.1 与 SigInsight reader/Catalog。
4. schema readiness 通过后启动服务并写入真实 OTLP fixtures。
5. 执行 Logs、Traces、Metrics Explorer、Service Map、Exceptions、Alert、Retention 和 Meter 验证。

旧 reader/writer 不能连接 M16 schema；这是刻意的破坏性边界，而非可就地回滚的升级。

### SigInsight reader/Catalog 同步清单

SigInsight 必须在同一发布单元更新下列引用：

- Logs reader、字段 catalog、retention：`logs`、`resource_sets`、`field_values`；
- Traces/Trace Detail/Exceptions/Service Map：`spans`、`resource_sets`、`field_values`、
  `exceptions`、`operations`、`trace_summary`、`service_edges`；
- Metrics compiler、metadata/explorer、retention：`metric_points`、`metric_series`、
  `metric_rollup_5m`、`metric_rollup_30m`、`metric_series_6h/1d/1w`、`metadata`；
- Rule history 与 Meter：`rule_state_history`、`meter_points`、`meter_rollup_1d`。

配置、SQL fixture 和 schema catalog 中不得继续引用旧 versioned 名称。冻结 migration DDL、
bootstrap 旧安装识别及 denylist 测试中的旧名称不属于运行时引用。

## Collector 测试门槛

1. `system.tables/system.columns` canonical allowlist fingerprint；
2. 旧对象、旧列、camelCase Trace alias 和业务 `_v2/_v3` 名称 denylist；
3. 每个 exporter 的 batch INSERT 列顺序、类型和默认值；
4. ClickHouse 25.5.6 真实 OTLP logs/traces/Gauge/Sum/Regular Histogram/meter 写入；
5. 所有 MVs 的 source、target、字段和结果行；
6. 删除 metadataexporter 后 keys/value metadata 仍正确产生；
7. TTL、partition、skip index、resource fingerprint；
8. 缺失表/列时 schema readiness 返回明确错误。

## 验证记录

已在 ClickHouse 25.5.6 上完成：

- `go test ./cmd/siginsightschemamigrator/schema_migrator ./cmd/siginsightotelcollector/migrate ./exporter/clickhouselogsexporter ./exporter/clickhousetracesexporter ./exporter/signozclickhousemetrics`；
- `./scripts/test-v1-baseline.sh`，验证 fresh direct baseline、sync/async 二次执行幂等、
  canonical 表/列 fingerprint、legacy object/DDL denylist、无 cluster/distributed 对象、真实
  OTLP logs/traces/metrics/meter 写入和完整 MV 级联；
- SigInsight 认证 API 协作测试 `compat/03_litequery_collector_collaboration.py`：当前 Collector
  写入后，V5 查询读取 Logs、Traces、Metrics、Meter；
- SigInsight 的完整复现脚本覆盖 collaboration、materialized catalog 与 retention，两次独立
  运行均为 `17 passed`（最新运行 `246.78s`）。

协作 query log 证明 Logs、Traces、Metrics 与 Meter 查询均命中各自的 canonical 表且返回非空
结果；Meter rollup 的异步触发会影响具体行数。没有 unknown table/column 或旧表读取。完整复现入口为
SigInsight 的 `tests/integration/scripts/run-canonical-schema-cutover.sh`。

SigInsight 的静态或 OpAMP 生成 Collector 配置不得引用 `metadataexporter`。reader/Catalog 已按
上方清单切换，只依赖各信号 exporter 维护的 keys/value metadata 表。
