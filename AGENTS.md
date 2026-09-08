# AGENTS.md

Self-hosted RSS/Atom feed generator for phrack.org. Go module `github.com/Vuks69/phrack-rss-go`, single binary at `cmd/phrack-rss`. No Makefile, no CI, no README — the package doc comments in `main.go`, `scraper/source.go`, and `internal/pipeline/pipeline.go` are the spec.

## Commands and environment

- Dev env comes from `flake.nix` (direnv `.envrc` = `use flake`). Ships `golangci-lint`, `gosec`, `staticcheck`, `govulncheck`, `pre-commit`. Use `nix develop` if direnv isn't loaded.
- `go test ./...` — all tests are offline (httptest fixtures; the phrack scraper tests fake the site). No network or services needed.
- Lint: `golangci-lint run ./...` and `gosec ./...` (upstream expects both; the `#nosec` annotations in `store.go`, `internal/server/server.go`, and `internal/scraper/phrack/phrack.go` only matter under gosec). `go vet ./...` is clean.
- Build one-shots: `go build ./cmd/phrack-rss`. For a real run use `PHRACK_RSS_DATA_DIR=$(mktemp -d) go run ./cmd/phrack-rss generate`.
- Do not touch the `result` symlink (flake build output; gitignored).

## Architecture

- Pipeline flow (`internal/pipeline`): for each enabled source run `Discover` → `Enrich` (only URLs not already in the store; fixed worker pool of `Concurrency`, per-item errors are non-fatal) → dedupe into store on canonical URL → render `<source>.xml` + `.atom` into the data dir. Store `data.json` is written atomically (tmp + rename).
- `scraper.Source` (`internal/scraper/source.go`): `Discover` must be cheap (index links only, never per-item fetches); `Enrich` does the expensive per-item fetch. New sites: implement `Source` and register a constructor in the `registry` map in `internal/providers/providers.go` — the map key MUST equal the returned `Source.ID()`.
- Config is `PHRACK_RSS_*` env vars with defaults in `internal/config/config.go` (see `usage()` in `main.go` for the full list). The server binds loopback only (default `127.0.0.1:58315`) by design.
- HTTP/feed routes (`internal/server`): `/feed.xml`/`.atom` map to the FIRST source in `cfg.Sources`; `/feeds/<source>.xml` to others. Non-existent feeds return `503 Retry-After` (not 404) until the first scrape finishes.
- An item is persisted/rendered only if both `URL` and `IssuedAt` are set (`model.Item.Usable()`).

## Gotchas

- The scraper throttles requests (`MinGap` politeness gap) and retries 429/5xx with backoff — it hits the live phrack.org site. For scraper tests set `cfg.MinGap = 0` and point `BaseURL` at an httptest server (see `internal/scraper/phrack/phrack_test.go`).
- `config.Version` (`internal/config/config.go`) is the **single** source of truth for the version string in the binary (`phrack-rss version`, User-Agent). No ldflags injection. The `version` attribute in `flake.nix` is only nix package metadata (store path), not the binary's version — bump it separately if/when it matters.
- go.mod requires a very new Go (`go 1.26`); the flake pins nixpkgs providing it. Don't downgrade the toolchain line.