package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorixpay/internal/settings"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestRegistrationGateFollowsTheSetting pins the register.mode gate that 上游 Epay
// calls reg_open: a closed site refuses every signup, an invite-only site refuses
// one without a code, and an open site lets it through to the normal flow.
func TestRegistrationGateFollowsTheSetting(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE system_settings (setting_key TEXT PRIMARY KEY, setting_value TEXT NOT NULL)`).Error; err != nil {
		t.Fatal(err)
	}
	handler := &Handler{settings: settings.NewReader(db)}
	ctx := context.Background()

	if got := handler.registrationMode(ctx); got != registrationOpen {
		t.Fatalf("默认应为开放注册，得到 %q", got)
	}

	setMode := func(value string) {
		if err := db.Exec(`INSERT INTO system_settings (setting_key, setting_value) VALUES (?, ?)
			ON CONFLICT(setting_key) DO UPDATE SET setting_value = excluded.setting_value`,
			settingRegisterMode, `"`+value+`"`).Error; err != nil {
			t.Fatal(err)
		}
	}

	setMode("closed")
	if got := handler.registrationMode(ctx); got != registrationClosed {
		t.Fatalf("closed 未生效：%q", got)
	}
	setMode("invite")
	if got := handler.registrationMode(ctx); got != registrationInvite {
		t.Fatalf("invite 未生效：%q", got)
	}
	setMode("bogus")
	if got := handler.registrationMode(ctx); got != registrationOpen {
		t.Fatalf("非法值应回落 open：%q", got)
	}
}

// TestRegisterRejectsWhenClosed exercises the HTTP branch itself, so a future
// refactor cannot quietly drop the check.
func TestRegisterRejectsWhenClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE system_settings (setting_key TEXT PRIMARY KEY, setting_value TEXT NOT NULL)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO system_settings (setting_key, setting_value) VALUES ('register.mode', '"closed"')`).Error; err != nil {
		t.Fatal(err)
	}
	handler := &Handler{settings: settings.NewReader(db)}

	router := gin.New()
	router.POST("/register", handler.registerMerchant)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/register",
		strings.NewReader(`{"identifier":"a@example.com","password":"secret123"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("状态码 = %d，应为 403", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "register_closed") {
		t.Fatalf("响应体 = %s", recorder.Body.String())
	}
}
