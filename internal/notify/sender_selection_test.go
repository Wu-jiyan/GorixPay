package notify

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
)

// TestSuppressedSenderNeverPrintsTheBody pins the production behaviour: with no
// delivery channel configured and token logging switched off, the verification
// code and the reset token must not reach stdout. The log sender used to print
// the whole message, so the token leaked even with logTokens=false.
func TestSuppressedSenderNeverPrintsTheBody(t *testing.T) {
	var buffer bytes.Buffer
	sender := newSuppressedSender(&buffer)
	const token = "RESET-TOKEN-SECRET-VALUE"
	if err := sender.SendMail(context.Background(), "merchant@example.test", "找回密码", "您的重置链接：https://pay.example/reset?token="+token); err != nil {
		t.Fatal(err)
	}
	if err := sender.SendSMS(context.Background(), "13800001111", "验证码 123456"); err != nil {
		t.Fatal(err)
	}
	output := buffer.String()
	if strings.Contains(output, token) || strings.Contains(output, "123456") {
		t.Fatalf("suppressed sender leaked the message body: %q", output)
	}
	if !strings.Contains(output, "merchant@example.test") && !strings.Contains(output, "me***@example.test") {
		t.Fatalf("suppressed sender should still identify the recipient: %q", output)
	}
	if !strings.Contains(output, "no delivery channel configured") {
		t.Fatalf("suppressed sender should say why nothing was delivered: %q", output)
	}
}

// TestLogSenderIsOnlyUsedWhenTokenLoggingIsOn keeps the development path working
// and proves the production path selects the suppressed sender.
func TestLogSenderIsOnlyUsedWhenTokenLoggingIsOn(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{
		Driver: "sqlite", DSN: "file:notify-sender-choice?mode=memory&cache=shared",
	})
	if err != nil {
		t.Fatal(err)
	}
	development, err := NewService(Config{DB: db, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := development.sender.(*logSender); !ok || !development.logTokens {
		t.Fatalf("development sender = %T logTokens=%v, want *notify.logSender true", development.sender, development.logTokens)
	}
	production, err := NewService(Config{DB: db, Now: time.Now, LogTokens: boolPointer(false)})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := production.sender.(*suppressedSender); !ok || production.logTokens {
		t.Fatalf("production sender = %T logTokens=%v, want *notify.suppressedSender false", production.sender, production.logTokens)
	}
}

func boolPointer(value bool) *bool { return &value }
