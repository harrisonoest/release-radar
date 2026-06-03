package db

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpen_Close(t *testing.T) {
	t.Run("opens and closes successfully", func(t *testing.T) {
		tmpDir := t.TempDir()
		store, err := Open(tmpDir)
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		if store == nil {
			t.Fatal("expected non-nil store")
		}
		if err := store.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})

	t.Run("creates directory if not exists", func(t *testing.T) {
		tmpDir := filepath.Join(t.TempDir(), "nonexistent", "subdir")
		store, err := Open(tmpDir)
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		if err := store.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})

	t.Run("uses in-memory database", func(t *testing.T) {
		store, err := Open("")
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		if err := store.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})
}

func TestReplaceArtists_ListArtists(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := Open(tmpDir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	t.Run("replaces artists", func(t *testing.T) {
		artists := []Artist{
			{
				LibraryID: "lib1",
				CatalogID: "cat1",
				Name:      "Artist One",
				Href:      "https://example.com/1",
				LastSeen:  time.Now().Format(time.RFC3339),
			},
			{
				LibraryID: "lib2",
				CatalogID: "cat2",
				Name:      "Artist Two",
				Href:      "https://example.com/2",
				LastSeen:  time.Now().Format(time.RFC3339),
			},
		}

		if err := store.ReplaceArtists(artists); err != nil {
			t.Fatalf("ReplaceArtists failed: %v", err)
		}

		listed, err := store.ListArtists()
		if err != nil {
			t.Fatalf("ListArtists failed: %v", err)
		}

		if len(listed) != 2 {
			t.Errorf("expected 2 artists, got %d", len(listed))
		}

		if listed[0].Name != "Artist One" {
			t.Errorf("expected Artist One, got %s", listed[0].Name)
		}

		if listed[1].Name != "Artist Two" {
			t.Errorf("expected Artist Two, got %s", listed[1].Name)
		}
	})

	t.Run("replaces all existing artists", func(t *testing.T) {
		newArtists := []Artist{
			{
				LibraryID: "lib3",
				CatalogID: "cat3",
				Name:      "Artist Three",
				Href:      "https://example.com/3",
				LastSeen:  time.Now().Format(time.RFC3339),
			},
		}

		if err := store.ReplaceArtists(newArtists); err != nil {
			t.Fatalf("ReplaceArtists failed: %v", err)
		}

		listed, err := store.ListArtists()
		if err != nil {
			t.Fatalf("ListArtists failed: %v", err)
		}

		if len(listed) != 1 {
			t.Errorf("expected 1 artist after replace, got %d", len(listed))
		}

		if listed[0].Name != "Artist Three" {
			t.Errorf("expected Artist Three, got %s", listed[0].Name)
		}
	})

	t.Run("returns empty list when no artists", func(t *testing.T) {
		tmpDir2 := t.TempDir()
		store2, err := Open(tmpDir2)
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		defer store2.Close()

		listed, err := store2.ListArtists()
		if err != nil {
			t.Fatalf("ListArtists failed: %v", err)
		}

		if len(listed) != 0 {
			t.Errorf("expected 0 artists, got %d", len(listed))
		}
	})
}

func TestCountArtists(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := Open(tmpDir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	count, err := store.CountArtists()
	if err != nil {
		t.Fatalf("CountArtists failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 artists, got %d", count)
	}

	artists := []Artist{
		{LibraryID: "lib1", CatalogID: "cat1", Name: "Artist 1", Href: "href1", LastSeen: time.Now().Format(time.RFC3339)},
		{LibraryID: "lib2", CatalogID: "cat2", Name: "Artist 2", Href: "href2", LastSeen: time.Now().Format(time.RFC3339)},
	}

	if err := store.ReplaceArtists(artists); err != nil {
		t.Fatalf("ReplaceArtists failed: %v", err)
	}

	count, err = store.CountArtists()
	if err != nil {
		t.Fatalf("CountArtists failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 artists, got %d", count)
	}
}

func TestScanState_GetSet(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := Open(tmpDir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	t.Run("returns empty state when not set", func(t *testing.T) {
		state, err := store.GetScanState()
		if err != nil {
			t.Fatalf("GetScanState failed: %v", err)
		}
		if state == nil {
			t.Fatal("expected non-nil state")
		}
		if state.LastScan != "" {
			t.Errorf("expected empty last_scan, got %s", state.LastScan)
		}
		if state.AlbumsFound != 0 {
			t.Errorf("expected 0 albums_found, got %d", state.AlbumsFound)
		}
		if state.AlbumsAdded != 0 {
			t.Errorf("expected 0 albums_added, got %d", state.AlbumsAdded)
		}
	})

	t.Run("sets and gets scan state", func(t *testing.T) {
		state := &ScanState{
			LastScan:    "2024-01-01T00:00:00Z",
			AlbumsFound: 10,
			AlbumsAdded: 5,
		}

		if err := store.SetScanState(state); err != nil {
			t.Fatalf("SetScanState failed: %v", err)
		}

		retrieved, err := store.GetScanState()
		if err != nil {
			t.Fatalf("GetScanState failed: %v", err)
		}

		if retrieved.LastScan != state.LastScan {
			t.Errorf("last_scan = %s, want %s", retrieved.LastScan, state.LastScan)
		}
		if retrieved.AlbumsFound != state.AlbumsFound {
			t.Errorf("albums_found = %d, want %d", retrieved.AlbumsFound, state.AlbumsFound)
		}
		if retrieved.AlbumsAdded != state.AlbumsAdded {
			t.Errorf("albums_added = %d, want %d", retrieved.AlbumsAdded, state.AlbumsAdded)
		}
	})

	t.Run("MarkScanned sets current time", func(t *testing.T) {
		err := store.MarkScanned(15, 8)
		if err != nil {
			t.Fatalf("MarkScanned failed: %v", err)
		}

		state, err := store.GetScanState()
		if err != nil {
			t.Fatalf("GetScanState failed: %v", err)
		}

		_, err = time.Parse(time.RFC3339, state.LastScan)
		if err != nil {
			t.Errorf("last_scan is not valid RFC3339: %s", state.LastScan)
		}

		if state.AlbumsFound != 15 {
			t.Errorf("albums_found = %d, want 15", state.AlbumsFound)
		}
		if state.AlbumsAdded != 8 {
			t.Errorf("albums_added = %d, want 8", state.AlbumsAdded)
		}
	})
}

func TestAuth_GetSet(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := Open(tmpDir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	t.Run("returns nil when not set", func(t *testing.T) {
		auth, err := store.GetAuth()
		if err != nil {
			t.Fatalf("GetAuth failed: %v", err)
		}
		if auth != nil {
			t.Errorf("expected nil auth, got %v", auth)
		}
	})

	t.Run("sets and gets auth tokens", func(t *testing.T) {
		auth := &AuthTokens{
			DeveloperToken: "dev-token-123",
			DeveloperExp:   "2024-12-31T00:00:00Z",
			MusicUserToken: "user-token-456",
		}

		if err := store.SetAuth(auth); err != nil {
			t.Fatalf("SetAuth failed: %v", err)
		}

		retrieved, err := store.GetAuth()
		if err != nil {
			t.Fatalf("GetAuth failed: %v", err)
		}

		if retrieved.DeveloperToken != auth.DeveloperToken {
			t.Errorf("developer_token = %s, want %s", retrieved.DeveloperToken, auth.DeveloperToken)
		}
		if retrieved.DeveloperExp != auth.DeveloperExp {
			t.Errorf("developer_exp = %s, want %s", retrieved.DeveloperExp, auth.DeveloperExp)
		}
		if retrieved.MusicUserToken != auth.MusicUserToken {
			t.Errorf("music_user_token = %s, want %s", retrieved.MusicUserToken, auth.MusicUserToken)
		}
	})
}

func TestIgnoredArtists(t *testing.T) {
	t.Run("ignore and check artist", func(t *testing.T) {
		tmpDir := t.TempDir()
		store, err := Open(tmpDir)
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		defer store.Close()

		err = store.IgnoreArtist("cat123", "Test Artist")
		if err != nil {
			t.Fatalf("IgnoreArtist failed: %v", err)
		}

		ignored, err := store.IsArtistIgnored("cat123")
		if err != nil {
			t.Fatalf("IsArtistIgnored failed: %v", err)
		}
		if !ignored {
			t.Error("expected artist to be ignored")
		}

		ignored, err = store.IsArtistIgnored("cat999")
		if err != nil {
			t.Fatalf("IsArtistIgnored failed: %v", err)
		}
		if ignored {
			t.Error("expected artist not to be ignored")
		}
	})

	t.Run("unignore artist", func(t *testing.T) {
		tmpDir := t.TempDir()
		store, err := Open(tmpDir)
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		defer store.Close()

		if err := store.IgnoreArtist("cat456", "Another Artist"); err != nil {
			t.Fatalf("IgnoreArtist failed: %v", err)
		}

		if err := store.UnignoreArtist("cat456"); err != nil {
			t.Fatalf("UnignoreArtist failed: %v", err)
		}

		ignored, err := store.IsArtistIgnored("cat456")
		if err != nil {
			t.Fatalf("IsArtistIgnored failed: %v", err)
		}
		if ignored {
			t.Error("expected artist not to be ignored after unignore")
		}
	})

	t.Run("list ignored artists", func(t *testing.T) {
		tmpDir := t.TempDir()
		store, err := Open(tmpDir)
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		defer store.Close()

		store.IgnoreArtist("cat1", "Z Artist")
		store.IgnoreArtist("cat2", "A Artist")
		store.IgnoreArtist("cat3", "M Artist")

		ignored, err := store.ListIgnoredArtists()
		if err != nil {
			t.Fatalf("ListIgnoredArtists failed: %v", err)
		}

		if len(ignored) != 3 {
			t.Errorf("expected 3 ignored artists, got %d", len(ignored))
		}

		if ignored[0].Name != "A Artist" {
			t.Errorf("first artist should be sorted by name: %s", ignored[0].Name)
		}

		if ignored[1].Name != "M Artist" {
			t.Errorf("second artist should be sorted by name: %s", ignored[1].Name)
		}

		if ignored[2].Name != "Z Artist" {
			t.Errorf("third artist should be sorted by name: %s", ignored[2].Name)
		}
	})

	t.Run("count ignored artists", func(t *testing.T) {
		tmpDir := t.TempDir()
		store, err := Open(tmpDir)
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		defer store.Close()

		store.IgnoreArtist("cat1", "Z Artist")
		store.IgnoreArtist("cat2", "A Artist")
		store.IgnoreArtist("cat3", "M Artist")

		count, err := store.CountIgnoredArtists()
		if err != nil {
			t.Fatalf("CountIgnoredArtists failed: %v", err)
		}
		if count != 3 {
			t.Errorf("expected 3 ignored artists, got %d", count)
		}
	})

	t.Run("get ignored catalog IDs", func(t *testing.T) {
		tmpDir := t.TempDir()
		store, err := Open(tmpDir)
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		defer store.Close()

		store.IgnoreArtist("cat1", "Z Artist")
		store.IgnoreArtist("cat2", "A Artist")
		store.IgnoreArtist("cat3", "M Artist")

		ids, err := store.GetIgnoredCatalogIDs()
		if err != nil {
			t.Fatalf("GetIgnoredCatalogIDs failed: %v", err)
		}

		if len(ids) != 3 {
			t.Errorf("expected 3 IDs, got %d", len(ids))
		}

		if !ids["cat1"] {
			t.Error("expected cat1 in IDs")
		}
		if !ids["cat2"] {
			t.Error("expected cat2 in IDs")
		}
		if !ids["cat3"] {
			t.Error("expected cat3 in IDs")
		}
	})
}

func TestMigration_FromJSON(t *testing.T) {
	t.Run("migrates artists from JSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		artists := []struct {
			ID        string `json:"id"`
			CatalogID string `json:"catalog_id"`
			Name      string `json:"name"`
			Href      string `json:"href"`
			LastSeen  string `json:"last_seen"`
		}{
			{
				ID:        "lib1",
				CatalogID: "cat1",
				Name:      "JSON Artist 1",
				Href:      "https://example.com/1",
				LastSeen:  "2024-01-01T00:00:00Z",
			},
			{
				ID:        "lib2",
				CatalogID: "cat2",
				Name:      "JSON Artist 2",
				Href:      "https://example.com/2",
				LastSeen:  "2024-01-02T00:00:00Z",
			},
		}

		artistsData, err := json.Marshal(map[string]interface{}{
			"artists": artists,
		})
		if err != nil {
			t.Fatalf("failed to marshal artists: %v", err)
		}

		artistsPath := filepath.Join(tmpDir, "artists.json")
		if err := os.WriteFile(artistsPath, artistsData, 0600); err != nil {
			t.Fatalf("failed to write artists.json: %v", err)
		}

		store, err := Open(tmpDir)
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		defer store.Close()

		listed, err := store.ListArtists()
		if err != nil {
			t.Fatalf("ListArtists failed: %v", err)
		}

		if len(listed) != 2 {
			t.Errorf("expected 2 artists after migration, got %d", len(listed))
		}

		if listed[0].Name != "JSON Artist 1" {
			t.Errorf("expected JSON Artist 1, got %s", listed[0].Name)
		}
	})

	t.Run("migrates scan_state from JSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		statePath := filepath.Join(tmpDir, "scan_state.json")
		stateData := map[string]interface{}{
			"last_scan":    "2024-01-15T00:00:00Z",
			"albums_found": 100,
			"albums_added": 25,
		}

		stateBytes, err := json.Marshal(stateData)
		if err != nil {
			t.Fatalf("failed to marshal state: %v", err)
		}

		if err := os.WriteFile(statePath, stateBytes, 0600); err != nil {
			t.Fatalf("failed to write scan_state.json: %v", err)
		}

		store, err := Open(tmpDir)
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		defer store.Close()

		storedState, err := store.GetScanState()
		if err != nil {
			t.Fatalf("GetScanState failed: %v", err)
		}

		if storedState.LastScan != "2024-01-15T00:00:00Z" {
			t.Errorf("last_scan = %s, want 2024-01-15T00:00:00Z", storedState.LastScan)
		}
		if storedState.AlbumsFound != 100 {
			t.Errorf("albums_found = %d, want 100", storedState.AlbumsFound)
		}
		if storedState.AlbumsAdded != 25 {
			t.Errorf("albums_added = %d, want 25", storedState.AlbumsAdded)
		}
	})

	t.Run("migrates auth from JSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		authPath := filepath.Join(tmpDir, "auth.json")
		authData := map[string]interface{}{
			"developer_token":  "jwt-token-xyz",
			"developer_exp":    "2024-12-31T00:00:00Z",
			"music_user_token": "user-token-abc",
		}

		authBytes, err := json.Marshal(authData)
		if err != nil {
			t.Fatalf("failed to marshal auth: %v", err)
		}

		if err := os.WriteFile(authPath, authBytes, 0600); err != nil {
			t.Fatalf("failed to write auth.json: %v", err)
		}

		store, err := Open(tmpDir)
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		defer store.Close()

		storedAuth, err := store.GetAuth()
		if err != nil {
			t.Fatalf("GetAuth failed: %v", err)
		}

		if storedAuth.DeveloperToken != "jwt-token-xyz" {
			t.Errorf("developer_token = %s, want jwt-token-xyz", storedAuth.DeveloperToken)
		}
		if storedAuth.DeveloperExp != "2024-12-31T00:00:00Z" {
			t.Errorf("developer_exp = %s, want 2024-12-31T00:00:00Z", storedAuth.DeveloperExp)
		}
		if storedAuth.MusicUserToken != "user-token-abc" {
			t.Errorf("music_user_token = %s, want user-token-abc", storedAuth.MusicUserToken)
		}
	})

	t.Run("does not migrate if artists already exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		store, err := Open(tmpDir)
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}

		artists := []Artist{
			{LibraryID: "lib999", CatalogID: "cat999", Name: "Pre-existing", Href: "href", LastSeen: time.Now().Format(time.RFC3339)},
		}
		if err := store.ReplaceArtists(artists); err != nil {
			t.Fatalf("ReplaceArtists failed: %v", err)
		}
		store.Close()

		newStore, err := Open(tmpDir)
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		defer newStore.Close()

		listed, err := newStore.ListArtists()
		if err != nil {
			t.Fatalf("ListArtists failed: %v", err)
		}

		if len(listed) != 1 {
			t.Errorf("expected 1 artist (not migrated), got %d", len(listed))
		}
	})

	t.Run("removes migrated JSON files", func(t *testing.T) {
		tmpDir := t.TempDir()

		artistsPath := filepath.Join(tmpDir, "artists.json")
		statePath := filepath.Join(tmpDir, "scan_state.json")
		authPath := filepath.Join(tmpDir, "auth.json")

		os.WriteFile(artistsPath, []byte(`{"artists":[{"id":"lib1","catalog_id":"cat1","name":"Test Artist","href":"href","last_seen":"2024-01-01"}]}`), 0600)
		os.WriteFile(statePath, []byte(`{}`), 0600)
		os.WriteFile(authPath, []byte(`{"music_user_token":"token"}`), 0600)

		_, err := Open(tmpDir)
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}

		if _, err := os.Stat(artistsPath); err == nil {
			t.Error("artists.json should be removed after migration")
		}
		if _, err := os.Stat(statePath); err == nil {
			t.Error("scan_state.json should be removed after migration")
		}
		if _, err := os.Stat(authPath); err != nil {
			t.Error("auth.json should NOT be removed")
		}
	})
}

