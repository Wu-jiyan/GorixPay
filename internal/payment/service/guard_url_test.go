package service

import (
	"context"
	"errors"
	"testing"

	paymentmodel "gorixpay/internal/payment/model"
)

// TestURLGuardRejectsInternalCallbackHosts covers the SSRF gate at its cheapest
// point: a merchant that registers notify_url=http://127.0.0.1/... is refused
// before any order row exists, instead of producing an order whose notification
// silently never leaves the box.
func TestURLGuardRejectsInternalCallbackHosts(t *testing.T) {
	db := testDatabase(t, "service-url-guard")
	service := New(db, nil).WithGuards(URLGuard{})

	for _, target := range []string{
		"http://127.0.0.1:8080/notify",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.7/notify",
		"http://localhost/notify",
	} {
		_, _, err := service.Create(context.Background(), CreateCommand{
			MerchantID: "merchant-1", OutTradeNo: "URL-" + target, PaymentType: "alipay", Name: "test",
			Amount: 100, NotifyURL: target,
		})
		var refused Rejection
		if !errors.As(err, &refused) || refused.Reason != URLGuardReason {
			t.Fatalf("notify_url %q: err=%v, want rejection %q", target, err, URLGuardReason)
		}
	}

	var count int64
	if err := db.Model(&paymentmodel.Order{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("orders persisted after rejection: count=%d err=%v", count, err)
	}

	order, created, err := service.Create(context.Background(), CreateCommand{
		MerchantID: "merchant-1", OutTradeNo: "URL-PUBLIC", PaymentType: "alipay", Name: "test",
		Amount: 100, NotifyURL: "https://merchant.example/notify", ReturnURL: "https://merchant.example/return",
	})
	if err != nil || !created || order.Amount != 100 {
		t.Fatalf("public target rejected: order=%+v created=%v err=%v", order, created, err)
	}
}

// TestURLGuardAllowPrivateKeepsSelfHostedWorking documents the operational
// switch: a same-host receiver is refused by default and accepted once the
// operator opts in.
func TestURLGuardAllowPrivateKeepsSelfHostedWorking(t *testing.T) {
	db := testDatabase(t, "service-url-guard-private")
	permissive := New(db, nil).WithGuards(URLGuard{AllowPrivate: true})
	order, created, err := permissive.Create(context.Background(), CreateCommand{
		MerchantID: "merchant-1", OutTradeNo: "URL-LAN", PaymentType: "alipay", Name: "test",
		Amount: 100, NotifyURL: "http://127.0.0.1:8081/notify",
	})
	if err != nil || !created {
		t.Fatalf("allowPrivate target rejected: created=%v err=%v", created, err)
	}
	if order.NotifyURL != "http://127.0.0.1:8081/notify" {
		t.Fatalf("notify_url = %q", order.NotifyURL)
	}
	// The metadata range stays refused even with the switch on.
	_, _, err = permissive.Create(context.Background(), CreateCommand{
		MerchantID: "merchant-1", OutTradeNo: "URL-METADATA", PaymentType: "alipay", Name: "test",
		Amount: 100, NotifyURL: "http://169.254.169.254/latest/meta-data/",
	})
	var refused Rejection
	if !errors.As(err, &refused) {
		t.Fatalf("metadata target err=%v, want rejection", err)
	}
}
