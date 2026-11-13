package httpapi

import (
	"net/http"
	"strconv"
	"time"

	financemodel "gorixpay/internal/finance/model"
	identitymodel "gorixpay/internal/identity/model"
	paymentmodel "gorixpay/internal/payment/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	defaultStatsDays = 14
	maxStatsDays     = 90
)

type ExtrasHandler struct {
	db  *gorm.DB
	now func() time.Time
}

type extrasCredential struct {
	ID             string                         `json:"id"`
	CredentialType identitymodel.CredentialType   `json:"credential_type"`
	KeyID          *string                        `json:"key_id"`
	Status         identitymodel.CredentialStatus `json:"status"`
	CreatedAt      time.Time                      `json:"created_at"`
}

type extrasAccount struct {
	ID        string `json:"id" gorm:"column:id"`
	Purpose   string `json:"purpose" gorm:"column:purpose"`
	Currency  string `json:"currency" gorm:"column:currency"`
	Available int64  `json:"available" gorm:"column:available"`
	Frozen    int64  `json:"frozen" gorm:"column:frozen"`
}

type dailyStat struct {
	Date       string `json:"date"`
	OrderCount int64  `json:"order_count"`
	PaidCount  int64  `json:"paid_count"`
	PaidAmount int64  `json:"paid_amount"`
}

type summaryStat struct {
	MerchantCount      int64 `json:"merchant_count"`
	OrderCount         int64 `json:"order_count"`
	PaidOrderCount     int64 `json:"paid_order_count"`
	PaidAmount         int64 `json:"paid_amount"`
	TodayPaidAmount    int64 `json:"today_paid_amount"`
	PendingRefunds     int64 `json:"pending_refunds"`
	PendingSettlements int64 `json:"pending_settlements"`
}

// RegisterExtras mounts the additional administrator endpoints. The group is
// the already authenticated /api/v1/admin group; Register stays untouched.
func RegisterExtras(group *gin.RouterGroup, db *gorm.DB) {
	handler := &ExtrasHandler{db: db, now: time.Now}
	group.GET("/merchants/:id", handler.merchantDetail)
	group.GET("/stats/daily", handler.statsDaily)
	group.GET("/stats/summary", handler.statsSummary)
}

func (handler *ExtrasHandler) merchantDetail(ctx *gin.Context) {
	request := ctx.Request.Context()
	var merchant identitymodel.Merchant
	if err := handler.db.WithContext(request).First(&merchant, "id = ?", ctx.Param("id")).Error; err != nil {
		notFound(ctx)
		return
	}
	var credentials []identitymodel.MerchantCredential
	if err := handler.db.WithContext(request).
		Where("merchant_id = ?", merchant.ID).
		Order("created_at DESC").Find(&credentials).Error; err != nil {
		internalError(ctx)
		return
	}
	summaries := make([]extrasCredential, 0, len(credentials))
	for _, credential := range credentials {
		summaries = append(summaries, extrasCredential{
			ID: credential.ID, CredentialType: credential.CredentialType, KeyID: credential.KeyID,
			Status: credential.Status, CreatedAt: credential.CreatedAt,
		})
	}
	var accounts []extrasAccount
	if err := handler.db.WithContext(request).
		Table("ledger_accounts AS a").
		Select("a.id AS id, a.purpose AS purpose, a.currency AS currency, COALESCE(b.available, 0) AS available, COALESCE(b.frozen, 0) AS frozen").
		Joins("LEFT JOIN ledger_balances AS b ON b.account_id = a.id").
		Where("a.owner_type = ? AND a.owner_id = ?", "merchant", merchant.ID).
		Order("a.purpose ASC, a.currency ASC").
		Scan(&accounts).Error; err != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{
		"id":           merchant.ID,
		"legacy_uid":   merchant.LegacyUID,
		"display_name": merchant.DisplayName,
		"email":        merchant.Email,
		"phone":        merchant.Phone,
		"status":       merchant.Status,
		"created_at":   merchant.CreatedAt,
		"updated_at":   merchant.UpdatedAt,
		"credentials":  summaries,
		"accounts":     accounts,
	})
}

func (handler *ExtrasHandler) statsDaily(ctx *gin.Context) {
	days := defaultStatsDays
	if raw := ctx.Query("days"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maxStatsDays {
			invalidRequest(ctx)
			return
		}
		days = parsed
	}
	request := ctx.Request.Context()
	today := startOfDay(handler.now().UTC())
	items := make([]dailyStat, 0, days)
	for offset := days - 1; offset >= 0; offset-- {
		dayStart := today.AddDate(0, 0, -offset)
		dayEnd := dayStart.AddDate(0, 0, 1)
		item := dailyStat{Date: dayStart.Format("2006-01-02")}
		if err := handler.db.WithContext(request).Model(&paymentmodel.Order{}).
			Where("created_at >= ? AND created_at < ?", dayStart, dayEnd).
			Count(&item.OrderCount).Error; err != nil {
			internalError(ctx)
			return
		}
		paidQuery := func() *gorm.DB {
			return handler.db.WithContext(request).Model(&paymentmodel.Order{}).
				Where("status = ? AND paid_at >= ? AND paid_at < ?", paymentmodel.StatusPaid, dayStart, dayEnd)
		}
		if err := paidQuery().Count(&item.PaidCount).Error; err != nil {
			internalError(ctx)
			return
		}
		if err := paidQuery().Select("COALESCE(SUM(amount), 0)").Scan(&item.PaidAmount).Error; err != nil {
			internalError(ctx)
			return
		}
		items = append(items, item)
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items})
}

func (handler *ExtrasHandler) statsSummary(ctx *gin.Context) {
	request := ctx.Request.Context()
	db := handler.db.WithContext(request)
	summary := summaryStat{}
	today := startOfDay(handler.now().UTC())
	if err := db.Model(&identitymodel.Merchant{}).Count(&summary.MerchantCount).Error; err != nil {
		internalError(ctx)
		return
	}
	if err := db.Model(&paymentmodel.Order{}).Count(&summary.OrderCount).Error; err != nil {
		internalError(ctx)
		return
	}
	if err := db.Model(&paymentmodel.Order{}).Where("status = ?", paymentmodel.StatusPaid).
		Count(&summary.PaidOrderCount).Error; err != nil {
		internalError(ctx)
		return
	}
	if err := db.Model(&paymentmodel.Order{}).Where("status = ?", paymentmodel.StatusPaid).
		Select("COALESCE(SUM(amount), 0)").Scan(&summary.PaidAmount).Error; err != nil {
		internalError(ctx)
		return
	}
	if err := db.Model(&paymentmodel.Order{}).Where("status = ? AND paid_at >= ?", paymentmodel.StatusPaid, today).
		Select("COALESCE(SUM(amount), 0)").Scan(&summary.TodayPaidAmount).Error; err != nil {
		internalError(ctx)
		return
	}
	if err := db.Model(&financemodel.Refund{}).Where("status = ?", financemodel.StatusPending).
		Count(&summary.PendingRefunds).Error; err != nil {
		internalError(ctx)
		return
	}
	if err := db.Model(&financemodel.Settlement{}).Where("status = ?", financemodel.StatusPending).
		Count(&summary.PendingSettlements).Error; err != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, summary)
}

func startOfDay(at time.Time) time.Time {
	utc := at.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}
