package service

import (
	"context"
	"errors"
	"testing"
	"time"

	paymentmodel "gorixpay/internal/payment/model"
)

func TestFindOrderEnforcesMerchantOwnership(t *testing.T) {
	db := testDatabase(t, "find-order")
	service := New(db, time.Now)
	created, _, err := service.Create(context.Background(), CreateCommand{
		MerchantID: "merchant-1", OutTradeNo: "ORDER-1", PaymentType: "alipay",
		Name: "test", Amount: 100, NotifyURL: "https://merchant.example/notify",
	})
	if err != nil {
		t.Fatal(err)
	}
	found, err := service.Find(context.Background(), "merchant-1", created.TradeNo, "")
	if err != nil || found.ID != created.ID {
		t.Fatalf("find by trade no: %+v err=%v", found, err)
	}
	found, err = service.Find(context.Background(), "merchant-1", "", created.OutTradeNo)
	if err != nil || found.ID != created.ID {
		t.Fatalf("find by out trade no: %+v err=%v", found, err)
	}
	if _, err := service.Find(context.Background(), "merchant-2", created.TradeNo, ""); !errors.Is(err, ErrOrderNotFound) {
		t.Fatalf("cross merchant find error=%v", err)
	}
	if _, err := service.Find(context.Background(), "merchant-1", "", ""); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("empty find error=%v", err)
	}
}

func TestCloseOrderIsIdempotentAndRejectsPaidOrder(t *testing.T) {
	db := testDatabase(t, "close-order")
	service := New(db, time.Now)
	created, _, err := service.Create(context.Background(), CreateCommand{
		MerchantID: "merchant-1", OutTradeNo: "ORDER-1", PaymentType: "alipay",
		Name: "test", Amount: 100, NotifyURL: "https://merchant.example/notify",
	})
	if err != nil {
		t.Fatal(err)
	}
	closed, changed, err := service.Close(context.Background(), "merchant-1", created.TradeNo, "")
	if err != nil || !changed || closed.Status != paymentmodel.StatusClosed {
		t.Fatalf("close: %+v changed=%v err=%v", closed, changed, err)
	}
	closed, changed, err = service.Close(context.Background(), "merchant-1", created.TradeNo, "")
	if err != nil || changed || closed.Status != paymentmodel.StatusClosed {
		t.Fatalf("repeat close: %+v changed=%v err=%v", closed, changed, err)
	}

	paid, _, err := service.Create(context.Background(), CreateCommand{
		MerchantID: "merchant-1", OutTradeNo: "ORDER-2", PaymentType: "alipay",
		Name: "paid", Amount: 100, NotifyURL: "https://merchant.example/notify",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&paymentmodel.Order{}).Where("id = ?", paid.ID).Update("status", paymentmodel.StatusPaid).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Close(context.Background(), "merchant-1", paid.TradeNo, ""); !errors.Is(err, ErrOrderNotClosable) {
		t.Fatalf("paid close error=%v", err)
	}
}

func TestFindPublicOnlyReturnsOrderSafeFields(t *testing.T) {
	db := testDatabase(t, "public-order")
	service := New(db, time.Now)
	created, _, err := service.Create(context.Background(), CreateCommand{
		MerchantID: "merchant-1", OutTradeNo: "ORDER-1", PaymentType: "alipay",
		Name: "test", Amount: 100, NotifyURL: "https://private.example/notify", Attach: "private",
	})
	if err != nil {
		t.Fatal(err)
	}
	public, err := service.FindPublic(context.Background(), created.TradeNo)
	if err != nil || public.TradeNo != created.TradeNo || public.Name != "test" || public.Amount != 100 {
		t.Fatalf("public order: %+v err=%v", public, err)
	}
}
