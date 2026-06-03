package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/harrisonoest/release-radar/internal/auth"
	"github.com/harrisonoest/release-radar/pkg/config"
)

func createTestKeyFile(t *testing.T) string {
	t.Helper()
	tmpFile, err := os.CreateTemp("", "test-key-*.p8")
	if err != nil {
		t.Fatalf("failed to create temp key file: %v", err)
	}
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })
	if _, err := tmpFile.WriteString("-----BEGIN PRIVATE KEY-----\nMC4CAQAwBQYDK2VwBCIEIMWJxk+R\n-----END PRIVATE KEY-----\n"); err != nil {
		t.Fatalf("failed to write key: %v", err)
	}
	tmpFile.Close()
	return tmpFile.Name()
}

func TestNewClient(t *testing.T) {
	keyPath := createTestKeyFile(t)
	cfg := &config.Config{
		Apple: config.AppleConfig{
			TeamID:          "TESTTEAMID",
			MusicKitKeyID:   "TESTKEYID",
			MusicKitKeyPath: keyPath,
		},
	}

	authenticator := &auth.Authenticator{}

	client, err := NewClient(cfg, authenticator)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestGetStorefront(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/me/storefront", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		response := map[string]interface{}{
			"data": []map[string]interface{}{
				{"id": "us", "type": "storefronts"},
			},
		}
		json.NewEncoder(w).Encode(response)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	cfg := &config.Config{
		Apple: config.AppleConfig{
			TeamID:          "TESTTEAMID",
			MusicKitKeyID:   "TESTKEYID",
			MusicKitKeyPath: createTestKeyFile(t),
		},
	}
	authenticator := &auth.Authenticator{}

	client, err := NewClient(cfg, authenticator)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	client.BaseURL = baseURL

	storefront, err := client.GetStorefront(context.Background())
	if err != nil {
		t.Fatalf("GetStorefront failed: %v", err)
	}
	if storefront != "us" {
		t.Errorf("storefront = %q, want us", storefront)
	}

	storefront2, err := client.GetStorefront(context.Background())
	if err != nil {
		t.Fatalf("second GetStorefront failed: %v", err)
	}
	if storefront2 != "us" {
		t.Errorf("cached storefront = %q, want us", storefront2)
	}
}

func TestGetStorefront_EmptyResponse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/me/storefront", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		response := map[string]interface{}{"data": []interface{}{}}
		json.NewEncoder(w).Encode(response)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	cfg := &config.Config{
		Apple: config.AppleConfig{
			TeamID:          "TESTTEAMID",
			MusicKitKeyID:   "TESTKEYID",
			MusicKitKeyPath: createTestKeyFile(t),
		},
	}
	authenticator := &auth.Authenticator{}

	client, err := NewClient(cfg, authenticator)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	client.BaseURL = baseURL

	_, err = client.GetStorefront(context.Background())
	if err == nil {
		t.Fatal("expected error for empty storefront response")
	}
}

func TestGetStorefront_HTTPError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/me/storefront", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	cfg := &config.Config{
		Apple: config.AppleConfig{
			TeamID:          "TESTTEAMID",
			MusicKitKeyID:   "TESTKEYID",
			MusicKitKeyPath: createTestKeyFile(t),
		},
	}
	authenticator := &auth.Authenticator{}

	client, err := NewClient(cfg, authenticator)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	client.BaseURL = baseURL

	_, err = client.GetStorefront(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestGetStorefront_ContextCancelled(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/me/storefront", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	cfg := &config.Config{
		Apple: config.AppleConfig{
			TeamID:          "TESTTEAMID",
			MusicKitKeyID:   "TESTKEYID",
			MusicKitKeyPath: createTestKeyFile(t),
		},
	}
	authenticator := &auth.Authenticator{}

	client, err := NewClient(cfg, authenticator)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	client.BaseURL = baseURL

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.GetStorefront(ctx)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestGetArtistAlbums_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/catalog/us/artists/123/albums", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		response := map[string]interface{}{
			"data": []map[string]interface{}{
				{"id": "1", "type": "albums", "attributes": map[string]interface{}{"name": "Album 1"}},
				{"id": "2", "type": "albums", "attributes": map[string]interface{}{"name": "Album 2"}},
			},
			"meta": map[string]interface{}{"total": 2},
		}
		json.NewEncoder(w).Encode(response)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	cfg := &config.Config{
		Apple: config.AppleConfig{
			TeamID:          "TESTTEAMID",
			MusicKitKeyID:   "TESTKEYID",
			MusicKitKeyPath: createTestKeyFile(t),
		},
	}
	authenticator := &auth.Authenticator{}

	client, err := NewClient(cfg, authenticator)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	client.BaseURL = baseURL

	result, err := client.GetArtistAlbums(context.Background(), "us", "123", time.Now())
	if err != nil {
		t.Fatalf("GetArtistAlbums failed: %v", err)
	}
	if len(result.Albums) != 2 {
		t.Errorf("albums count = %d, want 2", len(result.Albums))
	}
	if result.Total != 2 {
		t.Errorf("total = %d, want 2", result.Total)
	}
}

