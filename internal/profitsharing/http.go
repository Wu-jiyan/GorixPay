package profitsharing

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	identityhttp "gorixpay/internal/identity/httpapi"
	ledgerservice "gorixpay/internal/ledger/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Handler exposes the profit sharing endpoints.
type Handler struct {
	service *Service
}

// Register builds a service and mounts its routes. group is the /api/v1 group;
// sessions must be the identity middleware, so the admin routes can require the
// administrator principal.
func Register(group *gin.RouterGroup, db *gorm.DB, sessions gin.HandlerFunc, ledger *ledgerservice.Service) {
	if db == nil || ledger == nil {
		return
	}
	Mount(group, sessions, New(db, ledger, time.Now))
}

// Mount wires the routes onto an existing service. Use it when the same instance
// also has to be registered as a payment hook or given a fee resolver.
func Mount(group *gin.RouterGroup, sessions gin.HandlerFunc, service *Service) {
	if group == nil || sessions == nil || service == nil {
		return
	}
	handler := &Handler{service: service}
	merchant := group.Group("/merchant", sessions)
	merchant.GET("/profit-sharing/receivers", handler.merchantReceivers)
	merchant.POST("/profit-sharing/receivers", handler.merchantCreateReceiver)
	merchant.PATCH("/profit-sharing/receivers/:id", handler.merchantUpdateReceiver)
	merchant.DELETE("/profit-sharing/receivers/:id", handler.merchantDeleteReceiver)
	merchant.GET("/profit-sharing/orders", handler.merchantOrders)
	admin := group.Group("/admin", sessions, identityhttp.RequireAdministrator())
	admin.GET("/profit-sharing/receivers", handler.adminReceivers)
	admin.POST("/profit-sharing/receivers", handler.adminCreateReceiver)
	admin.PATCH("/profit-sharing/receivers/:id", handler.adminUpdateReceiver)
	admin.DELETE("/profit-sharing/receivers/:id", handler.adminDeleteReceiver)
	admin.GET("/profit-sharing/orders", handler.adminOrders)
	admin.POST("/profit-sharing/orders/:id/retry", handler.adminRetry)
}

type createReceiverRequest struct {
	MerchantID   string `json:"merchant_id"`
	ReceiverType string `json:"receiver_type"`
	Account      string `json:"account"`
	Name         string `json:"name"`
	RateBp       *int   `json:"rate_bp"`
	Status       string `json:"status"`
}

type updateReceiverRequest struct {
	ReceiverType *string `json:"receiver_type"`
	Account      *string `json:"account"`
	Name         *string `json:"name"`
	RateBp       *int    `json:"rate_bp"`
	Status       *string `json:"status"`
}

func (handler *Handler) merchantReceivers(ctx *gin.Context) {
	principal := identityhttp.MustSession(ctx)
	status, ok := receiverStatusFromQuery(ctx)
	if !ok {
		return
	}
	items, err := handler.service.Receivers(ctx.Request.Context(), ReceiverFilter{MerchantID: principal.PrincipalID, Status: status})
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items})
}

func (handler *Handler) merchantCreateReceiver(ctx *gin.Context) {
	principal := identityhttp.MustSession(ctx)
	var request createReceiverRequest
	if err := ctx.ShouldBindJSON(&request); err != nil || request.RateBp == nil {
		invalid(ctx)
		return
	}
	receiver, err := handler.service.CreateReceiver(ctx.Request.Context(), ReceiverRequest{
		MerchantID:   principal.PrincipalID,
		ReceiverType: ReceiverType(strings.TrimSpace(request.ReceiverType)),
		Account:      request.Account,
		Name:         request.Name,
		RateBp:       *request.RateBp,
		Status:       ReceiverStatus(strings.TrimSpace(request.Status)),
	})
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, receiver)
}

func (handler *Handler) merchantUpdateReceiver(ctx *gin.Context) {
	principal := identityhttp.MustSession(ctx)
	update, ok := bindUpdate(ctx)
	if !ok {
		return
	}
	receiver, err := handler.service.UpdateReceiver(ctx.Request.Context(), principal.PrincipalID, ctx.Param("id"), update)
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, receiver)
}

func (handler *Handler) merchantDeleteReceiver(ctx *gin.Context) {
	principal := identityhttp.MustSession(ctx)
	if err := handler.service.DeleteReceiver(ctx.Request.Context(), principal.PrincipalID, ctx.Param("id")); err != nil {
		writeError(ctx, err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (handler *Handler) merchantOrders(ctx *gin.Context) {
	principal := identityhttp.MustSession(ctx)
	page, ok := pageFromQuery(ctx)
	if !ok {
		return
	}
	status, ok := orderStatusFromQuery(ctx)
	if !ok {
		return
	}
	items, total, err := handler.service.Orders(ctx.Request.Context(), OrderFilter{
		MerchantID: principal.PrincipalID,
		OrderID:    strings.TrimSpace(ctx.Query("order_id")),
		Status:     status,
	}, page)
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page.Number, "page_size": page.Limit()})
}

func (handler *Handler) adminReceivers(ctx *gin.Context) {
	status, ok := receiverStatusFromQuery(ctx)
	if !ok {
		return
	}
	items, err := handler.service.Receivers(ctx.Request.Context(), ReceiverFilter{
		MerchantID: strings.TrimSpace(ctx.Query("merchant_id")),
		Status:     status,
	})
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items})
}