func TestSchemaMigration(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("creates schema from scratch", func(t *testing.T) {
		store, err := Open(tmpDir)
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		defer store.Close()

		var count int
		err = store.db.QueryRow("SELECT COUNT(*) FROM artists").Scan(&count)
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
	})

	t.Run("migrates old schema with catalog_id as pk", func(t *testing.T) {
		store, err := Open(tmpDir)
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}

		oldSchema := `
			CREATE TABLE IF NOT EXISTS artists (
				catalog_id TEXT PRIMARY KEY,
				library_id TEXT,
				name TEXT NOT NULL,
				href TEXT NOT NULL,
				last_seen TEXT NOT NULL
			);
		`
		if _, err := store.db.Exec(oldSchema); err != nil {
			t.Fatalf("Failed to create old schema: %v", err)
		}

		if _, err := store.db.Exec(`INSERT INTO artists (catalog_id, library_id, name, href, last_seen) VALUES ('cat1', 'lib1', 'Test', 'href', '2024-01-01')`); err != nil {
			t.Fatalf("Failed to insert test data: %v", err)
		}

		store.Close()

		newStore, err := Open(tmpDir)
		if err != nil {
			t.Fatalf("Open after migration failed: %v", err)
		}
		defer newStore.Close()

		artists, err := newStore.ListArtists()
		if err != nil {
			t.Fatalf("ListArtists failed: %v", err)
		}

		if len(artists) != 1 {
			t.Errorf("expected 1 artist, got %d", len(artists))
		}

		if artists[0].CatalogID != "cat1" {
			t.Errorf("catalog_id = %s, want cat1", artists[0].CatalogID)
		}
		if artists[0].LibraryID != "lib1" {
			t.Errorf("library_id = %s, want lib1", artists[0].LibraryID)
		}
	})
}

