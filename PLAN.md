# Release Radar — Design Spec

**Date:** 2026-06-02
**Status:** Approved design, pending implementation

## 1. Overview

This spec describes a rework of release-radar that addresses two observed problems:

1. **Re-adding of already-seen releases.** Albums with far-future placeholder dates keep satisfying the date filter and are re-added to the Release Radar playlist on every scan.
2. **Missing new releases.** The current artist source (the user's library) misses artists they actively listen to in playlists, liked songs, etc.

The rework introduces two cooperating subsystems: a **multi-source artist tracking** system and a **release state machine**. The state machine is the single source of truth for "have we processed this album?" — replacing the current playlist-track-based dedup, which is fragile.

## 2. Goals

- New albums (including singles and EPs) by tracked artists are added to the Release Radar playlist exactly once.
- The set of tracked artists is the union of multiple Apple Music data sources, not just the library.
- The system degrades gracefully on API errors and surfaces permanent failures.
- All existing CLI commands (`init`, `scan`, `status`, `ignore`, `config show`) keep working with no flag changes.
- New commands (`sources`, `releases`) are additive.

## 3. Non-Goals

- Notifications, webhooks, or background daemons.
- Music Harbor or other third-party integrations.
- Multiple output playlists.
- Changes to the OAuth or auth flow.
- Surfacing "upcoming" releases before they appear in Apple's catalog (the API does not expose editorial release dates).
- Cross-storefront support beyond the user's detected storefront.

## 4. Background: Current State

The current pipeline:

```
init:   GET /v1/me/library/artists?include=catalog
        → resolve to catalog IDs, dedup collaborations, store in `artists` table
scan:   for each catalog artist: GET /v1/catalog/{sf}/artists/{id}/albums (paginated)
        → filter: releaseDate > last_scan
        → add to Release Radar playlist, dedup via existing tracks in playlist
```

**Why releases get re-added:**

- Dedup relies on comparing catalog track IDs against tracks already in the Release Radar playlist. If the user has manually removed tracks, or if the catalog track IDs differ between pre-release and final release, the dedup misses and the album is added again.
- Albums with placeholder future dates (e.g., `2026-12-31`) are re-evaluated on every scan.

**Why artists are missed:**

- Only artists that appear in the user's library are tracked. Tracks added to playlists (e.g., from "For You" recommendations, shared links, or curated playlists) do not add the artist to the library.
- The collaboration filter (`isCollaboration`) drops artists whose only library entry is a collaboration, even if the user follows them via playlists.

## 5. Proposed Solution

Two new tables, a new `internal/source` package, and refactors to `scanner`, `playlist`, and `init`.

**Single source of truth:** the `releases` table. The scanner queries it to skip already-processed albums. The playlist manager updates it on successful adds. The playlist-track-based dedup is still called, but only to log a diff (how many catalog tracks were already in the playlist vs. how many are being newly added) — it no longer drives any decision.

**Sources as a first-class concept:** every catalog ID we track is associated with one or more sources that discovered it. Adding/removing a source adjusts coverage. New sources (library albums, library songs, liked songs, named playlists) can be added to capture more artists.

## 6. Data Model

Two new tables. Existing tables (`artists`, `scan_state`, `auth`, `ignored_artists`) are unchanged.

```sql
CREATE TABLE artist_sources (
    catalog_id  TEXT NOT NULL,
    source_type TEXT NOT NULL,    -- 'library_artists' | 'library_albums' | 'library_songs' | 'liked_songs' | 'playlist'
    source_id   TEXT NOT NULL DEFAULT '',  -- playlist ID for source_type='playlist'; empty otherwise
    added_at    TEXT NOT NULL,
    PRIMARY KEY (catalog_id, source_type, source_id)
);
CREATE INDEX idx_artist_sources_catalog_id ON artist_sources(catalog_id);

CREATE TABLE releases (
    album_id            TEXT PRIMARY KEY,
    catalog_artist_id   TEXT NOT NULL,
    artist_name         TEXT NOT NULL,
    name                TEXT NOT NULL,
    release_date        TEXT NOT NULL,
    track_count         INTEGER NOT NULL DEFAULT 0,
    state               TEXT NOT NULL,    -- 'seen' | 'added' | 'ignored'
    first_seen_at       TEXT NOT NULL,
    added_at            TEXT
);
CREATE INDEX idx_releases_state ON releases(state);
CREATE INDEX idx_releases_artist ON releases(catalog_artist_id);
CREATE INDEX idx_releases_release_date ON releases(release_date);
```

**State semantics:**

| State     | Meaning                                                                           | Set when                                                      |
| --------- | --------------------------------------------------------------------------------- | ------------------------------------------------------------- |
| `seen`    | Catalog returned this album; we have not acted on it                              | Scanner observes a new album for the first time               |
| `added`   | Successfully added to the Release Radar playlist                                  | Playlist manager confirms add                                 |
| `ignored` | User explicitly told us to never suggest this album                               | User runs `releases ignore <album_id>`                         |

The scanner filters out `state IN ('added', 'ignored')` before returning. `state='seen'` releases pass through (so the user can see and act on them), but they are not duplicated in the table.

## 7. Source System — `internal/source`

A new package implementing a common interface for all artist-discovery sources.

```go
// internal/source/source.go
type Source interface {
    Type() string                                  // 'library_artists' | 'library_albums' | 'library_songs' | 'liked_songs' | 'playlist'
    ID() string                                    // playlist ID, or "" for built-ins
    DisplayName() string                           // "Library" | "Playlist: My Artists"
    Fetch(ctx context.Context, c *api.Client) ([]RawArtist, error)
}

type RawArtist struct {
    Name      string
    CatalogID string  // empty if unresolved
}
```

**Source implementations:**

| File                                  | Source          | Catalog ID source                                       |
| ------------------------------------- | --------------- | ------------------------------------------------------- |
| `internal/source/library_artists.go`  | `library_artists` | Direct, via `?include=catalog` (existing code)         |
| `internal/source/library_albums.go`  | `library_albums` | Direct, via `?include=artists`                          |
| `internal/source/library_songs.go`   | `library_songs`  | Resolve names via `?types=artists` search, batched 50  |
| `internal/source/liked.go`           | `liked_songs`    | Resolve names via search                                |
| `internal/source/playlist.go`        | `playlist:<id>`  | Resolve names via search                                |

**Aggregator** (`internal/source/aggregator.go`):

```
for each enabled source:
    fetch raw artists
for all raw artists:
    resolve to catalog_id (skip if not resolvable, log warning)
    apply isCollaboration filter
    shortest-name-wins per catalog_id
upsert into artists
upsert into artist_sources
```

**Source lifecycle (snapshot semantics):**

- `sources add <type> [id]`: fetch immediately, populate `artists` and `artist_sources`. The source is then "enabled" — included in subsequent `init --all`.
- `sources remove <type> [id]`: delete rows from `artist_sources` matching `(type, id)`. Artists covered by other sources remain. Artists with no remaining source are left in the `artists` table (the next scan will skip them) but not hard-deleted. A `sources scan --prune` flag can be added later to hard-delete.
- `sources scan`: re-run fetch + aggregation for all enabled sources, idempotent.
- `init --from-playlist <name|id>`: one-shot fetch + merge. Does NOT save the playlist as a permanent source.

**Catalog ID resolution for non-library sources:**

Apple Music's library-songs and playlist-tracks return `ArtistName` as a string, not a catalog ID. To resolve to a catalog ID we use:

```
GET /v1/catalog/{storefront}/search?types=artists&term={name}&limit=1
```

The first result is used. Names that do not resolve are logged and skipped. Batching is by name uniqueness (one resolution per unique name, not per track), to keep API calls down.

**Ambiguity risk:** the search may return the wrong artist for ambiguous names (e.g., "John" → "John Williams" when the user means "John Frusciante"). For names shared with high-profile artists, the resolution may be wrong. The aggregator logs every name that resolved to a catalog ID where the catalog name does not exactly match the source name; a `releases unignore`-style re-resolution path is not in scope but should be considered for a future iteration.

## 8. Scanner Changes — `internal/scanner`

`checkArtist` and `Scan` get these changes:

1. **Skip known releases.** Before returning the result list, drop any release whose `AlbumID` is in `releases` with `state IN ('added', 'ignored')`. `state='seen'` releases pass through.
2. **Watermark pagination.** Replace the current "paginate until empty" with "paginate until the oldest album in the current page is older than `since - 30 days`." The 30-day buffer catches late catalog updates and announced-but-not-yet-released albums. This avoids fetching every page of a 200-album artist's catalog.
3. **Surface permanent failures.** The current scanner counts retries as "errors." A new `permanent_failures` counter tracks artists that exhausted all 5 retries. The final summary lists them by name.
4. **No type filtering.** `IsSingle`, EP/album distinction: not filtered. Singles, EPs, albums, compilations, live albums all pass through. (Per the no-type-tagging decision.)
5. **Explicit release-type inclusion.** The catalog call adds `?filter[albums]=albums,singles,eps,compilations,live-albums` to ensure all types are returned (defensive — most return by default).

**No new public API on `pkg/api.Client`.** The existing `GetArtistAlbums` is reused; the `?filter` parameter is added internally.

## 9. Playlist Integration — `internal/playlist`

`AddReleases` is updated to update the `releases` table on successful adds:

```
for each release added to playlist successfully:
    INSERT INTO releases (album_id, catalog_artist_id, artist_name, name, release_date, track_count, state, first_seen_at, added_at)
    VALUES (...)
    ON CONFLICT (album_id) DO UPDATE SET state='added', added_at=excluded.added_at
```

Failed adds (e.g., album not yet on Apple Music) leave the release in `state='seen'`. The user can decide to `releases ignore` it.

**Lazy backfill on first scan after upgrade:**

```
if releases table is empty:
    fetch all tracks from the Release Radar playlist (both library and catalog variants)
    for each track:
        resolve to its album (one call per unique album)
        if album found:
            INSERT INTO releases (..., state='added', added_at=<existing added_at from playlist if available, else now>)
```

Backfill is gated by the `releases` table being empty, so it runs at most once per database. It is idempotent (`ON CONFLICT (album_id) DO UPDATE`) and shows progress in a separate phase of the existing progress bar. Users can skip it with `--no-backfill`.

## 10. CLI Surface

### New: `sources`

```
release-radar sources list
    # Show: type, ID, display name, count of artists contributed
release-radar sources add playlist <name|id>
    # Resolve name→ID; fetch; merge artists
release-radar sources add liked
    # Special: enables liked_songs source
release-radar sources remove <type> [id]
    # type: library_artists | library_albums | library_songs | liked_songs | playlist
    # For 'playlist', id is required. For others, id is ignored.
release-radar sources scan
    # Re-fetch all enabled sources
```

### New: `releases`

```
release-radar releases list [--state seen|added|ignored] [--artist <catalog_id>] [--limit N]
release-radar releases show <album_id>
release-radar releases ignore <album_id>
release-radar releases unignore <album_id>
release-radar releases remove <album_id>
    # Delete entirely (for accidental adds)
```

### Changed: `init`

```
release-radar init                    # library_artists only (backward-compatible default)
release-radar init --all              # all enabled sources (default: 4 built-ins enabled)
release-radar init --from-playlist <name|id>   # one-shot; artists are merged in but not saved as a permanent source
release-radar init --prune            # (optional flag) hard-delete artists with no remaining source
```

`init --from-playlist` is idempotent: if any of the playlist's artists are already in `artists`, the existing rows are updated and `artist_sources` is extended with a new `playlist:<id>` row for that catalog ID.

### Changed: `scan`

```
release-radar scan                    # default: skip added/ignored
release-radar scan --include-seen     # also re-show releases in 'seen' state
release-radar scan --no-backfill      # skip the lazy backfill (e.g., for CI runs)
```

### Changed: `status`

```
release-radar status
    # Existing output +
    # Sources: N
    # Tracked releases: N total (added: N, ignored: N, seen: N)
```

### Unchanged: `auth`, `config show`, `ignore {add,remove,list}`

## 11. Migration & Backward Compatibility

**Schema:** additive. Two new tables created on first run via `CREATE TABLE IF NOT EXISTS`. No changes to existing tables.

**Artist backfill:** on first run after migration, every `artists.catalog_id` is inserted into `artist_sources` as `source_type='library_artists', source_id=''`. This is done in a single transaction inside `Store.Open()`.

**`init` default:** unchanged. `release-radar init` continues to populate from `library_artists` only. `init --all` is opt-in for the new behavior.

**`scan` default:** the lazy backfill is internal — visible only as a brief progress bar phase. No CLI flag needed for users who don't care.

**`scan` no longer trusts playlist-track dedup as primary.** It still calls it (for logging visibility), but `releases` table is the authoritative source.

**No removed commands or flags.** All existing flags keep working.

## 12. Observability

- `scan` progress bar: add `seen: N` and `pruned: N` counters (releases skipped because already known).
- `scan` final summary: list permanently-failed artists by name + last error. List artists with 0 albums returned (possible catalog-mismatch).
- `status`: include source count, per-state release count.
- `sources list`: per-source artist counts.

## 13. Testing Strategy

Following the existing test pattern: unit tests with no network. API client is mocked via the existing `api.ClientInterface`.

**New test files:**

- `pkg/db/store_test.go` (extend existing): schema migration, new table CRUD, state transitions, source provenance, ON CONFLICT semantics.
- `internal/scanner/scanner_test.go` (extend existing): skip known releases, watermark pagination, surface permanent failures, `seen` pass-through.
- `internal/playlist/playlist_test.go` (extend existing): state-machine update on add, lazy backfill.
- `internal/source/library_artists_test.go`: wraps the existing init flow, dedup behavior preserved.
- `internal/source/library_albums_test.go`: album → artist extraction.
- `internal/source/library_songs_test.go`: name → catalog ID resolution.
- `internal/source/liked_test.go`: special playlist handling.
- `internal/source/playlist_test.go`: named playlist fetch + artist extraction.
- `internal/source/aggregator_test.go`: dedup, collaboration filter, shortest-name-wins.

**Coverage target:** ≥ 80% line coverage on new code.

**Manual verification:**

- `make build` clean, `make test` clean, `make lint` clean.
- `release-radar init --all` on a real library returns a larger artist count than `release-radar init`.
- `release-radar scan` followed by another `release-radar scan --dry-run` reports zero new releases (dedup works).
- Manually add a far-future-dated album via the playlist, then `release-radar scan` does not re-add it.

## 14. Implementation Order

Phased to keep each step shippable:

1. **Schema migration** — new tables, artist backfill, store methods.
2. **State machine in scanner** — skip known releases (using the empty table initially).
3. **State machine in playlist** — upsert on add, lazy backfill.
4. **Source package** — `library_artists` first (re-wrap existing logic), then `library_albums`, `library_songs`, `liked_songs`, `playlist`.
5. **Aggregator** — unify dedup + collaboration filter, wire into `init`.
6. **CLI surface** — `sources` and `releases` subcommands.
7. **`init --all` flag** — wire sources into init.
8. **Observability** — progress bar counters, `status` extension.

## 15. Out of Scope (Explicit)

- Webhooks, push notifications, email digests.
- Music Harbor or other third-party service integration.
- Multiple output playlists (e.g., per-source routing).
- Cross-storefront releases.
- "Upcoming release" predictions (no API signal).
- OAuth/auth changes.
- Web UI.

## 16. Open Questions

None at design time. Implementation may surface questions about edge cases (e.g., what happens to `seen` releases whose artist is later ignored — current design: they stay `seen`, but the artist is filtered out of the scan, so they will never re-surface). These are deferred to implementation.
