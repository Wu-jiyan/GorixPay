package reporting

import (
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// RegisterMerchantExports mounts the complaint and profit-sharing CSV downloads
// for the merchant portal. The caller passes the authenticated /api/v1 group and
// the session middleware, matching Register.
func RegisterMerchantExports(group *gin.RouterGroup, db *gorm.DB, sessions gin.HandlerFunc) {
	if group == nil || db == nil || sessions == nil {
		return
	}
	handler := &Handler{db: db, now: time.Now}
	merchant := group.Group("/merchant", sessions, requireMerchant)
	merchant.GET("/export/complaints.csv", handler.exportMerchantComplaints)
	merchant.GET("/export/profit-sharing.csv", handler.exportMerchantProfitSharing)
}

type merchantComplaintRow struct {
	ComplaintNo string     `gorm:"column:complaint_no"`
	Channel     string     `gorm:"column:channel"`
	Status      string     `gorm:"column:status"`
	Reason      string     `gorm:"column:reason"`
	AmountCents int64      `gorm:"column:amount_cents"`
	DeadlineAt  *time.Time `gorm:"column:deadline_at"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
}

func (merchantComplaintRow) TableName() string { return "complaints" }

type merchantProfitRow struct {
	ID              string    `gorm:"column:id"`
	OrderID         string    `gorm:"column:order_id"`
	ReceiverName    *string   `gorm:"column:receiver_name"`
	ReceiverAccount *string   `gorm:"column:receiver_account"`
	Amount          int64     `gorm:"column:amount"`
	Status          string    `gorm:"column:status"`
	CreatedAt       time.Time `gorm:"column:created_at"`
}

func (handler *Handler) exportMerchantComplaints(ctx *gin.Context) {
	merchantID := identityMerchantID(ctx)
	var rows []merchantComplaintRow
	if err := handler.db.WithContext(ctx.Request.Context()).
		Where("merchant_id = ?", merchantID).
		Order("created_at DESC").
		Limit(maxExportRows + 1).Find(&rows).Error; err != nil {
		internalError(ctx)
		return
	}
	rows, truncated := trimExport(rows)
	header := []string{"投诉单号", "渠道", "状态", "原因", "金额", "创建时间"}
	records := make([][]string, 0, len(rows))
	now := handler.now().UTC()
	for _, row := range rows {
		status := row.Status
		if status == "pending" && row.DeadlineAt != nil && now.After(row.DeadlineAt.UTC()) {
			status = "overdue"
		}
		records = append(records, []string{
			row.ComplaintNo,
			row.Channel,
			status,
			row.Reason,
			yuan(row.AmountCents),
			timeValue(row.CreatedAt),
		})
	}
	writeExport(ctx, "merchant-complaints", header, records, truncated)
}

func (handler *Handler) exportMerchantProfitSharing(ctx *gin.Context) {
	merchantID := identityMerchantID(ctx)
	var rows []merchantProfitRow
	if err := handler.db.WithContext(ctx.Request.Context()).
		Table("profit_sharing_orders AS o").
		Select("o.id, o.order_id, r.name AS receiver_name, r.account AS receiver_account, o.amount, o.status, o.created_at").
		Joins("LEFT JOIN profit_sharing_receivers AS r ON r.id = o.receiver_id").
		Where("o.merchant_id = ?", merchantID).
		Order("o.created_at DESC").
		Limit(maxExportRows + 1).
		Scan(&rows).Error; err != nil {
		internalError(ctx)
		return
	}
	rows, truncated := trimExport(rows)
	header := []string{"分账单号", "订单号", "接收方", "金额", "状态", "创建时间"}
	records := make([][]string, 0, len(rows))
	for _, row := range rows {
		receiver := textPointer(row.ReceiverName)
		if receiver == "" {
			receiver = textPointer(row.ReceiverAccount)
		}
		records = append(records, []string{
			row.ID,
			row.OrderID,
			receiver,
			yuan(row.Amount),
			row.Status,
			timeValue(row.CreatedAt),
		})
	}
	writeExport(ctx, "merchant-profit-sharing", header, records, truncated)
}
