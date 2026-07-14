package httpserver

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func NewRouter(logger *slog.Logger, mode string) *gin.Engine {
	gin.SetMode(mode)
	router := gin.New()
	router.Use(requestID(), securityHeaders(), accessLog(logger), recovery(logger))
	router.NoRoute(func(ctx *gin.Context) {
		ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "resource not found"})
	})
	return router
}

func requestID() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		id := ctx.GetHeader("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}
		ctx.Set("request_id", id)
		ctx.Header("X-Request-ID", id)
		ctx.Next()
	}
}

func securityHeaders() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Header("X-Content-Type-Options", "nosniff")
		ctx.Header("X-Frame-Options", "DENY")
		ctx.Header("Referrer-Policy", "no-referrer")
		ctx.Next()
	}
}

func accessLog(logger *slog.Logger) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		started := time.Now()
		ctx.Next()
		logger.Info("http request",
			"request_id", ctx.GetString("request_id"),
			"method", ctx.Request.Method,
			"path", ctx.Request.URL.Path,
			"status", ctx.Writer.Status(),
			"duration_ms", time.Since(started).Milliseconds(),
		)
	}
}

func recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer func() {
			if value := recover(); value != nil {
				logger.Error("http panic", "request_id", ctx.GetString("request_id"), "panic", value, "stack", string(debug.Stack()))
				ctx.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
			}
		}()
		ctx.Next()
	}
}
