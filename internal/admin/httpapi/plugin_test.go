package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	channelconfig "gorixpay/internal/channel/config"
	channelrouter "gorixpay/internal/channel/router"
	paymentmodel "gorixpay/internal/payment/model"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/secret"
	pluginpayment "gorixpay/internal/plugin/payment"
	"gorixpay/internal/plugin/payment/alipay"
	"gorixpay/internal/plugin/payment/epay"
	"gorixpay/internal/plugin/payment/paymenttest"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestAdminPluginsListsRegistryMetadata(t *testing.T) {
	router, _, _, plugins := adminPluginRouter(t, "admin-plugins-list")
	response := performAdmin(t, router, http.MethodGet, "/api/v1/admin/plugins", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Items []struct {
			Code         string   `json:"code"`
			Name         string   `json:"name"`
			Capabilities []string `json:"capabilities"`
			ConfigFields []struct {
				Key      string `json:"key"`
				Label    string `json:"label"`
				Type     string `json:"type"`
				Required bool   `json:"required"`
				Secret   bool   `json:"secret"`
			} `json:"config_fields"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != len(plugins.List()) {
		t.Fatalf("items=%d registry=%d", len(body.Items), len(plugins.List()))
	}
	secretFields := 0
	for _, item := range body.Items {
		if item.Code == "" || item.Name == "" || len(item.Capabilities) == 0 {
			t.Fatalf("item=%+v", item)
		}
		for _, field := range item.ConfigFields {
			if field.Key == "" || field.Label == "" || field.Type == "" {
				t.Fatalf("field=%+v", field)
			}
			if field.Secret {
				secretFields++
			}
		}
	}
	if secretFields == 0 {
		t.Fatal("no secret field published")
	}
	if strings.Contains(response.Body.String(), "PRIVATE KEY") {
		t.Fatal("plugin list leaked key material")
	}
}

func TestAdminChannelCreateAcceptsConfigMapForNonEpayPlugin(t *testing.T) {
	router, db, _, _ := adminPluginRouter(t, "admin-channel-config")
	merchant := paymenttest.MustMerchantKeys(t)
	payload := map[string]any{
		"name": "支付宝主通道", "plugin_code": "alipay", "payment_type": "alipay",
		"priority": 1, "weight": 100, "status": "active",
		"config": map[string]string{"app_id": "2021000000000001", "private_key": merchant.PrivatePEM},
	}
	response := performAdmin(t, router, http.MethodPost, "/api/v1/admin/channels", payload)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("body=%s err=%v", response.Body.String(), err)
	}
	var channel channelrouter.Channel
	if err := db.First(&channel, "id = ?", created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if channel.ConfigEncrypted == "" || strings.Contains(channel.ConfigEncrypted, "PRIVATE KEY") {
		t.Fatal("channel configuration stored unencrypted")
	}

	listed := performAdmin(t, router, http.MethodGet, "/api/v1/admin/channels", nil)
	if listed.Code != http.StatusOK {
		t.Fatalf("list status=%d", listed.Code)
	}
	var list struct {
		Items []struct {
			ID         string `json:"id"`
			PluginCode string `json:"plugin_code"`
			Configured bool   `json:"configured"`
		} `json:"items"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].PluginCode != "alipay" || !list.Items[0].Configured {
		t.Fatalf("items=%+v", list.Items)
	}
	if strings.Contains(listed.Body.String(), "PRIVATE KEY") || strings.Contains(listed.Body.String(), "2021000000000001") {
		t.Fatalf("channel list leaked configuration: %s", listed.Body.String())
	}
}

func TestAdminChannelUpdateMergesLegacyAndConfigFields(t *testing.T) {
	router, db, configs, _ := adminPluginRouter(t, "admin-channel-update")
	response := performAdmin(t, router, http.MethodPost, "/api/v1/admin/channels", map[string]any{
		"name": "epay", "plugin_code": "epay", "payment_type": "alipay", "priority": 1, "weight": 10,
		"api_url": "https://upstream.example", "pid": "1001", "key": "legacy-secret",
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	update := performAdmin(t, router, http.MethodPut, "/api/v1/admin/channels/"+created.ID, map[string]any{
		"name": "epay", "plugin_code": "epay", "payment_type": "alipay", "priority": 2, "weight": 20,
		"api_url": "https://upstream.example", "pid": "1001", "key": "legacy-secret",
		"config": map[string]string{"retry": "3"},
	})
	if update.Code != http.StatusNoContent {
		t.Fatalf("update status=%d body=%s", update.Code, update.Body.String())
	}
	var channel channelrouter.Channel
	if err := db.First(&channel, "id = ?", created.ID).Error; err != nil {
		t.Fatal(err)
	}
	decoded, err := configs.Decrypt(channel.ID, channel.ConfigEncrypted)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.APIURL != "https://upstream.example" || decoded.PID != "1001" || decoded.Key != "legacy-secret" {
		t.Fatalf("config=%+v", decoded)
	}
	if decoded.Extra["retry"] != "3" || decoded.Extra["api_url"] != "https://upstream.example" {
		t.Fatalf("extra=%+v", decoded.Extra)
	}
	values := decoded.Values()
	if values["pid"] != "1001" || values["retry"] != "3" {
		t.Fatalf("values=%+v", values)
	}
}

func TestAdminChannelCreateRejectsEmptyConfiguration(t *testing.T) {
	router, _, _, _ := adminPluginRouter(t, "admin-channel-invalid")
	for name, payload := range map[string]map[string]any{
		"nothing": {"name": "x", "plugin_code": "alipay", "payment_type": "alipay", "weight": 1},
		"blank-config": {"name": "x", "plugin_code": "alipay", "payment_type": "alipay", "weight": 1,
			"config": map[string]string{"app_id": "   "}},
		"partial-legacy": {"name": "x", "plugin_code": "epay", "payment_type": "alipay", "weight": 1,
			"api_url": "https://upstream.example"},
	} {
		t.Run(name, func(t *testing.T) {
			if response := performAdmin(t, router, http.MethodPost, "/api/v1/admin/channels", payload); response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestAdminChannelTestInvokesPluginWithoutPersisting(t *testing.T) {
	var called bool
	gateway := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		called = true
		if err := request.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if request.PostForm.Get("method") != "alipay.trade.precreate" {
			t.Errorf("form=%+v", request.PostForm)
		}
		_, _ = writer.Write([]byte(`{"alipay_trade_precreate_response":{"code":"10000","msg":"Success","qr_code":"https://qr.alipay.com/test"}}`))
	}))
	defer gateway.Close()

	router, db, _, _ := adminPluginRouter(t, "admin-channel-test")
	merchant := paymenttest.MustMerchantKeys(t)
	created := performAdmin(t, router, http.MethodPost, "/api/v1/admin/channels", map[string]any{
		"name": "支付宝测试", "plugin_code": "alipay", "payment_type": "alipay", "priority": 1, "weight": 100,
		"config": map[string]string{"app_id": "2021000000000001", "private_key": merchant.PrivatePEM, "gateway_url": gateway.URL},
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var channel struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &channel); err != nil {
		t.Fatal(err)
	}
	response := performAdmin(t, router, http.MethodPost, "/api/v1/admin/channels/"+channel.ID+"/test", map[string]any{
		"payment_type": "alipay", "amount": 1234,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		OK      bool   `json:"ok"`
		PayType string `json:"pay_type"`
		PayInfo string `json:"pay_info"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.OK || body.PayType != "qrcode" || body.PayInfo != "https://qr.alipay.com/test" || body.Error != "" {
		t.Fatalf("body=%+v", body)
	}
	if !called {
		t.Fatal("plugin was not invoked")
	}
	var orders int64
	if err := db.Model(&paymentmodel.Order{}).Count(&orders).Error; err != nil {
		t.Fatal(err)
	}
	if orders != 0 {
		t.Fatalf("test endpoint persisted %d orders", orders)
	}
}

func TestAdminChannelTestReportsPluginFailure(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"alipay_trade_precreate_response":{"code":"40004","msg":"Business Failed"}}`))
	}))
	defer gateway.Close()
	router, _, _, _ := adminPluginRouter(t, "admin-channel-test-failure")
	merchant := paymenttest.MustMerchantKeys(t)
	created := performAdmin(t, router, http.MethodPost, "/api/v1/admin/channels", map[string]any{
		"name": "支付宝失败", "plugin_code": "alipay", "payment_type": "alipay", "weight": 100,
		"config": map[string]string{"app_id": "app", "private_key": merchant.PrivatePEM, "gateway_url": gateway.URL},
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var channel struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &channel); err != nil {
		t.Fatal(err)
	}
	response := performAdmin(t, router, http.MethodPost, "/api/v1/admin/channels/"+channel.ID+"/test", map[string]any{"amount": 100})
	var body struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.OK || !strings.Contains(body.Error, "40004") {
		t.Fatalf("body=%+v", body)
	}
	if missing := performAdmin(t, router, http.MethodPost, "/api/v1/admin/channels/missing/test", map[string]any{"amount": 100}); missing.Code != http.StatusNotFound {
		t.Fatalf("missing channel status=%d", missing.Code)
	}
}

// adminPluginRouter wires only the channel CRUD handlers plus the plugin routes,
// which is enough to exercise the plugin contract end to end.
func adminPluginRouter(t *testing.T, name string) (*gin.Engine, *gorm.DB, *channelconfig.Service, *pluginpayment.Registry) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&channelrouter.Channel{}, &paymentmodel.Order{}); err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher([]byte("01234567890123456789012345678901"), "v1")
	if err != nil {
		t.Fatal(err)
	}
	configs := channelconfig.NewService(cipher)
	plugins := pluginpayment.NewRegistry()
	for _, plugin := range []pluginpayment.Plugin{
		alipay.New(nil), epay.New(nil), stubPlugin{},
	} {
		if err := plugins.Register(plugin); err != nil {
			t.Fatalf("register %s: %v", plugin.Metadata().Code, err)
		}
	}
	handler := &Handler{db: db, configs: configs}
	router := gin.New()
	group := router.Group("/api/v1/admin")
	group.GET("/channels", handler.channels)
	group.POST("/channels", handler.createChannel)
	group.PUT("/channels/:id", handler.updateChannel)
	RegisterPlugins(group, db, configs, plugins)
	return router, db, configs, plugins
}

// paymentStubPlugin publishes no configuration schema so the list handler has to
// tolerate a plugin that predates Configurable.
type stubPlugin struct{}

func (stubPlugin) Metadata() pluginpayment.Metadata {
	return pluginpayment.Metadata{Code: "legacy", Name: "Legacy", Capabilities: []pluginpayment.Capability{pluginpayment.CapabilityPayment}}
}

func (stubPlugin) CreatePayment(context.Context, pluginpayment.CreateRequest) (pluginpayment.Action, error) {
	return pluginpayment.Action{Type: pluginpayment.ActionJump, URL: "https://legacy.example/pay"}, nil
}

func performAdmin(t *testing.T, router http.Handler, method, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var body *strings.Reader
	if payload == nil {
		body = strings.NewReader("")
	} else {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = strings.NewReader(string(encoded))
	}
	request := httptest.NewRequest(method, path, body)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
