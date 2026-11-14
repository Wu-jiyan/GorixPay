package oauth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	identityauth "gorixpay/internal/identity/auth"
)

func TestStateIsSingleUseAndExpires(t *testing.T) {
	fixture := newFixture(t, "oauth-state")
	ctx := context.Background()
	state, err := fixture.service.CreateState(ctx, "wechat", "/merchant")
	if err != nil {
		t.Fatal(err)
	}
	consumed, err := fixture.service.ConsumeState(ctx, "wechat", state)
	if err != nil || consumed.RedirectPath != "/merchant" {
		t.Fatalf("consume=%+v err=%v", consumed, err)
	}
	if _, err := fixture.service.ConsumeState(ctx, "wechat", state); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("replay err=%v", err)
	}
	expiring, err := fixture.service.CreateState(ctx, "wechat", "")
	if err != nil {
		t.Fatal(err)
	}
	fixture.advance(11 * time.Minute)
	if _, err := fixture.service.ConsumeState(ctx, "wechat", expiring); !errors.Is(err, ErrStateExpired) {
		t.Fatalf("expired err=%v", err)
	}
	if _, err := fixture.service.CreateState(ctx, "wechat", "https://evil.example"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("absolute redirect err=%v", err)
	}
	if _, err := fixture.service.CreateState(ctx, "github", "/merchant"); !errors.Is(err, ErrProviderUnknown) {
		t.Fatalf("unknown provider err=%v", err)
	}
	unconfigured := New(fixture.db, fixture.sessions, nil, "https://pay.example")
	if _, err := unconfigured.CreateState(ctx, "wechat", "/merchant"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("unconfigured err=%v", err)
	}
}

