package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func originTestRouter(publicURL string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(OriginGuard(publicURL))
	router.POST("/api/v1/merchant/orders", func(ctx *gin.Context) { ctx.Status(http.StatusOK) })
	router.POST("/submit.php", func(ctx *gin.Context) { ctx.Status(http.StatusOK) })
	return router
}

func performOriginRequest(router http.Handler, path, origin, referer string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, nil)
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if referer != "" {
		request.Header.Set("Referer", referer)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

// TestOriginGuardRejectsForeignConsoleRequests pins the CSRF backstop: with a
// cookie session, a state change announced by another origin must not go through.
func TestOriginGuardRejectsForeignConsoleRequests(t *testing.T) {
	router := originTestRouter("https://pay.example")
	cases := []struct {
		name    string
		origin  string
		referer string
		want    int
	}{
		{"same origin", "https://pay.example", "", http.StatusOK},
		{"same origin with port", "https://pay.example:443", "", http.StatusForbidden},
		{"foreign origin", "https://evil.example", "", http.StatusForbidden},
		{"foreign referer", "", "https://evil.example/page", http.StatusForbidden},
		{"same origin referer", "", "https://pay.example/merchant/orders", http.StatusOK},
		{"opaque origin", "null", "", http.StatusForbidden},
		{"server to server", "", "", http.StatusOK},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := performOriginRequest(router, "/api/v1/merchant/orders", testCase.origin, testCase.referer)
			if response.Code != testCase.want {
				t.Fatalf("status = %d, want %d (body=%s)", response.Code, testCase.want, response.Body.String())
			}
		})
	}
}

// TestOriginGuardLeavesEpayEndpointsAlone keeps the browser form flow working: a
// merchant page posts to /submit.php from its own origin.
func TestOriginGuardLeavesEpayEndpointsAlone(t *testing.T) {
	router := originTestRouter("https://pay.example")
	response := performOriginRequest(router, "/submit.php", "https://shop.example", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
}