func TestGetArtistAlbums_Pagination(t *testing.T) {
	requestCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/catalog/us/artists/123/albums", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		requestCount++

		albums := []map[string]interface{}{
			{"id": "1", "type": "albums"},
			{"id": "2", "type": "albums"},
			{"id": "3", "type": "albums"},
			{"id": "4", "type": "albums"},
			{"id": "5", "type": "albums"},
		}
		total := 5

		response := map[string]interface{}{
			"data": albums,
			"meta": map[string]interface{}{"total": total},
		}
		json.NewEncoder(w).Encode(response)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	cfg := &config.Config{
		Apple: config.AppleConfig{
			TeamID:          "TESTTEAMID",
			MusicKitKeyID:   "TESTKEYID",
			MusicKitKeyPath: createTestKeyFile(t),
		},
	}
	authenticator := &auth.Authenticator{}

	client, err := NewClient(cfg, authenticator)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	client.BaseURL = baseURL

	result, err := client.GetArtistAlbums(context.Background(), "us", "123", time.Now())
	if err != nil {
		t.Fatalf("GetArtistAlbums failed: %v", err)
	}
	if len(result.Albums) != 5 {
		t.Errorf("albums count = %d, want 5", len(result.Albums))
	}
	if result.Total != 5 {
		t.Errorf("total = %d, want 5", result.Total)
	}
	if requestCount != 1 {
		t.Errorf("request count = %d, want 1", requestCount)
	}
}

func TestGetArtistAlbums_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/catalog/us/artists/123/albums", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	cfg := &config.Config{
		Apple: config.AppleConfig{
			TeamID:          "TESTTEAMID",
			MusicKitKeyID:   "TESTKEYID",
			MusicKitKeyPath: createTestKeyFile(t),
		},
	}
	authenticator := &auth.Authenticator{}

	client, err := NewClient(cfg, authenticator)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	client.BaseURL = baseURL

	result, err := client.GetArtistAlbums(context.Background(), "us", "123", time.Now())
	if err != nil {
		t.Fatalf("GetArtistAlbums should not error on 404: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Albums) != 0 {
		t.Errorf("expected empty albums, got %d", len(result.Albums))
	}
}

func TestGetArtistAlbums_RateLimited(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/catalog/us/artists/123/albums", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	cfg := &config.Config{
		Apple: config.AppleConfig{
			TeamID:          "TESTTEAMID",
			MusicKitKeyID:   "TESTKEYID",
			MusicKitKeyPath: createTestKeyFile(t),
		},
	}
	authenticator := &auth.Authenticator{}

	client, err := NewClient(cfg, authenticator)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	client.BaseURL = baseURL

	_, err = client.GetArtistAlbums(context.Background(), "us", "123", time.Now())
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
}

