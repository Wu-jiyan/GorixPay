package httpserver

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRouterProvidesRequestIDSecurityHeadersAndJSONNotFound(t *testing.T) {
	var logs strings.Builder
	router := NewRouter(slog.New(slog.NewJSONHandler(&logs, nil)), gin.TestMode)
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	request.Header.Set("Authorization", "secret-token")
	request.Header.Set("Cookie", "session=secret")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || response.Header().Get("X-Request-ID") == "" {
		t.Fatalf("status = %d, request ID = %q", response.Code, response.Header().Get("X-Request-ID"))
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(response.Body.String(), `"code":"not_found"`) {
		t.Fatal("security headers or JSON error missing")
	}
	if strings.Contains(logs.String(), "secret-token") || strings.Contains(logs.String(), "session=secret") {
		t.Fatal("sensitive request headers leaked to logs")
	}
}

func TestRouterRecoversAsJSON(t *testing.T) {
	router := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), gin.TestMode)
	router.GET("/panic", func(*gin.Context) { panic("failure") })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), `"code":"internal_error"`) {
		t.Fatalf("unexpected recovery response: %d %s", response.Code, response.Body.String())
	}
}
