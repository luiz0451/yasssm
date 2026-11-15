package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

func loadTOTPKey(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := strings.ToUpper(strings.TrimRight(strings.TrimSpace(string(b)), "="))
	key, err := totpEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid base32 secret: %w", path, err)
	}
	if len(key) < 10 {
		return nil, fmt.Errorf("%s: secret too short", path)
	}
	return key, nil
}

// totpCode is RFC 6238 TOTP (HMAC-SHA1, 6 digits) for a 30s time-step counter.
func totpCode(key []byte, counter int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(counter))
	h := hmac.New(sha1.New, key)
	h.Write(msg[:])
	sum := h.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1_000_000)
}

// totp verifies codes with ±1 step of clock skew and rejects reuse of a step
// that already logged in (replay protection, in memory). It re-reads the
// secret file when its mtime changes, so `yasssm totp-init -force` takes
// effect on the next login without a restart.
type totp struct {
	path string // empty: fixed key (tests)
	mu   sync.Mutex
	key  []byte
	mod  time.Time
	last int64
}

func newTOTP(path string) (*totp, error) {
	t := &totp{path: path}
	if err := t.reload(); err != nil {
		return nil, err
	}
	return t, nil
}

// reload loads the secret if the file changed. Callers hold t.mu, except
// newTOTP before t is shared.
func (t *totp) reload() error {
	st, err := os.Stat(t.path)
	if err != nil {
		return err
	}
	if t.key != nil && st.ModTime().Equal(t.mod) {
		return nil
	}
	key, err := loadTOTPKey(t.path)
	if err != nil {
		return err
	}
	if t.key != nil {
		slog.Info("totp secret reloaded", "path", t.path)
	}
	t.key, t.mod, t.last = key, st.ModTime(), 0
	return nil
}

func (t *totp) verify(code string, now time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}
	c := now.Unix() / 30
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.path != "" {
		if err := t.reload(); err != nil {
			// Keep the key already loaded; a half-written file must not lock the user out.
			slog.Error("totp secret reload failed", "err", err)
		}
	}
	for _, n := range []int64{c - 1, c, c + 1} {
		if n <= t.last {
			continue
		}
		if hmac.Equal([]byte(totpCode(t.key, n)), []byte(code)) {
			t.last = n
			return true
		}
	}
	return false
}

type sessions struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]time.Time
}

func newSessions(ttl time.Duration) *sessions {
	return &sessions{ttl: ttl, m: map[string]time.Time{}}
}

func (s *sessions) create(now time.Time) string {
	b := make([]byte, 32)
	rand.Read(b)
	tok := hex.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, exp := range s.m {
		if now.After(exp) {
			delete(s.m, k)
		}
	}
	s.m[tok] = now.Add(s.ttl)
	return tok
}

func (s *sessions) valid(tok string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.m[tok]
	if ok && now.After(exp) {
		delete(s.m, tok)
		return false
	}
	return ok
}

func (s *sessions) delete(tok string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, tok)
}

// limiter locks login for lockFor after maxFails consecutive failures.
// One global counter: there is a single user.
type limiter struct {
	mu    sync.Mutex
	fails int
	until time.Time
}

const (
	maxFails = 5
	lockFor  = 5 * time.Minute
)

func (l *limiter) wait(now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Before(l.until) {
		return l.until.Sub(now)
	}
	return 0
}

func (l *limiter) fail(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fails++
	if l.fails >= maxFails {
		l.fails = 0
		l.until = now.Add(lockFor)
	}
}

func (l *limiter) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fails = 0
}
