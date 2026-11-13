package service

import (
	"context"
	"errors"
	"testing"

	paymentmodel "gorixpay/internal/payment/model"
)

type stubGuard struct {
	err   error
	calls int
}

func (guard *stubGuard) Check(context.Context, CreateCommand) error {
	guard.calls++
	return guard.err
}

func TestGuardsRunBeforeOrderCreation(t *testing.T) {
	db := testDatabase(t, "service-guard")
	rejection := stubGuard{err: Rejection{Reason: "请先完成实名认证"}}
	service := New(db, nil).WithGuards(&rejection)
	_, _, err := service.Create(context.Background(), CreateCommand{
		MerchantID: "merchant-1", OutTradeNo: "GUARD-1", PaymentType: "alipay", Name: "test",
		Amount: 100, NotifyURL: "https://merchant.example/notify",
	})
	var refused Rejection
	if !errors.As(err, &refused) || refused.Reason != "请先完成实名认证" {
		t.Fatalf("err=%v", err)
	}
	if rejection.calls != 1 {
		t.Fatalf("guard calls=%d", rejection.calls)
	}
	var count int64
	if err := db.Model(&paymentmodel.Order{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("order persisted after rejection: count=%d err=%v", count, err)
	}

	passing := stubGuard{}
	allowed := New(db, nil).WithGuards(&passing)
	order, created, err := allowed.Create(context.Background(), CreateCommand{
		MerchantID: "merchant-1", OutTradeNo: "GUARD-2", PaymentType: "alipay", Name: "test",
		Amount: 100, NotifyURL: "https://merchant.example/notify",
	})
	if err != nil || !created || order.Amount != 100 {
		t.Fatalf("order=%+v created=%v err=%v", order, created, err)
	}
}
