# phrack-rss-go

Self-hosted RSS/Atom feed generator for [phrack.org](https://phrack.org).

Scrapes the live Phrack website, extracts article metadata, deduplicates items, persists them to an on-disk JSON store, and renders RSS 2.0 and Atom feeds served over HTTP.

## Quick start

```bash
go build ./cmd/phrack-rss

# One-shot scrape (smoke test / CI)
PHRACK_RSS_DATA_DIR=$(mktemp -d) ./phrack-rss generate

# Long-running server (scrape loop + HTTP on 127.0.0.1:58315)
./phrack-rss serve
```

## Configuration

All settings are via `PHRACK_RSS_*` environment variables:

| Variable | Description | Default |
|---|---|---|
| `PHRACK_RSS_DATA_DIR` | Data + feed file directory | `./data` |
| `PHRACK_RSS_LISTEN` | Bind address | `127.0.0.1` |
| `PHRACK_RSS_PORT` | TCP port | `58315` |
| `PHRACK_RSS_BASE_URL` | Upstream site to scrape | `https://phrack.org` |
| `PHRACK_RSS_INTERVAL` | Rescan interval (serve mode) | `1h` |
| `PHRACK_RSS_FEED_LENGTH` | Newest items in rendered feed | `20` |
| `PHRACK_RSS_MAX_ITEMS` | Capped stored items per source (0 = keep all) | `0` |
| `PHRACK_RSS_SOURCES` | Comma-separated provider IDs | `phrack` |
| `PHRACK_RSS_HTTP_TIMEOUT` | Per-request timeout | `30s` |
| `PHRACK_RSS_MIN_GAP` | Politeness gap between requests | `200ms` |
| `PHRACK_RSS_CONCURRENCY` | Parallel enrich fetches | `8` |
| `PHRACK_RSS_USER_AGENT` | Outbound User-Agent string | `phrack-rss/<version>` |

## HTTP routes

| Path | Description |
|---|---|
| `/healthz` | Health check (`200 ok`) |
| `/feed.xml` / `/feed.atom` | First enabled source's feed |
| `/feeds/<source>.xml` / `.atom` | Specific source's feed |
| `/` | Plain-text index of all feed URLs |

Non-existent feeds return `503 Retry-After: 300` until the first scrape finishes.

## Build

```bash
go build ./cmd/phrack-rss
```

Nix:

```bash
nix build
```

Container (Podman/Docker):

```bash
podman build -t phrack-rss -f Containerfile .
```

## Test

```bash
go test ./...
```

All tests are offline (httptest fixtures). No network or external services needed.

## Lint

```bash
golangci-lint run ./...
gosec ./...
go vet ./...
```

## Architecture

```
cmd/phrack-rss/        Entry point (CLI: serve / generate / version)
internal/
  config/              Env-var config with defaults
  feed/                RSS 2.0 + Atom rendering (gorilla/feeds)
  model/               Item type, sorting, normalization
  pipeline/            Discover → Enrich → Store → Render orchestration
  providers/           Source registry (constructor map)
  scraper/             Source interface + Phrack implementation
  server/              HTTP server, feed file serving
  store/               JSON-backed item store (atomic writes)
```

**Pipeline flow** (per source): `Discover` (cheap index-page links) → `Enrich` (per-item fetch, worker pool) → dedupe into store on canonical URL → render `<source>.xml` + `.atom` → atomic `data.json` save.

## License

AGPL-3.0
