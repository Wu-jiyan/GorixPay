package channelops

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	identityhttp "gorixpay/internal/identity/httpapi"
	"gorixpay/internal/platform/secret"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Handler exposes the payment catalogue, rotation and WeChat MP endpoints.
type Handler struct {
	service *Service
}

// Register builds a service and mounts the public catalogue route plus the
// administrator endpoints. group is the /api/v1 group; sessions must be the
// identity middleware.
func Register(group *gin.RouterGroup, db *gorm.DB, sessions gin.HandlerFunc, cipher *secret.Cipher) {
	if db == nil || cipher == nil {
		return
	}
	Mount(group, sessions, New(db, cipher, time.Now))
}

// Mount wires the routes onto an existing service. Use it when the same instance
// also has to satisfy channelops.RollResolver for the channel router.
func Mount(group *gin.RouterGroup, sessions gin.HandlerFunc, service *Service) {
	if group == nil || sessions == nil || service == nil {
		return
	}
	handler := &Handler{service: service}
	group.GET("/public/pay-types", handler.publicPayTypes)
	admin := group.Group("/admin", sessions, identityhttp.RequireAdministrator())
	admin.GET("/pay-types", handler.adminPayTypes)
	admin.POST("/pay-types", handler.createPayType)
	admin.PUT("/pay-types/:id", handler.updatePayType)
	admin.PATCH("/pay-types/:id/status", handler.setPayTypeStatus)
	admin.DELETE("/pay-types/:id", handler.deletePayType)
	admin.GET("/pay-rolls", handler.adminPayRolls)
	admin.POST("/pay-rolls", handler.createPayRoll)
	admin.POST("/pay-rolls/generate", handler.generatePayRolls)
	admin.PUT("/pay-rolls/:id", handler.updatePayRoll)
	admin.DELETE("/pay-rolls/:id", handler.deletePayRoll)
	admin.GET("/wechat-mp-configs", handler.adminWeChatConfigs)
	admin.POST("/wechat-mp-configs", handler.createWeChatConfig)
	admin.PUT("/wechat-mp-configs/:id", handler.updateWeChatConfig)
	admin.DELETE("/wechat-mp-configs/:id", handler.deleteWeChatConfig)
}

type payTypeRequest struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Icon   string `json:"icon"`
	Sort   int    `json:"sort"`
	Status string `json:"status"`
}

type statusRequest struct {
	Status string `json:"status"`
}

type rollRequest struct {
	ChannelID string `json:"channel_id"`
	RollDate  string `json:"roll_date"`
	Weight    *int   `json:"weight"`
	Status    string `json:"status"`
}

type rollUpdateRequest struct {
	Weight *int    `json:"weight"`
	Status *string `json:"status"`
}

type generateRequest struct {
	ChannelIDs []string `json:"channel_ids"`
	From       string   `json:"from"`
	To         string   `json:"to"`
}

type weChatRequest struct {
	MerchantID string `json:"merchant_id"`
	AppID      string `json:"app_id"`
	AppSecret  string `json:"app_secret"`
	Token      string `json:"token"`
	AESKey     string `json:"aes_key"`
	Status     string `json:"status"`
}

