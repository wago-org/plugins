package api

import (
	"crypto/rand"
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/wago-org/registry-backend/internal/auth"
	"github.com/wago-org/registry-backend/internal/httpx"
)

// Device authorization grant (RFC 8628) for logging in a headless or remote
// machine — a CLI where the loopback browser redirect can't reach it. The CLI
// asks for a device+user code, shows the user a URL and short code to enter on
// any device, and polls until the request is approved, denied, or expires.
//
// Grants are ephemeral (minutes), so they live in memory rather than the Store.

const (
	deviceCodeTTL   = 15 * time.Minute
	devicePollEvery = 5 // seconds the CLI should wait between polls (RFC 8628 interval)
)

// userCodeAlphabet omits easily-confused characters (0/O, 1/I) so a code is safe
// to read aloud and type.
const userCodeAlphabet = "BCDFGHJKLMNPQRSTVWXYZ23456789"

// deviceGrant is one in-flight authorization, keyed by its opaque device_code.
type deviceGrant struct {
	userCode  string
	expiresAt time.Time
	interval  int
	lastPoll  time.Time // for slow_down enforcement

	approved bool
	denied   bool
	token    string // plaintext API token, handed to the CLI on its next poll
}

// deviceAuth holds the live grants and the user_code → device_code index.
type deviceAuth struct {
	mu       sync.Mutex
	byDevice map[string]*deviceGrant
	byUser   map[string]string
}

func newDeviceAuth() *deviceAuth {
	return &deviceAuth{
		byDevice: map[string]*deviceGrant{},
		byUser:   map[string]string{},
	}
}

// gc drops expired grants. Callers hold d.mu.
func (d *deviceAuth) gc(now time.Time) {
	for dc, g := range d.byDevice {
		if now.After(g.expiresAt) {
			delete(d.byUser, g.userCode)
			delete(d.byDevice, dc)
		}
	}
}

// selfBase returns the backend's own public origin (scheme://host), derived from
// the configured OAuth redirect URL, falling back to the request. The device
// verification page is served by this backend, so links must point back to it.
func (a *App) selfBase(r *http.Request) string {
	if a.Cfg.OAuthRedirectURL != "" {
		if u, err := url.Parse(a.Cfg.OAuthRedirectURL); err == nil && u.Host != "" {
			return u.Scheme + "://" + u.Host
		}
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// randomUserCode returns a grouped human code like "BCDF-GH23".
func randomUserCode() (string, error) {
	const n = 8
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, 0, n+1)
	for i := 0; i < n; i++ {
		if i == n/2 {
			out = append(out, '-')
		}
		out = append(out, userCodeAlphabet[int(b[i])%len(userCodeAlphabet)])
	}
	return string(out), nil
}

// normalizeUserCode canonicalizes user input (case, spaces, missing dash) to the
// stored "XXXX-XXXX" form. Returns "" when the input has the wrong length.
func normalizeUserCode(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	var b strings.Builder
	for _, c := range s {
		if strings.IndexRune(userCodeAlphabet, c) >= 0 {
			b.WriteRune(c)
		}
	}
	t := b.String()
	if len(t) != 8 {
		return ""
	}
	return t[:4] + "-" + t[4:]
}

// handleDeviceCode (POST /api/device/code) mints a device+user code pair.
func (a *App) handleDeviceCode(w http.ResponseWriter, r *http.Request) {
	deviceCode, err := auth.RandomToken(32)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "code generation failed")
		return
	}
	userCode, err := randomUserCode()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "code generation failed")
		return
	}
	now := time.Now()
	g := &deviceGrant{
		userCode:  userCode,
		expiresAt: now.Add(deviceCodeTTL),
		interval:  devicePollEvery,
	}
	a.Device.mu.Lock()
	a.Device.gc(now)
	a.Device.byDevice[deviceCode] = g
	a.Device.byUser[userCode] = deviceCode
	a.Device.mu.Unlock()

	verify := a.selfBase(r) + "/device"
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"device_code":               deviceCode,
		"user_code":                 userCode,
		"verification_uri":          verify,
		"verification_uri_complete": verify + "?code=" + url.QueryEscape(userCode),
		"expires_in":                int(deviceCodeTTL / time.Second),
		"interval":                  devicePollEvery,
	})
}

