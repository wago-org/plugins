// Package auth provides GitHub OAuth and signed-cookie sessions for the registry.
// It depends on config (for cookie flags and the HMAC secret), store (to resolve
// a session's user), and model.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/wago-org/registry-backend/internal/config"
	"github.com/wago-org/registry-backend/internal/model"
	"github.com/wago-org/registry-backend/internal/store"
)

const (
	// SessionCookieName holds the signed user session.
	SessionCookieName = "wago_session"
	// StateCookieName holds the short-lived OAuth CSRF state.
	StateCookieName = "wago_oauth_state"

	sessionTTL    = 30 * 24 * time.Hour
	oauthStateTTL = 10 * time.Minute
)

// Sessions issues and verifies signed-cookie sessions.
type Sessions struct {
	secret       []byte
	devMode      bool
	cookieDomain string
	store        store.Store
}

// NewSessions builds a Sessions from config and the backing store.
func NewSessions(cfg config.Config, st store.Store) *Sessions {
	return &Sessions{
		secret:       cfg.SessionSecret,
		devMode:      cfg.DevMode,
		cookieDomain: cfg.CookieDomain,
		store:        st,
	}
}

// payload is the JSON body signed into a cookie.
type payload struct {
	UID string `json:"uid"`
	Exp int64  `json:"exp"`
}

// sign produces "base64url(payload).base64url(HMAC-SHA256(payload))".
func sign(body, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(body) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// verify checks a token's signature and returns the raw payload.
func verify(token string, secret []byte) ([]byte, error) {
	var payloadPart, sigPart string
	for i := 0; i < len(token); i++ {
		if token[i] == '.' {
			payloadPart = token[:i]
			sigPart = token[i+1:]
			break
		}
	}
	if payloadPart == "" || sigPart == "" {
		return nil, errors.New("malformed token")
	}
	body, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return nil, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(sigPart)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, errors.New("bad signature")
	}
	return body, nil
}

// NewSessionCookie builds the signed session cookie for a user id.
func (s *Sessions) NewSessionCookie(uid string) *http.Cookie {
	body, _ := json.Marshal(payload{UID: uid, Exp: time.Now().Add(sessionTTL).Unix()})
	c := &http.Cookie{
		Name:     SessionCookieName,
		Value:    sign(body, s.secret),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   !s.devMode,
		Expires:  time.Now().Add(sessionTTL),
		MaxAge:   int(sessionTTL / time.Second),
	}
	if s.cookieDomain != "" {
		c.Domain = s.cookieDomain
	}
	return c
}

// ClearSessionCookie returns a cookie that expires the session immediately.
func (s *Sessions) ClearSessionCookie() *http.Cookie {
	c := &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   !s.devMode,
		MaxAge:   -1,
	}
	if s.cookieDomain != "" {
		c.Domain = s.cookieDomain
	}
	return c
}

// NewStateCookie builds the signed short-lived OAuth state cookie.
func (s *Sessions) NewStateCookie(state string) *http.Cookie {
	body, _ := json.Marshal(payload{UID: state, Exp: time.Now().Add(oauthStateTTL).Unix()})
	return &http.Cookie{
		Name:     StateCookieName,
		Value:    sign(body, s.secret),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   !s.devMode,
		MaxAge:   int(oauthStateTTL / time.Second),
	}
}

// CLICookieName carries the CLI-login loopback context (port|state) across the
// OAuth round-trip so the callback knows to mint a token and redirect locally.
const CLICookieName = "wago_cli"

// NewCLICookie signs the CLI-login context (an opaque "port|state" string).
func (s *Sessions) NewCLICookie(data string) *http.Cookie {
	body, _ := json.Marshal(payload{UID: data, Exp: time.Now().Add(oauthStateTTL).Unix()})
	return &http.Cookie{
		Name:     CLICookieName,
		Value:    sign(body, s.secret),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   !s.devMode,
		MaxAge:   int(oauthStateTTL / time.Second),
	}
}

// CLIContext returns the signed CLI-login context, or ("", false) if absent or
// invalid.
func (s *Sessions) CLIContext(r *http.Request) (string, bool) {
	c, err := r.Cookie(CLICookieName)
	if err != nil {
		return "", false
	}
	body, err := verify(c.Value, s.secret)
	if err != nil {
		return "", false
	}
	var p payload
	if err := json.Unmarshal(body, &p); err != nil || time.Now().Unix() > p.Exp {
		return "", false
	}
	return p.UID, true
}

// ClearCLICookie expires the CLI-login cookie.
func (s *Sessions) ClearCLICookie() *http.Cookie {
	return &http.Cookie{Name: CLICookieName, Path: "/", MaxAge: -1, Secure: !s.devMode, HttpOnly: true}
}

// ClearStateCookie expires the OAuth state cookie with the same security flags
// it was set with (HttpOnly, Secure in prod, SameSite=Lax).
func (s *Sessions) ClearStateCookie() *http.Cookie {
	return &http.Cookie{
		Name: StateCookieName, Path: "/", MaxAge: -1,
		Secure: !s.devMode, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	}
}

// VerifyState checks the OAuth state cookie against the state query parameter.
func (s *Sessions) VerifyState(r *http.Request, state string) bool {
	c, err := r.Cookie(StateCookieName)
	if err != nil {
		return false
	}
	body, err := verify(c.Value, s.secret)
	if err != nil {
		return false
	}
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return false
	}
	if time.Now().Unix() > p.Exp {
		return false
	}
	return p.UID == state && state != ""
}

// CurrentUser returns the authenticated user for a request, or nil when there is
// no valid session. It accepts either a session cookie (browser) or an
// "Authorization: Bearer <token>" API token (CLI / CI).
func (s *Sessions) CurrentUser(r *http.Request) *model.User {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		if u, ok := s.store.UserByToken(strings.TrimSpace(h[len("Bearer "):])); ok {
			return &u
		}
		return nil
	}
	c, err := r.Cookie(SessionCookieName)
	if err != nil {
		return nil
	}
	body, err := verify(c.Value, s.secret)
	if err != nil {
		return nil
	}
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil
	}
	if time.Now().Unix() > p.Exp {
		return nil
	}
	u, ok := s.store.GetUser(p.UID)
	if !ok {
		return nil
	}
	return &u
}

// RandomToken returns a URL-safe random string with n bytes of entropy.
func RandomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
