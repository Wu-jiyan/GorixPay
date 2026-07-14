package model

import (
	"testing"
	"time"
)

func TestSessionActive(t *testing.T) {
	now := time.Now().UTC()
	active := Session{ExpiresAt: now.Add(time.Minute)}
	if !active.Active(now) {
		t.Fatal("valid session is inactive")
	}
	revokedAt := now.Add(-time.Second)
	active.RevokedAt = &revokedAt
	if active.Active(now) {
		t.Fatal("revoked session is active")
	}
	if (Session{ExpiresAt: now}).Active(now) {
		t.Fatal("expired session is active")
	}
}
