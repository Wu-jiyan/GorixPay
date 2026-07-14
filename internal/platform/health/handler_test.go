package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type checkerFunc func(context.Context) error

func (function checkerFunc) Check(ctx context.Context) error {
	return function(ctx)
}

func TestHealthHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Register(router, checkerFunc(func(context.Context) error { return nil }))
	for _, path := range []string{"/health/live", "/health/ready"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"ok"`) {
			t.Fatalf("%s: %d %s", path, response.Code, response.Body.String())
		}
	}
}

func TestReadinessFailureDoesNotLeakDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Register(router, checkerFunc(func(context.Context) error { return errors.New("postgres://user:password@db/private") }))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "password") {
		t.Fatalf("unsafe readiness response: %d %s", response.Code, response.Body.String())
	}
}
