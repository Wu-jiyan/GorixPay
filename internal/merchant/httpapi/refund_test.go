package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	financemodel "gorixpay/internal/finance/model"
	financeservice "gorixpay/internal/finance/service"
	"gorixpay/internal/funding"
	identityhttp "gorixpay/internal/identity/httpapi"
	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/identity/session"
	ledgerservice "gorixpay/internal/ledger/service"
	paymentmodel "gorixpay/internal/payment/model"
	paymentservice "gorixpay/internal/payment/service"

	"github.com/gin-gonic/gin"
)

func TestCreateRefundRejectsFundingOrdersWith409(t *testing.T) {
	_, db, _, _ := setupMerchant(t, "merchant-refund-funding")
	ctx := context.Background()
	sessions := session.NewService(db, time.Now)
	token, _, err := sessions.Create(ctx, identitymodel.PrincipalMerchant, "merchant-1", time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ledgerWriter := ledgerservice.New(db, time.Now)
	fundingService := funding.New(db, ledgerWriter, "https://gorix.example")
	recharge, err := fundingService.CreateRecharge(ctx, "merchant-1", 5000)
	if err != nil {
		t.Fatal(err)
	}
	var rechargeOrder paymentmodel.Order
	if err := db.Where("trade_no = ?", recharge.TradeNo).First(&rechargeOrder).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.Model(&paymentmodel.Order{}).Where("id = ?", rechargeOrder.ID).
		Updates(map[string]any{"status": paymentmodel.StatusPaid, "paid_at": now, "updated_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	finance := financeservice.New(db, ledgerWriter, time.Now)
	engine := gin.New()
	RegisterFinance(engine.Group("/api/v1/merchant", identityhttp.Authenticate(sessions)), finance)
	response := performJSON(t, engine, http.MethodPost, "/api/v1/merchant/refunds", token, map[string]any{
		"payment_order_id": rechargeOrder.ID, "out_refund_no": "RF-FUNDING-1", "amount": 1000, "reason": "want it back",
	})
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "refund_not_allowed" || !strings.Contains(body["message"].(string), "充值") {
		t.Fatalf("body=%v", body)
	}
	var refunds int64
	if err := db.Model(&financemodel.Refund{}).Count(&refunds).Error; err != nil || refunds != 0 {
		t.Fatalf("refund rows=%d err=%v", refunds, err)
	}
}

func TestCreateRefundStillAcceptsSaleOrders(t *testing.T) {
	_, db, _, _ := setupMerchant(t, "merchant-refund-sale")
	ctx := context.Background()
	sessions := session.NewService(db, time.Now)
	token, _, err := sessions.Create(ctx, identitymodel.PrincipalMerchant, "merchant-1", time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	order, _, err := paymentservice.New(db, time.Now).Create(ctx, paymentservice.CreateCommand{
		MerchantID: "merchant-1", OutTradeNo: "SALE-1", PaymentType: "alipay", Name: "sale", Amount: 5000,
		NotifyURL: "https://merchant.example/notify",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.Model(&paymentmodel.Order{}).Where("id = ?", order.ID).
		Updates(map[string]any{"status": paymentmodel.StatusPaid, "paid_at": now, "updated_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	ledgerWriter := ledgerservice.New(db, time.Now)
	engine := gin.New()
	RegisterFinance(engine.Group("/api/v1/merchant", identityhttp.Authenticate(sessions)), financeservice.New(db, ledgerWriter, time.Now))
	response := performJSON(t, engine, http.MethodPost, "/api/v1/merchant/refunds", token, map[string]any{
		"payment_order_id": order.ID, "out_refund_no": "RF-SALE-1", "amount": 1000, "reason": "customer request",
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
