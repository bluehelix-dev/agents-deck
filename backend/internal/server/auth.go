package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"os"
	"strings"
	"sync"
	"time"
)

// RandomToken returns 32 random bytes as hex.
func RandomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// LoadOrCreateSecret reads a 0600 secret file, creating it when missing.
func LoadOrCreateSecret(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) >= 32 {
		return strings.TrimSpace(string(b)), nil
	}
	return RotateSecret(path)
}

func RotateSecret(path string) (string, error) {
	t := RandomToken()
	return t, os.WriteFile(path, []byte(t+"\n"), 0o600)
}

func equal(a, b string) bool {
	return len(a) > 0 && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// loginCodes are one-time, 60-second codes that turn into a web session cookie.
// Each login gets its own random session token, kept here in memory for as
// long as the cookie lives (so a daemon restart means logging in again). A
// spent code is remembered for a while: if it comes back, someone else got to
// the link first, and the session it gave out is ended.
type loginCodes struct {
	mu       sync.Mutex
	codes    map[string]time.Time
	spent    map[string]spentCode // code -> the session it turned into
	sessions map[string]time.Time // session token -> expiry
}

type spentCode struct {
	session string
	until   time.Time
}

const (
	webSessionTTL = 30 * 24 * time.Hour // the cookie's Max-Age
	spentCodeTTL  = 10 * time.Minute    // how long a used code is recognised
)

// loginResult is what redeeming a login code came to.
type loginResult int

const (
	loginOK      loginResult = iota
	loginExpired             // unknown or too old
	loginReused              // used before: that login's session has been ended
)

func (l *loginCodes) issue() string { return l.issueFor(60 * time.Second) }

func (l *loginCodes) issueFor(ttl time.Duration) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.codes == nil {
		l.codes = map[string]time.Time{}
	}
	now := time.Now()
	l.prune(now)
	c := RandomToken()
	l.codes[c] = now.Add(ttl)
	return c
}

// prune drops expired codes, spent codes and sessions. Call with l.mu held.
func (l *loginCodes) prune(now time.Time) {
	for c, exp := range l.codes {
		if now.After(exp) {
			delete(l.codes, c)
		}
	}
	for c, sp := range l.spent {
		if now.After(sp.until) {
			delete(l.spent, c)
		}
	}
	for t, exp := range l.sessions {
		if now.After(exp) {
			delete(l.sessions, t)
		}
	}
}

// redeem turns a code into a new session token (256 random bits), once.
func (l *loginCodes) redeem(code string) (string, loginResult) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for c, sp := range l.spent {
		if equal(c, code) {
			delete(l.sessions, sp.session) // whoever came first is logged out
			sp.session = ""
			l.spent[c] = sp
			return "", loginReused
		}
	}
	for c, exp := range l.codes {
		if equal(c, code) {
			delete(l.codes, c)
			if now.After(exp) {
				return "", loginExpired
			}
			if l.spent == nil {
				l.spent = map[string]spentCode{}
			}
			if l.sessions == nil {
				l.sessions = map[string]time.Time{}
			}
			t := RandomToken()
			l.sessions[t] = now.Add(webSessionTTL)
			l.spent[c] = spentCode{session: t, until: now.Add(spentCodeTTL)}
			return t, loginOK
		}
	}
	return "", loginExpired
}

// session: token is a live session from a login.
func (l *loginCodes) session(token string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for t, exp := range l.sessions {
		if equal(t, token) {
			return now.Before(exp)
		}
	}
	return false
}
