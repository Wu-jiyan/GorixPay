package redisx

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestOpenWithoutAddressDisablesRedis(t *testing.T) {
	client, err := Open(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if client != nil {
		t.Fatalf("expected nil client, got %+v", client)
	}
	if client.Enabled() {
		t.Fatal("nil client reported as enabled")
	}
	ctx := context.Background()
	if ok, err := client.Once(ctx, "replay", time.Minute); err != nil || !ok {
		t.Fatalf("degraded Once: ok=%v err=%v", ok, err)
	}
	if ok, err := client.Allow(ctx, "start", 1, time.Minute); err != nil || !ok {
		t.Fatalf("degraded Allow: ok=%v err=%v", ok, err)
	}
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("degraded Ping: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("degraded Close: %v", err)
	}
}

func TestOpenReturnsErrorForUnreachableServer(t *testing.T) {
	server := miniredis.RunT(t)
	addr := server.Addr()
	server.Close()
	if _, err := Open(Config{Addr: addr}); err == nil {
		t.Fatal("connect to closed server succeeded")
	}
}

func TestOnceClaimsKeyOnlyOnce(t *testing.T) {
	server := miniredis.RunT(t)
	client, err := Open(Config{Addr: server.Addr()})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if !client.Enabled() {
		t.Fatal("client reported as disabled")
	}
	ctx := context.Background()
	claimed, err := client.Once(ctx, "replay:abc", 30*time.Second)
	if err != nil || !claimed {
		t.Fatalf("first claim: claimed=%v err=%v", claimed, err)
	}
	claimed, err = client.Once(ctx, "replay:abc", 30*time.Second)
	if err != nil || claimed {
		t.Fatalf("second claim: claimed=%v err=%v", claimed, err)
	}
	server.FastForward(31 * time.Second)
	claimed, err = client.Once(ctx, "replay:abc", 30*time.Second)
	if err != nil || !claimed {
		t.Fatalf("claim after ttl: claimed=%v err=%v", claimed, err)
	}
}

func TestOnceAppliesPrefix(t *testing.T) {
	server := miniredis.RunT(t)
	client, err := Open(Config{Addr: server.Addr(), Prefix: "gorixpay"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.Once(context.Background(), "replay:abc", time.Minute); err != nil {
		t.Fatal(err)
	}
	if !server.Exists("gorixpay:replay:abc") {
		t.Fatalf("prefixed key missing, keys=%v", server.Keys())
	}
}

func TestAllowEnforcesFixedWindow(t *testing.T) {
	server := miniredis.RunT(t)
	client, err := Open(Config{Addr: server.Addr()})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	for attempt := 1; attempt <= 3; attempt++ {
		allowed, err := client.Allow(ctx, "cashier:start:1|127.0.0.1", 3, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if !allowed {
			t.Fatalf("attempt %d rejected inside the window", attempt)
		}
	}
	allowed, err := client.Allow(ctx, "cashier:start:1|127.0.0.1", 3, time.Minute)
	if err != nil || allowed {
		t.Fatalf("attempt beyond limit: allowed=%v err=%v", allowed, err)
	}
	server.FastForward(61 * time.Second)
	allowed, err = client.Allow(ctx, "cashier:start:1|127.0.0.1", 3, time.Minute)
	if err != nil || !allowed {
		t.Fatalf("attempt after window: allowed=%v err=%v", allowed, err)
	}
}

func TestAllowRejectsNonPositiveLimit(t *testing.T) {
	server := miniredis.RunT(t)
	client, err := Open(Config{Addr: server.Addr()})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	allowed, err := client.Allow(context.Background(), "key", 0, time.Minute)
	if err != nil || allowed {
		t.Fatalf("zero limit: allowed=%v err=%v", allowed, err)
	}
}

func TestPingAndClose(t *testing.T) {
	server := miniredis.RunT(t)
	client, err := Open(Config{Addr: server.Addr()})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestKeyPrefixNormalization(t *testing.T) {
	client := &Client{prefix: "gorix"}
	if got := client.key("a"); got != "gorix:a" {
		t.Fatalf("key=%q", got)
	}
	client.prefix = "gorix:"
	if got := client.key("a"); got != "gorix:a" {
		t.Fatalf("key=%q", got)
	}
	client.prefix = ""
	if got := client.key("a"); got != "a" {
		t.Fatalf("key=%q", got)
	}
}
