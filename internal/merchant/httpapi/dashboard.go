package httpapi

import (
	"net/http"
	"time"

	financemodel "gorixpay/internal/finance/model"
	identityhttp "gorixpay/internal/identity/httpapi"
	ledgermodel "gorixpay/internal/ledger/model"
	paymentmodel "gorixpay/internal/payment/model"

	"github.com/gin-gonic/gin"
)

type dashboardResponse struct {
	TodayOrderCount    int64 `json:"today_order_count"`
	TodayPaidAmount    int64 `json:"today_paid_amount"`
	TotalPaidAmount    int64 `json:"total_paid_amount"`
	AvailableBalance   int64 `json:"available_balance"`
	FrozenBalance      int64 `json:"frozen_balance"`
	PendingRefunds     int64 `json:"pending_refunds"`
	PendingSettlements int64 `json:"pending_settlements"`
}

// dashboard reports the merchant's own order, balance and review counters. All
// amounts are in the smallest currency unit (cents), matching the V1 protocol.
func (handler *ProfileHandler) dashboard(ctx *gin.Context) {
	ctxRequest := ctx.Request.Context()
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	today := startOfDay(time.Now().UTC())
	result := dashboardResponse{}
	db := handler.db.WithContext(ctxRequest)

	if err := db.Model(&paymentmodel.Order{}).
		Where("merchant_id = ? AND created_at >= ?", merchantID, today).
		Count(&result.TodayOrderCount).Error; err != nil {
		profileInternalError(ctx)
		return
	}
	if err := db.Model(&paymentmodel.Order{}).
		Where("merchant_id = ? AND status = ? AND paid_at >= ?", merchantID, paymentmodel.StatusPaid, today).
		Select("COALESCE(SUM(amount), 0)").Scan(&result.TodayPaidAmount).Error; err != nil {
		profileInternalError(ctx)
		return
	}
	if err := db.Model(&paymentmodel.Order{}).
		Where("merchant_id = ? AND status = ?", merchantID, paymentmodel.StatusPaid).
		Select("COALESCE(SUM(amount), 0)").Scan(&result.TotalPaidAmount).Error; err != nil {
		profileInternalError(ctx)
		return
	}
	balance := struct {
		Available int64
		Frozen    int64
	}{}
	if err := db.Model(&ledgermodel.Account{}).
		Select("COALESCE(SUM(ledger_balances.available), 0) AS available, COALESCE(SUM(ledger_balances.frozen), 0) AS frozen").
		Joins("LEFT JOIN ledger_balances ON ledger_balances.account_id = ledger_accounts.id").
		Where("ledger_accounts.owner_type = ? AND ledger_accounts.owner_id = ?", "merchant", merchantID).
		Scan(&balance).Error; err != nil {
		profileInternalError(ctx)
		return
	}
	result.AvailableBalance = balance.Available
	result.FrozenBalance = balance.Frozen
	if err := db.Model(&financemodel.Refund{}).
		Where("merchant_id = ? AND status = ?", merchantID, financemodel.StatusPending).
		Count(&result.PendingRefunds).Error; err != nil {
		profileInternalError(ctx)
		return
	}
	if err := db.Model(&financemodel.Settlement{}).
		Where("merchant_id = ? AND status = ?", merchantID, financemodel.StatusPending).
		Count(&result.PendingSettlements).Error; err != nil {
		profileInternalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, result)
}

func startOfDay(at time.Time) time.Time {
	utc := at.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}
