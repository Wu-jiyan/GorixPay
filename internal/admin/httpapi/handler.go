package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	channelconfig "gorixpay/internal/channel/config"
	channelrouter "gorixpay/internal/channel/router"
	financemodel "gorixpay/internal/finance/model"
	financeservice "gorixpay/internal/finance/service"
	identityhttp "gorixpay/internal/identity/httpapi"
	identitymodel "gorixpay/internal/identity/model"
	ledgermodel "gorixpay/internal/ledger/model"
	ledgerquery "gorixpay/internal/ledger/query"
	ledgerservice "gorixpay/internal/ledger/service"
	"gorixpay/internal/outbox"
	paymentmodel "gorixpay/internal/payment/model"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Handler struct {
	db           *gorm.DB
	configs      *channelconfig.Service
	ledger       *ledgerquery.Service
	ledgerWriter *ledgerservice.Service
	finance      *financeservice.Service
	outbox       *outbox.QueryService
}

type channelRequest struct {
	Name        string               `json:"name" binding:"required"`
	PluginCode  string               `json:"plugin_code" binding:"required"`
	PaymentType string               `json:"payment_type" binding:"required"`
	Priority    int                  `json:"priority"`
	Weight      int                  `json:"weight"`
	Status      channelrouter.Status `json:"status"`
	APIURL      string               `json:"api_url" binding:"required"`
	PID         string               `json:"pid" binding:"required"`
	Key         string               `json:"key" binding:"required"`
}

func Register(group *gin.RouterGroup, db *gorm.DB, sessions gin.HandlerFunc, configs *channelconfig.Service, ledger *ledgerquery.Service, ledgerWriter *ledgerservice.Service, finance *financeservice.Service, outboxQuery *outbox.QueryService) {
	handler := &Handler{db: db, configs: configs, ledger: ledger, ledgerWriter: ledgerWriter, finance: finance, outbox: outboxQuery}
	admin := group.Group("/admin", sessions, identityhttp.RequireAdministrator())
	admin.Use(handler.audit)
	admin.GET("/dashboard", handler.dashboard)
	admin.GET("/settings", handler.settings)
	admin.PUT("/settings/:key", handler.updateSetting)
	admin.GET("/audit-events", handler.auditEvents)
	admin.GET("/merchants", handler.merchants)
	admin.PATCH("/merchants/:id/status", handler.merchantStatus)
	admin.GET("/orders", handler.orders)
	admin.GET("/orders/:id", handler.order)
	admin.POST("/orders/:id/close", handler.closeOrder)
	admin.GET("/channels", handler.channels)
	admin.POST("/channels", handler.createChannel)
	admin.PUT("/channels/:id", handler.updateChannel)
	admin.PATCH("/channels/:id/status", handler.channelStatus)
	admin.GET("/refunds", handler.refunds)
	admin.GET("/refunds/:id", handler.refund)
	admin.POST("/refunds/:id/process", handler.processRefund)
	admin.GET("/settlements", handler.settlements)
	admin.GET("/settlements/:id", handler.settlement)
	admin.POST("/settlements/:id/process", handler.processSettlement)
	admin.GET("/ledger/accounts", handler.ledgerAccounts)
	admin.POST("/ledger/accounts", handler.createLedgerAccount)
	admin.GET("/ledger/accounts/:id", handler.ledgerAccount)
	admin.GET("/ledger/accounts/:id/entries", handler.ledgerEntries)
	admin.GET("/ledger/transactions", handler.ledgerTransactions)
	admin.GET("/ledger/transactions/:id", handler.ledgerTransaction)
	admin.GET("/ledger/operations", handler.ledgerOperations)
	admin.GET("/ledger/operations/:id", handler.ledgerOperation)
	admin.POST("/ledger/operations", handler.createLedgerOperation)
	admin.POST("/ledger/operations/:id/process", handler.processLedgerOperation)
	admin.GET("/outbox/messages", handler.outboxMessages)
	admin.GET("/outbox/stats", handler.outboxStats)
	admin.POST("/outbox/requeue-dead", handler.outboxRequeueDead)
}