type rollView struct {
	ID        string    `json:"id"`
	ChannelID string    `json:"channel_id"`
	RollDate  string    `json:"roll_date"`
	Weight    int       `json:"weight"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (handler *Handler) publicPayTypes(ctx *gin.Context) {
	items, err := handler.service.PublicPayTypes(ctx.Request.Context())
	if err != nil {
		writeError(ctx, err)
		return
	}
	public := make([]gin.H, 0, len(items))
	for _, item := range items {
		public = append(public, gin.H{"code": item.Code, "name": item.Name, "icon": item.Icon, "sort": item.Sort})
	}
	ctx.JSON(http.StatusOK, gin.H{"items": public})
}

func (handler *Handler) adminPayTypes(ctx *gin.Context) {
	items, err := handler.service.PayTypes(ctx.Request.Context())
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items})
}

func (handler *Handler) createPayType(ctx *gin.Context) {
	var request payTypeRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		invalid(ctx)
		return
	}
	entry, err := handler.service.CreatePayType(ctx.Request.Context(), PayTypeRequest{
		Code: request.Code, Name: request.Name, Icon: request.Icon,
		Sort: request.Sort, Status: Status(strings.TrimSpace(request.Status)),
	})
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, entry)
}

func (handler *Handler) updatePayType(ctx *gin.Context) {
	var request payTypeRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		invalid(ctx)
		return
	}
	entry, err := handler.service.UpdatePayType(ctx.Request.Context(), ctx.Param("id"), PayTypeRequest{
		Code: request.Code, Name: request.Name, Icon: request.Icon,
		Sort: request.Sort, Status: Status(strings.TrimSpace(request.Status)),
	})
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, entry)
}

func (handler *Handler) setPayTypeStatus(ctx *gin.Context) {
	var request statusRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		invalid(ctx)
		return
	}
	entry, err := handler.service.SetPayTypeStatus(ctx.Request.Context(), ctx.Param("id"), Status(strings.TrimSpace(request.Status)))
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, entry)
}

func (handler *Handler) deletePayType(ctx *gin.Context) {
	if err := handler.service.DeletePayType(ctx.Request.Context(), ctx.Param("id")); err != nil {
		writeError(ctx, err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (handler *Handler) adminPayRolls(ctx *gin.Context) {
	page, ok := pageFromQuery(ctx)
	if !ok {
		return
	}
	filter := PayRollFilter{
		ChannelID: strings.TrimSpace(ctx.Query("channel_id")),
		Status:    Status(strings.TrimSpace(ctx.Query("status"))),
	}
	if raw := strings.TrimSpace(ctx.Query("from")); raw != "" {
		day, parsed := parseDay(raw)
		if !parsed {
			invalid(ctx)
			return
		}
		filter.From = &day
	}
	if raw := strings.TrimSpace(ctx.Query("to")); raw != "" {
		day, parsed := parseDay(raw)
		if !parsed {
			invalid(ctx)
			return
		}
		filter.To = &day
	}
	items, total, err := handler.service.PayRolls(ctx.Request.Context(), filter, page)
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": rollViews(items), "total": total, "page": page.Number, "page_size": page.Limit()})
}

func (handler *Handler) createPayRoll(ctx *gin.Context) {
	var request rollRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		invalid(ctx)
		return
	}
	day, parsed := parseDay(request.RollDate)
	if !parsed {
		invalid(ctx)
		return
	}
	weight := 1
	if request.Weight != nil {
		weight = *request.Weight
	}
	roll, err := handler.service.CreatePayRoll(ctx.Request.Context(), PayRollRequest{
		ChannelID: request.ChannelID, Day: day, Weight: weight,
		Status: Status(strings.TrimSpace(request.Status)),
	})
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, rollViewOf(roll))
}

func (handler *Handler) updatePayRoll(ctx *gin.Context) {
	var request rollUpdateRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		invalid(ctx)
		return
	}
	update := PayRollUpdate{Weight: request.Weight}
	if request.Status != nil {
		status := Status(strings.TrimSpace(*request.Status))
		update.Status = &status
	}
	roll, err := handler.service.UpdatePayRoll(ctx.Request.Context(), ctx.Param("id"), update)
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, rollViewOf(roll))
}

func (handler *Handler) deletePayRoll(ctx *gin.Context) {
	if err := handler.service.DeletePayRoll(ctx.Request.Context(), ctx.Param("id")); err != nil {
		writeError(ctx, err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (handler *Handler) generatePayRolls(ctx *gin.Context) {
	var request generateRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		invalid(ctx)
		return
	}
	from, okFrom := parseDay(request.From)
	to, okTo := parseDay(request.To)
	if !okFrom || !okTo {
		invalid(ctx)
		return
	}
	created, err := handler.service.GeneratePayRolls(ctx.Request.Context(), GenerateRequest{
		ChannelIDs: request.ChannelIDs, From: from, To: to,
	})
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"created": created})
}

func (handler *Handler) adminWeChatConfigs(ctx *gin.Context) {
	page, ok := pageFromQuery(ctx)
	if !ok {
		return
	}
	items, total, err := handler.service.WeChatConfigs(ctx.Request.Context(), strings.TrimSpace(ctx.Query("merchant_id")), page)
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page.Number, "page_size": page.Limit()})
}

func (handler *Handler) createWeChatConfig(ctx *gin.Context) {
	var request weChatRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		invalid(ctx)
		return
	}
	view, err := handler.service.CreateWeChatConfig(ctx.Request.Context(), WeChatMPRequest{
		MerchantID: request.MerchantID, AppID: request.AppID, AppSecret: request.AppSecret,
		Token: request.Token, AESKey: request.AESKey, Status: Status(strings.TrimSpace(request.Status)),
	})
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, view)
}

func (handler *Handler) updateWeChatConfig(ctx *gin.Context) {
	var request weChatRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		invalid(ctx)
		return
	}
	view, err := handler.service.UpdateWeChatConfig(ctx.Request.Context(), ctx.Param("id"), WeChatMPRequest{
		MerchantID: request.MerchantID, AppID: request.AppID, AppSecret: request.AppSecret,
		Token: request.Token, AESKey: request.AESKey, Status: Status(strings.TrimSpace(request.Status)),
	})
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, view)
}

func (handler *Handler) deleteWeChatConfig(ctx *gin.Context) {
	if err := handler.service.DeleteWeChatConfig(ctx.Request.Context(), ctx.Param("id")); err != nil {
		writeError(ctx, err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func rollViews(rolls []PayRoll) []rollView {
	views := make([]rollView, 0, len(rolls))
	for _, roll := range rolls {
		views = append(views, rollViewOf(roll))
	}
	return views
}

func rollViewOf(roll PayRoll) rollView {
	return rollView{
		ID: roll.ID, ChannelID: roll.ChannelID, RollDate: roll.RollDate.Format("2006-01-02"),
		Weight: roll.Weight, Status: roll.Status, CreatedAt: roll.CreatedAt, UpdatedAt: roll.UpdatedAt,
	}
}

func pageFromQuery(ctx *gin.Context) (Page, bool) {
	page := Page{Number: 1, Size: 20}
	if raw := strings.TrimSpace(ctx.DefaultQuery("page", "1")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			invalid(ctx)
			return Page{}, false
		}
		page.Number = parsed
	}
	if raw := strings.TrimSpace(ctx.DefaultQuery("page_size", "20")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			invalid(ctx)
			return Page{}, false
		}
		page.Size = parsed
	}
	return page, true
}

// parseDay accepts a plain date and, for convenience, an RFC3339 timestamp.
func parseDay(raw string) (time.Time, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse("2006-01-02", trimmed); err == nil {
		return DayStart(parsed), true
	}
	if parsed, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return DayStart(parsed), true
	}
	return time.Time{}, false
}

func invalid(ctx *gin.Context) {
	ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "invalid request"})
}

func writeError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		invalid(ctx)
	case errors.Is(err, ErrNotFound):
		ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "record not found"})
	case errors.Is(err, ErrDuplicate):
		ctx.JSON(http.StatusConflict, gin.H{"code": "duplicate", "message": "record already exists"})
	case errors.Is(err, ErrInUse):
		ctx.JSON(http.StatusConflict, gin.H{"code": "in_use", "message": "record is still referenced"})
	default:
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
	}
}
