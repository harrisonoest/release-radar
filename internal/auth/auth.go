package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/harrisonoest/release-radar/pkg/config"
	"github.com/harrisonoest/release-radar/pkg/db"
)

const (
	developerTokenTTL = 6 * 30 * 24 * time.Hour
	callbackPath      = "/callback"

	musickitAuthPage = `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Release Radar — Sign In</title></head>
<body style="font-family:-apple-system,BlinkMacSystemFont,sans-serif;text-align:center;padding-top:60px">
<h2>Release Radar</h2>
<p id="status">Loading MusicKit…</p>
<div id="manual" style="display:none;margin-top:20px">
  <p>If automatic token capture fails, open your browser devtools (F12), go to the
  <b>Application</b> tab → <b>Local Storage</b> → <b>localhost</b>,
  find the key <b>musicUserToken</b>, copy its value, and paste it below:</p>
  <input id="tokenInput" type="text" style="width:400px;padding:8px" placeholder="Paste music user token here…">
  <br><br>
  <button onclick="submitManual()" style="padding:8px 20px;cursor:pointer">Submit Token</button>
  <p id="manualStatus" style="color:#666"></p>
</div>
<script src="https://js-cdn.music.apple.com/musickit/v3/musickit.js"></script>
<script>
var callbackURL = '<<CALLBACK>>';

async function sendToken(token) {
  document.getElementById('status').textContent = 'Saving token…';
  var resp = await fetch(callbackURL, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ music_user_token: token })
  });
  if (!resp.ok) throw new Error('Server rejected token');
  document.getElementById('status').innerHTML = '&#10003; Signed in successfully.<br><small>You can close this page.</small>';
  document.getElementById('manual').style.display = 'none';
}

function showError(msg) {
  document.getElementById('status').innerHTML = '&#10007; ' + msg;
  document.getElementById('manual').style.display = 'block';
}

async function submitManual() {
  var token = document.getElementById('tokenInput').value.trim();
  if (!token) return;
  try { await sendToken(token); } catch(e) { showError(e.message); }
}

function getTokenFromStorage() {
  try {
    var t = localStorage.getItem('musicUserToken');
    if (t) return t;
    t = localStorage.getItem('MUSIC_USER_TOKEN');
    if (t) return t;
    for (var i=0; i<localStorage.length; i++) {
      var k = localStorage.key(i);
      if (/token/i.test(k)) {
        var v = localStorage.getItem(k);
        if (v && v.length > 20) return v;
      }
    }
  } catch(e) {}
  return null;
}

document.addEventListener('musickitloaded', async () => {
  try {
    var music = await MusicKit.configure({
      developerToken: '<<DEVTOKEN>>',
      app: { name: 'Release Radar', build: '1.0.0' }
    });

    document.getElementById('status').textContent = 'Signing in to Apple Music…';

    // Attempt authorize with timeout
    var authPromise = music.authorize();
    var timeout = new Promise((_, reject) => setTimeout(() => reject(new Error('Authorization timed out after 60s')), 60000));
    await Promise.race([authPromise, timeout]);

    var userToken = music.userToken || music.musicUserToken;

    if (!userToken) {
      userToken = getTokenFromStorage();
    }

    if (userToken) {
      await sendToken(userToken);
    } else {
      showError('Authorized but could not read token automatically.');
    }

  } catch(e) {
    var msg = e.message || String(e);

    // Check if we're authorized even though authorize() errored
    var stored = getTokenFromStorage();
    if (stored) {
      try { await sendToken(stored); } catch(err) {
        showError(msg + ' — ' + err.message);
      }
      return;
    }

    showError(msg);
  }
});
</script>
</body></html>`
)

type TokenCache struct {
	DeveloperToken string    `json:"developer_token"`
	DeveloperExp   time.Time `json:"developer_exp"`
	MusicUserToken string    `json:"music_user_token"`
}

type Authenticator struct {
	cfg      *config.Config
	store    *db.Store
	cacheDir string
}

func NewAuthenticator(cfg *config.Config) (*Authenticator, error) {
	return NewAuthenticatorWithCacheDir(cfg, "")
}

func NewAuthenticatorWithStore(cfg *config.Config, store *db.Store) (*Authenticator, error) {
	if cfg.Apple.TeamID == "" {
		return nil, fmt.Errorf("apple.team_id is not set in config")
	}
	if cfg.Apple.MusicKitKeyID == "" {
		return nil, fmt.Errorf("apple.musickit_key_id is not set in config")
	}
	if cfg.Apple.MusicKitKeyPath == "" {
		return nil, fmt.Errorf("apple.musickit_key_path is not set in config")
	}
	return &Authenticator{cfg: cfg, store: store}, nil
}

func NewAuthenticatorWithCacheDir(cfg *config.Config, cacheDir string) (*Authenticator, error) {
	if cfg.Apple.TeamID == "" {
		return nil, fmt.Errorf("apple.team_id is not set in config")
	}
	if cfg.Apple.MusicKitKeyID == "" {
		return nil, fmt.Errorf("apple.musickit_key_id is not set in config")
	}
	if cfg.Apple.MusicKitKeyPath == "" {
		return nil, fmt.Errorf("apple.musickit_key_path is not set in config")
	}
	return &Authenticator{cfg: cfg, cacheDir: cacheDir}, nil
}

