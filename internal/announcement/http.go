package announcement

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

type writeRequest struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Status  Status `json:"status"`
}

// Register mounts the public, merchant and administrator announcement routes.
// The merchant and administrator groups are already authenticated by the
// caller, so this package only adds feature routes.
func Register(router *gin.Engine, groupMerchant *gin.RouterGroup, groupAdmin *gin.RouterGroup, service *Service) {
	handler := &Handler{service: service}
	router.GET("/api/v1/public/announcements", handler.published)
	router.GET("/api/v1/public/site", handler.site)
	groupMerchant.GET("/announcements", handler.published)
	groupAdmin.GET("/announcements", handler.list)
	groupAdmin.POST("/announcements", handler.create)
	groupAdmin.PUT("/announcements/:id", handler.update)
	groupAdmin.DELETE("/announcements/:id", handler.remove)
}

func (handler *Handler) site(ctx *gin.Context) {
	site, err := handler.service.Site(ctx.Request.Context())
	if err != nil {
		announcementInternalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, site)
}

func (handler *Handler) published(ctx *gin.Context) {
	items, err := handler.service.ListPublished(ctx.Request.Context())
	if err != nil {
		announcementInternalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items})
}

func (handler *Handler) list(ctx *gin.Context) {
	page, size := announcementPagination(ctx)
	items, total, err := handler.service.List(ctx.Request.Context(), Page{Number: page, Size: size})
	if err != nil {
		announcementInternalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

func (handler *Handler) create(ctx *gin.Context) {
	var request writeRequest
	if ctx.ShouldBindJSON(&request) != nil {
		announcementInvalidRequest(ctx)
		return
	}
	created, err := handler.service.Create(ctx.Request.Context(), CreateInput{Title: request.Title, Content: request.Content, Status: request.Status})
	if err != nil {
		announcementError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, created)
}

func (handler *Handler) update(ctx *gin.Context) {
	var request writeRequest
	if ctx.ShouldBindJSON(&request) != nil {
		announcementInvalidRequest(ctx)
		return
	}
	updated, err := handler.service.Update(ctx.Request.Context(), ctx.Param("id"), UpdateInput{Title: request.Title, Content: request.Content, Status: request.Status})
	if err != nil {
		announcementError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, updated)
}

func (handler *Handler) remove(ctx *gin.Context) {
	if err := handler.service.Delete(ctx.Request.Context(), ctx.Param("id")); err != nil {
		announcementError(ctx, err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func announcementPagination(ctx *gin.Context) (int, int) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}

func announcementError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		announcementInvalidRequest(ctx)
	case errors.Is(err, ErrNotFound):
		ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "announcement not found"})
	default:
		announcementInternalError(ctx)
	}
}

func announcementInvalidRequest(ctx *gin.Context) {
	ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "invalid request"})
}

func announcementInternalError(ctx *gin.Context) {
	ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
}