func (handler *Handler) dashboard(ctx *gin.Context) {
	var merchants, orders, paidOrders int64
	var paidAmount int64
	handler.db.WithContext(ctx.Request.Context()).Model(&identitymodel.Merchant{}).Count(&merchants)
	handler.db.WithContext(ctx.Request.Context()).Model(&paymentmodel.Order{}).Count(&orders)
	handler.db.WithContext(ctx.Request.Context()).Model(&paymentmodel.Order{}).Where("status = ?", paymentmodel.StatusPaid).Count(&paidOrders)
	handler.db.WithContext(ctx.Request.Context()).Model(&paymentmodel.Order{}).Where("status = ?", paymentmodel.StatusPaid).Select("COALESCE(SUM(amount), 0)").Scan(&paidAmount)
	ctx.JSON(http.StatusOK, gin.H{"merchant_count": merchants, "order_count": orders, "paid_order_count": paidOrders, "paid_amount": paidAmount})
}

func (handler *Handler) merchants(ctx *gin.Context) {
	page, size := pagination(ctx)
	query := handler.db.WithContext(ctx.Request.Context()).Model(&identitymodel.Merchant{})
	if keyword := strings.TrimSpace(ctx.Query("keyword")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("display_name LIKE ? OR email LIKE ? OR phone LIKE ?", like, like, like)
	}
	if status := ctx.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}
	var total int64
	query.Count(&total)
	var items []identitymodel.Merchant
	if err := query.Order("created_at DESC").Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

