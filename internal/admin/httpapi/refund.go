package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	financeservice "gorixpay/internal/finance/service"
	identityhttp "gorixpay/internal/identity/httpapi"
	paymentmodel "gorixpay/internal/payment/model"
	paymentorchestrator "gorixpay/internal/payment/orchestrator"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// refundReconciler is the slice of the payment orchestrator a refund needs.
// Wiring the interface keeps the handler testable without a live router.
type refundReconciler interface {
	Refund(ctx context.Context, order paymentmodel.Order, refundNo string, amount int64, description string) (string, error)
}

// refundOrderRequest is the operator's refund: amount defaults to the full order,
// execute=false only files the request for the normal approval flow.
type refundOrderRequest struct {
	Amount  int64  `json:"amount"`
	Reason  string `json:"reason"`
	Execute bool   `json:"execute"`
}

// refundOrder refunds a paid order from the order list.
//
// It always records a finance refund first, so the money movement is auditable
// even when the channel call fails, and only then asks the channel that captured
// the payment to return the money. A channel that cannot refund still gets the
// bookkeeping record, left pending for a manual bank transfer.
func (handler *Handler) refundOrder(ctx *gin.Context) {
	var request refundOrderRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		invalidRequest(ctx)
		return
	}
	var order paymentmodel.Order
	if err := handler.db.WithContext(ctx.Request.Context()).First(&order, "id = ?", ctx.Param("id")).Error; err != nil {
		notFound(ctx)
		return
	}
	if order.Status != paymentmodel.StatusPaid {
		ctx.JSON(http.StatusConflict, gin.H{"code": "order_not_refundable", "message": "只有已支付的订单可以退款"})
		return
	}
	amount := request.Amount
	if amount <= 0 {
		amount = order.Amount
	}
	if amount > order.Amount {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": "refund_amount_invalid", "message": "退款金额不能超过订单金额"})
		return
	}
	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		reason = "管理员退款"
	}
	refund, _, err := handler.finance.CreateRefund(ctx.Request.Context(), financeservice.CreateRefundRequest{
		MerchantID:     order.MerchantID,
		PaymentOrderID: order.ID,
		OutRefundNo:    "AR" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))[:20],
		Amount:         amount,
		Reason:         reason,
	})
	if err != nil {
		financeError(ctx, err)
		return
	}
	if !request.Execute {
		ctx.JSON(http.StatusOK, gin.H{"refund": refund, "channel": "pending", "message": "退款申请已提交，等待审核"})
		return
	}
	channelRefundNo, err := handler.refundChannel(ctx, order, refund.RefundNo, amount, reason)
	if err != nil {
		// The record stays pending: the operator can retry once the channel problem
		// is fixed, and nothing is silently marked as refunded.
		ctx.JSON(http.StatusBadGateway, gin.H{
			"refund": refund, "code": "channel_refund_failed",
			"message": "通道退款失败：" + err.Error(),
		})
		return
	}
	principal := identityhttp.MustSession(ctx)
	approved, err := handler.finance.ProcessRefund(ctx.Request.Context(), financeservice.ProcessRequest{
		ID:          refund.ID,
		Action:      "approve",
		ProcessedBy: principal.PrincipalID,
	})
	if err != nil {
		financeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"refund": approved, "channel": "refunded", "channel_refund_no": channelRefundNo})
}

// refundChannel asks the capturing channel to return the money. A deployment
// without an orchestrator, or a channel without refund support, answers with a
// typed error the caller reports instead of pretending the refund happened.
func (handler *Handler) refundChannel(ctx *gin.Context, order paymentmodel.Order, refundNo string, amount int64, reason string) (string, error) {
	if handler.channelRefund == nil {
		return "", paymentorchestrator.ErrRefundUnsupported
	}
	return handler.channelRefund.Refund(ctx.Request.Context(), order, refundNo, amount, reason)
}

var _ = errors.Is
