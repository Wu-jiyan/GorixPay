package health

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

type Checker interface {
	Check(context.Context) error
}

func Register(router gin.IRoutes, checker Checker) {
	router.GET("/health/live", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/health/ready", func(ctx *gin.Context) {
		if err := checker.Check(ctx.Request.Context()); err != nil {
			ctx.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
			return
		}
		ctx.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
}
