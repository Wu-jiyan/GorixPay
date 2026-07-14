package callback

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	channelconfig "gorixpay/internal/channel/config"
	channelrouter "gorixpay/internal/channel/router"
	"gorixpay/internal/compat/epay/signing"
	"gorixpay/internal/outbox"
	paymentmodel "gorixpay/internal/payment/model"
	"gorixpay/internal/platform/secret"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestEpayHandlerVerifiesAndCompletesPayment(t *testing.T) {
	db, order := callbackDatabase(t, "callback-http-success")
	configs := callbackChannel(t, db)
	router := gin.New()
	RegisterEpay(router, db, configs, New(db, time.Now))
	values := callbackValues(order.TradeNo, order.Amount, "UPSTREAM-1", "secret")
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/payment/callback/epay/channel-1", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != "success" {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	var updated paymentmodel.Order
	if err := db.First(&updated, "id = ?", order.ID).Error; err != nil || updated.Status != paymentmodel.StatusPaid {
		t.Fatalf("order=%+v err=%v", updated, err)
	}
	var count int64
	if err := db.Model(&outbox.Message{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("outbox count=%d err=%v", count, err)
	}
}

func TestEpayHandlerRejectsTamperedCallback(t *testing.T) {
	db, order := callbackDatabase(t, "callback-http-tampered")
	configs := callbackChannel(t, db)
	router := gin.New()
	RegisterEpay(router, db, configs, New(db, time.Now))
	values := callbackValues(order.TradeNo, order.Amount, "UPSTREAM-1", "secret")
	values.Set("money", "9.99")
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/payment/callback/epay/channel-1", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(response, request)
	if strings.TrimSpace(response.Body.String()) == "success" {
		t.Fatal("tampered callback accepted")
	}
	var updated paymentmodel.Order
	if err := db.First(&updated, "id = ?", order.ID).Error; err != nil || updated.Status != paymentmodel.StatusPending {
		t.Fatalf("order=%+v err=%v", updated, err)
	}
}

func callbackChannel(t *testing.T, db *gorm.DB) *channelconfig.Service {
	t.Helper()
	if err := db.AutoMigrate(&channelrouter.Channel{}); err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher([]byte("01234567890123456789012345678901"), "v1")
	if err != nil {
		t.Fatal(err)
	}
	configs := channelconfig.NewService(cipher)
	encrypted, err := configs.Encrypt("channel-1", channelconfig.Config{APIURL: "https://upstream.example", PID: "1001", Key: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	channel := channelrouter.Channel{ID: "channel-1", Name: "epay", PluginCode: "epay", PaymentType: "alipay", Priority: 1, Weight: 100, Status: channelrouter.StatusActive, ConfigEncrypted: encrypted}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	return configs
}

func callbackValues(tradeNo string, amount int64, upstreamTradeNo, key string) url.Values {
	params := map[string]any{
		"pid": "1001", "trade_no": upstreamTradeNo, "out_trade_no": tradeNo,
		"type": "alipay", "name": "test", "money": formatCallbackAmount(amount),
		"trade_status": "TRADE_SUCCESS", "sign_type": "MD5",
	}
	params["sign"] = signing.SignMD5(params, key)
	values := make(url.Values, len(params))
	for name, value := range params {
		values.Set(name, value.(string))
	}
	return values
}