// handleDeviceToken (POST /api/device/token) is the CLI's poll endpoint. It
// returns {"token":...} once approved, or an RFC 8628 status in {"error":...}:
// authorization_pending, slow_down, access_denied, or expired_token.
func (a *App) handleDeviceToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceCode string `json:"device_code"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.DeviceCode == "" {
		httpx.WriteError(w, http.StatusBadRequest, "device_code required")
		return
	}
	now := time.Now()
	a.Device.mu.Lock()
	defer a.Device.mu.Unlock()

	g := a.Device.byDevice[req.DeviceCode]
	if g == nil || now.After(g.expiresAt) {
		if g != nil {
			delete(a.Device.byUser, g.userCode)
			delete(a.Device.byDevice, req.DeviceCode)
		}
		httpx.WriteError(w, http.StatusBadRequest, "expired_token")
		return
	}
	if g.denied {
		delete(a.Device.byUser, g.userCode)
		delete(a.Device.byDevice, req.DeviceCode)
		httpx.WriteError(w, http.StatusBadRequest, "access_denied")
		return
	}
	// Enforce the polling interval; a too-eager CLI is told to slow down.
	if !g.lastPoll.IsZero() && now.Sub(g.lastPoll) < time.Duration(g.interval)*time.Second {
		httpx.WriteError(w, http.StatusBadRequest, "slow_down")
		return
	}
	g.lastPoll = now
	if !g.approved {
		httpx.WriteError(w, http.StatusBadRequest, "authorization_pending")
		return
	}
	// Approved: hand over the token once, then consume the grant.
	token := g.token
	delete(a.Device.byUser, g.userCode)
	delete(a.Device.byDevice, req.DeviceCode)
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"token": token})
}

// handleDeviceApprove (POST /api/device/approve) is called by the verification
// page for a signed-in user. It approves (mints a token bound to the grant) or
// denies the device request identified by user_code.
func (a *App) handleDeviceApprove(w http.ResponseWriter, r *http.Request) {
	u := a.Sessions.CurrentUser(r)
	if u == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		UserCode string `json:"user_code"`
		Action   string `json:"action"` // "approve" (default) or "deny"
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	code := normalizeUserCode(req.UserCode)
	if code == "" {
		httpx.WriteError(w, http.StatusBadRequest, "enter the 8-character code shown in your terminal")
		return
	}

	now := time.Now()
	a.Device.mu.Lock()
	a.Device.gc(now)
	g := a.grantByUserCode(code)
	if g == nil {
		a.Device.mu.Unlock()
		httpx.WriteError(w, http.StatusNotFound, "that code is unknown or has expired")
		return
	}
	if g.approved || g.denied {
		a.Device.mu.Unlock()
		httpx.WriteError(w, http.StatusConflict, "that code has already been used")
		return
	}
	if req.Action == "deny" {
		g.denied = true
		a.Device.mu.Unlock()
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "denied"})
		return
	}
	a.Device.mu.Unlock()

	// Mint the token outside the lock (a Store write), then re-check the grant is
	// still pending before binding it.
	plaintext, _, err := a.Store.CreateToken(u.ID, "wago-cli (device)")
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "token creation failed")
		return
	}
	a.Device.mu.Lock()
	g = a.grantByUserCode(code)
	if g == nil || g.approved || g.denied || now.After(g.expiresAt) {
		a.Device.mu.Unlock()
		httpx.WriteError(w, http.StatusConflict, "that code is no longer valid")
		return
	}
	g.approved = true
	g.token = plaintext
	a.Device.mu.Unlock()
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "approved", "login": u.Login})
}

// grantByUserCode resolves a user code to its grant. Callers hold d.mu.
func (a *App) grantByUserCode(code string) *deviceGrant {
	dc, ok := a.Device.byUser[code]
	if !ok {
		return nil
	}
	return a.Device.byDevice[dc]
}

// handleDevicePage (GET /device) serves the self-contained approval page. It
// prefills the code from ?code= and posts to /api/device/approve (same-origin,
// so the session cookie authenticates the user). It's a plain page, not part of
// the SPA, so it works even where the frontend isn't deployed.
func (a *App) handleDevicePage(w http.ResponseWriter, r *http.Request) {
	code := html.EscapeString(r.URL.Query().Get("code"))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Not cacheable — the page reflects a specific pending code.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(strings.Replace(devicePageHTML, "{{CODE}}", code, 1)))
}

// devicePageHTML is the approval page. {{CODE}} is replaced with the (escaped)
// code from the query string.
const devicePageHTML = `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>wago — authorize device</title>
<style>
  :root{color-scheme:light dark}
  body{font-family:system-ui,-apple-system,Segoe UI,sans-serif;max-width:26rem;margin:0 auto;padding:3rem 1.25rem;line-height:1.5}
  h1{font-size:1.35rem;margin:0 0 .25rem}
  p{color:#666;margin:.25rem 0 1.25rem}
  label{display:block;font-size:.85rem;color:#666;margin-bottom:.4rem}
  input{width:100%;box-sizing:border-box;font:600 1.5rem/1 ui-monospace,SFMono-Regular,Menlo,monospace;
    letter-spacing:.15em;text-align:center;text-transform:uppercase;padding:.75rem;border:1px solid #bbb;border-radius:.6rem;background:transparent;color:inherit}
  .row{display:flex;gap:.6rem;margin-top:1.1rem}
  button{flex:1;font:600 1rem/1 inherit;padding:.8rem;border-radius:.6rem;border:1px solid transparent;cursor:pointer}
  .approve{background:#2563eb;color:#fff}
  .deny{background:transparent;border-color:#bbb;color:inherit}
  button:disabled{opacity:.5;cursor:default}
  .msg{margin-top:1.1rem;padding:.8rem 1rem;border-radius:.6rem;font-size:.95rem;display:none}
  .msg.show{display:block}
  .ok{background:#dcfce7;color:#166534}
  .err{background:#fee2e2;color:#991b1b}
  a{color:#2563eb}
</style></head><body>
<h1>Authorize a device</h1>
<p>Confirm the code shown in your terminal to sign that device in to your wago account.</p>
<label for="code">Device code</label>
<input id="code" autocomplete="off" autocapitalize="characters" spellcheck="false" value="{{CODE}}" placeholder="XXXX-XXXX">
<div class="row">
  <button class="approve" id="approve">Authorize</button>
  <button class="deny" id="deny">Reject</button>
</div>
<div class="msg" id="msg"></div>
<script>
  var msg = document.getElementById('msg');
  var approve = document.getElementById('approve');
  var deny = document.getElementById('deny');
  function show(kind, html){ msg.className = 'msg show ' + kind; msg.innerHTML = html; }
  function done(){ approve.disabled = true; deny.disabled = true; }
  function send(action){
    var code = document.getElementById('code').value.trim();
    if(!code){ show('err','Enter the code shown in your terminal.'); return; }
    approve.disabled = true; deny.disabled = true;
    fetch('/api/device/approve', {
      method:'POST', credentials:'same-origin',
      headers:{'Content-Type':'application/json'},
      body: JSON.stringify({ user_code: code, action: action })
    }).then(function(res){
      return res.json().then(function(b){ return { status: res.status, body: b }; });
    }).then(function(r){
      if(r.status === 401){
        show('err','You need to <a href="/auth/github/login">sign in with GitHub</a> first, then reopen this page.');
        return;
      }
      if(r.status !== 200){
        show('err', (r.body && r.body.error) || 'Something went wrong.');
        approve.disabled = false; deny.disabled = false;
        return;
      }
      if(r.body.status === 'denied'){ show('err','Request rejected. You can close this tab.'); done(); return; }
      show('ok','Device authorized' + (r.body.login ? ' as <b>' + r.body.login + '</b>' : '') + '. Return to your terminal — you can close this tab.');
      done();
    }).catch(function(){
      show('err','Network error. Please try again.');
      approve.disabled = false; deny.disabled = false;
    });
  }
  approve.addEventListener('click', function(){ send('approve'); });
  deny.addEventListener('click', function(){ send('deny'); });
</script>
</body></html>`
