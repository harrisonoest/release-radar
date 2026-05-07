package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/harrisonoest/release-radar/pkg/config"
)

func generateTestKey(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate test key: %v", err)
	}

	keyBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("failed to marshal key: %v", err)
	}

	tmpFile, err := os.CreateTemp("", "test-musickit-*.p8")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if _, err := tmpFile.Write(pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyBytes,
	})); err != nil {
		t.Fatalf("failed to write key: %v", err)
	}
	tmpFile.Close()
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })

	return key, tmpFile.Name()
}

func testConfig(keyPath string) *config.Config {
	return &config.Config{
		Apple: config.AppleConfig{
			TeamID:          "TESTTEAMID",
			MusicKitKeyID:   "TESTKEYID",
			MusicKitKeyPath: keyPath,
		},
	}
}

func newTestAuth(t *testing.T, cacheDir string) *Authenticator {
	t.Helper()
	_, keyPath := generateTestKey(t)
	a, err := NewAuthenticatorWithCacheDir(testConfig(keyPath), cacheDir)
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}
	return a
}

func TestNewAuthenticator_MissingConfig(t *testing.T) {
	tests := []struct {
		name   string
		cfg    *config.Config
		errMsg string
	}{
		{
			name:   "missing team id",
			cfg:    &config.Config{},
			errMsg: "apple.team_id",
		},
		{
			name: "missing key id",
			cfg: &config.Config{
				Apple: config.AppleConfig{TeamID: "TEAM"},
			},
			errMsg: "apple.musickit_key_id",
		},
		{
			name: "missing key path",
			cfg: &config.Config{
				Apple: config.AppleConfig{TeamID: "TEAM", MusicKitKeyID: "KEY"},
			},
			errMsg: "apple.musickit_key_path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewAuthenticatorWithCacheDir(tt.cfg, "")
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("error %q should contain %q", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestNewAuthenticator_ValidConfig(t *testing.T) {
	path, err := os.CreateTemp("", "valid-key-*.p8")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(path.Name())
	if _, err := path.WriteString("dummy"); err != nil {
		t.Fatalf("failed to write: %v", err)
	}
	path.Close()

	a, err := NewAuthenticatorWithCacheDir(testConfig(path.Name()), "")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if a == nil {
		t.Fatal("expected authenticator")
	}
}

func TestGenerateDeveloperToken(t *testing.T) {
	key, keyPath := generateTestKey(t)
	a := &Authenticator{cfg: testConfig(keyPath)}

	tokenStr, err := a.generateDeveloperToken()
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}
	if tokenStr == "" {
		t.Fatal("expected non-empty token string")
	}

	parsed, err := jwt.ParseWithClaims(tokenStr, &jwt.MapClaims{}, func(tok *jwt.Token) (interface{}, error) {
		if _, ok := tok.Method.(*jwt.SigningMethodECDSA); !ok {
			t.Fatalf("expected ECDSA signing method, got %v", tok.Method.Alg())
		}
		return &key.PublicKey, nil
	})
	if err != nil {
		t.Fatalf("failed to parse generated JWT: %v", err)
	}
	if !parsed.Valid {
		t.Fatal("generated JWT is not valid")
	}

	claims, ok := parsed.Claims.(*jwt.MapClaims)
	if !ok {
		t.Fatal("failed to get claims")
	}

	iss, _ := claims.GetIssuer()
	if iss != "TESTTEAMID" {
		t.Errorf("iss = %q, want %q", iss, "TESTTEAMID")
	}

	if kid, ok := parsed.Header["kid"]; !ok || kid != "TESTKEYID" {
		t.Errorf("kid = %v, want TESTKEYID", kid)
	}
}

func TestGenerateDeveloperToken_LastsSixMonths(t *testing.T) {
	_, keyPath := generateTestKey(t)
	a := &Authenticator{cfg: testConfig(keyPath)}

	tokenStr, err := a.generateDeveloperToken()
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	claims := jwt.MapClaims{}
	parser := jwt.NewParser()
	_, _, err = parser.ParseUnverified(tokenStr, &claims)
	if err != nil {
		t.Fatalf("failed to parse token: %v", err)
	}

	iat := time.Unix(int64(claims["iat"].(float64)), 0)
	exp := time.Unix(int64(claims["exp"].(float64)), 0)

	diff := exp.Sub(iat)
	expected := 6 * 30 * 24 * time.Hour
	if diff < expected-5*time.Second || diff > expected+5*time.Second {
		t.Errorf("token TTL = %v, want ~%v", diff, expected)
	}
}

func TestCache_SaveAndLoad(t *testing.T) {
	a := newTestAuth(t, t.TempDir())

	cache := &TokenCache{
		DeveloperToken: "test-dev-token",
		DeveloperExp:   time.Now().Add(time.Hour),
		MusicUserToken: "test-user-token",
	}

	if err := a.saveCache(cache); err != nil {
		t.Fatalf("saveCache failed: %v", err)
	}

	loaded, err := a.loadCache()
	if err != nil {
		t.Fatalf("loadCache failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil cache")
	}
	if loaded.DeveloperToken != "test-dev-token" {
		t.Errorf("developer token = %q, want test-dev-token", loaded.DeveloperToken)
	}
	if loaded.MusicUserToken != "test-user-token" {
		t.Errorf("music user token = %q, want test-user-token", loaded.MusicUserToken)
	}
}

func TestCache_LoadNonExistent(t *testing.T) {
	a := newTestAuth(t, t.TempDir())

	cache, err := a.loadCache()
	if err != nil {
		t.Fatalf("loadCache should not error for missing file: %v", err)
	}
	if cache != nil {
		t.Error("expected nil cache for non-existent file")
	}
}

func TestMusicUserToken_NotAuthenticated(t *testing.T) {
	a := newTestAuth(t, t.TempDir())

	_, err := a.MusicUserToken()
	if err == nil {
		t.Fatal("expected error for missing music user token")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("error %q should mention authentication", err.Error())
	}
}

func TestBuildAuthPage(t *testing.T) {
	a := &Authenticator{}
	page := a.buildAuthPage("test-token", "http://localhost:1234/callback")

	if !strings.Contains(page, "test-token") {
		t.Error("page should contain the developer token")
	}
	if !strings.Contains(page, "http://localhost:1234/callback") {
		t.Error("page should contain the callback URL")
	}
	if strings.Contains(page, "<<DEVTOKEN>>") {
		t.Error("page should not contain placeholder <<DEVTOKEN>>")
	}
	if strings.Contains(page, "<<CALLBACK>>") {
		t.Error("page should not contain placeholder <<CALLBACK>>")
	}
	if !strings.Contains(page, "musickit.js") {
		t.Error("page should load MusicKit JS")
	}
}

func TestDeveloperToken_UsesCachedWhenValid(t *testing.T) {
	_, keyPath := generateTestKey(t)
	a, err := NewAuthenticatorWithCacheDir(testConfig(keyPath), t.TempDir())
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}

	cache := &TokenCache{
		DeveloperToken: "cached-token",
		DeveloperExp:   time.Now().Add(time.Hour),
		MusicUserToken: "cached-user-token",
	}
	if err := a.saveCache(cache); err != nil {
		t.Fatalf("failed to save cache: %v", err)
	}

	token, err := a.DeveloperToken()
	if err != nil {
		t.Fatalf("DeveloperToken failed: %v", err)
	}
	if token != "cached-token" {
		t.Errorf("DeveloperToken = %q, want cached-token", token)
	}
}

func TestDeveloperToken_RegeneratesWhenExpired(t *testing.T) {
	_, keyPath := generateTestKey(t)
	a, err := NewAuthenticatorWithCacheDir(testConfig(keyPath), t.TempDir())
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}

	cache := &TokenCache{
		DeveloperToken: "expired-token",
		DeveloperExp:   time.Now().Add(-time.Hour),
		MusicUserToken: "user-token",
	}
	if err := a.saveCache(cache); err != nil {
		t.Fatalf("failed to save cache: %v", err)
	}

	token, err := a.DeveloperToken()
	if err != nil {
		t.Fatalf("DeveloperToken failed: %v", err)
	}
	if token == "expired-token" {
		t.Error("should have regenerated expired token")
	}
	if token == "" {
		t.Error("expected non-empty token")
	}
}

func TestGenerateDeveloperToken_InvalidKey(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "bad-key-*.p8")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString("not-a-valid-key"); err != nil {
		t.Fatalf("failed to write: %v", err)
	}
	tmpFile.Close()

	a := &Authenticator{cfg: testConfig(tmpFile.Name())}
	_, err = a.generateDeveloperToken()
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
}
