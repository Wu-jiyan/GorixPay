package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	financemodel "gorixpay/internal/finance/model"
	financeservice "gorixpay/internal/finance/service"
	identityhttp "gorixpay/internal/identity/httpapi"
	"gorixpay/internal/settings"

	"github.com/gin-gonic/gin"
)

type FinanceHandler struct {
	finance *financeservice.Service
	// settings reads payment.merchant_refund_enabled: the operator can take the
	// self-service refund button away from every merchant at once.
	settings *settings.Reader
}

func RegisterFinance(group *gin.RouterGroup, finance *financeservice.Service) {
	handler := &FinanceHandler{finance: finance}
	registerFinanceRoutes(group, handler)
}

// RegisterFinanceWithSettings mounts the same routes with the setting reader that
// gates merchant self-service refunds.
func RegisterFinanceWithSettings(group *gin.RouterGroup, finance *financeservice.Service, reader *settings.Reader) {
	handler := &FinanceHandler{finance: finance, settings: reader}
	registerFinanceRoutes(group, handler)
}

func registerFinanceRoutes(group *gin.RouterGroup, handler *FinanceHandler) {
	group.GET("/refunds", handler.refunds)
	group.POST("/refunds", handler.createRefund)
	group.GET("/refunds/:id", handler.refund)
	group.POST("/settlements", handler.createSettlement)
	group.GET("/settlements", handler.settlements)
	group.GET("/settlements/:id", handler.settlement)
}

// refundAllowed answers whether merchants may start refunds themselves. Missing
// setting or missing reader means enabled, which is the historical behaviour.
func (handler *FinanceHandler) refundAllowed(ctx *gin.Context) bool {
	if handler.settings == nil {
		return true
	}
	return handler.settings.Bool(ctx.Request.Context(), "payment.merchant_refund_enabled", true)
}

func (handler *FinanceHandler) refunds(ctx *gin.Context) {
	page, size := financePagination(ctx)
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	items, total, err := handler.finance.Refunds(ctx.Request.Context(), financeservice.Filter{MerchantID: merchantID, Status: financemodel.Status(strings.TrimSpace(ctx.Query("status"))), Keyword: strings.TrimSpace(ctx.Query("keyword"))}, financeservice.Page{Number: page, Size: size})
	if err != nil {
		financeInternalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

func (handler *FinanceHandler) createRefund(ctx *gin.Context) {
	if !handler.refundAllowed(ctx) {
		ctx.JSON(http.StatusForbidden, gin.H{"code": "refund_disabled", "message": "平台未开放商户自助退款，请联系运营"})
		return
	}
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	var request struct {
		PaymentOrderID string `json:"payment_order_id" binding:"required"`
		OutRefundNo    string `json:"out_refund_no" binding:"required"`
		Amount         int64  `json:"amount" binding:"required"`
		Reason         string `json:"reason" binding:"required"`
	}
	if ctx.ShouldBindJSON(&request) != nil {
		invalidFinanceRequest(ctx)
		return
	}
	refund, created, err := handler.finance.CreateRefund(ctx.Request.Context(), financeservice.CreateRefundRequest{MerchantID: merchantID, PaymentOrderID: request.PaymentOrderID, OutRefundNo: request.OutRefundNo, Amount: request.Amount, Reason: request.Reason})
	if err != nil {
		financeError(ctx, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	ctx.JSON(status, refund)
}

func (handler *FinanceHandler) refund(ctx *gin.Context) {
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	item, err := handler.finance.Refund(ctx.Request.Context(), merchantID, ctx.Param("id"))
	if err != nil {
		financeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, item)
}

func (handler *FinanceHandler) createSettlement(ctx *gin.Context) {
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	var request struct {
		OutSettlementNo string `json:"out_settlement_no" binding:"required"`
		Amount          int64  `json:"amount" binding:"required"`
		Currency        string `json:"currency" binding:"required"`
		Destination     string `json:"destination" binding:"required"`
	}
	if ctx.ShouldBindJSON(&request) != nil {
		invalidFinanceRequest(ctx)
		return
	}
	settlement, created, err := handler.finance.CreateSettlement(ctx.Request.Context(), financeservice.CreateSettlementRequest{MerchantID: merchantID, OutSettlementNo: request.OutSettlementNo, Amount: request.Amount, Currency: request.Currency, Destination: request.Destination})
	if err != nil {
		financeError(ctx, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	ctx.JSON(status, settlement)
}

func (handler *FinanceHandler) settlements(ctx *gin.Context) {
	page, size := financePagination(ctx)
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	items, total, err := handler.finance.Settlements(ctx.Request.Context(), financeservice.Filter{MerchantID: merchantID, Status: financemodel.Status(strings.TrimSpace(ctx.Query("status"))), Keyword: strings.TrimSpace(ctx.Query("keyword"))}, financeservice.Page{Number: page, Size: size})
	if err != nil {
		financeInternalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

func (handler *FinanceHandler) settlement(ctx *gin.Context) {
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	item, err := handler.finance.Settlement(ctx.Request.Context(), merchantID, ctx.Param("id"))
	if err != nil {
		financeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, item)
}

func financeError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, financeservice.ErrInvalidRequest):
		invalidFinanceRequest(ctx)
	case errors.Is(err, financeservice.ErrNotFound):
		financeNotFound(ctx)
	case errors.Is(err, financeservice.ErrRefundNotSale):
		ctx.JSON(http.StatusConflict, gin.H{"code": "refund_not_allowed", "message": "充值/保证金订单不支持退款"})
	case errors.Is(err, financeservice.ErrPaymentNotPaid), errors.Is(err, financeservice.ErrRefundAmountExceeded), errors.Is(err, financeservice.ErrStateConflict), errors.Is(err, financeservice.ErrIdempotencyConflict):
		ctx.JSON(http.StatusConflict, gin.H{"code": "finance_conflict", "message": err.Error()})
	default:
		financeInternalError(ctx)
	}
}

func financePagination(ctx *gin.Context) (int, int) {
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

func invalidFinanceRequest(ctx *gin.Context) {
	ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "invalid request"})
}
func financeNotFound(ctx *gin.Context) {
	ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "resource not found"})
}
func financeInternalError(ctx *gin.Context) {
	ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
}