func TestGetArtistAlbums_HTTPError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/catalog/us/artists/123/albums", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	cfg := &config.Config{
		Apple: config.AppleConfig{
			TeamID:          "TESTTEAMID",
			MusicKitKeyID:   "TESTKEYID",
			MusicKitKeyPath: createTestKeyFile(t),
		},
	}
	authenticator := &auth.Authenticator{}

	client, err := NewClient(cfg, authenticator)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	client.BaseURL = baseURL

	_, err = client.GetArtistAlbums(context.Background(), "us", "123", time.Now())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestGetArtistAlbums_ContextCancelled(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/catalog/us/artists/123/albums", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	cfg := &config.Config{
		Apple: config.AppleConfig{
			TeamID:          "TESTTEAMID",
			MusicKitKeyID:   "TESTKEYID",
			MusicKitKeyPath: createTestKeyFile(t),
		},
	}
	authenticator := &auth.Authenticator{}

	client, err := NewClient(cfg, authenticator)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	client.BaseURL = baseURL

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.GetArtistAlbums(ctx, "us", "123", time.Now())
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestGetAllLibraryArtists_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/me/library/artists", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		response := map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id":   "1",
					"type": "library-artists",
					"attributes": map[string]interface{}{
						"name": "Artist 1",
					},
					"relationships": map[string]interface{}{
						"catalog": map[string]interface{}{
							"data": []map[string]interface{}{
								{"id": "catalog-1", "type": "artists"},
							},
						},
					},
				},
			},
			"meta": map[string]interface{}{"total": 1},
		}
		json.NewEncoder(w).Encode(response)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	cfg := &config.Config{
		Apple: config.AppleConfig{
			TeamID:          "TESTTEAMID",
			MusicKitKeyID:   "TESTKEYID",
			MusicKitKeyPath: createTestKeyFile(t),
		},
	}
	authenticator := &auth.Authenticator{}

	client, err := NewClient(cfg, authenticator)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	client.BaseURL = baseURL

	var progressCalls []int
	result, err := client.GetAllLibraryArtists(context.Background(), 25, func(done, total int) {
		progressCalls = append(progressCalls, done)
	})
	if err != nil {
		t.Fatalf("GetAllLibraryArtists failed: %v", err)
	}
	if len(result.Data) != 1 {
		t.Errorf("artists count = %d, want 1", len(result.Data))
	}
	if len(progressCalls) != 1 {
		t.Errorf("progress calls = %d, want 1", len(progressCalls))
	}
}

func TestGetAllLibraryArtists_Pagination(t *testing.T) {
	requestCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/me/library/artists", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		requestCount++
		offset := r.URL.Query().Get("offset")

		var artists []map[string]interface{}
		var total int

		if offset == "0" {
			artists = []map[string]interface{}{
				{
					"id":   "1",
					"type": "library-artists",
					"attributes": map[string]interface{}{
						"name": "Artist 1",
					},
					"relationships": map[string]interface{}{
						"catalog": map[string]interface{}{
							"data": []map[string]interface{}{
								{"id": "catalog-1", "type": "artists"},
							},
						},
					},
				},
				{
					"id":   "2",
					"type": "library-artists",
					"attributes": map[string]interface{}{
						"name": "Artist 2",
					},
					"relationships": map[string]interface{}{
						"catalog": map[string]interface{}{
							"data": []map[string]interface{}{
								{"id": "catalog-2", "type": "artists"},
							},
						},
					},
				},
			}
			total = 3
		} else {
			artists = []map[string]interface{}{
				{
					"id":   "3",
					"type": "library-artists",
					"attributes": map[string]interface{}{
						"name": "Artist 3",
					},
					"relationships": map[string]interface{}{
						"catalog": map[string]interface{}{
							"data": []map[string]interface{}{
								{"id": "catalog-3", "type": "artists"},
							},
						},
					},
				},
			}
		}

		response := map[string]interface{}{
			"data": artists,
			"meta": map[string]interface{}{"total": total},
			"next": nil,
		}
		json.NewEncoder(w).Encode(response)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	cfg := &config.Config{
		Apple: config.AppleConfig{
			TeamID:          "TESTTEAMID",
			MusicKitKeyID:   "TESTKEYID",
			MusicKitKeyPath: createTestKeyFile(t),
		},
	}
	authenticator := &auth.Authenticator{}

	client, err := NewClient(cfg, authenticator)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	client.BaseURL = baseURL

	result, err := client.GetAllLibraryArtists(context.Background(), 2, nil)
	if err != nil {
		t.Fatalf("GetAllLibraryArtists failed: %v", err)
	}
	if len(result.Data) != 3 {
		t.Errorf("artists count = %d, want 3", len(result.Data))
	}
	if requestCount != 2 {
		t.Errorf("request count = %d, want 2", requestCount)
	}
}

