# Release Radar — Technical Plan

## Overview

A Linux CLI tool that scans a user's Apple Music library artists, checks for new album releases, and adds them to a dedicated "Release Radar" playlist.

- **Platform:** Linux CLI
- **Language:** Go 1.21+
- **Streaming service:** Apple Music
- **Artist identity:** Keyed by Apple Music's unique artist ID (no name collisions)

---

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                    release-radar                     │
├───────────┬──────────┬──────────┬──────────────────┤
│   auth    │  cache   │ scanner  │  playlist        │
├───────────┴──────────┴──────────┴──────────────────┤
│           minchao/go-apple-music (API client)       │
├────────────────────────────────────────────────────┤
│           HTTP/REST → api.music.apple.com           │
└────────────────────────────────────────────────────┘
```

| Package | Responsibility |
|---------|---------------|
| `internal/auth` | Developer token (JWT) generation, OAuth proxy for Music User Token, token caching |
| `internal/cache` | Local artist store (JSON), last-scan timestamps, scan state persistence |
| `internal/scanner` | Concurrent artist-album pipeline: fetch artists → query albums → diff by date → collect new releases |
| `internal/playlist` | Find or create "Release Radar" playlist, dedup, add tracks |

---

## Authentication

Apple Music requires two tokens:

| Token | Scope | Acquisition |
|-------|-------|-------------|
| **Developer Token (JWT)** | Catalog API | Generated locally: Team ID + MusicKit key (.p8) → ES256 JWT |
| **Music User Token** | `/v1/me/*` (library, playlists) | OAuth proxy flow: spawn local HTTP server → open browser → capture redirect |

### OAuth Proxy Flow (`release-radar auth`)

1. Spawn temporary HTTP server on `localhost:19876`
2. Open browser to Apple's authorize URL with redirect back to `localhost:19876/callback`
3. User signs in with Apple ID
4. Apple redirects back with authorization code
5. Exchange code for Music User Token
6. Cache both tokens to `~/.config/release-radar/auth.json` (permissions 0600)

---

## Core Data Flow

### `release-radar init`
```
GET /v1/me/library/artists
  → Extract: {id, name} per artist
  → Write: ~/.config/release-radar/artists.json
  → Write: scan_state.json (baseline timestamp = now)
```

### `release-radar scan`
```
1. Load artists from cache
2. For each artist (concurrent, 10 workers):
     GET /v1/catalog/{storefront}/artists/{id}/albums?limit=10
     → Filter: albums where releaseDate > last_scan
3. Find or create "Release Radar" playlist
4. Dedup against existing playlist tracks
5. Add new albums to playlist
6. Update last_scan timestamp
```

### New Release Logic
- Compare `releaseDate` on each album resource against the `last_scan` timestamp from `scan_state.json`
- Overrideable via `--since YYYY-MM-DD` flag

---

## CLI Interface

```
release-radar auth         # First-time OAuth authentication
release-radar init         # Pull artist library, set baseline timestamp
release-radar scan         # Check for new releases, add to playlist
release-radar status       # Show tracked artist count + last scan info
release-radar config show  # Display current configuration
```

### Flags
- `--dry-run` — Preview new releases without modifying playlist
- `--verbose` — Per-artist progress during scan
- `--concurrency N` — Worker count (default: 10)
- `--since YYYY-MM-DD` — Override lookback date
- `--config PATH` — Custom config file path

---

## Configuration

### File: `~/.config/release-radar/config.toml`

```toml
[apple]
team_id = ""
musickit_key_id = ""
musickit_key_path = ""

[playlist]
name = "Release Radar"
auto_create = true

[scan]
concurrency = 10
max_albums_per_artist = 10
```

### Cache Files: `~/.config/release-radar/`

```
config.toml         # User-editable configuration
auth.json           # Tokens (0600 permissions)
artists.json        # [{id, name, genre, last_seen}]
scan_state.json     # {last_scan: RFC3339, albums_found: int, albums_added: int}
```

---

## Go Module Structure

```
release-radar/
├── main.go
├── go.mod
├── go.sum
├── cmd/
│   ├── auth.go
│   ├── init.go
│   ├── scan.go
│   ├── status.go
│   └── config.go
├── internal/
│   ├── auth/
│   │   └── auth.go
│   ├── cache/
│   │   ├── artists.go
│   │   └── state.go
│   ├── scanner/
│   │   └── scanner.go
│   └── playlist/
│       └── playlist.go
└── pkg/
    └── config/
        └── config.go
```

---

## Dependencies

| Package | Version | Purpose |
|---------|---------|---------|
| `github.com/minchao/go-apple-music` | latest | Apple Music API client (catalog + library + JWT) |
| `github.com/spf13/cobra` | latest | CLI command framework |
| `github.com/spf13/viper` | latest | TOML config management |
| `golang.org/x/oauth2` | latest | OAuth2 helper |

All other needs (HTTP, crypto/JWT, JSON, semaphore concurrency) are covered by Go stdlib.

---

## Concurrency Strategy

A semaphore-bounded worker pool using goroutines:

```go
sem := make(chan struct{}, concurrency) // buffered channel acts as semaphore
for _, artist := range artists {
    wg.Add(1)
    go func(a Artist) {
        defer wg.Done()
        sem <- struct{}{}        // acquire slot
        defer func() { <-sem }() // release slot
        // query catalog, filter by date
    }(artist)
}
```

**Performance:** 500 artists, 10 concurrent workers, ~200ms avg latency = **~10 seconds** full scan.

---

## Key Design Decisions

1. **No Apple Music SDK** — Only the REST API (`go-apple-music` wrapper). No platform-specific SDK exists for Linux.
2. **OAuth proxy, not manual token** — Browser-based auth flow is the only reliable way to get a Music User Token on Linux.
3. **Artist ID as primary key** — Avoids name collision between same-named artists.
4. **releaseDate as the signal** — Simplest and most reliable "is this new?" heuristic.
5. **Destination playlist configurable** — Defaults to "Release Radar" but can be overridden in config.

---

## Implementation Phases

| Phase | Description | Status |
|-------|-------------|--------|
| **1. Scaffold** | Go module, cobra commands, config, directory structure | Current |
| **2. Auth** | JWT token generation, OAuth proxy, token caching | — |
| **3. Cache** | Artist library pull, persist to JSON, scan state | — |
| **4. Scanner** | Concurrent artist-album pipeline, new release detection | — |
| **5. Playlist** | Create/find playlist, add tracks with dedup, dry-run | — |
| **6. Polish** | Progress output, error handling, rate-limit backoff, tests | — |
| **Future** | SoundCloud support, webhook notifications, systemd timer | — |

---

## Prerequisites

- Go 1.21+
- Apple Developer account ($99/year) for MusicKit key
- MusicKit private key (.p8 file) from Apple Developer portal
- Team ID + Key ID
- Active Apple Music subscription
