package auth

import (
	"testing"
	"time"
)

func TestPassword(t *testing.T) {
	h, err := HashPassword("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "s3cret") {
		t.Fatal("correct password must verify")
	}
	if CheckPassword(h, "wrong") {
		t.Fatal("wrong password must fail")
	}
}

func TestSession(t *testing.T) {
	secret := []byte("server-secret")
	tok := SignSession(secret, "alice", "super_admin")
	u, r, ok := ParseSession(secret, tok)
	if !ok || u != "alice" || r != "super_admin" {
		t.Fatalf("roundtrip failed: %q %q %v", u, r, ok)
	}
	if _, _, ok := ParseSession(secret, tok+"x"); ok {
		t.Fatal("tampered token must fail")
	}
	if _, _, ok := ParseSession([]byte("other"), tok); ok {
		t.Fatal("wrong secret must fail")
	}
}

func TestSession_Expired(t *testing.T) {
	secret := []byte("server-secret")
	// 用负 TTL 签发一个已过期的 token，必须被拒。
	expired := signSession(secret, "alice", "user", -time.Hour)
	if _, _, ok := ParseSession(secret, expired); ok {
		t.Fatal("expired token must fail")
	}
	// 正常 TTL 的 token 仍有效。
	valid := signSession(secret, "alice", "user", time.Hour)
	if _, _, ok := ParseSession(secret, valid); !ok {
		t.Fatal("unexpired token must pass")
	}
}
