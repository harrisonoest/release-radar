package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/harrisonoest/release-radar/pkg/config"
	_ "modernc.org/sqlite"
)

type Artist struct {
	CatalogID string
	LibraryID string
	Name      string
	Href      string
	LastSeen  string
}

type ScanState struct {
	LastScan    string
	AlbumsFound int
	AlbumsAdded int
}

type ArtistSource struct {
	CatalogID  string
	SourceType string
	SourceID   string
	AddedAt    string
}

type Release struct {
	AlbumID         string
	CatalogArtistID string
	ArtistName      string
	Name            string
	ReleaseDate     string
	TrackCount      int
	State           string
	FirstSeenAt     string
	AddedAt         string
}

type AuthTokens struct {
	DeveloperToken string
	DeveloperExp   string
	MusicUserToken string
}

type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS artists (
	library_id TEXT PRIMARY KEY,
	catalog_id TEXT NOT NULL DEFAULT '',
	name TEXT NOT NULL,
	href TEXT NOT NULL,
	last_seen TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS scan_state (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	last_scan TEXT NOT NULL DEFAULT '',
	albums_found INTEGER NOT NULL DEFAULT 0,
	albums_added INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS auth (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	developer_token TEXT NOT NULL DEFAULT '',
	developer_exp TEXT NOT NULL DEFAULT '',
	music_user_token TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS ignored_artists (
	catalog_id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	ignored_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS artist_sources (
	catalog_id  TEXT NOT NULL,
	source_type TEXT NOT NULL,
	source_id   TEXT NOT NULL DEFAULT '',
	added_at    TEXT NOT NULL,
	PRIMARY KEY (catalog_id, source_type, source_id)
);
CREATE INDEX IF NOT EXISTS idx_artist_sources_catalog_id ON artist_sources(catalog_id);

CREATE TABLE IF NOT EXISTS releases (
	album_id            TEXT PRIMARY KEY,
	catalog_artist_id   TEXT NOT NULL,
	artist_name         TEXT NOT NULL,
	name                TEXT NOT NULL,
	release_date        TEXT NOT NULL,
	track_count         INTEGER NOT NULL DEFAULT 0,
	state               TEXT NOT NULL,
	first_seen_at       TEXT NOT NULL,
	added_at            TEXT
);
CREATE INDEX IF NOT EXISTS idx_releases_state ON releases(state);
CREATE INDEX IF NOT EXISTS idx_releases_artist ON releases(catalog_artist_id);
CREATE INDEX IF NOT EXISTS idx_releases_release_date ON releases(release_date);
`

func Open(cacheDir string) (*Store, error) {
	if cacheDir == "" {
		var err error
		cacheDir, err = config.ConfigDir()
		if err != nil {
			return nil, err
		}
	}

	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return nil, fmt.Errorf("cannot create cache directory: %w", err)
	}

	dbPath := filepath.Join(cacheDir, "release-radar.db")

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("cannot open database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("cannot create schema: %w", err)
	}

	store := &Store{db: db}

	if err := store.migrateSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("cannot migrate schema: %w", err)
	}

	if err := store.migrateFromJSON(cacheDir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: JSON migration failed: %v\n", err)
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrateSchema() error {
	var columnName string
	err := s.db.QueryRow("PRAGMA table_info(artists)").Scan(
		new(int), new(string), new(string), new(int), new(interface{}), new(int),
	)
	if err != nil {
		return nil
	}

	err = s.db.QueryRow("SELECT name FROM pragma_table_info('artists') WHERE name = 'library_id' AND pk = 1").Scan(&columnName)
	if err == nil {
		return nil
	}

	err = s.db.QueryRow("SELECT name FROM pragma_table_info('artists') WHERE name = 'catalog_id' AND pk = 1").Scan(&columnName)
	if err == nil {
		fmt.Fprintf(os.Stderr, "Migrating database schema…\n")
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		if _, err := tx.Exec("CREATE TABLE IF NOT EXISTS artists_new (library_id TEXT PRIMARY KEY, catalog_id TEXT NOT NULL DEFAULT '', name TEXT NOT NULL, href TEXT NOT NULL, last_seen TEXT NOT NULL)"); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO artists_new (library_id, catalog_id, name, href, last_seen) SELECT library_id, catalog_id, name, href, last_seen FROM artists"); err != nil {
			return err
		}
		if _, err := tx.Exec("DROP TABLE artists"); err != nil {
			return err
		}
		if _, err := tx.Exec("ALTER TABLE artists_new RENAME TO artists"); err != nil {
			return err
		}
		return tx.Commit()
	}

	return nil
}

func (s *Store) migrateFromJSON(cacheDir string) error {
	artistsPath := filepath.Join(cacheDir, "artists.json")
	statePath := filepath.Join(cacheDir, "scan_state.json")
	authPath := filepath.Join(cacheDir, "auth.json")

	// Only migrate if no artists are present
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM artists").Scan(&count)
	if count > 0 {
		return nil
	}

	migrated := false

	// Migrate artists
	if data, err := os.ReadFile(artistsPath); err == nil {
		var cache struct {
			Artists []struct {
				ID        string `json:"id"`
				CatalogID string `json:"catalog_id"`
				Name      string `json:"name"`
				Href      string `json:"href"`
				LastSeen  string `json:"last_seen"`
			} `json:"artists"`
		}
		if json.Unmarshal(data, &cache) == nil && len(cache.Artists) > 0 {
			tx, _ := s.db.Begin()
			if tx != nil {
				stmt, err := tx.Prepare("INSERT OR REPLACE INTO artists (library_id, catalog_id, name, href, last_seen) VALUES (?, ?, ?, ?, ?)")
				if err == nil {
					for _, a := range cache.Artists {
						stmt.Exec(a.ID, a.CatalogID, a.Name, a.Href, a.LastSeen)
					}
					tx.Commit()
					migrated = true
				} else {
					tx.Rollback()
				}
			}
		}
	}

	// Migrate scan state
	if data, err := os.ReadFile(statePath); err == nil {
		var state struct {
			LastScan    string `json:"last_scan"`
			AlbumsFound int    `json:"albums_found"`
			AlbumsAdded int    `json:"albums_added"`
		}
		if json.Unmarshal(data, &state) == nil {
			s.db.Exec(`INSERT OR REPLACE INTO scan_state (id, last_scan, albums_found, albums_added) VALUES (1, ?, ?, ?)`,
				state.LastScan, state.AlbumsFound, state.AlbumsAdded)
		}
	}

	// Migrate auth
	var authCount int
	s.db.QueryRow("SELECT COUNT(*) FROM auth").Scan(&authCount)
	if authCount == 0 {
		if data, err := os.ReadFile(authPath); err == nil {
			var auth struct {
				DeveloperToken string `json:"developer_token"`
				DeveloperExp   string `json:"developer_exp"`
				MusicUserToken string `json:"music_user_token"`
			}
			if json.Unmarshal(data, &auth) == nil && auth.MusicUserToken != "" {
				s.db.Exec(`INSERT INTO auth (id, developer_token, developer_exp, music_user_token) VALUES (1, ?, ?, ?)`,
					auth.DeveloperToken, auth.DeveloperExp, auth.MusicUserToken)
			}
		}
	}

	// Clean up migrated JSON files
	if migrated {
		os.Remove(artistsPath)
		os.Remove(statePath)
	}

	return nil
}

func (s *Store) ReplaceArtists(artists []Artist) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM artists"); err != nil {
		return err
	}

	stmt, err := tx.Prepare("INSERT INTO artists (library_id, catalog_id, name, href, last_seen) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, a := range artists {
		if _, err := stmt.Exec(a.LibraryID, a.CatalogID, a.Name, a.Href, a.LastSeen); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) ListArtists() ([]Artist, error) {
	rows, err := s.db.Query("SELECT library_id, catalog_id, name, href, last_seen FROM artists ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var artists []Artist
	for rows.Next() {
		var a Artist
		if err := rows.Scan(&a.LibraryID, &a.CatalogID, &a.Name, &a.Href, &a.LastSeen); err != nil {
			return nil, err
		}
		artists = append(artists, a)
	}
	return artists, rows.Err()
}

func (s *Store) UpsertArtistSource(src ArtistSource) error {
	_, err := s.db.Exec(`INSERT INTO artist_sources (catalog_id, source_type, source_id, added_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (catalog_id, source_type, source_id) DO UPDATE SET added_at = excluded.added_at`,
		src.CatalogID, src.SourceType, src.SourceID, src.AddedAt)
	return err
}

func (s *Store) ListArtistSources() ([]ArtistSource, error) {
	rows, err := s.db.Query("SELECT catalog_id, source_type, source_id, added_at FROM artist_sources")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sources []ArtistSource
	for rows.Next() {
		var src ArtistSource
		if err := rows.Scan(&src.CatalogID, &src.SourceType, &src.SourceID, &src.AddedAt); err != nil {
			return nil, err
		}
		sources = append(sources, src)
	}
	return sources, rows.Err()
}

func (s *Store) CountArtists() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM artists").Scan(&count)
	return count, err
}

func (s *Store) GetScanState() (*ScanState, error) {
	var state ScanState
	err := s.db.QueryRow("SELECT last_scan, albums_found, albums_added FROM scan_state WHERE id = 1").Scan(
		&state.LastScan, &state.AlbumsFound, &state.AlbumsAdded)
	if err == sql.ErrNoRows {
		return &ScanState{}, nil
	}
	if err != nil {
		return nil, err
	}
	return &state, nil
}

func (s *Store) SetScanState(state *ScanState) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO scan_state (id, last_scan, albums_found, albums_added) VALUES (1, ?, ?, ?)`,
		state.LastScan, state.AlbumsFound, state.AlbumsAdded)
	return err
}

func (s *Store) MarkScanned(found, added int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`INSERT OR REPLACE INTO scan_state (id, last_scan, albums_found, albums_added) VALUES (1, ?, ?, ?)`,
		now, found, added)
	return err
}

func (s *Store) GetAuth() (*AuthTokens, error) {
	var auth AuthTokens
	err := s.db.QueryRow("SELECT developer_token, developer_exp, music_user_token FROM auth WHERE id = 1").Scan(
		&auth.DeveloperToken, &auth.DeveloperExp, &auth.MusicUserToken)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &auth, nil
}

func (s *Store) SetAuth(auth *AuthTokens) error {
	_, err := s.db.Exec(`INSERT INTO auth (id, developer_token, developer_exp, music_user_token) VALUES (1, ?, ?, ?)`,
		auth.DeveloperToken, auth.DeveloperExp, auth.MusicUserToken)
	return err
}

func (s *Store) IgnoreArtist(catalogID, name string) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO ignored_artists (catalog_id, name, ignored_at) VALUES (?, ?, ?)`,
		catalogID, name, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *Store) UnignoreArtist(catalogID string) error {
	_, err := s.db.Exec(`DELETE FROM ignored_artists WHERE catalog_id = ?`, catalogID)
	return err
}

func (s *Store) IsArtistIgnored(catalogID string) (bool, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM ignored_artists WHERE catalog_id = ?", catalogID).Scan(&count)
	return count > 0, err
}

func (s *Store) ListIgnoredArtists() ([]Artist, error) {
	rows, err := s.db.Query("SELECT catalog_id, name, '' AS library_id, '' AS href, ignored_at FROM ignored_artists ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var artists []Artist
	for rows.Next() {
		var a Artist
		if err := rows.Scan(&a.CatalogID, &a.Name, &a.LibraryID, &a.Href, &a.LastSeen); err != nil {
			return nil, err
		}
		artists = append(artists, a)
	}
	return artists, rows.Err()
}

func (s *Store) CountIgnoredArtists() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM ignored_artists").Scan(&count)
	return count, err
}

func (s *Store) GetIgnoredCatalogIDs() (map[string]bool, error) {
	rows, err := s.db.Query("SELECT catalog_id FROM ignored_artists")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}