func (handler *Handler) adminCreateReceiver(ctx *gin.Context) {
	var request createReceiverRequest
	if err := ctx.ShouldBindJSON(&request); err != nil || request.RateBp == nil {
		invalid(ctx)
		return
	}
	receiver, err := handler.service.CreateReceiver(ctx.Request.Context(), ReceiverRequest{
		MerchantID:   request.MerchantID,
		ReceiverType: ReceiverType(strings.TrimSpace(request.ReceiverType)),
		Account:      request.Account,
		Name:         request.Name,
		RateBp:       *request.RateBp,
		Status:       ReceiverStatus(strings.TrimSpace(request.Status)),
	})
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, receiver)
}

func (handler *Handler) adminUpdateReceiver(ctx *gin.Context) {
	update, ok := bindUpdate(ctx)
	if !ok {
		return
	}
	receiver, err := handler.service.UpdateReceiver(ctx.Request.Context(), "", ctx.Param("id"), update)
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, receiver)
}

func (handler *Handler) adminDeleteReceiver(ctx *gin.Context) {
	if err := handler.service.DeleteReceiver(ctx.Request.Context(), "", ctx.Param("id")); err != nil {
		writeError(ctx, err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (handler *Handler) adminOrders(ctx *gin.Context) {
	page, ok := pageFromQuery(ctx)
	if !ok {
		return
	}
	status, ok := orderStatusFromQuery(ctx)
	if !ok {
		return
	}
	items, total, err := handler.service.Orders(ctx.Request.Context(), OrderFilter{
		MerchantID: strings.TrimSpace(ctx.Query("merchant_id")),
		OrderID:    strings.TrimSpace(ctx.Query("order_id")),
		Status:     status,
	}, page)
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page.Number, "page_size": page.Limit()})
}

func (handler *Handler) adminRetry(ctx *gin.Context) {
	row, err := handler.service.Retry(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, row)
}

func bindUpdate(ctx *gin.Context) (ReceiverUpdate, bool) {
	var request updateReceiverRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		invalid(ctx)
		return ReceiverUpdate{}, false
	}
	update := ReceiverUpdate{RateBp: request.RateBp}
	if request.ReceiverType != nil {
		value := ReceiverType(strings.TrimSpace(*request.ReceiverType))
		update.ReceiverType = &value
	}
	if request.Account != nil {
		update.Account = request.Account
	}
	if request.Name != nil {
		update.Name = request.Name
	}
	if request.Status != nil {
		value := ReceiverStatus(strings.TrimSpace(*request.Status))
		update.Status = &value
	}
	return update, true
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
		if err != nil || parsed < 1 || parsed > 100 {
			invalid(ctx)
			return Page{}, false
		}
		page.Size = parsed
	}
	return page, true
}

func receiverStatusFromQuery(ctx *gin.Context) (ReceiverStatus, bool) {
	raw := strings.TrimSpace(ctx.Query("status"))
	if raw == "" {
		return "", true
	}
	status := ReceiverStatus(raw)
	if !validReceiverStatus(status) {
		invalid(ctx)
		return "", false
	}
	return status, true
}

func orderStatusFromQuery(ctx *gin.Context) (OrderStatus, bool) {
	raw := strings.TrimSpace(ctx.Query("status"))
	if raw == "" {
		return "", true
	}
	status := OrderStatus(raw)
	if !validOrderStatus(status) {
		invalid(ctx)
		return "", false
	}
	return status, true
}

func invalid(ctx *gin.Context) {
	ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "invalid request"})
}

func writeError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		invalid(ctx)
	case errors.Is(err, ErrNotFound):
		ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "profit sharing record not found"})
	case errors.Is(err, ErrRateExceeded):
		ctx.JSON(http.StatusConflict, gin.H{"code": "rate_exceeded", "message": "receiver rates exceed 100%"})
	case errors.Is(err, ErrReceiverInUse):
		ctx.JSON(http.StatusConflict, gin.H{"code": "receiver_in_use", "message": "receiver already has profit sharing records, disable it instead"})
	case errors.Is(err, ErrOrderNotPaid):
		ctx.JSON(http.StatusConflict, gin.H{"code": "order_not_paid", "message": "payment order is not paid"})
	default:
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
	}
}
