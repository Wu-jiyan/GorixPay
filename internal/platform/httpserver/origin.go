package httpserver

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// OriginGuard is the second line of CSRF defence for the console API.
//
// The SPA authenticates with an HttpOnly cookie, and SameSite=Lax already stops a
// cross-site form POST from carrying it. This guard covers the remaining cases
// (same-site subdomains, older WebViews, a browser that still attaches the
// cookie): a state-changing /api/v1 request whose Origin or Referer names another
// host is refused.
//
// Requests without either header are server-to-server clients (protocol
// integrations, curl, tests) and are left alone. The classic Epay endpoints
// (/api.php, /submit.php, /mapi.php, payment callbacks) are not under /api/v1 and
// are never touched: their callers are merchant websites and upstream channels.
func OriginGuard(publicURL string) gin.HandlerFunc {
	allowed := publicHost(publicURL)
	return func(ctx *gin.Context) {
		if allowed == "" || !strings.HasPrefix(ctx.Request.URL.Path, "/api/v1/") {
			ctx.Next()
			return
		}
		switch ctx.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			ctx.Next()
			return
		}
		origin := strings.TrimSpace(ctx.GetHeader("Origin"))
		if origin == "" {
			origin = strings.TrimSpace(ctx.GetHeader("Referer"))
		}
		if origin == "" {
			// No browser origin at all: a server-to-server client.
			ctx.Next()
			return
		}
		if origin == "null" {
			// Opaque origin: a sandboxed frame or a file:// page.
			ctx.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    "cross_origin_rejected",
				"message": "cross-origin request rejected",
			})
			return
		}
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host == "" {
			ctx.Next()
			return
		}
		if strings.EqualFold(parsed.Host, allowed) {
			ctx.Next()
			return
		}
		ctx.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"code":    "cross_origin_rejected",
			"message": "cross-origin request rejected",
		})
	}
}

func publicHost(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return parsed.Host
}