func TestCallbackSignsInBoundPrincipal(t *testing.T) {
	fixture := newFixture(t, "oauth-callback-bound")
	ctx := context.Background()
	binding := Binding{
		ID: "binding-1", PrincipalType: "merchant", PrincipalID: testMerchantID,
		Provider: "wechat", OpenID: fixture.provider.identity.OpenID,
		CreatedAt: fixture.clock.value, UpdatedAt: fixture.clock.value,
	}
	if err := fixture.db.Create(&binding).Error; err != nil {
		t.Fatal(err)
	}
	state, err := fixture.service.CreateState(ctx, "wechat", "/merchant")
	if err != nil {
		t.Fatal(err)
	}
	result, err := fixture.service.Callback(ctx, "wechat", "code-1", state, ClientMetadata{IPAddress: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Token == "" || result.PrincipalType != "merchant" || result.ExpiresAt.IsZero() {
		t.Fatalf("result=%+v", result)
	}
	session, err := fixture.sessions.Authenticate(ctx, result.Token)
	if err != nil || session.PrincipalID != testMerchantID {
		t.Fatalf("session=%+v err=%v", session, err)
	}
	if fixture.provider.lastCode != "code-1" {
		t.Fatalf("provider saw code=%q", fixture.provider.lastCode)
	}
	if _, err := fixture.service.Callback(ctx, "wechat", "code-1", state, ClientMetadata{}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("state replay err=%v", err)
	}
}

func TestCallbackReturnsBindTicketForUnknownIdentity(t *testing.T) {
	fixture := newFixture(t, "oauth-callback-unbound")
	ctx := context.Background()
	state, err := fixture.service.CreateState(ctx, "wechat", "/merchant")
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.service.Callback(ctx, "wechat", "code-1", state, ClientMetadata{})
	var notBound *NotBoundError
	if !errors.As(err, &notBound) || notBound.BindTicket == "" {
		t.Fatalf("err=%v", err)
	}
	payload, err := fixture.service.parseBindTicket(notBound.BindTicket)
	if err != nil || payload.Provider != "wechat" || payload.OpenID != fixture.provider.identity.OpenID {
		t.Fatalf("payload=%+v err=%v", payload, err)
	}
	fixture.provider.exchangeErr = ErrExchangeFailed
	state2, err := fixture.service.CreateState(ctx, "wechat", "/merchant")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Callback(ctx, "wechat", "code-2", state2, ClientMetadata{}); !errors.Is(err, ErrExchangeFailed) {
		t.Fatalf("exchange failure err=%v", err)
	}
}

func TestBindRequiresPasswordAndCreatesBinding(t *testing.T) {
	fixture := newFixture(t, "oauth-bind")
	ctx := context.Background()
	ticket, err := fixture.service.issueBindTicket("wechat", fixture.provider.identity)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Bind(ctx, ticket, testEmail, "wrong-password", ClientMetadata{}); !errors.Is(err, identityauth.ErrInvalidCredentials) {
		t.Fatalf("wrong password err=%v", err)
	}
	result, err := fixture.service.Bind(ctx, ticket, testEmail, testPassword, ClientMetadata{})
	if err != nil || result.Token == "" || result.PrincipalType != "merchant" {
		t.Fatalf("bind result=%+v err=%v", result, err)
	}
	var bindings []Binding
	if err := fixture.db.Find(&bindings).Error; err != nil || len(bindings) != 1 {
		t.Fatalf("bindings=%d err=%v", len(bindings), err)
	}
	if bindings[0].PrincipalID != testMerchantID || bindings[0].OpenID != fixture.provider.identity.OpenID {
		t.Fatalf("binding=%+v", bindings[0])
	}
	// Binding the same identity to the same merchant again is idempotent.
	if _, err := fixture.service.Bind(ctx, ticket, testEmail, testPassword, ClientMetadata{}); err != nil {
		t.Fatalf("re-bind err=%v", err)
	}
	// A second merchant cannot steal an identity that is already bound.
	fixture.createMerchant(t, "merchant-2", "other@example.com")
	if _, err := fixture.service.Bind(ctx, ticket, "other@example.com", testPassword, ClientMetadata{}); !errors.Is(err, ErrAlreadyBound) {
		t.Fatalf("steal binding err=%v", err)
	}
	expiredTicket, err := fixture.service.issueBindTicket("wechat", Identity{OpenID: "open-late"})
	if err != nil {
		t.Fatal(err)
	}
	fixture.advance(11 * time.Minute)
	if _, err := fixture.service.Bind(ctx, expiredTicket, testEmail, testPassword, ClientMetadata{}); !errors.Is(err, ErrBindTicketExpired) {
		t.Fatalf("expired ticket err=%v", err)
	}
	if _, err := fixture.service.Bind(ctx, "garbage", testEmail, testPassword, ClientMetadata{}); !errors.Is(err, ErrInvalidBindTicket) {
		t.Fatalf("invalid ticket err=%v", err)
	}
}

func TestBindingsListAndDelete(t *testing.T) {
	fixture := newFixture(t, "oauth-bindings")
	ctx := context.Background()
	now := fixture.clock.value
	for _, name := range []string{"wechat", "qq"} {
		binding := Binding{
			ID: "binding-" + name, PrincipalType: "merchant", PrincipalID: testMerchantID,
			Provider: name, OpenID: "open-" + name, CreatedAt: now, UpdatedAt: now,
		}
		if err := fixture.db.Create(&binding).Error; err != nil {
			t.Fatal(err)
		}
	}
	items, err := fixture.service.ListBindings(ctx, "merchant", testMerchantID)
	if err != nil || len(items) != 2 {
		t.Fatalf("items=%d err=%v", len(items), err)
	}
	if err := fixture.service.DeleteBinding(ctx, "merchant", testMerchantID, "wechat"); err != nil {
		t.Fatal(err)
	}
	items, err = fixture.service.ListBindings(ctx, "merchant", testMerchantID)
	if err != nil || len(items) != 1 || items[0].Provider != "qq" {
		t.Fatalf("items=%v err=%v", items, err)
	}
	if err := fixture.service.DeleteBinding(ctx, "merchant", testMerchantID, "wechat"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete again err=%v", err)
	}
}

func TestQRTicketLifecycle(t *testing.T) {
	fixture := newFixture(t, "oauth-qr")
	ctx := context.Background()
	ticket, err := fixture.service.CreateQRTicket(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Ticket == "" || !strings.Contains(ticket.SVG, "<svg") || !strings.Contains(ticket.SVG, "<rect") {
		t.Fatalf("ticket=%+v", ticket)
	}
	if !strings.Contains(ticket.SVG, "qr-login") && len(ticket.SVG) < 100 {
		t.Fatalf("svg too small: %d", len(ticket.SVG))
	}
	poll, err := fixture.service.PollQRTicket(ctx, ticket.Ticket, ClientMetadata{})
	if err != nil || poll.Status != statusPending {
		t.Fatalf("poll=%+v err=%v", poll, err)
	}
	if status, err := fixture.service.ScanQRTicket(ctx, ticket.Ticket); err != nil || status != statusScanned {
		t.Fatalf("scan=%q err=%v", status, err)
	}
	poll, err = fixture.service.PollQRTicket(ctx, ticket.Ticket, ClientMetadata{})
	if err != nil || poll.Status != statusScanned || poll.Token != "" {
		t.Fatalf("poll after scan=%+v err=%v", poll, err)
	}
	if err := fixture.service.ConfirmQRTicket(ctx, ticket.Ticket, "merchant", testMerchantID); err != nil {
		t.Fatal(err)
	}
	confirmed, err := fixture.service.PollQRTicket(ctx, ticket.Ticket, ClientMetadata{IPAddress: "10.0.0.1"})
	if err != nil || confirmed.Status != statusConfirmed || confirmed.Token == "" {
		t.Fatalf("confirmed=%+v err=%v", confirmed, err)
	}
	if _, err := fixture.sessions.Authenticate(ctx, confirmed.Token); err != nil {
		t.Fatalf("token invalid: %v", err)
	}
	second, err := fixture.service.PollQRTicket(ctx, ticket.Ticket, ClientMetadata{})
	if err != nil || second.Status != statusConfirmed || second.Token != "" {
		t.Fatalf("second poll=%+v err=%v", second, err)
	}
	if _, err := fixture.service.PollQRTicket(ctx, "missing", ClientMetadata{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing ticket err=%v", err)
	}
}

func TestQRTicketExpiryAndConfirm(t *testing.T) {
	fixture := newFixture(t, "oauth-qr-expiry")
	ctx := context.Background()
	ticket, err := fixture.service.CreateQRTicket(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.ConfirmQRTicket(ctx, ticket.Ticket, "merchant", testMerchantID); err != nil {
		t.Fatal(err)
	}
	fixture.advance(6 * time.Minute)
	poll, err := fixture.service.PollQRTicket(ctx, ticket.Ticket, ClientMetadata{})
	if err != nil || poll.Status != statusExpired || poll.Token != "" {
		t.Fatalf("expired poll=%+v err=%v", poll, err)
	}
	stale, err := fixture.service.CreateQRTicket(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fixture.advance(6 * time.Minute)
	if err := fixture.service.ConfirmQRTicket(ctx, stale.Ticket, "merchant", testMerchantID); !errors.Is(err, ErrExpired) {
		t.Fatalf("confirm expired err=%v", err)
	}
	if _, err := fixture.service.ScanQRTicket(ctx, stale.Ticket); err != nil {
		t.Fatalf("scan expired err=%v", err)
	}
}

func TestPurgeExpiredRemovesOldRecords(t *testing.T) {
	fixture := newFixture(t, "oauth-purge")
	ctx := context.Background()
	ticket, err := fixture.service.CreateQRTicket(ctx)
	if err != nil {
		t.Fatal(err)
	}
	state, err := fixture.service.CreateState(ctx, "wechat", "/merchant")
	if err != nil {
		t.Fatal(err)
	}
	fixture.advance(25 * time.Hour)
	removed, err := fixture.service.PurgeExpired(ctx)
	if err != nil || removed < 2 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	if _, err := fixture.service.PollQRTicket(ctx, ticket.Ticket, ClientMetadata{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("purged ticket err=%v", err)
	}
	if _, err := fixture.service.ConsumeState(ctx, "wechat", state); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("purged state err=%v", err)
	}
}
