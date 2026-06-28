# Example deployment

Single-host docker-compose stack that runs SigNoz with **this fork's
`signoz-otel-collector`** image (pulled from GHCR) on top of a single-node
ClickHouse cluster.

## Layout

```
example/docker/
├── docker-compose.yaml            this file's stack definition
├── .env.example                   tag overrides (OTELCOL_TAG, VERSION, ...)
├── otel-collector-config.yaml     receivers/processors/exporters for the collector
└── common/                        configs mounted into containers
    ├── clickhouse/                config.xml, users.xml, cluster.xml, ...
    └── signoz/
        └── otel-collector-opamp-config.yaml
```

The `common/` tree is a copy of the upstream
[`SigNoz/signoz/deploy/docker/common`](https://github.com/SigNoz/signoz/tree/main/deploy/docker/common)
directory and is committed as-is for convenience. Update it when the
upstream stack changes if you want to track new defaults.

## Quick start

```bash
cd example/docker
cp .env.example .env                      # set OTELCOL_TAG to the GHCR tag
# put your ClickHouse / opamp / collector configs in place
docker compose up -d
docker compose logs -f signoz-otel-collector
```

Open the UI at <http://localhost:8081>.

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
    image: mirror-ghcr-hvfd.onrender.com/forza0310/signoz-otel-collector:staging-0533e75
  signoz-telemetrystore-migrator:
    image: mirror-ghcr-hvfd.onrender.com/forza0310/signoz-otel-collector:staging-0533e75
```

## Notes

- `signoz-telemetrystore-migrator` and `signoz-otel-collector` MUST use
  the same image tag — both bake the schema migration list into the
  binary, and a mismatch will make the collector's `migrate sync check`
  fail or, worse, write into a stale schema. The compose file uses the
  single `OTELCOL_TAG` variable for both.
- The amd64 image is the only one this fork's CI builds. On arm64 hosts
  you'll need to re-enable arm64 in `.github/workflows/build.yaml` and
  rebuild.
- Data volumes (`signoz-clickhouse`, `signoz-sqlite`, `signoz-zookeeper-1`)
  persist across `docker compose down`. Use `down -v` to wipe.
