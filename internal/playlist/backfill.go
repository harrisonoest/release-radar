package playlist

import (
	"context"
	"fmt"
	"time"

	"github.com/harrisonoest/release-radar/pkg/db"
)

// BackfillTrack is a single track observed in the Release Radar playlist during
// backfill. It carries enough metadata to populate a releases row without
// requiring additional API calls.
type BackfillTrack struct {
	TrackID     string
	AlbumID     string
	AlbumName   string
	ArtistName  string
	ReleaseDate string
}

// backfillFetchTracksFunc is a package-level seam for tests. When set, the
// Manager uses this to fetch tracks from the playlist. When nil, the default
// implementation is used. Tests must not run in parallel because of this
// package-level state.
var backfillFetchTracksFunc func(ctx context.Context, playlistID string) ([]BackfillTrack, error)

// BackfillFromPlaylist walks the configured playlist and inserts every album
// found into the releases table with state='added'. It is a no-op if the
// releases table already contains any rows. Returns the number of releases
// inserted.
func (m *Manager) BackfillFromPlaylist(ctx context.Context, playlistID string) (int, error) {
	if m.store == nil {
		return 0, nil
	}

	counts, err := m.store.CountReleasesByState()
	if err != nil {
		return 0, fmt.Errorf("failed to load existing releases: %w", err)
	}
	for _, c := range counts {
		if c > 0 {
			return 0, nil
		}
	}

	tracks, err := m.backfillFetchTracks(ctx, playlistID)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch playlist tracks: %w", err)
	}

	seen := make(map[string]BackfillTrack)
	for _, t := range tracks {
		if t.AlbumID == "" {
			continue
		}
		if _, ok := seen[t.AlbumID]; !ok {
			seen[t.AlbumID] = t
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	releases := make([]db.Release, 0, len(seen))
	for _, t := range seen {
		releases = append(releases, db.Release{
			AlbumID:     t.AlbumID,
			ArtistName:  t.ArtistName,
			Name:        t.AlbumName,
			ReleaseDate: t.ReleaseDate,
			State:       "added",
			FirstSeenAt: now,
			AddedAt:     now,
		})
	}
	if err := m.store.UpsertReleases(releases); err != nil {
		return 0, fmt.Errorf("failed to backfill releases: %w", err)
	}
	return len(releases), nil
}

func (m *Manager) backfillFetchTracks(ctx context.Context, playlistID string) ([]BackfillTrack, error) {
	if backfillFetchTracksFunc != nil {
		return backfillFetchTracksFunc(ctx, playlistID)
	}
	return m.defaultBackfillFetch(ctx, playlistID)
}

func (m *Manager) defaultBackfillFetch(ctx context.Context, playlistID string) ([]BackfillTrack, error) {
	if m.client == nil {
		return nil, fmt.Errorf("client is nil; cannot fetch playlist")
	}
	raw, err := m.client.GetLibraryPlaylistCatalogTracks(ctx, playlistID, 100)
	if err != nil {
		return nil, err
	}
	out := make([]BackfillTrack, 0, len(raw))
	for _, t := range raw {
		out = append(out, BackfillTrack{
			TrackID:     t.TrackID,
			AlbumID:     t.AlbumID,
			AlbumName:   t.AlbumName,
			ArtistName:  t.ArtistName,
			ReleaseDate: t.ReleaseDate,
		})
	}
	return out, nil
}
