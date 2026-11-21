package httpapi

import (
	"errors"
	"net/http"

	identityhttp "gorixpay/internal/identity/httpapi"
	"gorixpay/internal/outbox"
	paymentquery "gorixpay/internal/payment/query"

	"github.com/gin-gonic/gin"
)

// OrderNotifyHandler re-arms the asynchronous notification of one order.
//
// Upstream Epay offers the same button on the merchant order list
// (user/ajax2.php act=notify): when the merchant's callback endpoint was down for
// a while the message is dead, and without a manual resend the merchant has no
// way to reconcile the order.
type OrderNotifyHandler struct {
	orders *paymentquery.Service
	outbox *outbox.QueryService
}

// RegisterOrderNotify mounts POST /merchant/orders/:id/notify. A nil outbox keeps
// the route off, so a deployment without the worker does not advertise a button
// that cannot do anything.
func RegisterOrderNotify(group *gin.RouterGroup, orders *paymentquery.Service, queue *outbox.QueryService) {
	if orders == nil || queue == nil {
		return
	}
	handler := &OrderNotifyHandler{orders: orders, outbox: queue}
	group.POST("/orders/:id/notify", handler.resend)
}

func (handler *OrderNotifyHandler) resend(ctx *gin.Context) {
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
	count, err := handler.outbox.RequeueByAggregate(ctx.Request.Context(), order.ID)
	if err != nil {
		orderInternalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"requeued": count})
}