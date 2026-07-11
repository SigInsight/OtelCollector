# Go 库发布方案分析

## 一、背景

目标：在 `https://github.com/SigInsight/OtelCollector.git` 发布库文件，使得
其他人可以像 `github.com/SigNoz/signoz/pkg` 那样，通过
`go get github.com/SigInsight/OtelCollector/<sub-package>` 引用本项目的子包
（connectors / exporter / processor / pkg 等），并最终在
[pkg.go.dev](https://pkg.go.dev) 上可见。

参考效果：<https://pkg.go.dev/github.com/SigNoz/signoz/pkg>

---

## 二、参考库 `github.com/SigNoz/signoz/pkg` 的发布方式分析

`github.com/SigNoz/signoz/pkg` 是**纯粹的 Go module 发布**，不是二进制发布：

```
$ go list -m -json github.com/SigNoz/signoz@latest
{
    "Path": "github.com/SigNoz/signoz",
    "Version": "v0.132.2",
    "Time": "2026-07-10T12:19:09Z",
    ...
}
```

它的发布机制是：

1. **module path = 仓库地址**：`go.mod` 里 `module github.com/SigNoz/signoz`，
   仓库托管在 `github.com/SigNoz/signoz`，两者完全一致。
2. **打 Git tag**：发布时打 `vX.Y.Z` 格式的 tag（如 `v0.132.2`），推送到 GitHub。
3. **Go module proxy 自动同步**：Go 官方 proxy（`proxy.golang.org`）会自动
   拉取 GitHub 上的 tag 并建立索引，无需任何 CI。
4. **pkg.go.dev 自动收录**：pkg.go.dev 从 module proxy 拉数据，一旦某个版本
   被任何人 `go get` 过，pkg.go.dev 就会展示该包。

**核心结论：Go 库发布不需要任何 CI / workflow，只需要"module path 正确 + 打
tag + 推到 GitHub"。** 这是 Go 生态的约定俗成机制。

---

## 三、本项目现状分析

### 3.1 根提交 `f5684cf` 里有什么

根提交 `f5684cf`（`Initial import from upstream`）里**没有 Go 库发布 CI**，
它包含的是：

| Workflow | 作用 | 类型 |
|---|---|---|
| `ci.yaml` | go-test / go-fmt / go-lint（调用 SigNoz 私有 `primus.workflows`） | 测试 |
| `build.yaml` | 打 tag 时构建 Docker 镜像（调用 SigNoz 私有 `primus.workflows`） | Docker 镜像 |
| `build-staging.yaml` | staging 部署（调用 SigNoz 私有 `primus.workflows`） | 部署 |
| `gor-signozotelcollector.yaml` | goreleaser 发布 Linux/macOS **二进制**到 GitHub Releases | 二进制 |
| `gor-signozschemamigrator.yaml` | goreleaser 发布 schema-migrator **二进制** | 二进制 |
| `postci.yaml` | PR label 管理 | 辅助 |

`gor-*.yaml` 是 goreleaser **二进制发布**（发布可执行文件 + tar.gz 到 GitHub
Releases），**不是 Go 库发布**。两者完全不同：

- **二进制发布**：产物是 `.tar.gz` 可执行文件，给用户下载运行。
- **Go 库发布**：产物是 Git tag，让用户 `go get` 拉取源码作为依赖。

**所以根提交里其实没有"针对 Go 进行库文件发布"的 CI**，用户记忆可能有偏差。
但这不影响我们实现目标 -- Go 库发布本来就不需要 CI。

### 3.2 当前 fork 的状态

- `0533e75` 提交删除了 `gor-*.yaml`、`postci.yaml`、`build-staging.yaml`，
  把 `ci.yaml` 和 `build.yaml` 改写为原生实现（不再依赖 SigNoz 私有
  `primus.workflows`）。
- 当前只保留：`ci.yaml`（test/fmt/lint）、`build.yaml`（GHCR Docker 镜像）、
  `build-staging.yaml`。
- `cmd/signozotelcollector/.goreleaser.yaml` 配置文件**还在**（只是 workflow
  被删了）。

### 3.3 致命问题：module path 与仓库地址不匹配 ⚠️

```
go.mod:       module github.com/SigNoz/signoz-otel-collector
GitHub 仓库:  github.com/SigInsight/OtelCollector
```

Go module 系统要求 `go.mod` 里的 module path **必须**与仓库地址一致，否则
`go get` 会报错。实测验证：

```
$ go get github.com/SigInsight/OtelCollector@v0.4.2
go: github.com/SigInsight/OtelCollector@v0.4.2 requires
    github.com/SigInsight/OtelCollector@v0.4.2: parsing go.mod:
        module declares its path as: github.com/SigNoz/signoz-otel-collector
        but was required as: github.com/SigInsight/OtelCollector
```

**当前所有现有 tag（v0.1.0 ~ v0.4.2）都用了旧的 module path，全部不可用。**

影响范围：`grep` 统计有 **302 处** `.go` 文件 import 了
`github.com/SigNoz/signoz-otel-collector/...`，全都需要批量替换。

### 3.4 现有 tag 与 GitHub 状态

本地 tag：`v0.1.0`、`v0.1.1`、`v0.2.0`、`v0.3.0`、`v0.4.0`、`v0.4.2`

GitHub 上已推送的 tag：`v0.1.1`、`v0.2.0`、`v0.3.0`、`v0.4.0`、`v0.4.2`
（`v0.1.0` 未推到 GitHub）。

**这些已发布的 tag 因为 module path 不匹配，`go get` 全部失败。**
module proxy 已经缓存了 v0.4.2 的（错误的）go.mod，需要处理。

---

## 四、实施方案

### 方案：修正 module path + 重新打 tag 发布

Go 库发布不需要写任何 CI，核心就两步：(1) 修正 module path；(2) 打新 tag。
可选地加一个 CI 来"触发" proxy 主动索引（加速 pkg.go.dev 收录）。

#### 第 1 步：批量替换 module path（必做）

把 `go.mod` 第一行改为：

```
module github.com/SigInsight/OtelCollector
```

然后批量替换所有 `.go` 文件里的 import：

```bash
# 1. 改 go.mod
sed -i 's|module github.com/SigNoz/signoz-otel-collector|module github.com/SigInsight/OtelCollector|' go.mod

# 2. 批量替换所有 .go 文件中的 import 路径
#    （仅替换 import 字符串，不影响注释/文档里的文字描述，如需连文档一起改可去掉 --include 过滤）
find . -type f -name "*.go" -exec \
  sed -i 's|github.com/SigNoz/signoz-otel-collector|github.com/SigInsight/OtelCollector|g' {} +

# 3. 检查是否还有遗漏（注意 go.sum 里可能有，但 go mod tidy 会重建）
grep -rn "github.com/SigNoz/signoz-otel-collector" --include="*.go" | wc -l

# 4. 重新整理依赖
go mod tidy

# 5. 验证编译 + 测试
go build ./...
go test ./...
```

**注意事项**：
- `go.sum` 里的旧 module path 条目 `go mod tidy` 会自动清理重建。
- `Makefile`、`Dockerfile`、goreleaser 配置里如果有引用 module path 也要
  一起改（`grep -rn "signoz-otel-collector"` 全仓搜一遍）。
- 建议用一条独立的 commit 完成，便于 review。

#### 第 2 步：打新 tag 发布（必做）

现有 tag 都作废（module path 不对），需要打新版本号。建议：

```bash
# 方案 A：用 v0.5.0 表示一个新起点（推荐，语义清晰）
git tag v0.5.0
git push github v0.5.0

# 方案 B：如果不想让别人误用旧 tag，可以删除 GitHub 上的旧 tag
# （但 proxy 可能已缓存，删除 GitHub tag 无法清除 proxy 缓存，
#  只能靠新版本号让用户用新的）
git push github --delete v0.1.1 v0.2.0 v0.3.0 v0.4.0 v0.4.2
```

**为什么不能复用旧 tag**：Git tag 是不可变的，强行改 tag 指向会让已拉取过
的用户校验失败；而且 Go module proxy 会用 `go.mod` 的 hash 做校验，改了
module path 的 tag 会被 proxy 拒绝（"verifying ...: checksum mismatch"）。
只能用新的版本号。

#### 第 3 步：触发 module proxy 索引（可选，加速收录）

打 tag 后，Go proxy 不会立刻收录，需要有人 `go get` 一次来触发。可以加一个
极简 CI 在 tag 推送时主动触发：

新建 `.github/workflows/go-publish.yaml`：

```yaml
name: go-publish

# Go module 发布不需要构建产物。这个 workflow 的唯一作用是：
# 打 tag 后主动请求 Go module proxy 索引该版本，加速 pkg.go.dev 收录。
# （只要有人 go get 一次，proxy 就会自动索引，所以这步是可选的加速手段。）

on:
  push:
    tags:
      - 'v[0-9]+.[0-9]+.[0-9]+'
      - 'v[0-9]+.[0-9]+.[0-9]+-rc.[0-9]+'

permissions:
  contents: read

jobs:
  trigger-proxy:
    runs-on: ubuntu-latest
    steps:
      - name: request module proxy to index this version
        run: |
          version="${GITHUB_REF#refs/tags/}"
          # 向 proxy.golang.org 发起索引请求
          # 这个接口只是"提示" proxy 去拉取，不保证立即完成
          curl -sSf -o /dev/null \
            "https://proxy.golang.org/github.com/SigInsight/OtelCollector/@v/${version}.info" \
            || echo "proxy index request failed (non-fatal, will be picked up on first go get)"
      - name: verify go get works
        run: |
          version="${GITHUB_REF#refs/tags/}"
          # 验证该版本可以被 go get 成功（module path 与仓库一致）
          curl -sSf -o /dev/null \
            "https://proxy.golang.org/github.com/SigInsight/OtelCollector/@v/${version}.mod" \
            && echo "module ${version} is now resolvable via proxy"
```

#### 第 4 步：验证发布

```bash
# 1. 本地验证 go get 能拉到新版本
cd /tmp && rm -rf verify && mkdir verify && cd verify
go mod init verify
go get github.com/SigInsight/OtelCollector@v0.5.0
# 应能成功，不再报 "module declares its path as ..." 错误

# 2. 确认子包可被引用
cat > main.go <<'EOF'
package main

import (
    _ "github.com/SigInsight/OtelCollector/connectors/signozmeterconnector"
)

func main() {}
EOF
go build ./...

# 3. 几分钟后在 pkg.go.dev 查看收录情况（首次访问会触发页面生成）：
#    https://pkg.go.dev/github.com/SigInsight/OtelCollector
```

---

## 五、关于旧 tag 的处理建议

| 选项 | 操作 | 优点 | 缺点 |
|---|---|---|---|
| A（推荐） | 保留旧 tag，只发新版本 v0.5.0+ | 简单、不破坏 git 历史 | pkg.go.dev 上会显示旧版本（但 go get 会失败） |
| B | 删除 GitHub 上旧 tag | 避免误导 | proxy 缓存无法清除，删除 tag 反而可能让已引用旧 tag 的构建报错 |
| C | 用新 module path 重打旧 tag 的内容 | — | **严重违规**，会破坏所有已拉取用户的校验，强烈不推荐 |

建议选 **A**：保留旧 tag，从 v0.5.0 起用正确的 module path。在 README 里注明
"v0.5.0 起为本仓库原生 module path，之前版本请使用上游
`github.com/SigNoz/signoz-otel-collector`"。

---

## 六、是否需要恢复 goreleaser 二进制发布？

根提交里的 `gor-*.yaml` 是**二进制发布**（goreleaser），与 Go 库发布是两码事。
当前 fork 已删除这些 workflow。是否恢复取决于是否需要给用户提供"下载即用的
可执行二进制"：

- **如果只要 Go 库**（`go get` 引用子包）：不需要恢复 goreleaser，完成上面
  四步即可。
- **如果还要二进制发布**：需恢复 `gor-signozotelcollector.yaml`，并把里面的
  `DOCKERHUB_USERNAME/TOKEN` 改为 GHCR（用 `GITHUB_TOKEN`），以及更新
  goreleaser 配置里的 `files: conf` 路径（当前 `conf/` 目录已合并进 `config/`，
  见提交 `2f58e4c`）。

---

## 七、执行清单（TL;DR）

- [ ] 1. 批量替换 module path：`go.mod` + 302 处 `.go` import + Makefile/Dockerfile
- [ ] 2. `go mod tidy` + `go build ./...` + `go test ./...` 验证
- [ ] 3. 提交一个独立的 commit："chore: rename module path to SigInsight/OtelCollector"
- [ ] 4. 打 tag `v0.5.0`，推到 GitHub：`git tag v0.5.0 && git push github v0.5.0`
- [ ] 5.（可选）添加 `.github/workflows/go-publish.yaml` 加速 proxy 索引
- [ ] 6. 本地 `go get github.com/SigInsight/OtelCollector@v0.5.0` 验证
- [ ] 7. 访问 <https://pkg.go.dev/github.com/SigInsight/OtelCollector> 确认收录
- [ ] 8. 在 README 注明 v0.5.0 起为本仓库原生 module path

**核心认知：Go 库发布不需要 CI，"module path 正确 + 打 tag 推 GitHub"即完成
发布。当前唯一阻塞是 module path 与仓库地址不匹配。**
