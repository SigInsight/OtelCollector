# SigInsight ClickHouse Schema 收敛：Collector 审核结论

状态：S2 Implemented（Collector canonical schema 已实现并通过 ClickHouse 25.5.6 验证）
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
S1 已同步更新 INSERT 模板和 batch record；冻结 baseline fingerprint 保持不变。

### S1 迁移后不再存在

- `span_attributes`、`samples_v2`、`time_series_v2`；
- 所有 `distributed_*`；
- `column_evolution_metadata`、`updated_metadata`；
- `body_v2`、`body_promoted`、JSON path metadata 及相关索引。

冻结的 v1 baseline DDL 不回写。新安装会先创建 v1 baseline，再由 post-baseline
异步迁移收敛；已有安装运行同一组幂等迁移。这样可以保持 baseline fingerprint
和已发布 migration 历史稳定。

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

## S2 canonical schema

S2 不使用 `RENAME TABLE`。ClickHouse 25.5.6 实测表明，重命名 source/target
不会重写 Materialized View 的定义；后续向新表名写入时，旧 MV 链不会被触发。
因此 sync migration 从冻结 v1 DDL 派生并创建空 canonical 表和重写后的 MV，
Collector/SigInsight 在停服发布窗口一起切换，async migration 最后删除旧对象。
旧数据不复制、不回填，也没有双写或运行时 fallback。

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

### S2 migration IDs

| Database | Sync create | Async cleanup |
| --- | --- | --- |
| `siginsight_traces` | `2002` | `2003` |
| `siginsight_metrics` | `2003` | `2004` |
| `siginsight_logs` | `2004` | `2005` |
| `siginsight_analytics` | `2001` | `2002` |
| `siginsight_meter` | `2001` | `2002` |

Sync migration 只依赖同库更早的 sync migration。Async migration 依赖同库所有 ID
更小的 sync/async migration。该规则允许后续发布增加高 ID sync migration，而不会被
先前发布的低 ID async cleanup 错误阻塞，同时保证删除旧表前 canonical 表已创建。

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

1. 停止 SigInsight 和 Collector，阻止旧 writer 在切换窗口继续写入。
2. 执行 sync migration，创建空 canonical 表及完整 MV 链；不复制旧数据。
3. 部署同一发布单元中的新 Collector writer 与 SigInsight reader/Catalog。
4. schema readiness 通过后启动服务并写入真实 OTLP fixtures。
5. 执行 Logs、Traces、Metrics Explorer、Service Map、Exceptions、Alert、Retention 和 Meter 验证。
6. 执行 async migration 删除旧 versioned 对象；不保留运行时 fallback。

不能在旧 SigInsight reader 仍运行时执行 async cleanup，也不能让新 Collector writer 在
sync migration 前启动。部署编排必须把这两条作为硬性前置条件。

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

2026-08-05 S1、S2 已通过：

- `go test ./...`；
- `golangci-lint --config .golangci.yml run --new-from-rev=HEAD --timeout=10m`，`0 issues`；
- `CLICKHOUSE_IMAGE=clickhouse/clickhouse-server:25.5.6 ./scripts/test-v1-baseline.sh`；
- fresh baseline、模拟旧安装升级、sync/async migration 二次运行幂等；
- canonical 表 allowlist、旧 versioned 表和普通 metrics ingestion timestamp 列 denylist；
- OTLP logs/traces/metrics 写入，以及 logs/traces/metrics 字段元数据 smoke check；
- Trace operations/summary、Service Map DB edge、Metrics rollup/series、Meter rollup 的完整 Materialized View 级联。

SigInsight 协作项：其静态或 OpAMP 生成的 Collector 配置不得再引用
`metadataexporter`。SigInsight reader/Catalog 必须按上方同步清单切换，并继续只依赖各信号
exporter 维护的 keys/value metadata 表。本仓库不修改 SigInsight 代码。
