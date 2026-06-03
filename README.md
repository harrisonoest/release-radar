# Release Radar

Track new music releases from artists in your Apple Music library and add them to a playlist.

## How it works

```
release-radar auth       # Sign in to Apple Music (one-time)
release-radar init       # Pull your library artists
release-radar scan       # Check for new releases, add to playlist
```

`init` pulls artists from your library (and optionally other sources via `--all` or `--from-playlist`), resolves names to catalog IDs, and deduplicates — collaborative entries like "Joris Voorn & Goodboys" are collapsed into the standalone artist. Only artists with at least one standalone entry are tracked.

`scan` queries each artist's catalog albums, skips releases already in `added` or `ignored` state, compares release dates against your last scan timestamp, and adds new releases to your "Release Radar" playlist. On first run, it lazy-backfills existing playlist tracks into the releases table.

`sources` manages where your artists come from — library, albums, songs, liked songs, and external playlists. `releases` lets you view and manage the state of every tracked release.

`scan` displays a live progress bar. `init` and `sources scan` log progress when `-v` is set.

All data is stored in a local SQLite database. JSON files from earlier versions are migrated automatically on first run.

## Prerequisites

- **Go 1.25+**
- **Apple Developer account** ($99/year)
- **MusicKit private key** (.p8 file) — generate one at [developer.apple.com](https://developer.apple.com/account/resources/authkeys/list)
- **Apple Music subscription**

## Installation

```bash
git clone <repo-url>
cd release-radar
make install        # builds and copies to ~/.local/bin/
```

Or manually:

```bash
go build -o release-radar .
mv release-radar ~/.local/bin/
```

Make targets:

| `make`               | Action                          |
| -------------------- | ------------------------------- |
| `build`              | Compile binary                  |
| `test`               | Run all tests                   |
| `vet`                | Static analysis                 |
| `lint`               | vet + gofmt check               |
| `clean`              | Remove binary                   |
| `install`            | Build + copy to `~/.local/bin/` |
| `run ARGS="scan -v"` | Build + run with args           |

## Setup

### 1. Get your Apple credentials

From the Apple Developer portal:
- **Team ID** — found in your [membership details](https://developer.apple.com/account/#/membership)
- **Key ID** — the identifier for your MusicKit private key
- **Private key** — download the `.p8` file

### 2. Create config file

```bash
mkdir -p ~/.config/release-radar
```

`~/.config/release-radar/config.toml`:

```toml
[apple]
team_id = "ABC123XYZ"
musickit_key_id = "X3Z9GKY38J"
musickit_key_path = "/home/you/.config/release-radar/AuthKey_X3Z9GKY38J.p8"

[playlist]
name = "Release Radar"
auto_create = true

[scan]
concurrency = 5
```

### 3. Authenticate

```bash
release-radar auth
```

Opens your browser for Apple Music sign-in via MusicKit JS. Tokens are stored in the SQLite database. A manual paste fallback is shown if automatic capture fails.

### 4. Index your library

```bash
release-radar init
```

Fetches all library artists (and optional additional sources), resolves catalog IDs, and deduplicates collaborative entries. A ~7700-entry library typically yields ~2900 unique standalone artists.

## Usage

```bash
# First-time setup
release-radar init              # Pull + deduplicate from library (~2 min)
release-radar init --all        # Include library albums, songs, and liked songs
release-radar init --from-playlist "name:Discover Weekly"   # Also pull from a playlist

# Manage sources
release-radar sources list              # List configured sources
release-radar sources add playlist <id> # Add a playlist source
release-radar sources add liked_songs   # Add liked songs source
release-radar sources remove playlist <id>
release-radar sources scan              # Re-fetch all sources

# Scan for new releases
release-radar scan              # Check all artists, add to playlist
release-radar scan --dry-run    # Preview without modifying playlist
release-radar scan --since 2026-01-01     # Override lookback date
release-radar scan --limit-artists 100    # Test with first 100 artists
release-radar scan --concurrency 3       # Adjust parallelism
release-radar scan --no-backfill         # Skip lazy backfill on first run
release-radar scan -v           # Verbose output

# Manage releases
release-radar releases list               # List releases (default: added + ignored)
release-radar releases list --state seen  # List new/unprocessed releases
release-radar releases show <album_id>    # Show full details for a release
release-radar releases ignore <album_id>  # Ignore a release
release-radar releases unignore <album_id>
release-radar releases remove <album_id>  # Delete from tracking

# Info
release-radar status            # Show tracked artist count + last scan + releases by state
release-radar config show       # Show current configuration

# Ignore artists (exclude from scans)
release-radar ignore add <catalog_id> <name>   # Ignore an artist
release-radar ignore remove <catalog_id>        # Stop ignoring
release-radar ignore list                       # List ignored artists
```

Run `scan` on a cron job for weekly updates:

```
0 9 * * 1 /home/you/.local/bin/release-radar scan
```

## Configuration reference

| Key                          | Default         | Description                             |
| ---------------------------- | --------------- | --------------------------------------- |
| `apple.team_id`              | —               | Apple Developer Team ID                 |
| `apple.musickit_key_id`      | —               | MusicKit private key ID                 |
| `apple.musickit_key_path`    | —               | Path to `.p8` private key file          |
| `playlist.name`              | `Release Radar` | Playlist name for new releases          |
| `playlist.auto_create`       | `true`          | Create playlist if not found            |
| `playlist.id`                | —               | Override playlist by ID instead of name |
| `scan.concurrency`           | `5`             | Concurrent artist queries               |
| `scan.ignored_artists`       | `[]`            | Catalog IDs to exclude from scans       |

## Data storage

All data is stored in a single SQLite database at `~/.config/release-radar/release-radar.db` using [WAL mode](https://www.sqlite.org/wal.html) with a 5-second busy timeout. Six tables: `artists`, `scan_state`, `auth`, `ignored_artists`, `artist_sources`, and `releases`.

```
~/.config/release-radar/
├── config.toml              # Your configuration
├── AuthKey_*.p8             # MusicKit private key
└── release-radar.db         # SQLite database (all runtime data)
```

### Querying the database

Use the `sqlite3` CLI to inspect data directly:

```bash
# List all tracked artists
sqlite3 ~/.config/release-radar/release-radar.db "SELECT * FROM artists LIMIT 10"

# Count artists
sqlite3 ~/.config/release-radar/release-radar.db "SELECT COUNT(*) FROM artists"

# Find a specific artist
sqlite3 ~/.config/release-radar/release-radar.db "SELECT * FROM artists WHERE name LIKE '%Joris%'"

# Check last scan info
sqlite3 ~/.config/release-radar/release-radar.db "SELECT * FROM scan_state"

# Check authentication status
sqlite3 ~/.config/release-radar/release-radar.db "SELECT music_user_token != '' AS authenticated FROM auth"

# List ignored artists
sqlite3 ~/.config/release-radar/release-radar.db "SELECT * FROM ignored_artists"

# List configured sources
sqlite3 ~/.config/release-radar/release-radar.db "SELECT * FROM artist_sources"

# Show releases by state
sqlite3 ~/.config/release-radar/release-radar.db "SELECT state, COUNT(*) FROM releases GROUP BY state"
```

### Schema

```sql
-- Core artist table (one row per unique catalog artist)
CREATE TABLE artists (
    catalog_id TEXT PRIMARY KEY,
    library_id TEXT NOT NULL,
    name       TEXT NOT NULL,
    href       TEXT NOT NULL,
    last_seen  TEXT NOT NULL
);

-- Scan tracking (singleton)
CREATE TABLE scan_state (
    id           INTEGER PRIMARY KEY CHECK (id = 1),
    last_scan    TEXT NOT NULL DEFAULT '',
    albums_found INTEGER NOT NULL DEFAULT 0,
    albums_added INTEGER NOT NULL DEFAULT 0
);

-- Auth tokens (singleton)
CREATE TABLE auth (
    id               INTEGER PRIMARY KEY CHECK (id = 1),
    developer_token  TEXT NOT NULL DEFAULT '',
    developer_exp    TEXT NOT NULL DEFAULT '',
    music_user_token TEXT NOT NULL DEFAULT ''
);

-- Ignored artists (excluded from scans)
CREATE TABLE ignored_artists (
    catalog_id TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    ignored_at TEXT NOT NULL
);

-- Artist provenance (multi-source tracking)
CREATE TABLE artist_sources (
    catalog_id  TEXT NOT NULL,
    source_type TEXT NOT NULL,
    source_id   TEXT NOT NULL DEFAULT '',
    added_at    TEXT NOT NULL,
    PRIMARY KEY (catalog_id, source_type, source_id)
);

-- Release state machine (per-album lifecycle)
CREATE TABLE releases (
    album_id          TEXT PRIMARY KEY,
    catalog_artist_id TEXT NOT NULL,
    artist_name       TEXT NOT NULL,
    name              TEXT NOT NULL,
    release_date      TEXT NOT NULL,
    track_count       INTEGER NOT NULL DEFAULT 0,
    state             TEXT NOT NULL DEFAULT 'seen',
    first_seen_at     TEXT NOT NULL,
    added_at          TEXT
);
```

## Architecture

```
cmd/                    CLI commands (cobra)
  auth.go               OAuth proxy server + MusicKit JS page
  init.go               Multi-source artist aggregation
  scan.go               Scan orchestration + playlist wiring
  status.go             Info display
  config.go             Config inspection
  ignore.go             Artist ignore list management
  sources.go            Source add/remove/list/scan
  releases.go           Release list/show/ignore/unignore/remove
  root.go               Root command + shared flags
internal/
  auth/                 JWT generation, token cache, OAuth flow
  scanner/              Concurrent artist→album pipeline, date filtering
  playlist/             Find/create playlist, dedup tracks, add to playlist, lazy backfill
  source/               Source interface + aggregator with name resolution + collaboration filter
pkg/
  api/                  Apple Music API client (go-apple-music wrapper)
  config/               Viper/TOML config loading
  db/                   SQLite store (artists, scan state, auth tokens, sources, releases)
```

## Performance

- **Storage**: SQLite with WAL mode. Loading 2931 artists takes ~2ms (25x faster than JSON).
- **init**: ~310 API pages, ~2 minutes for 7700 library entries. Live progress bar.
- **scan**: 2931 artists, 5 concurrent, ~4 minutes. Exponential backoff handles 429 rate limits.
