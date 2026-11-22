package health

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Build information, overridden at link time by the release workflow:
//
//	go build -ldflags "-X gorixpay/internal/platform/health.Version=v2.3.0 \
//	  -X gorixpay/internal/platform/health.Commit=abc1234 \
//	  -X gorixpay/internal/platform/health.BuiltAt=2026-11-22T08:00:00Z"
//
// A plain `go build` leaves them as "dev", which is what a local run should say.
var (
	Version = "dev"
	Commit  = "unknown"
	BuiltAt = "unknown"
)

type Checker interface {
	Check(context.Context) error
}

// Register mounts the two probes. /health/live answers with the build identity so
// an operator can confirm which release is actually serving traffic without
// reading the process list; /health/ready additionally checks the dependencies.
func Register(router gin.IRoutes, checker Checker) {
	router.GET("/health/live", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{
			"status":   "ok",
			"version":  Version,
			"commit":   Commit,
			"built_at": BuiltAt,
		})
	})
	router.GET("/health/ready", func(ctx *gin.Context) {
		if err := checker.Check(ctx.Request.Context()); err != nil {
			ctx.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable", "version": Version})
			return
		}
		ctx.JSON(http.StatusOK, gin.H{"status": "ok", "version": Version})
	})
}
