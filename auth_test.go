package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// RFC 6238 appendix B, SHA1 vectors (8 digits; a 6-digit code is the last 6).
func TestTOTPCodeRFC6238(t *testing.T) {
	key := []byte("12345678901234567890")
	cases := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	}
	for _, c := range cases {
		if got := totpCode(key, c.unix/30); got != c.want {
			t.Errorf("t=%d: got %s, want %s", c.unix, got, c.want)
		}
	}
}

func TestTOTPVerifyRejectsReplayAndAcceptsSkew(t *testing.T) {
	key := []byte("12345678901234567890")
	now := time.Unix(1234567890, 0)
	tp := &totp{key: key}

	prev := totpCode(key, now.Unix()/30-1)
	if !tp.verify(prev, now) {
		t.Fatal("code from previous step should be accepted")
	}
	if tp.verify(prev, now) {
		t.Fatal("reused code must be rejected")
	}
	cur := totpCode(key, now.Unix()/30)
	if !tp.verify(cur, now) {
		t.Fatal("current code should be accepted after an older step was used")
	}
	if tp.verify(totpCode(key, now.Unix()/30-2), now) {
		t.Fatal("code two steps old must be rejected")
	}
}

// A secret replaced with `totp-init -force` while the server runs must be
// used on the next login; the old one must stop working.
func TestTOTPReloadsReplacedSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "totp")
	oldKey, newKey := []byte("12345678901234567890"), []byte("abcdefghijabcdefghij")
	if err := os.WriteFile(path, []byte(totpEncoding.EncodeToString(oldKey)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tp, err := newTOTP(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1234567890, 0)

	if err := os.WriteFile(path, []byte(totpEncoding.EncodeToString(newKey)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if tp.verify(totpCode(oldKey, now.Unix()/30), now) {
		t.Fatal("code from the replaced secret must be rejected")
	}
	if !tp.verify(totpCode(newKey, now.Unix()/30), now) {
		t.Fatal("code from the new secret should be accepted without a restart")
	}
}
