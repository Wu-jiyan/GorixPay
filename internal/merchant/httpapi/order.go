package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	identityhttp "gorixpay/internal/identity/httpapi"
	paymentmodel "gorixpay/internal/payment/model"
	paymentquery "gorixpay/internal/payment/query"

	"github.com/gin-gonic/gin"
)

type OrderHandler struct {
	orders *paymentquery.Service
}

func RegisterOrders(group *gin.RouterGroup, orders *paymentquery.Service) {
	handler := &OrderHandler{orders: orders}
	group.GET("/orders", handler.list)
	group.GET("/orders/statistics", handler.statistics)
	group.GET("/orders/:id", handler.detail)
}

func (handler *OrderHandler) list(ctx *gin.Context) {
	filter, page, ok := orderQuery(ctx)
	if !ok {
		return
	}
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	items, total, err := handler.orders.Orders(ctx.Request.Context(), merchantID, filter, page)
	if err != nil {
		orderInternalError(ctx)
		return
	}
	responses := make([]gin.H, 0, len(items))
	for _, item := range items {
		responses = append(responses, orderResponse(item))
	}
	ctx.JSON(http.StatusOK, gin.H{"items": responses, "total": total, "page": page.Number, "page_size": page.Size})
}

func (handler *OrderHandler) detail(ctx *gin.Context) {
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	order, err := handler.orders.Order(ctx.Request.Context(), merchantID, ctx.Param("id"))
	if err != nil {
		if errors.Is(err, paymentquery.ErrNotFound) {
			ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "order not found"})
			return
		}
		orderInternalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, orderResponse(order))
}

func (handler *OrderHandler) statistics(ctx *gin.Context) {
	filter, _, ok := orderQuery(ctx)
	if !ok {
		return
	}
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	statistics, err := handler.orders.Statistics(ctx.Request.Context(), merchantID, filter)
	if err != nil {
		orderInternalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, statistics)
}

func orderQuery(ctx *gin.Context) (paymentquery.OrderFilter, paymentquery.Page, bool) {
	page, err := positiveInt(ctx.DefaultQuery("page", "1"))
	if err != nil {
		orderInvalidRequest(ctx)
		return paymentquery.OrderFilter{}, paymentquery.Page{}, false
	}
	size, err := positiveInt(ctx.DefaultQuery("page_size", "20"))
	if err != nil || size > 100 {
		orderInvalidRequest(ctx)
		return paymentquery.OrderFilter{}, paymentquery.Page{}, false
	}
	status := paymentmodel.Status(ctx.Query("status"))
	if status != "" && !validOrderStatus(status) {
		orderInvalidRequest(ctx)
		return paymentquery.OrderFilter{}, paymentquery.Page{}, false
	}
	from, err := optionalTime(ctx.Query("created_from"))
	if err != nil {
		orderInvalidRequest(ctx)
		return paymentquery.OrderFilter{}, paymentquery.Page{}, false
	}
	to, err := optionalTime(ctx.Query("created_to"))
	if err != nil || (from != nil && to != nil && !from.Before(*to)) {
		orderInvalidRequest(ctx)
		return paymentquery.OrderFilter{}, paymentquery.Page{}, false
	}
	return paymentquery.OrderFilter{Status: status, PaymentType: ctx.Query("payment_type"), Keyword: ctx.Query("keyword"), CreatedFrom: from, CreatedTo: to}, paymentquery.Page{Number: page, Size: size}, true
}

func positiveInt(value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return 0, errors.New("positive integer required")
	}
	return parsed, nil
}

func optionalTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func validOrderStatus(status paymentmodel.Status) bool {
	return status == paymentmodel.StatusPending || status == paymentmodel.StatusProcessing || status == paymentmodel.StatusPaid || status == paymentmodel.StatusClosed || status == paymentmodel.StatusFailed
}

func orderResponse(order paymentmodel.Order) gin.H {
	return gin.H{"id": order.ID, "trade_no": order.TradeNo, "out_trade_no": order.OutTradeNo, "payment_type": order.PaymentType, "name": order.Name, "amount": order.Amount, "currency": order.Currency, "status": order.Status, "notify_url": order.NotifyURL, "return_url": order.ReturnURL, "client_ip": order.ClientIP, "attach": order.Attach, "channel_trade_no": order.ChannelTradeNo, "paid_at": order.PaidAt, "expires_at": order.ExpiresAt, "created_at": order.CreatedAt, "updated_at": order.UpdatedAt}
}

func orderInvalidRequest(ctx *gin.Context) {
	ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "invalid request"})
}

func orderInternalError(ctx *gin.Context) {
	ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
}
