package session

import (
	"context"
	"testing"
	"time"

	"gorixpay/internal/identity/model"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
)

func TestServiceCreatesAuthenticatesAndRevokesSession(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:session?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Session{}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	service := NewService(db, func() time.Time { return now })
	plain, created, err := service.Create(context.Background(), model.PrincipalMerchant, "merchant-1", time.Hour, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatal(err)
	}
	if plain == "" || created.TokenHash == plain || created.TokenHash != HashToken(plain) {
		t.Fatal("session token was not hashed")
	}
	authenticated, err := service.Authenticate(context.Background(), plain)
	if err != nil || authenticated.PrincipalID != "merchant-1" {
		t.Fatalf("authenticate: session=%+v err=%v", authenticated, err)
	}
	if err := service.Revoke(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(context.Background(), plain); err != ErrInvalidSession {
		t.Fatalf("revoked session error = %v", err)
	}
}

func TestServiceRejectsExpiredAndUnknownSession(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:expired-session?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Session{}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	service := NewService(db, func() time.Time { return now })
	plain, _, err := service.Create(context.Background(), model.PrincipalAdministrator, "admin-1", time.Minute, "", "")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := service.Authenticate(context.Background(), plain); err != ErrInvalidSession {
		t.Fatalf("expired session error = %v", err)
	}
	if _, err := service.Authenticate(context.Background(), "unknown"); err != ErrInvalidSession {
		t.Fatalf("unknown session error = %v", err)
	}
}