func TestGetAllLibraryArtists_RetryOn5xx(t *testing.T) {
	requestCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/me/library/artists", func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		if requestCount <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		response := map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id":   "1",
					"type": "library-artists",
					"attributes": map[string]interface{}{
						"name": "Artist 1",
					},
					"relationships": map[string]interface{}{
						"catalog": map[string]interface{}{
							"data": []map[string]interface{}{
								{"id": "catalog-1", "type": "artists"},
							},
						},
					},
				},
			},
			"meta": map[string]interface{}{"total": 1},
		}
		json.NewEncoder(w).Encode(response)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	cfg := &config.Config{
		Apple: config.AppleConfig{
			TeamID:          "TESTTEAMID",
			MusicKitKeyID:   "TESTKEYID",
			MusicKitKeyPath: createTestKeyFile(t),
		},
	}
	authenticator := &auth.Authenticator{}

	client, err := NewClient(cfg, authenticator)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	client.BaseURL = baseURL

	result, err := client.GetAllLibraryArtists(context.Background(), 25, nil)
	if err != nil {
		t.Fatalf("GetAllLibraryArtists failed: %v", err)
	}
	if len(result.Data) != 1 {
		t.Errorf("artists count = %d, want 1", len(result.Data))
	}
	if requestCount != 3 {
		t.Errorf("request count = %d, want 3 (2 retries)", requestCount)
	}
}

func TestGetAllLibraryArtists_RetryOn429(t *testing.T) {
	requestCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/me/library/artists", func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		if requestCount <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		response := map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id":   "1",
					"type": "library-artists",
					"attributes": map[string]interface{}{
						"name": "Artist 1",
					},
					"relationships": map[string]interface{}{
						"catalog": map[string]interface{}{
							"data": []map[string]interface{}{
								{"id": "catalog-1", "type": "artists"},
							},
						},
					},
				},
			},
			"meta": map[string]interface{}{"total": 1},
		}
		json.NewEncoder(w).Encode(response)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	cfg := &config.Config{
		Apple: config.AppleConfig{
			TeamID:          "TESTTEAMID",
			MusicKitKeyID:   "TESTKEYID",
			MusicKitKeyPath: createTestKeyFile(t),
		},
	}
	authenticator := &auth.Authenticator{}

	client, err := NewClient(cfg, authenticator)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	client.BaseURL = baseURL

	result, err := client.GetAllLibraryArtists(context.Background(), 25, nil)
	if err != nil {
		t.Fatalf("GetAllLibraryArtists failed: %v", err)
	}
	if len(result.Data) != 1 {
		t.Errorf("artists count = %d, want 1", len(result.Data))
	}
	if requestCount != 3 {
		t.Errorf("request count = %d, want 3 (2 retries)", requestCount)
	}
}

func TestGetAllLibraryArtists_RetryExhausted(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/me/library/artists", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	cfg := &config.Config{
		Apple: config.AppleConfig{
			TeamID:          "TESTTEAMID",
			MusicKitKeyID:   "TESTKEYID",
			MusicKitKeyPath: createTestKeyFile(t),
		},
	}
	authenticator := &auth.Authenticator{}

	client, err := NewClient(cfg, authenticator)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	client.BaseURL = baseURL

	_, err = client.GetAllLibraryArtists(context.Background(), 25, nil)
	if err == nil {
		t.Fatal("expected error after retry exhaustion")
	}
}

func TestGetAllLibraryArtists_ContextCancelled(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/me/library/artists", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	cfg := &config.Config{
		Apple: config.AppleConfig{
			TeamID:          "TESTTEAMID",
			MusicKitKeyID:   "TESTKEYID",
			MusicKitKeyPath: createTestKeyFile(t),
		},
	}
	authenticator := &auth.Authenticator{}

	client, err := NewClient(cfg, authenticator)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	client.BaseURL = baseURL

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.GetAllLibraryArtists(ctx, 25, nil)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestGetAllLibraryArtists_NoProgressCallback(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/me/library/artists", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		response := map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id":   "1",
					"type": "library-artists",
					"attributes": map[string]interface{}{
						"name": "Artist 1",
					},
					"relationships": map[string]interface{}{
						"catalog": map[string]interface{}{
							"data": []map[string]interface{}{
								{"id": "catalog-1", "type": "artists"},
							},
						},
					},
				},
			},
			"meta": map[string]interface{}{"total": 1},
		}
		json.NewEncoder(w).Encode(response)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	cfg := &config.Config{
		Apple: config.AppleConfig{
			TeamID:          "TESTTEAMID",
			MusicKitKeyID:   "TESTKEYID",
			MusicKitKeyPath: createTestKeyFile(t),
		},
	}
	authenticator := &auth.Authenticator{}

	client, err := NewClient(cfg, authenticator)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	client.BaseURL = baseURL

	result, err := client.GetAllLibraryArtists(context.Background(), 25, nil)
	if err != nil {
		t.Fatalf("GetAllLibraryArtists failed: %v", err)
	}
	if len(result.Data) != 1 {
		t.Errorf("artists count = %d, want 1", len(result.Data))
	}
}
