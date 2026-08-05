# SigInsight ClickHouse Schema 收敛：Collector 审核结论

状态：S1 Implemented（Collector 已实现并通过 ClickHouse 25.5.6 验证）
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

## 必须保留的生产写入链

| 能力 | Source | Target/consumer |
| --- | --- | --- |
| Trace 查询 | trace exporter | `span_index_v3` |
| Trace resource filter | trace exporter | `traces_v3_resource` |
| Trace field autocomplete | trace exporter | `span_attributes_keys`、`tag_attributes_v2` |
| Services operations | `span_index_v3` | `root_operations`、`sub_root_operations` -> `top_level_operations` |
| Trace Detail summary | `span_index_v3` | `trace_summary_mv` -> `trace_summary` |
| Service Map | `span_index_v3` | 3 个 dependency graph MVs -> `dependency_graph_minutes_v2` |
| Exceptions | trace exporter | `error_index_v2` |
| Logs 查询/resource filter | logs exporter | `logs_v2`、`logs_v2_resource` |
| Logs autocomplete | logs exporter | keys tables、`tag_attributes_v2` |
| Metrics | metrics exporter | `samples_v4`、`time_series_v4`、rollups、`metadata` |
| Meter | meter exporter | `samples`、`samples_agg_1d_mv` -> `samples_agg_1d` |

`root_operations` 和 `sub_root_operations` 是当前生产 MV，不是 legacy 对象。删除其中任一项都会使 Services operation 列表随时间失真。

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

1. 先在现有 schema 上实施死对象清理，验证现有 UI/API 不变。
2. 停止 SigInsight 和 Collector。
3. 在空 canonical 表上创建 DDL，不复制旧数据。
4. 同一发布单元切换 Collector INSERT、MVs 和 SigInsight reader/Catalog。
5. schema readiness 通过后启动服务并写入真实 OTLP fixtures。
6. 执行 Logs、Traces、Metrics Explorer、Service Map、Exceptions、Alert、Retention 和 Meter 验证。
7. 删除旧 versioned 对象；不保留运行时 fallback。

## Collector 测试门槛

1. `system.tables/system.columns` canonical allowlist fingerprint；
2. 旧对象、旧列、camelCase Trace alias 和业务 `_v2/_v3` 名称 denylist；
3. 每个 exporter 的 batch INSERT 列顺序、类型和默认值；
4. ClickHouse 25.5.6 真实 OTLP logs/traces/Gauge/Sum/Regular Histogram/meter 写入；
5. 所有 MVs 的 source、target、字段和结果行；
6. 删除 metadataexporter 后 keys/value metadata 仍正确产生；
7. TTL、partition、skip index、resource fingerprint；
8. 缺失表/列时 schema readiness 返回明确错误。

## S1 验证记录

2026-08-05 已通过：

- `go test ./...`；
- `golangci-lint --config .golangci.yml run --new-from-rev=HEAD --timeout=10m`，`0 issues`；
- `CLICKHOUSE_IMAGE=clickhouse/clickhouse-server:25.5.6 ./scripts/test-v1-baseline.sh`；
- fresh baseline、模拟旧安装升级、post-baseline migration 二次运行幂等；
- 旧表和普通 metrics ingestion timestamp 列 denylist；
- OTLP logs/traces/metrics 写入，以及 logs/traces/metrics 字段元数据 smoke check。

SigInsight 协作项：其静态或 OpAMP 生成的 Collector 配置不得再引用
`metadataexporter`。SigInsight reader/Catalog 必须继续只依赖各信号 exporter 维护的
keys/value metadata 表。本仓库不修改 SigInsight 代码。

后续 canonical rename/重建阶段开始前，Collector 和 SigInsight 两份文档必须保持同一个
对象矩阵、字段 allowlist 和 schema fingerprint。
