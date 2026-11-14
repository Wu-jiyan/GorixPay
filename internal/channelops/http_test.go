package channelops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/platform/secret"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const principalContextKey = "identity_principal"

func TestPublicCatalogueAndAdminPayTypes(t *testing.T) {
	_, db, cipher := channelopsFixture(t, "channelops-http-paytypes")
	router := channelopsRouter(db, cipher)
	ctx := context.Background()
	service := New(db, cipher, func() time.Time { return testNow })
	if _, err := service.CreatePayType(ctx, PayTypeRequest{Code: "alipay", Name: "支付宝", Sort: 1, Icon: "https://cdn.example/a.svg"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreatePayType(ctx, PayTypeRequest{Code: "wxpay", Name: "微信支付", Sort: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreatePayType(ctx, PayTypeRequest{Code: "qqpay", Name: "QQ", Sort: 3, Status: StatusDisabled}); err != nil {
		t.Fatal(err)
	}

	response := perform(router, http.MethodGet, "/api/v1/public/pay-types", "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("public: %d %s", response.Code, response.Body.String())
	}
	var public struct {
		Items []struct {
			Code string `json:"code"`
			Name string `json:"name"`
			Icon string `json:"icon"`
			Sort int    `json:"sort"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &public); err != nil {
		t.Fatal(err)
	}
	if len(public.Items) != 2 || public.Items[0].Code != "alipay" || public.Items[1].Code != "wxpay" {
		t.Fatalf("public items=%+v", public.Items)
	}
	if strings.Contains(response.Body.String(), "qqpay") || strings.Contains(response.Body.String(), `"status"`) {
		t.Fatalf("public payload leaks disabled rows or internals: %s", response.Body.String())
	}

	response = perform(router, http.MethodPost, "/api/v1/admin/pay-types", `{"code":"usdt","name":"USDT","sort":4}`, "admin-1")
	if response.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", response.Code, response.Body.String())
	}
	var created PayType
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Code != "usdt" || created.Status != StatusActive {
		t.Fatalf("created=%+v", created)
	}
	if response := perform(router, http.MethodPost, "/api/v1/admin/pay-types", `{"code":"usdt","name":"dup"}`, "admin-1"); response.Code != http.StatusConflict {
		t.Fatalf("duplicate: %d %s", response.Code, response.Body.String())
	}
	if response := perform(router, http.MethodPost, "/api/v1/admin/pay-types", `{"code":"bad code","name":"x"}`, "admin-1"); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid: %d %s", response.Code, response.Body.String())
	}

	response = perform(router, http.MethodGet, "/api/v1/admin/pay-types", "", "admin-1")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "qqpay") {
		t.Fatalf("admin list: %d %s", response.Code, response.Body.String())
	}
	response = perform(router, http.MethodPut, "/api/v1/admin/pay-types/"+created.ID, `{"name":"USDT 泰达","sort":5,"status":"disabled"}`, "admin-1")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "USDT 泰达") {
		t.Fatalf("update: %d %s", response.Code, response.Body.String())
	}
	response = perform(router, http.MethodPatch, "/api/v1/admin/pay-types/"+created.ID+"/status", `{"status":"active"}`, "admin-1")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"active"`) {
		t.Fatalf("status: %d %s", response.Code, response.Body.String())
	}
	if response := perform(router, http.MethodPut, "/api/v1/admin/pay-types/missing", `{"name":"x"}`, "admin-1"); response.Code != http.StatusNotFound {
		t.Fatalf("update missing: %d %s", response.Code, response.Body.String())
	}
	if response := perform(router, http.MethodDelete, "/api/v1/admin/pay-types/"+created.ID, "", "admin-1"); response.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", response.Code, response.Body.String())
	}
	// A merchant principal gets 403 on every admin route.
	if response := perform(router, http.MethodGet, "/api/v1/admin/pay-types", "", "merchant-1"); response.Code != http.StatusForbidden {
		t.Fatalf("merchant on admin: %d %s", response.Code, response.Body.String())
	}
}

func TestPayRollEndpoints(t *testing.T) {
	_, db, cipher := channelopsFixture(t, "channelops-http-rolls")
	router := channelopsRouter(db, cipher)

	response := perform(router, http.MethodPost, "/api/v1/admin/pay-rolls", `{"channel_id":"channel-1","roll_date":"2026-11-14","weight":3}`, "admin-1")
	if response.Code != http.StatusCreated {
		t.Fatalf("create roll: %d %s", response.Code, response.Body.String())
	}
	var created rollView
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.RollDate != "2026-11-14" || created.Weight != 3 || created.Status != StatusActive {
		t.Fatalf("created=%+v", created)
	}
	if response := perform(router, http.MethodPost, "/api/v1/admin/pay-rolls", `{"channel_id":"channel-1","roll_date":"2026-11-14"}`, "admin-1"); response.Code != http.StatusConflict {
		t.Fatalf("duplicate roll: %d %s", response.Code, response.Body.String())
	}
	if response := perform(router, http.MethodPost, "/api/v1/admin/pay-rolls", `{"channel_id":"channel-1","roll_date":"14/11/2026"}`, "admin-1"); response.Code != http.StatusBadRequest {
		t.Fatalf("bad date: %d %s", response.Code, response.Body.String())
	}

	response = perform(router, http.MethodPost, "/api/v1/admin/pay-rolls/generate",
		`{"channel_ids":["channel-1","channel-2"],"from":"2026-11-15","to":"2026-11-17"}`, "admin-1")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"created":6`) {
		t.Fatalf("generate: %d %s", response.Code, response.Body.String())
	}
	response = perform(router, http.MethodPost, "/api/v1/admin/pay-rolls/generate",
		`{"channel_ids":["channel-1"],"from":"2026-11-15","to":"2026-11-17"}`, "admin-1")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"created":0`) {
		t.Fatalf("regenerate: %d %s", response.Code, response.Body.String())
	}

	response = perform(router, http.MethodGet, "/api/v1/admin/pay-rolls?channel_id=channel-1&page=1&page_size=2", "", "admin-1")
	if response.Code != http.StatusOK {
		t.Fatalf("list rolls: %d %s", response.Code, response.Body.String())
	}
	var listing struct {
		Items    []rollView `json:"items"`
		Total    int64      `json:"total"`
		Page     int        `json:"page"`
		PageSize int        `json:"page_size"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	if listing.Total != 4 || len(listing.Items) != 2 || listing.Page != 1 || listing.PageSize != 2 {
		t.Fatalf("listing=%+v", listing)
	}
	if listing.Items[0].RollDate != "2026-11-17" {
		t.Fatalf("rolls are not newest first: %+v", listing.Items)
	}

	response = perform(router, http.MethodPut, "/api/v1/admin/pay-rolls/"+created.ID, `{"weight":0,"status":"disabled"}`, "admin-1")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"weight":0`) {
		t.Fatalf("update roll: %d %s", response.Code, response.Body.String())
	}
	if response := perform(router, http.MethodGet, "/api/v1/admin/pay-rolls?from=oops", "", "admin-1"); response.Code != http.StatusBadRequest {
		t.Fatalf("bad from: %d %s", response.Code, response.Body.String())
	}
	if response := perform(router, http.MethodDelete, "/api/v1/admin/pay-rolls/"+created.ID, "", "admin-1"); response.Code != http.StatusNoContent {
		t.Fatalf("delete roll: %d %s", response.Code, response.Body.String())
	}
	if response := perform(router, http.MethodDelete, "/api/v1/admin/pay-rolls/"+created.ID, "", "admin-1"); response.Code != http.StatusNotFound {
		t.Fatalf("delete roll twice: %d %s", response.Code, response.Body.String())
	}
}

func TestWeChatConfigEndpointsKeepSecretsOutOfResponses(t *testing.T) {
	_, db, cipher := channelopsFixture(t, "channelops-http-wechat")
	router := channelopsRouter(db, cipher)

	response := perform(router, http.MethodPost, "/api/v1/admin/wechat-mp-configs",
		`{"merchant_id":"merchant-1","app_id":"wx-app","app_secret":"top-secret","token":"tk","aes_key":"aes-secret"}`, "admin-1")
	if response.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "top-secret") || strings.Contains(response.Body.String(), "aes-secret") {
		t.Fatalf("response leaks plaintext secrets: %s", response.Body.String())
	}
	var view WeChatMPView
	if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if !view.SecretConfigured || !view.AESKeyConfigured || view.AppID != "wx-app" {
		t.Fatalf("view=%+v", view)
	}

	response = perform(router, http.MethodGet, "/api/v1/admin/wechat-mp-configs?merchant_id=merchant-1", "", "admin-1")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "wx-app") || strings.Contains(response.Body.String(), "top-secret") {
		t.Fatalf("list: %d %s", response.Code, response.Body.String())
	}
	response = perform(router, http.MethodPut, "/api/v1/admin/wechat-mp-configs/"+view.ID, `{"app_id":"wx-app","token":"tk2"}`, "admin-1")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"secret_configured":true`) {
		t.Fatalf("update keeps secret: %d %s", response.Code, response.Body.String())
	}
	var stored WeChatMPConfig
	if err := db.First(&stored, "id = ?", view.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.MerchantID == nil || *stored.MerchantID != "merchant-1" {
		t.Fatalf("merchant was detached by the update: %v", stored.MerchantID)
	}
	if stored.Token == nil || *stored.Token != "tk2" {
		t.Fatalf("token not updated: %v", stored.Token)
	}
	if response := perform(router, http.MethodPost, "/api/v1/admin/wechat-mp-configs", `{"app_id":"wx-app-2"}`, "admin-1"); response.Code != http.StatusBadRequest {
		t.Fatalf("missing secret: %d %s", response.Code, response.Body.String())
	}
	if response := perform(router, http.MethodDelete, "/api/v1/admin/wechat-mp-configs/"+view.ID, "", "admin-1"); response.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", response.Code, response.Body.String())
	}
}

func channelopsRouter(db *gorm.DB, cipher *secret.Cipher) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Register(router.Group("/api/v1"), db, stubSessions(), cipher)
	return router
}

func stubSessions() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		principalID := strings.TrimSpace(ctx.GetHeader("X-Principal"))
		if principalID == "" {
			principalID = "admin-1"
		}
		principalType := identitymodel.PrincipalAdministrator
		if !strings.HasPrefix(principalID, "admin-") {
			principalType = identitymodel.PrincipalMerchant
		}
		ctx.Set(principalContextKey, identitymodel.Session{PrincipalType: principalType, PrincipalID: principalID})
		ctx.Next()
	}
}

func perform(router http.Handler, method, path, body, principal string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if principal != "" {
		request.Header.Set("X-Principal", principal)
	}
	router.ServeHTTP(response, request)
	return response
}

func TestMountUsesTheProvidedService(t *testing.T) {
	service, _, _ := channelopsFixture(t, "channelops-http-mount")
	ctx := context.Background()
	if _, err := service.CreatePayType(ctx, PayTypeRequest{Code: "alipay", Name: "支付宝"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreatePayRoll(ctx, PayRollRequest{ChannelID: "channel-1", Day: DayStart(testNow), Weight: 6}); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Mount(router.Group("/api/v1"), stubSessions(), service)
	response := perform(router, http.MethodGet, "/api/v1/public/pay-types", "", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"code":"alipay"`) {
		t.Fatalf("mounted public route: %d %s", response.Code, response.Body.String())
	}
	// The same instance that serves HTTP answers the router lookup.
	if weight, ok := service.RollWeight(ctx, "channel-1", testNow); !ok || weight != 6 {
		t.Fatalf("roll weight=%d ok=%v", weight, ok)
	}
	var resolver RollResolver = service
	if _, ok := resolver.RollWeight(ctx, "channel-1", testNow); !ok {
		t.Fatal("service does not satisfy RollResolver")
	}
	// Missing dependencies are ignored instead of panicking.
	Mount(nil, nil, nil)
	Register(nil, nil, nil, nil)
}
