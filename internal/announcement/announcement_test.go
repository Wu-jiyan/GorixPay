package announcement

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestServiceCRUDPublishesAndHidesDrafts(t *testing.T) {
	service, _ := announcementDatabase(t, "announcement-crud")
	ctx := context.Background()
	draft, err := service.Create(ctx, CreateInput{Title: "草稿", Content: "内容", Status: StatusDraft})
	if err != nil {
		t.Fatal(err)
	}
	if draft.PublishedAt != nil || draft.Status != StatusDraft {
		t.Fatalf("draft=%+v", draft)
	}
	published, err := service.Create(ctx, CreateInput{Title: "公告", Content: "内容", Status: StatusPublished})
	if err != nil {
		t.Fatal(err)
	}
	if published.PublishedAt == nil {
		t.Fatalf("published=%+v", published)
	}
	items, err := service.ListPublished(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != published.ID {
		t.Fatalf("published items=%+v", items)
	}
	latest, err := service.LatestPublished(ctx)
	if err != nil || latest == nil || latest.ID != published.ID {
		t.Fatalf("latest=%+v err=%v", latest, err)
	}
	keep := published.PublishedAt
	updated, err := service.Update(ctx, published.ID, UpdateInput{Title: "公告2", Content: "内容2", Status: StatusPublished})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "公告2" || updated.PublishedAt == nil || !updated.PublishedAt.Equal(*keep) {
		t.Fatalf("updated=%+v", updated)
	}
	reverted, err := service.Update(ctx, published.ID, UpdateInput{Title: "公告2", Content: "内容2", Status: StatusDraft})
	if err != nil {
		t.Fatal(err)
	}
	if reverted.PublishedAt != nil {
		t.Fatalf("reverted=%+v", reverted)
	}
	if err := service.Delete(ctx, draft.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(ctx, draft.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get deleted err=%v", err)
	}
	if err := service.Delete(ctx, draft.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing err=%v", err)
	}
}

func TestServiceRejectsInvalidInput(t *testing.T) {
	service, _ := announcementDatabase(t, "announcement-validation")
	ctx := context.Background()
	for _, input := range []CreateInput{
		{Title: "", Content: "content", Status: StatusDraft},
		{Title: "title", Content: "", Status: StatusDraft},
		{Title: strings.Repeat("あ", 192), Content: "content", Status: StatusDraft},
		{Title: "title", Content: "content", Status: "archived"},
	} {
		if _, err := service.Create(ctx, input); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("input %+v err=%v", input, err)
		}
	}
	created, err := service.Create(ctx, CreateInput{Title: "title", Content: "content"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != StatusDraft {
		t.Fatalf("default status=%q", created.Status)
	}
	if _, err := service.Update(ctx, "missing", UpdateInput{Title: "t", Content: "c"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing err=%v", err)
	}
}

func TestSiteUsesSettingsAndLatestAnnouncement(t *testing.T) {
	service, db := announcementDatabase(t, "announcement-site")
	ctx := context.Background()
	site, err := service.Site(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if site.Name != defaultSiteName || site.LogoURL != "" || site.Announcement != nil {
		t.Fatalf("default site=%+v", site)
	}
	if len(site.PaymentTypes) != 3 || site.PaymentTypes[0] != "alipay" || site.PaymentTypes[2] != "qqpay" {
		t.Fatalf("payment types=%v", site.PaymentTypes)
	}
	now := time.Now().UTC()
	for key, value := range map[string]string{settingSiteName: "Gorix 支付", settingSiteLogo: "https://cdn.example/logo.png"} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&systemSetting{Key: key, Value: string(encoded)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.Create(ctx, CreateInput{Title: "草稿", Content: "内容", Status: StatusDraft}); err != nil {
		t.Fatal(err)
	}
	site, err = service.Site(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if site.Name != "Gorix 支付" || site.LogoURL != "https://cdn.example/logo.png" {
		t.Fatalf("site=%+v", site)
	}
	if site.Announcement != nil {
		t.Fatalf("draft leaked into site: %+v", site.Announcement)
	}
	published, err := service.Create(ctx, CreateInput{Title: "上线公告", Content: "内容", Status: StatusPublished})
	if err != nil {
		t.Fatal(err)
	}
	site, err = service.Site(ctx)
	if err != nil {
		t.Fatal(err)
	}
	latest, ok := site.Announcement.(*Announcement)
	if !ok || latest.ID != published.ID {
		t.Fatalf("site announcement=%+v", site.Announcement)
	}
	_ = now
}

func TestHTTPRoutesExposePublishedToPublicAndCRUDToAdmin(t *testing.T) {
	service, _ := announcementDatabase(t, "announcement-http")
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Register(router, router.Group("/api/v1/merchant"), router.Group("/api/v1/admin"), service)

	created := performAnnouncement(t, router, http.MethodPost, "/api/v1/admin/announcements", `{"title":"公告","content":"内容","status":"published"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var announcement Announcement
	if err := json.Unmarshal(created.Body.Bytes(), &announcement); err != nil {
		t.Fatal(err)
	}
	if announcement.PublishedAt == nil {
		t.Fatalf("announcement=%+v", announcement)
	}
	for _, path := range []string{"/api/v1/public/announcements", "/api/v1/merchant/announcements"} {
		response := performAnnouncement(t, router, http.MethodGet, path, "")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), announcement.ID) {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
	draft := performAnnouncement(t, router, http.MethodPost, "/api/v1/admin/announcements", `{"title":"草稿","content":"内容","status":"draft"}`)
	if draft.Code != http.StatusCreated {
		t.Fatalf("draft status=%d body=%s", draft.Code, draft.Body.String())
	}
	var draftBody Announcement
	if err := json.Unmarshal(draft.Body.Bytes(), &draftBody); err != nil {
		t.Fatal(err)
	}
	public := performAnnouncement(t, router, http.MethodGet, "/api/v1/public/announcements", "")
	if strings.Contains(public.Body.String(), draftBody.ID) {
		t.Fatalf("draft leaked: %s", public.Body.String())
	}
	listed := performAnnouncement(t, router, http.MethodGet, "/api/v1/admin/announcements", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "\"total\":2") {
		t.Fatalf("admin list status=%d body=%s", listed.Code, listed.Body.String())
	}
	updated := performAnnouncement(t, router, http.MethodPut, "/api/v1/admin/announcements/"+draftBody.ID, `{"title":"草稿改","content":"内容","status":"published"}`)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), "草稿改") {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
	if response := performAnnouncement(t, router, http.MethodPut, "/api/v1/admin/announcements/missing", `{"title":"t","content":"c","status":"draft"}`); response.Code != http.StatusNotFound {
		t.Fatalf("update missing status=%d", response.Code)
	}
	if response := performAnnouncement(t, router, http.MethodPost, "/api/v1/admin/announcements", `{"title":"","content":"c"}`); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid create status=%d", response.Code)
	}
	if response := performAnnouncement(t, router, http.MethodDelete, "/api/v1/admin/announcements/"+draftBody.ID, ""); response.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", response.Code, response.Body.String())
	}
	site := performAnnouncement(t, router, http.MethodGet, "/api/v1/public/site", "")
	if site.Code != http.StatusOK || !strings.Contains(site.Body.String(), "\"name\":\"GorixPay\"") || !strings.Contains(site.Body.String(), "qqpay") {
		t.Fatalf("site status=%d body=%s", site.Code, site.Body.String())
	}
}

func announcementDatabase(t *testing.T, name string) (*Service, *gorm.DB) {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Announcement{}, &systemSetting{}); err != nil {
		t.Fatal(err)
	}
	return New(db), db
}

func performAnnouncement(t *testing.T, router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	return response
}
