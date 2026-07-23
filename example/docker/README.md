# Example deployment

Single-host docker-compose stack that runs SigInsight with **this fork's
`siginsight-otel-collector`** image (pulled from GHCR) on top of a single-node
single-node ClickHouse instance.

## Layout

```
example/docker/
├── docker-compose.yaml            this file's stack definition
├── .env.example                   tag overrides (OTELCOL_TAG, VERSION, ...)
└── common/                        configs mounted into containers
    ├── clickhouse/                config.xml, users.xml, cluster.xml, ...
    └── siginsight/
        └── otel-collector-opamp-config.yaml
```

The collector's bootstrap config is the repo's canonical
[`config/collector.yaml`](../../config/collector.yaml), mounted straight into
the container (see the `otel-collector` volume in `docker-compose.yaml`) — the
demo does not keep its own copy. Because the collector runs in OpAMP mode
(`--manager-config` + `--copy-path`), the SigInsight backend overrides this
bootstrap at runtime anyway.

The `common/` tree is a copy of the upstream
[`SigInsight/siginsight/deploy/docker/common`](https://github.com/SigInsight/siginsight/tree/main/deploy/docker/common)
directory and is committed as-is for convenience. Update it when the
upstream stack changes if you want to track new defaults.

## Quick start

```bash
cd example/docker
cp .env.example .env                      # set OTELCOL_TAG to the GHCR tag
# put your ClickHouse / opamp / collector configs in place
docker compose up -d
docker compose logs -f siginsight-otel-collector
```

Open the UI at <http://localhost:8080>.

## Pulling from GHCR

If the GHCR package is **private**:

```bash
echo "$GHCR_PAT" | docker login ghcr.io -u <username> --password-stdin
```

The PAT needs the `read:packages` scope. Mark the package public on
GitHub (Packages → Package settings → Change visibility) to skip login.

You can also point at a GHCR mirror by overriding the image in
`docker-compose.override.yaml`:

```yaml
services:
  otel-collector:
    image: mirror-ghcr-hvfd.onrender.com/siginsight/siginsight-otel-collector:staging-0533e75
  siginsight-telemetrystore-migrator:
    image: mirror-ghcr-hvfd.onrender.com/siginsight/siginsight-otel-collector:staging-0533e75
```

## Notes

- `siginsight-telemetrystore-migrator` and `siginsight-otel-collector` MUST use
  the same image tag — both bake the schema migration list into the
  binary, and a mismatch will make the collector's `migrate sync check`
  fail or, worse, write into a stale schema. The compose file uses the
  single `OTELCOL_TAG` variable for both.
- The amd64 image is the only one this fork's CI builds. On arm64 hosts
  you'll need to re-enable arm64 in `.github/workflows/build.yaml` and
  rebuild.
- Data volumes (`siginsight-clickhouse`, `siginsight-sqlite`)
  persist across `docker compose down`. Use `down -v` to wipe.
