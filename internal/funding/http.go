package funding

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	identityhttp "gorixpay/internal/identity/httpapi"
	paymentmodel "gorixpay/internal/payment/model"
	paymentservice "gorixpay/internal/payment/service"

	"github.com/gin-gonic/gin"
)

const (
	maxPageSizeRequest = 100
)

type Handler struct {
	service *Service
}

// RegisterMerchant mounts the merchant funding routes on an already
// authenticated merchant group.
func RegisterMerchant(group *gin.RouterGroup, service *Service) {
	handler := &Handler{service: service}
	group.POST("/recharge", handler.createRecharge)
	group.GET("/recharge", handler.listRecharge)
	group.POST("/deposit", handler.createDeposit)
	group.GET("/deposit", handler.depositInfo)
	group.GET("/deposit/orders", handler.listDeposit)
}

// RegisterAdmin mounts the read-only administrator funding listings on an
// already authenticated administrator group. No endpoint here moves money.
func RegisterAdmin(group *gin.RouterGroup, service *Service) {
	handler := &Handler{service: service}
	group.GET("/funding/recharge", handler.adminList(paymentmodel.KindRecharge))
	group.GET("/funding/deposit", handler.adminList(paymentmodel.KindDeposit))
}

func (handler *Handler) createRecharge(ctx *gin.Context) {
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	amount, ok := fundingAmount(ctx)
	if !ok {
		return
	}
	result, err := handler.service.CreateRecharge(ctx.Request.Context(), merchantID, amount)
	if err != nil {
		fundingError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, result)
}

func (handler *Handler) createDeposit(ctx *gin.Context) {
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	amount, ok := fundingAmount(ctx)
	if !ok {
		return
	}
	result, err := handler.service.CreateDeposit(ctx.Request.Context(), merchantID, amount)
	if err != nil {
		fundingError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, result)
}

func (handler *Handler) listRecharge(ctx *gin.Context) {
	handler.list(ctx, paymentmodel.KindRecharge, identityhttp.MustSession(ctx).PrincipalID)
}

func (handler *Handler) listDeposit(ctx *gin.Context) {
	handler.list(ctx, paymentmodel.KindDeposit, identityhttp.MustSession(ctx).PrincipalID)
}

func (handler *Handler) depositInfo(ctx *gin.Context) {
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	info, err := handler.service.DepositInfo(ctx.Request.Context(), merchantID)
	if err != nil {
		fundingError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, info)
}

func (handler *Handler) adminList(kind paymentmodel.Kind) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		handler.list(ctx, kind, strings.TrimSpace(ctx.Query("merchant_id")))
	}
}

func (handler *Handler) list(ctx *gin.Context, kind paymentmodel.Kind, merchantID string) {
	page, size := fundingPagination(ctx)
	items, total, err := handler.service.List(ctx.Request.Context(), ListFilter{MerchantID: merchantID, Kind: kind}, Page{Number: page, Size: size})
	if err != nil {
		fundingError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": fundingOrders(items), "total": total, "page": page, "page_size": size})
}

func fundingOrders(items []paymentmodel.Order) []gin.H {
	responses := make([]gin.H, 0, len(items))
	for _, item := range items {
		responses = append(responses, gin.H{
			"id": item.ID, "trade_no": item.TradeNo, "out_trade_no": item.OutTradeNo,
			"payment_type": item.PaymentType, "name": item.Name, "amount": item.Amount,
			"currency": item.Currency, "status": item.Status, "kind": item.Kind,
			"notify_url": item.NotifyURL, "return_url": item.ReturnURL, "client_ip": item.ClientIP,
			"attach": item.Attach, "channel_trade_no": item.ChannelTradeNo, "paid_at": item.PaidAt,
			"expires_at": item.ExpiresAt, "created_at": item.CreatedAt, "updated_at": item.UpdatedAt,
		})
	}
	return responses
}

func fundingAmount(ctx *gin.Context) (int64, bool) {
	var request struct {
		Amount *int64 `json:"amount"`
	}
	if ctx.ShouldBindJSON(&request) != nil || request.Amount == nil {
		fundingInvalidRequest(ctx)
		return 0, false
	}
	if paymentmodel.ValidateAmount(*request.Amount) != nil {
		fundingInvalidRequest(ctx)
		return 0, false
	}
	return *request.Amount, true
}

func fundingPagination(ctx *gin.Context) (int, int) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > maxPageSizeRequest {
		size = maxPageSizeRequest
	}
	return page, size
}

func fundingError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidRequest), errors.Is(err, paymentservice.ErrInvalidCommand):
		fundingInvalidRequest(ctx)
	case errors.Is(err, paymentservice.ErrIdempotencyConflict):
		ctx.JSON(http.StatusConflict, gin.H{"code": "idempotency_conflict", "message": "重复的充值请求"})
	default:
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
	}
}

func fundingInvalidRequest(ctx *gin.Context) {
	ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "invalid request"})
}
