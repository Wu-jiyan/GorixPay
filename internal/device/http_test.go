package device

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identitymodel "gorixpay/internal/identity/model"

	"github.com/gin-gonic/gin"
)

func TestVoiceDeviceHTTPFlowAndSecretHygiene(t *testing.T) {
	env := newDeviceEnv(t, "device-http-voice")
	router := deviceRouter(t, env, identitymodel.PrincipalAdministrator)

	created := performDevice(t, router, http.MethodPost, "/api/v1/admin/voice-devices",
		`{"merchant_id":"merchant-1","name":"前台音箱","device_no":"VD-001"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var createdBody struct {
		Device VoiceDevice `json:"device"`
		Secret string      `json:"secret"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatal(err)
	}
	if createdBody.Secret == "" || createdBody.Device.DeviceNo != "VD-001" {
		t.Fatalf("created=%+v", createdBody)
	}

	listed := performDevice(t, router, http.MethodGet, "/api/v1/admin/voice-devices", "")
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), createdBody.Secret) {
		t.Fatalf("list leaked secret or failed: %s", listed.Body.String())
	}

	if response := performDevice(t, router, http.MethodPost, "/api/v1/device/voice/heartbeat",
		`{"device_no":"VD-001","secret":"wrong"}`); response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong secret status=%d body=%s", response.Code, response.Body.String())
	}

	queued := performDevice(t, router, http.MethodPost, "/api/v1/admin/voice-devices/"+createdBody.Device.ID+"/test", `{"text":"测试播报"}`)
	if queued.Code != http.StatusOK || !strings.Contains(queued.Body.String(), `"queued":true`) {
		t.Fatalf("test endpoint status=%d body=%s", queued.Code, queued.Body.String())
	}
	heartbeat := performDevice(t, router, http.MethodPost, "/api/v1/device/voice/heartbeat",
		`{"device_no":"VD-001","secret":"`+createdBody.Secret+`"}`)
	if heartbeat.Code != http.StatusOK || !strings.Contains(heartbeat.Body.String(), "测试播报") {
		t.Fatalf("heartbeat status=%d body=%s", heartbeat.Code, heartbeat.Body.String())
	}
	var heartbeatBody struct {
		Broadcasts []struct {
			ID string `json:"id"`
		} `json:"broadcasts"`
	}
	if err := json.Unmarshal(heartbeat.Body.Bytes(), &heartbeatBody); err != nil {
		t.Fatal(err)
	}
	if len(heartbeatBody.Broadcasts) != 1 {
		t.Fatalf("broadcasts=%+v", heartbeatBody.Broadcasts)
	}
	ackBody := `{"device_no":"VD-001","secret":"` + createdBody.Secret + `","ids":["` + heartbeatBody.Broadcasts[0].ID + `"]}`
	if response := performDevice(t, router, http.MethodPost, "/api/v1/device/voice/ack", ackBody); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"acked":0`) {
		t.Fatalf("ack status=%d body=%s", response.Code, response.Body.String())
	}

	reset := performDevice(t, router, http.MethodPost, "/api/v1/admin/voice-devices/"+createdBody.Device.ID+"/reset-secret", "")
	if reset.Code != http.StatusOK || strings.Contains(reset.Body.String(), createdBody.Secret) {
		t.Fatalf("reset status=%d body=%s", reset.Code, reset.Body.String())
	}
	if response := performDevice(t, router, http.MethodPost, "/api/v1/device/voice/heartbeat",
		`{"device_no":"VD-001","secret":"`+createdBody.Secret+`"}`); response.Code != http.StatusUnauthorized {
		t.Fatalf("old secret after reset status=%d", response.Code)
	}
}

func TestPrinterHTTPFlow(t *testing.T) {
	env := newDeviceEnv(t, "device-http-printer")
	router := deviceRouter(t, env, identitymodel.PrincipalAdministrator)
	created := performDevice(t, router, http.MethodPost, "/api/v1/admin/printers", `{"merchant_id":"merchant-1","name":"打印机"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var body struct {
		Printer Printer `json:"printer"`
		Secret  string  `json:"secret"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Secret == "" {
		t.Fatal("printer secret missing")
	}
	if response := performDevice(t, router, http.MethodPost, "/api/v1/admin/printers/"+body.Printer.ID+"/test", ""); response.Code != http.StatusOK {
		t.Fatalf("test endpoint status=%d body=%s", response.Code, response.Body.String())
	}
	pollBody := `{"printer_no":"` + body.Printer.ID + `","secret":"` + body.Secret + `"}`
	poll := performDevice(t, router, http.MethodPost, "/api/v1/device/printer/poll", pollBody)
	if poll.Code != http.StatusOK || !strings.Contains(poll.Body.String(), "测试小票") {
		t.Fatalf("poll status=%d body=%s", poll.Code, poll.Body.String())
	}
	var pollResult struct {
		Jobs []struct {
			ID string `json:"id"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(poll.Body.Bytes(), &pollResult); err != nil {
		t.Fatal(err)
	}
	if len(pollResult.Jobs) != 1 {
		t.Fatalf("jobs=%+v", pollResult.Jobs)
	}
	ackBody := `{"printer_no":"` + body.Printer.ID + `","secret":"` + body.Secret + `","ids":["` + pollResult.Jobs[0].ID + `"]}`
	if response := performDevice(t, router, http.MethodPost, "/api/v1/device/printer/ack", ackBody); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"acked":1`) {
		t.Fatalf("ack status=%d body=%s", response.Code, response.Body.String())
	}
	if response := performDevice(t, router, http.MethodPost, "/api/v1/device/printer/poll", `{"printer_no":"`+body.Printer.ID+`","secret":"nope"}`); response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong secret status=%d", response.Code)
	}
}

func TestCleanupAndTestOrderHTTP(t *testing.T) {
	env := newDeviceEnv(t, "device-http-admin")
	router := deviceRouter(t, env, identitymodel.PrincipalAdministrator)
	old := env.now.Add(-45 * 24 * time.Hour)
	if err := env.db.Exec("INSERT INTO audit_events (id, event_type, actor_type, payload, created_at) VALUES (?, ?, ?, ?, ?)",
		"old-1", "test", "administrator", "{}", old).Error; err != nil {
		t.Fatal(err)
	}
	dry := performDevice(t, router, http.MethodPost, "/api/v1/admin/maintenance/cleanup",
		`{"targets":["audit_events"],"before":"2026-10-01","dry_run":true}`)
	if dry.Code != http.StatusOK || !strings.Contains(dry.Body.String(), `"would_delete":1`) {
		t.Fatalf("dry status=%d body=%s", dry.Code, dry.Body.String())
	}
	missingConfirm := performDevice(t, router, http.MethodPost, "/api/v1/admin/maintenance/cleanup",
		`{"targets":["audit_events"],"before":"2026-10-01","dry_run":false}`)
	if missingConfirm.Code != http.StatusBadRequest || !strings.Contains(missingConfirm.Body.String(), "confirmation_required") {
		t.Fatalf("missing confirm status=%d body=%s", missingConfirm.Code, missingConfirm.Body.String())
	}
	real := performDevice(t, router, http.MethodPost, "/api/v1/admin/maintenance/cleanup",
		`{"targets":["audit_events"],"before":"2026-10-01","dry_run":false,"confirm":"DELETE"}`)
	if real.Code != http.StatusOK || !strings.Contains(real.Body.String(), `"deleted":1`) {
		t.Fatalf("real status=%d body=%s", real.Code, real.Body.String())
	}
	if response := performDevice(t, router, http.MethodPost, "/api/v1/admin/maintenance/cleanup",
		`{"targets":["audit_events"],"before":"2026-11-10","dry_run":true}`); response.Code != http.StatusBadRequest {
		t.Fatalf("fresh cutoff status=%d", response.Code)
	}
	if response := performDevice(t, router, http.MethodPost, "/api/v1/admin/channels/channel-1/test-order", `{"merchant_id":"merchant-1","amount_cents":100}`); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("test order without wiring status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAdminEndpointsRejectMerchantSession(t *testing.T) {
	env := newDeviceEnv(t, "device-http-guard")
	router := deviceRouter(t, env, identitymodel.PrincipalMerchant)
	if response := performDevice(t, router, http.MethodGet, "/api/v1/admin/voice-devices", ""); response.Code != http.StatusForbidden {
		t.Fatalf("merchant session status=%d body=%s", response.Code, response.Body.String())
	}
}

func deviceRouter(t *testing.T, env *deviceEnv, principal identitymodel.PrincipalType) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Register(router.Group("/api/v1"), env.db, func(ctx *gin.Context) {
		ctx.Set("identity_principal", identitymodel.Session{PrincipalType: principal, PrincipalID: "admin-1"})
		ctx.Next()
	}, Deps{})
	return router
}

func performDevice(t *testing.T, router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	router.ServeHTTP(response, request)
	return response
}