func (handler *Handler) merchantStatus(ctx *gin.Context) {
	var request struct {
		Status identitymodel.Status `json:"status" binding:"required"`
	}
	if ctx.ShouldBindJSON(&request) != nil || (request.Status != identitymodel.StatusActive && request.Status != identitymodel.StatusDisabled) {
		invalidRequest(ctx)
		return
	}
	result := handler.db.WithContext(ctx.Request.Context()).Model(&identitymodel.Merchant{}).Where("id = ?", ctx.Param("id")).Updates(map[string]any{"status": request.Status, "updated_at": time.Now().UTC()})
	if result.Error != nil || result.RowsAffected != 1 {
		notFound(ctx)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (handler *Handler) orders(ctx *gin.Context) {
	page, size := pagination(ctx)
	query := handler.db.WithContext(ctx.Request.Context()).Model(&paymentmodel.Order{})
	for field, value := range map[string]string{"status": ctx.Query("status"), "merchant_id": ctx.Query("merchant_id"), "payment_type": ctx.Query("payment_type")} {
		if value != "" {
			query = query.Where(field+" = ?", value)
		}
	}
	if keyword := strings.TrimSpace(ctx.Query("keyword")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("trade_no LIKE ? OR out_trade_no LIKE ? OR name LIKE ?", like, like, like)
	}
	var total int64
	query.Count(&total)
	var items []paymentmodel.Order
	if err := query.Order("created_at DESC").Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

func (handler *Handler) order(ctx *gin.Context) {
	var order paymentmodel.Order
	if err := handler.db.WithContext(ctx.Request.Context()).First(&order, "id = ?", ctx.Param("id")).Error; err != nil {
		notFound(ctx)
		return
	}
	ctx.JSON(http.StatusOK, order)
}

func (handler *Handler) closeOrder(ctx *gin.Context) {
	now := time.Now().UTC()
	result := handler.db.WithContext(ctx.Request.Context()).Model(&paymentmodel.Order{}).
		Where("id = ? AND status IN ?", ctx.Param("id"), []paymentmodel.Status{paymentmodel.StatusPending, paymentmodel.StatusProcessing}).
		Updates(map[string]any{"status": paymentmodel.StatusClosed, "updated_at": now})
	if result.Error != nil || result.RowsAffected != 1 {
		ctx.JSON(http.StatusConflict, gin.H{"code": "order_not_closable", "message": "order cannot be closed"})
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (handler *Handler) channels(ctx *gin.Context) {
	var channels []channelrouter.Channel
	if err := handler.db.WithContext(ctx.Request.Context()).Order("priority ASC, created_at DESC").Find(&channels).Error; err != nil {
		internalError(ctx)
		return
	}
	items := make([]gin.H, 0, len(channels))
	for _, channel := range channels {
		items = append(items, gin.H{"id": channel.ID, "name": channel.Name, "plugin_code": channel.PluginCode, "payment_type": channel.PaymentType, "priority": channel.Priority, "weight": channel.Weight, "status": channel.Status, "configured": channel.ConfigEncrypted != "", "created_at": channel.CreatedAt, "updated_at": channel.UpdatedAt})
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items})
}

func (handler *Handler) createChannel(ctx *gin.Context) {
	var request channelRequest
	if ctx.ShouldBindJSON(&request) != nil || request.Weight <= 0 {
		invalidRequest(ctx)
		return
	}
	now := time.Now().UTC()
	channel := channelrouter.Channel{ID: uuid.NewString(), Name: request.Name, PluginCode: request.PluginCode, PaymentType: request.PaymentType, Priority: request.Priority, Weight: request.Weight, Status: normalizeChannelStatus(request.Status), CreatedAt: now, UpdatedAt: now}
	encrypted, err := handler.configs.Encrypt(channel.ID, channelconfig.Config{APIURL: request.APIURL, PID: request.PID, Key: request.Key})
	if err != nil {
		invalidRequest(ctx)
		return
	}
	channel.ConfigEncrypted = encrypted
	if err := handler.db.WithContext(ctx.Request.Context()).Create(&channel).Error; err != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusCreated, gin.H{"id": channel.ID})
}

func (handler *Handler) updateChannel(ctx *gin.Context) {
	var request channelRequest
	if ctx.ShouldBindJSON(&request) != nil || request.Weight <= 0 {
		invalidRequest(ctx)
		return
	}
	encrypted, err := handler.configs.Encrypt(ctx.Param("id"), channelconfig.Config{APIURL: request.APIURL, PID: request.PID, Key: request.Key})
	if err != nil {
		invalidRequest(ctx)
		return
	}
	result := handler.db.WithContext(ctx.Request.Context()).Model(&channelrouter.Channel{}).Where("id = ?", ctx.Param("id")).Updates(map[string]any{"name": request.Name, "plugin_code": request.PluginCode, "payment_type": request.PaymentType, "priority": request.Priority, "weight": request.Weight, "status": normalizeChannelStatus(request.Status), "config_encrypted": encrypted, "updated_at": time.Now().UTC()})
	if result.Error != nil || result.RowsAffected != 1 {
		notFound(ctx)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (handler *Handler) channelStatus(ctx *gin.Context) {
	var request struct {
		Status channelrouter.Status `json:"status" binding:"required"`
	}
	if ctx.ShouldBindJSON(&request) != nil || (request.Status != channelrouter.StatusActive && request.Status != channelrouter.StatusDisabled) {
		invalidRequest(ctx)
		return
	}
	result := handler.db.WithContext(ctx.Request.Context()).Model(&channelrouter.Channel{}).Where("id = ?", ctx.Param("id")).Updates(map[string]any{"status": request.Status, "updated_at": time.Now().UTC()})
	if result.Error != nil || result.RowsAffected != 1 {
		notFound(ctx)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (handler *Handler) ledgerAccounts(ctx *gin.Context) {
	page, size := pagination(ctx)
	items, total, err := handler.ledger.Accounts(ctx.Request.Context(), ledgerquery.AccountFilter{OwnerType: ctx.Query("owner_type"), OwnerID: ctx.Query("owner_id"), Purpose: ctx.Query("purpose"), Currency: strings.ToUpper(ctx.Query("currency")), Status: ctx.Query("status")}, ledgerquery.Page{Number: page, Size: size})
	if err != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

func (handler *Handler) createLedgerAccount(ctx *gin.Context) {
	var request struct {
		OwnerType string                      `json:"owner_type" binding:"required"`
		OwnerID   string                      `json:"owner_id" binding:"required"`
		Purpose   string                      `json:"purpose" binding:"required"`
		Category  ledgermodel.AccountCategory `json:"category" binding:"required"`
		Currency  string                      `json:"currency" binding:"required"`
	}
	if ctx.ShouldBindJSON(&request) != nil {
		invalidRequest(ctx)
		return
	}
	account, err := handler.ledgerWriter.CreateAccount(ctx.Request.Context(), ledgerservice.AccountRequest{OwnerType: request.OwnerType, OwnerID: request.OwnerID, Purpose: request.Purpose, Category: request.Category, Currency: request.Currency})
	if err != nil {
		ledgerWriteError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, account)
}

func (handler *Handler) ledgerAccount(ctx *gin.Context) {
	item, err := handler.ledger.Account(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		ledgerError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, item)
}

func (handler *Handler) ledgerEntries(ctx *gin.Context) {
	page, size := pagination(ctx)
	if _, err := handler.ledger.Account(ctx.Request.Context(), ctx.Param("id")); err != nil {
		ledgerError(ctx, err)
		return
	}
	items, total, err := handler.ledger.Entries(ctx.Request.Context(), ctx.Param("id"), ledgerquery.Page{Number: page, Size: size})
	if err != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

func (handler *Handler) ledgerTransactions(ctx *gin.Context) {
	page, size := pagination(ctx)
	items, total, err := handler.ledger.Transactions(ctx.Request.Context(), ledgerquery.TransactionFilter{ReferenceType: ctx.Query("reference_type"), ReferenceID: ctx.Query("reference_id"), IdempotencyKey: ctx.Query("idempotency_key")}, ledgerquery.Page{Number: page, Size: size})
	if err != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

func (handler *Handler) ledgerTransaction(ctx *gin.Context) {
	item, err := handler.ledger.Transaction(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		ledgerError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, item)
}

func (handler *Handler) ledgerOperations(ctx *gin.Context) {
	page, size := pagination(ctx)
	items, total, err := handler.ledger.Operations(ctx.Request.Context(), ledgerquery.OperationFilter{Status: ctx.Query("status"), OperationType: ctx.Query("operation_type"), AccountID: ctx.Query("account_id"), ReferenceType: ctx.Query("reference_type"), ReferenceID: ctx.Query("reference_id"), CreatedBy: ctx.Query("created_by")}, ledgerquery.Page{Number: page, Size: size})
	if err != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

func (handler *Handler) ledgerOperation(ctx *gin.Context) {
	operation, err := handler.ledger.Operation(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		ledgerError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, operation)
}

func (handler *Handler) createLedgerOperation(ctx *gin.Context) {
	var request struct {
		IdempotencyKey        string                    `json:"idempotency_key" binding:"required"`
		OperationType         ledgermodel.OperationType `json:"operation_type" binding:"required"`
		AccountID             string                    `json:"account_id" binding:"required"`
		CounterpartyAccountID string                    `json:"counterparty_account_id" binding:"required"`
		Amount                int64                     `json:"amount" binding:"required"`
		ReferenceType         string                    `json:"reference_type" binding:"required"`
		ReferenceID           string                    `json:"reference_id" binding:"required"`
		Description           string                    `json:"description"`
		Metadata              string                    `json:"metadata"`
	}
	if ctx.ShouldBindJSON(&request) != nil {
		invalidRequest(ctx)
		return
	}
	principal := identityhttp.MustSession(ctx)
	operation, created, err := handler.ledgerWriter.CreateOperation(ctx.Request.Context(), ledgerservice.OperationRequest{IdempotencyKey: request.IdempotencyKey, OperationType: request.OperationType, AccountID: request.AccountID, CounterpartyAccountID: request.CounterpartyAccountID, Amount: request.Amount, ReferenceType: request.ReferenceType, ReferenceID: request.ReferenceID, Description: request.Description, Metadata: request.Metadata, CreatedBy: principal.PrincipalID})
	if err != nil {
		ledgerWriteError(ctx, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	ctx.JSON(status, operation)
}

func (handler *Handler) processLedgerOperation(ctx *gin.Context) {
	var request struct {
		Action          string `json:"action" binding:"required"`
		RejectionReason string `json:"rejection_reason"`
	}
	if ctx.ShouldBindJSON(&request) != nil {
		invalidRequest(ctx)
		return
	}
	principal := identityhttp.MustSession(ctx)
	operation, err := handler.ledgerWriter.ProcessOperation(ctx.Request.Context(), ledgerservice.ProcessOperationRequest{OperationID: ctx.Param("id"), Action: request.Action, ProcessedBy: principal.PrincipalID, RejectionReason: request.RejectionReason})
	if err != nil {
		ledgerWriteError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, operation)
}

func ledgerError(ctx *gin.Context, err error) {
	if errors.Is(err, ledgerquery.ErrNotFound) {
		notFound(ctx)
		return
	}
	internalError(ctx)
}

func ledgerWriteError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, ledgerservice.ErrInvalidRequest), errors.Is(err, ledgerservice.ErrUnbalanced):
		invalidRequest(ctx)
	case errors.Is(err, ledgerservice.ErrAccountNotFound), errors.Is(err, ledgerservice.ErrOperationNotFound):
		notFound(ctx)
	case errors.Is(err, ledgerservice.ErrInsufficientBalance):
		ctx.JSON(http.StatusConflict, gin.H{"code": "insufficient_balance", "message": "insufficient ledger balance"})
	case errors.Is(err, ledgerservice.ErrIdempotencyConflict), errors.Is(err, ledgerservice.ErrOperationProcessed):
		ctx.JSON(http.StatusConflict, gin.H{"code": "ledger_conflict", "message": "ledger request conflicts with existing state"})
	default:
		internalError(ctx)
	}
}

func pagination(ctx *gin.Context) (int, int) {
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

func normalizeChannelStatus(status channelrouter.Status) channelrouter.Status {
	if status == channelrouter.StatusDisabled {
		return status
	}
	return channelrouter.StatusActive
}

func (handler *Handler) refunds(ctx *gin.Context) {
	page, size := pagination(ctx)
	items, total, err := handler.finance.Refunds(ctx.Request.Context(), financeservice.Filter{MerchantID: ctx.Query("merchant_id"), Status: financemodel.Status(ctx.Query("status")), Keyword: ctx.Query("keyword")}, financeservice.Page{Number: page, Size: size})
	if err != nil { internalError(ctx); return }
	ctx.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

func (handler *Handler) refund(ctx *gin.Context) {
	item, err := handler.finance.Refund(ctx.Request.Context(), "", ctx.Param("id"))
	if err != nil { financeError(ctx, err); return }
	ctx.JSON(http.StatusOK, item)
}

func (handler *Handler) processRefund(ctx *gin.Context) {
	var request struct { Action string `json:"action" binding:"required"`; RejectionReason string `json:"rejection_reason"` }
	if ctx.ShouldBindJSON(&request) != nil { invalidRequest(ctx); return }
	principal := identityhttp.MustSession(ctx)
	item, err := handler.finance.ProcessRefund(ctx.Request.Context(), financeservice.ProcessRequest{ID: ctx.Param("id"), Action: request.Action, ProcessedBy: principal.PrincipalID, RejectionReason: request.RejectionReason})
	if err != nil { financeError(ctx, err); return }
	ctx.JSON(http.StatusOK, item)
}

func (handler *Handler) settlements(ctx *gin.Context) {
	page, size := pagination(ctx)
	items, total, err := handler.finance.Settlements(ctx.Request.Context(), financeservice.Filter{MerchantID: ctx.Query("merchant_id"), Status: financemodel.Status(ctx.Query("status")), Keyword: ctx.Query("keyword")}, financeservice.Page{Number: page, Size: size})
	if err != nil { internalError(ctx); return }
	ctx.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

func (handler *Handler) settlement(ctx *gin.Context) {
	item, err := handler.finance.Settlement(ctx.Request.Context(), "", ctx.Param("id"))
	if err != nil { financeError(ctx, err); return }
	ctx.JSON(http.StatusOK, item)
}

func (handler *Handler) processSettlement(ctx *gin.Context) {
	var request struct { Action string `json:"action" binding:"required"`; RejectionReason string `json:"rejection_reason"` }
	if ctx.ShouldBindJSON(&request) != nil { invalidRequest(ctx); return }
	principal := identityhttp.MustSession(ctx)
	item, err := handler.finance.ProcessSettlement(ctx.Request.Context(), financeservice.ProcessRequest{ID: ctx.Param("id"), Action: request.Action, ProcessedBy: principal.PrincipalID, RejectionReason: request.RejectionReason})
	if err != nil { financeError(ctx, err); return }
	ctx.JSON(http.StatusOK, item)
}

func financeError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, financeservice.ErrInvalidRequest): invalidRequest(ctx)
	case errors.Is(err, financeservice.ErrNotFound): notFound(ctx)
	case errors.Is(err, financeservice.ErrPaymentNotPaid), errors.Is(err, financeservice.ErrRefundAmountExceeded), errors.Is(err, financeservice.ErrStateConflict), errors.Is(err, financeservice.ErrIdempotencyConflict):
		ctx.JSON(http.StatusConflict, gin.H{"code": "finance_conflict", "message": err.Error()})
	default: internalError(ctx)
	}
}

func (handler *Handler) outboxMessages(ctx *gin.Context) {
	page, size := pagination(ctx)
	items, total, err := handler.outbox.Messages(ctx.Request.Context(), ctx.Query("status"), outbox.Page{Number: page, Size: size})
	if err != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": size})
}

func (handler *Handler) outboxStats(ctx *gin.Context) {
	stats, err := handler.outbox.Stats(ctx.Request.Context())
	if err != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, stats)
}

func (handler *Handler) outboxRequeueDead(ctx *gin.Context) {
	count, err := handler.outbox.RequeueDead(ctx.Request.Context())
	if err != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"requeued": count})
}

func invalidRequest(ctx *gin.Context) {
	ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "invalid request"})
}
func notFound(ctx *gin.Context) {
	ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "resource not found"})
}
func internalError(ctx *gin.Context) {
	ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
}
