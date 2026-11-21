package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	identityhttp "gorixpay/internal/identity/httpapi"
	paymentcallback "gorixpay/internal/payment/callback"
	paymentmodel "gorixpay/internal/payment/model"

	"github.com/gin-gonic/gin"
)

// completionService is the slice of the payment callback service a manual 补单
// needs: the same transaction a channel callback runs, so the ledger, the
// post-payment hooks and the merchant notification happen exactly once.
type completionService interface {
	Complete(ctx context.Context, completion paymentcallback.Completion) (paymentmodel.Order, bool, error)
}

// WithCompletionService wires manual completion (POST /admin/orders/:id/complete).
func WithCompletionService(service completionService) Option {
	return func(handler *Handler) { handler.completions = service }
}

// completeOrderRequest marks an order paid by hand. Upstream Epay calls this
// 手动补单 on the admin order list: when a channel confirmed the money but its
// callback never arrived, the operator still has to settle the order.
type completeOrderRequest struct {
	ChannelTradeNo string `json:"channel_trade_no"`
	Reason         string `json:"reason"`
}

// batchCloseRequest closes many orders at once, the 批量操作 of upstream's order
// list. Unpaid orders only: a paid order has to be refunded, not closed.
type batchCloseRequest struct {
	IDs []string `json:"ids"`
}

// batchCloseOrders closes the payable orders among ids and reports how many were
// closed, so the caller can tell "closed 7 of 9" from a silent failure.
func (handler *Handler) batchCloseOrders(ctx *gin.Context) {
	var request batchCloseRequest
	if err := ctx.ShouldBindJSON(&request); err != nil || len(request.IDs) == 0 {
		invalidRequest(ctx)
		return
	}
	if len(request.IDs) > 200 {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": "too_many_ids", "message": "单次最多处理 200 笔"})
		return
	}
	now := time.Now().UTC()
	result := handler.db.WithContext(ctx.Request.Context()).Model(&paymentmodel.Order{}).
		Where("id IN ? AND status IN ?", request.IDs, []paymentmodel.Status{paymentmodel.StatusPending, paymentmodel.StatusProcessing}).
		Updates(map[string]any{"status": paymentmodel.StatusClosed, "updated_at": now})
	if result.Error != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"closed": result.RowsAffected, "requested": len(request.IDs)})
}

// completeOrder settles an order by hand through the channel-callback path.
func (handler *Handler) completeOrder(ctx *gin.Context) {
	var request completeOrderRequest
	if err := ctx.ShouldBindJSON(&request); err != nil && !errors.Is(err, io.EOF) {
		invalidRequest(ctx)
		return
	}
	var order paymentmodel.Order
	if err := handler.db.WithContext(ctx.Request.Context()).First(&order, "id = ?", ctx.Param("id")).Error; err != nil {
		notFound(ctx)
		return
	}
	if order.Status == paymentmodel.StatusPaid {
		ctx.JSON(http.StatusConflict, gin.H{"code": "order_already_paid", "message": "订单已是已支付状态"})
		return
	}
	if order.Status != paymentmodel.StatusPending && order.Status != paymentmodel.StatusProcessing {
		ctx.JSON(http.StatusConflict, gin.H{"code": "order_not_completable", "message": "该订单状态不能补单"})
		return
	}
	if handler.completions == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": "completion_unavailable", "message": "支付完成服务未接线"})
		return
	}
	channelID, err := handler.orderChannelID(ctx, order.ID)
	if err != nil {
		ctx.JSON(http.StatusConflict, gin.H{"code": "order_channel_missing", "message": "该订单没有可用的支付通道，无法补单"})
		return
	}
	channelTradeNo := strings.TrimSpace(request.ChannelTradeNo)
	if channelTradeNo == "" {
		channelTradeNo = "MANUAL-" + order.TradeNo
	}
	principal := identityhttp.MustSession(ctx)
	// The digest is the idempotency key of the completion: one manual completion
	// per order, and a repeat answers with the already-paid order instead of
	// crediting the merchant twice.
	paid, applied, err := handler.completions.Complete(ctx.Request.Context(), paymentcallback.Completion{
		OrderID:        order.ID,
		ChannelID:      channelID,
		ChannelTradeNo: channelTradeNo,
		Amount:         order.Amount,
		Digest:         "manual:" + order.ID + ":" + principal.PrincipalID,
	})
	if err != nil {
		ctx.JSON(http.StatusConflict, gin.H{"code": "order_not_completable", "message": "补单失败：" + err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"order": paid, "applied": applied})
}

// orderChannelID finds the channel that was used for this order.
func (handler *Handler) orderChannelID(ctx *gin.Context, orderID string) (string, error) {
	var attempt paymentmodel.Attempt
	err := handler.db.WithContext(ctx.Request.Context()).
		Where("order_id = ? AND channel_id IS NOT NULL", orderID).
		Order("attempt_no DESC").First(&attempt).Error
	if err != nil {
		return "", err
	}
	if attempt.ChannelID == nil || strings.TrimSpace(*attempt.ChannelID) == "" {
		return "", errors.New("order has no channel attempt")
	}
	return *attempt.ChannelID, nil
}
