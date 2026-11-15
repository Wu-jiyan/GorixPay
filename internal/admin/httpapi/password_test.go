package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	identitymodel "gorixpay/internal/identity/model"
	identitypassword "gorixpay/internal/identity/password"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// adminPasswordRouter builds the smallest router that exercises changePassword:
// the real handler plus a middleware that plants the session the authenticated
// middleware would normally have stored.
func adminPasswordRouter(t *testing.T, name string, session identitymodel.Session) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&identitymodel.Administrator{}, &identitymodel.Session{}); err != nil {
		t.Fatal(err)
	}
	handler := &Handler{db: db}
	router := gin.New()
	group := router.Group("/api/v1/admin")
	group.Use(func(ctx *gin.Context) {
		ctx.Set("identity_principal", session)
		ctx.Next()
	})
	group.POST("/password/change", handler.changePassword)
	return router, db
}

// seedAdmin stores an administrator with a real argon2id hash plus one extra live
// session, so the test can prove the other session dies and the current one lives.
func seedAdmin(t *testing.T, db *gorm.DB, username, plain string, current, other string) identitymodel.Administrator {
	t.Helper()
	hash, err := identitypassword.Hash(plain)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	administrator := identitymodel.Administrator{
		ID: "admin-1", Username: username, PasswordHash: hash,
		DisplayName: "运营", Status: identitymodel.StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&administrator).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{current, other} {
		session := identitymodel.Session{
			ID: id, PrincipalType: identitymodel.PrincipalAdministrator, PrincipalID: administrator.ID,
			TokenHash: strings.Repeat(id, 8), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
		}
		if err := db.Create(&session).Error; err != nil {
			t.Fatal(err)
		}
	}
	return administrator
}

// TestAdminChangePasswordRotatesHashAndRevokesOtherSessions pins the whole point of
// the endpoint: the new password works, the old one does not, every other session is
// signed out, and the session that made the change survives.
func TestAdminChangePasswordRotatesHashAndRevokesOtherSessions(t *testing.T) {
	session := identitymodel.Session{
		ID: "sess-current", PrincipalType: identitymodel.PrincipalAdministrator,
		PrincipalID: "admin-1", ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	router, db := adminPasswordRouter(t, "admin-password-change", session)
	seedAdmin(t, db, "admin", "OldPass2026x", "sess-current", "sess-other")

	response := performAdmin(t, router, http.MethodPost, "/api/v1/admin/password/change", map[string]string{
		"old_password": "OldPass2026x",
		"new_password": "NewPass2026y",
	})
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	var administrator identitymodel.Administrator
	if err := db.First(&administrator, "id = ?", "admin-1").Error; err != nil {
		t.Fatal(err)
	}
	newMatched, _, err := identitypassword.Verify("NewPass2026y", administrator.PasswordHash)
	if err != nil || !newMatched {
		t.Fatalf("new password must verify: matched=%v err=%v", newMatched, err)
	}
	oldMatched, _, err := identitypassword.Verify("OldPass2026x", administrator.PasswordHash)
	if err != nil || oldMatched {
		t.Fatalf("old password must stop working: matched=%v err=%v", oldMatched, err)
	}

	var current, other identitymodel.Session
	if err := db.First(&current, "id = ?", "sess-current").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&other, "id = ?", "sess-other").Error; err != nil {
		t.Fatal(err)
	}
	if current.RevokedAt != nil {
		t.Fatal("the session that changed the password must stay valid")
	}
	if other.RevokedAt == nil {
		t.Fatal("other sessions must be revoked")
	}
}

// TestAdminChangePasswordRejections covers the three ways the call must fail without
// touching the stored hash: wrong current password, too short new password, no-op change.
func TestAdminChangePasswordRejections(t *testing.T) {
	session := identitymodel.Session{
		ID: "sess-current", PrincipalType: identitymodel.PrincipalAdministrator,
		PrincipalID: "admin-1", ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	router, db := adminPasswordRouter(t, "admin-password-reject", session)
	seedAdmin(t, db, "admin", "OldPass2026x", "sess-current", "sess-other")

	cases := []struct {
		name   string
		body   map[string]string
		status int
		code   string
	}{
		{"wrong current password", map[string]string{"old_password": "nope-nope", "new_password": "NewPass2026y"}, http.StatusBadRequest, "invalid_old_password"},
		{"too short", map[string]string{"old_password": "OldPass2026x", "new_password": "short"}, http.StatusBadRequest, "invalid_password"},
		{"same password", map[string]string{"old_password": "OldPass2026x", "new_password": "OldPass2026x"}, http.StatusConflict, "same_password"},
		{"missing field", map[string]string{"old_password": "OldPass2026x"}, http.StatusBadRequest, "invalid_request"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := performAdmin(t, router, http.MethodPost, "/api/v1/admin/password/change", testCase.body)
			if response.Code != testCase.status {
				t.Fatalf("status = %d, want %d, body = %s", response.Code, testCase.status, response.Body.String())
			}
			if body := response.Body.String(); !strings.Contains(body, testCase.code) {
				t.Fatalf("body = %s, want code %s", body, testCase.code)
			}
		})
	}

	var administrator identitymodel.Administrator
	if err := db.First(&administrator, "id = ?", "admin-1").Error; err != nil {
		t.Fatal(err)
	}
	matched, _, err := identitypassword.Verify("OldPass2026x", administrator.PasswordHash)
	if err != nil || !matched {
		t.Fatalf("rejected calls must leave the stored hash alone: matched=%v err=%v", matched, err)
	}
	var other identitymodel.Session
	if err := db.First(&other, "id = ?", "sess-other").Error; err != nil {
		t.Fatal(err)
	}
	if other.RevokedAt != nil {
		t.Fatal("rejected calls must not revoke sessions")
	}
}