func (a *Authenticator) Authenticate(ctx context.Context) error {
	devToken, err := a.DeveloperToken()
	if err != nil {
		return fmt.Errorf("failed to generate developer token: %w", err)
	}

	musicUserToken, err := a.startOAuthFlow(ctx, devToken)
	if err != nil {
		return fmt.Errorf("oauth flow failed: %w", err)
	}

	if a.store != nil {
		if err := a.store.SetAuth(&db.AuthTokens{
			DeveloperToken: devToken,
			DeveloperExp:   time.Now().Add(developerTokenTTL).UTC().Format(time.RFC3339),
			MusicUserToken: musicUserToken,
		}); err != nil {
			return fmt.Errorf("failed to cache tokens: %w", err)
		}
		return nil
	}

	cache := &TokenCache{
		DeveloperToken: devToken,
		DeveloperExp:   time.Now().Add(developerTokenTTL),
		MusicUserToken: musicUserToken,
	}

	if err := a.saveCache(cache); err != nil {
		return fmt.Errorf("failed to cache tokens: %w", err)
	}

	return nil
}

func (a *Authenticator) DeveloperToken() (string, error) {
	if a.store != nil {
		auth, err := a.store.GetAuth()
		if err == nil && auth != nil && auth.DeveloperToken != "" {
			exp, parseErr := time.Parse(time.RFC3339, auth.DeveloperExp)
			if parseErr == nil && time.Now().Before(exp) {
				return auth.DeveloperToken, nil
			}
		}
	} else {
		cached, err := a.loadCache()
		if err == nil && cached != nil && time.Now().Before(cached.DeveloperExp) {
			return cached.DeveloperToken, nil
		}
	}

	token, err := a.generateDeveloperToken()
	if err != nil {
		return "", err
	}

	return token, nil
}

func (a *Authenticator) MusicUserToken() (string, error) {
	if a.store != nil {
		auth, err := a.store.GetAuth()
		if err != nil {
			return "", err
		}
		if auth == nil || auth.MusicUserToken == "" {
			return "", fmt.Errorf("not authenticated; run `release-radar auth` first")
		}
		return auth.MusicUserToken, nil
	}

	cached, err := a.loadCache()
	if err != nil {
		return "", err
	}
	if cached == nil || cached.MusicUserToken == "" {
		return "", fmt.Errorf("not authenticated; run `release-radar auth` first")
	}
	return cached.MusicUserToken, nil
}

func (a *Authenticator) loadKey() (*ecdsa.PrivateKey, error) {
	keyBytes, err := os.ReadFile(a.cfg.Apple.MusicKitKeyPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read private key: %w", err)
	}

	block, _ := pem.Decode(keyBytes)
	if block == nil {
		return nil, fmt.Errorf("failed to parse PEM block from private key")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not an ECDSA key")
	}

	return ecKey, nil
}

func (a *Authenticator) generateDeveloperToken() (string, error) {
	ecKey, err := a.loadKey()
	if err != nil {
		return "", err
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iss": a.cfg.Apple.TeamID,
		"iat": now.Unix(),
		"exp": now.Add(developerTokenTTL).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = a.cfg.Apple.MusicKitKeyID

	signed, err := token.SignedString(ecKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign developer token: %w", err)
	}

	return signed, nil
}

func (a *Authenticator) startOAuthFlow(ctx context.Context, devToken string) (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("cannot start local server: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	callbackURL := fmt.Sprintf("http://localhost:%d%s", port, callbackPath)
	done := make(chan string, 1)
	errCh := make(chan error, 1)

	page := a.buildAuthPage(devToken, callbackURL)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page)
	})
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var body struct {
			MusicUserToken string `json:"music_user_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		if body.MusicUserToken == "" {
			http.Error(w, "missing music_user_token", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok"}`)
		done <- body.MusicUserToken
	})

	srv := &http.Server{Handler: mux}

	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	pageURL := fmt.Sprintf("http://localhost:%d", port)
	fmt.Printf("Opening browser for Apple Music sign-in…\n")
	fmt.Printf("If the browser does not open, visit: %s\n", pageURL)

	if err := openBrowser(pageURL); err != nil {
		fmt.Printf("Could not open browser automatically: %v\n", err)
	}

	select {
	case token := <-done:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
		return token, nil

	case err := <-errCh:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
		return "", err

	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
		return "", ctx.Err()

	case <-time.After(5 * time.Minute):
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
		return "", fmt.Errorf("authentication timed out after 5 minutes")
	}
}

func (a *Authenticator) buildAuthPage(devToken, callbackURL string) string {
	page := strings.Replace(musickitAuthPage, "<<DEVTOKEN>>", devToken, 1)
	page = strings.Replace(page, "<<CALLBACK>>", callbackURL, 1)
	return page
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "linux":
		return exec.Command("xdg-open", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("cmd", "/c", "start", url).Start()
	default:
		return fmt.Errorf("unsupported platform")
	}
}

func (a *Authenticator) cachePath() (string, error) {
	var dir string
	var err error
	if a.cacheDir != "" {
		dir = a.cacheDir
	} else {
		dir, err = config.ConfigDir()
		if err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("cannot create config directory: %w", err)
	}
	return filepath.Join(dir, "auth.json"), nil
}

func (a *Authenticator) loadCache() (*TokenCache, error) {
	path, err := a.cachePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cache TokenCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	return &cache, nil
}

func (a *Authenticator) saveCache(cache *TokenCache) error {
	path, err := a.cachePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