func TestErrorHandling(t *testing.T) {
	t.Run("invalid database path", func(t *testing.T) {
		invalidPath := filepath.Join("/nonexistent", "path", "to", "db")
		_, err := Open(invalidPath)
		if err == nil {
			t.Error("expected error for invalid path")
		}
	})
}

func TestConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := Open(tmpDir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			artists := []Artist{
				{LibraryID: "lib" + string(rune(id+'A')), CatalogID: "cat" + string(rune(id+'A')), Name: "Concurrent Artist", Href: "href", LastSeen: time.Now().Format(time.RFC3339)},
			}
			store.ReplaceArtists(artists)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestArtistSources_UpsertList(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := Open(tmpDir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	src := ArtistSource{
		CatalogID:  "123",
		SourceType: "library_artists",
		SourceID:   "",
		AddedAt:    time.Now().Format(time.RFC3339),
	}
	if err := store.UpsertArtistSource(src); err != nil {
		t.Fatalf("UpsertArtistSource failed: %v", err)
	}

	list, err := store.ListArtistSources()
	if err != nil {
		t.Fatalf("ListArtistSources failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 source, got %d", len(list))
	}
	if list[0].CatalogID != "123" || list[0].SourceType != "library_artists" {
		t.Errorf("unexpected source: %+v", list[0])
	}

	src.AddedAt = time.Now().Add(time.Hour).Format(time.RFC3339)
	if err := store.UpsertArtistSource(src); err != nil {
		t.Fatalf("UpsertArtistSource (update) failed: %v", err)
	}
	list, _ = store.ListArtistSources()
	if len(list) != 1 {
		t.Errorf("expected still 1 source after update, got %d", len(list))
	}
}

func TestReleases_UpsertStateTransitions(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := Open(tmpDir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	now := time.Now().Format(time.RFC3339)
	rel := Release{
		AlbumID:         "album-1",
		CatalogArtistID: "123",
		ArtistName:      "Test Artist",
		Name:            "Test Album",
		ReleaseDate:     "2026-06-01",
		TrackCount:      10,
		State:           "seen",
		FirstSeenAt:     now,
	}
	if err := store.UpsertRelease(rel); err != nil {
		t.Fatalf("UpsertRelease failed: %v", err)
	}

	fetched, err := store.GetRelease("album-1")
	if err != nil {
		t.Fatalf("GetRelease failed: %v", err)
	}
	if fetched == nil {
		t.Fatal("expected release, got nil")
	}
	if fetched.State != "seen" {
		t.Errorf("expected state 'seen', got %q", fetched.State)
	}

	// Transition to added
	rel.State = "added"
	rel.AddedAt = now
	if err := store.UpsertRelease(rel); err != nil {
		t.Fatalf("UpsertRelease (added) failed: %v", err)
	}
	fetched, _ = store.GetRelease("album-1")
	if fetched.State != "added" || fetched.AddedAt != now {
		t.Errorf("expected state=added, added_at=%s, got state=%s, added_at=%s", now, fetched.State, fetched.AddedAt)
	}

	// GetRelease on non-existent returns nil, no error
	missing, err := store.GetRelease("does-not-exist")
	if err != nil {
		t.Errorf("expected no error for missing release, got %v", err)
	}
	if missing != nil {
		t.Errorf("expected nil for missing release, got %+v", missing)
	}
}

func TestReleases_SkippedIDs(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := Open(tmpDir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	now := time.Now().Format(time.RFC3339)
	for _, state := range []string{"seen", "added", "ignored"} {
		rel := Release{
			AlbumID:         "album-" + state,
			CatalogArtistID: "123",
			ArtistName:      "Test",
			Name:            "Test " + state,
			ReleaseDate:     "2026-06-01",
			State:           state,
			FirstSeenAt:     now,
		}
		if err := store.UpsertRelease(rel); err != nil {
			t.Fatalf("UpsertRelease %s failed: %v", state, err)
		}
	}

	skipped, err := store.SkippedReleaseIDs()
	if err != nil {
		t.Fatalf("SkippedReleaseIDs failed: %v", err)
	}
	if _, ok := skipped["album-seen"]; ok {
		t.Error("'seen' releases should NOT be in skip set")
	}
	if _, ok := skipped["album-added"]; !ok {
		t.Error("'added' releases should be in skip set")
	}
	if _, ok := skipped["album-ignored"]; !ok {
		t.Error("'ignored' releases should be in skip set")
	}
}

func TestReleases_ListByStateAndCounts(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := Open(tmpDir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	now := time.Now().Format(time.RFC3339)
	seed := []Release{
		{AlbumID: "a1", CatalogArtistID: "x", Name: "n1", State: "added", FirstSeenAt: now, ReleaseDate: "2026-01-01"},
		{AlbumID: "a2", CatalogArtistID: "x", Name: "n2", State: "ignored", FirstSeenAt: now, ReleaseDate: "2026-02-01"},
		{AlbumID: "a3", CatalogArtistID: "y", Name: "n3", State: "seen", FirstSeenAt: now, ReleaseDate: "2026-03-01"},
		{AlbumID: "a4", CatalogArtistID: "y", Name: "n4", State: "added", FirstSeenAt: now, ReleaseDate: "2026-04-01"},
	}
	for _, r := range seed {
		if err := store.UpsertRelease(r); err != nil {
			t.Fatalf("UpsertRelease %s: %v", r.AlbumID, err)
		}
	}

	added, err := store.ListReleasesByState("added")
	if err != nil {
		t.Fatalf("ListReleasesByState: %v", err)
	}
	if len(added) != 2 {
		t.Errorf("expected 2 added, got %d", len(added))
	}

	counts, err := store.CountReleasesByState()
	if err != nil {
		t.Fatalf("CountReleasesByState: %v", err)
	}
	wantCounts := map[string]int{"added": 2, "ignored": 1, "seen": 1}
	for state, want := range wantCounts {
		if counts[state] != want {
			t.Errorf("state %s: got %d, want %d", state, counts[state], want)
		}
	}
}

func TestReleases_UpdateStateOnly(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := Open(tmpDir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	now := time.Now().Format(time.RFC3339)
	rel := Release{
		AlbumID: "a1", CatalogArtistID: "x", Name: "n1",
		State: "seen", FirstSeenAt: now, ReleaseDate: "2026-01-01",
	}
	if err := store.UpsertRelease(rel); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateReleaseState("a1", "ignored"); err != nil {
		t.Fatal(err)
	}
	fetched, _ := store.GetRelease("a1")
	if fetched.State != "ignored" {
		t.Errorf("expected state 'ignored', got %q", fetched.State)
	}
}

func TestArtistSources_BackfillOnOpen(t *testing.T) {
	tmpDir := t.TempDir()

	// First open: seed with artists
	store, err := Open(tmpDir)
	if err != nil {
		t.Fatalf("first Open failed: %v", err)
	}
	if err := store.ReplaceArtists([]Artist{
		{LibraryID: "l1", CatalogID: "c1", Name: "Artist One", Href: "h1", LastSeen: time.Now().Format(time.RFC3339)},
		{LibraryID: "l2", CatalogID: "c2", Name: "Artist Two", Href: "h2", LastSeen: time.Now().Format(time.RFC3339)},
	}); err != nil {
		t.Fatal(err)
	}
	store.Close()

	// Second open: backfill should have run
	store, err = Open(tmpDir)
	if err != nil {
		t.Fatalf("second Open failed: %v", err)
	}
	defer store.Close()

	sources, err := store.ListArtistSources()
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 {
		t.Fatalf("expected 2 backfilled sources, got %d", len(sources))
	}
	for _, src := range sources {
		if src.SourceType != "library_artists" {
			t.Errorf("expected source_type 'library_artists', got %q", src.SourceType)
		}
		if src.SourceID != "" {
			t.Errorf("expected empty source_id, got %q", src.SourceID)
		}
	}

	// Third open: backfill must be idempotent (no duplicates)
	store.Close()
	store, err = Open(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	sources, _ = store.ListArtistSources()
	if len(sources) != 2 {
		t.Errorf("expected 2 sources after re-open, got %d", len(sources))
	}
	store.Close()
}
