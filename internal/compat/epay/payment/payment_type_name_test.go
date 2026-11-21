package payment

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorixpay/internal/channelops"
	paymentmodel "gorixpay/internal/payment/model"

	"github.com/gin-gonic/gin"
)

// TestPaymentTypeNameUsesDisplayName pins the act=order / act=orders contract:
// upstream selects pre_type.name, so a client sees 支付宝 rather than the alipay
// code. An unknown code falls back to the code itself, which is what older
// clients parse.
func TestPaymentTypeNameUsesDisplayName(t *testing.T) {
	env := newAPITestEnv(t, "payment-type-name")
	if err := env.db.AutoMigrate(&channelops.PayType{}); err != nil {
		t.Fatal(err)
	}
	if err := env.db.Create(&channelops.PayType{
		ID: "paytype-alipay", Code: "alipay", Name: "支付宝",
		Status: channelops.StatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	handler := &APIHandler{db: env.db}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api.php?act=order", nil)

	if got := handler.paymentTypeName(ctx, "alipay"); got != "支付宝" {
		t.Fatalf("known code = %q want 支付宝", got)
	}
	if got := handler.paymentTypeName(ctx, "wxpay"); got != "wxpay" {
		t.Fatalf("unknown code = %q want the code itself", got)
	}
	if got := handler.paymentTypeName(ctx, ""); got != "" {
		t.Fatalf("empty code = %q want empty", got)
	}
}

// TestOrderPayloadCarriesBuyer keeps the buyer passthrough: upstream echoes
// pre_order.buyer, and a client that set sub_openid expects to see it back.
func TestOrderPayloadCarriesBuyer(t *testing.T) {
	env := newAPITestEnv(t, "order-buyer")
	buyer := "openid-123"
	order := env.createPaidOrder(t, "BUYER-1", 100)
	order.Buyer = &buyer
	if err := env.db.Model(&paymentmodel.Order{}).Where("id = ?", order.ID).
		Update("buyer", buyer).Error; err != nil {
		t.Fatal(err)
	}
	handler := &APIHandler{db: env.db}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api.php?act=order", nil)

	handler.writeOrder(ctx, order)
	if body := recorder.Body.String(); !strings.Contains(body, `"buyer":"openid-123"`) {
		t.Fatalf("payload = %s", body)
	}
}
