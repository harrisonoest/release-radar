# AGENTS.md — Release Radar

## Quick verify

```bash
/usr/local/go/bin/go build ./... && /usr/local/go/bin/go vet ./... && /usr/local/go/bin/go test ./...
```

`go` is at `/usr/local/go/bin/go` — not in `$PATH`. The Makefile assumes `go` is on PATH; if not, use the full path.

Tests live in a single file: `internal/auth/auth_test.go`. No integration tests exist — auth tests generate ephemeral ECDSA P-256 keys in `/tmp`.

## Architecture

```
cmd/{auth,init,scan,status,config}.go  → cobra commands, orchestration
internal/auth/                         → JWT signing (ES256), OAuth proxy, token cache
internal/scanner/                      → concurrent artist→album pipeline, date filtering
internal/playlist/                     → find/create playlist, add tracks with dedup
pkg/api/                               → Apple Music API client (wraps go-apple-music)
pkg/db/                                → SQLite store (modernc.org/sqlite, pure Go, no CGO)
pkg/config/                            → Viper/TOML config loading
```

`internal/cache/` is legacy — replaced by `pkg/db/` but not yet deleted.

## Key dependencies

| Package | Role |
|---------|------|
| `minchao/go-apple-music` | Apple Music REST API client (catalog + library) |
| `modernc.org/sqlite` | Embedded SQLite (WAL mode, single conn) |
| `golang-jwt/jwt/v5` | ES256 JWT signing for developer tokens |
| `vbauerster/mpb/v8` | Multi-progress bars (TTY-only — won't render in pipes) |
| `spf13/cobra` + `spf13/viper` | CLI + TOML config |

## Apple Music API gotchas

### Auth
- Two tokens required: **Developer Token** (ES256 JWT, signed with `.p8` key) and **Music User Token** (obtained via OAuth proxy).
- MusicKit JS v3: `MusicKit.configure()` is **async** — must `await` it before calling `getInstance()`.
- Tokens stored in SQLite `auth` table (not auth.json). Migration handles legacy JSON files.

### go-apple-music client quirks
- `Client.Do(ctx, req, v)` returns **(resp, err)** — for 4xx/5xx both may be non-nil. **Always check `resp.StatusCode` before `err`** when handling 404/429.
- Catalog IDs are **numeric strings** (e.g., `"1068300376"`). Library IDs are **prefixed** (e.g., `"r.xUjxaAb"`). Resolve library→catalog via `include=catalog`.
- `include=catalog` on deep pagination (>offset 6000) returns 400. Use page size ≤25.
- `Storefront.Id` field uses Go's `Id` not `ID` — the library predates Go naming conventions.

### Endpoints
- `GET /v1/me/library/artists?include=catalog` — fetches library artists with catalog relationships
- `GET /v1/catalog/{sf}/artists/{id}/albums` — artist albums (supports pagination, 404 = no albums)
- `GET /v1/me/library/playlists` — user playlists (for find/create)
- `POST /v1/me/library/playlists/{id}/tracks` — add tracks to playlist
- `GET /v1/me/storefront` — auto-detect user's storefront

## SQLite store

`pkg/db/store.go` — three tables: `artists`, `scan_state` (singleton), `auth` (singleton).

- `db.Open("")` opens `~/.config/release-radar/release-radar.db`. Empty string auto-resolves via `config.ConfigDir()`.
- **Auto-migration**: if artists table is empty on open, imports from legacy `artists.json`/`scan_state.json`/`auth.json` then removes the JSON files.
- **Single connection**: `MaxOpenConns=1` / `MaxIdleConns=1` (CLI is single-threaded, WAL handles concurrency if needed).
- DB path: `~/.config/release-radar/release-radar.db`

## Artist deduplication

`init` applies a two-pass filter after fetching library artists:

1. **Standalone detection**: artists without " & ", ", ", " X ", " feat. ", " ft. " in the name are "standalone"
2. **Standalone-only**: only catalog IDs with ≥1 standalone entry are kept
3. **Shortest-name wins**: when multiple library entries share a catalog ID, keep the shortest name

## Rate limiting

Exponential backoff in two places:
- `pkg/api/client.go` `GetAllLibraryArtists`: 3 retries, 0.5s/1s/1.5s delay on 5xx/429
- `internal/scanner/scanner.go` `checkArtist`: 5 retries, 1s/2s/4s/8s delay on 429

## Config

`~/.config/release-radar/config.toml` — TOML, loaded by Viper. Contains Apple Developer secrets (team_id, key_id, key_path). **Not committed to git.**

Config overrides: `--config PATH` global flag on every command. Scan-specific overrides: `--concurrency`, `--since`, `--limit-artists`.

## Running locally

```bash
make build              # compile
make test               # go test ./...
make lint               # vet + gofmt check
make install            # build + copy to ~/.local/bin/
release-radar auth      # first-time auth
release-radar init      # pull artists (~2 min)
release-radar scan      # check releases (~4 min)
release-radar status    # show DB stats
release-radar config show  # dump config
```

## .gitignore

`release-radar` binary, `*.p8` keys, `*.db` / `*-wal` / `*-shm` SQLite files. No config files or secrets are committed.
