package transfer

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	identityhttp "gorixpay/internal/identity/httpapi"

	"github.com/gin-gonic/gin"
)

// Handler exposes payout endpoints for the merchant portal and the admin console.
type Handler struct {
	service *Service
}

// RegisterMerchant mounts the merchant payout endpoints. The group is already
// authenticated and scoped to the calling merchant.
func RegisterMerchant(group *gin.RouterGroup, service *Service) {
	if group == nil || service == nil {
		return
	}
	handler := &Handler{service: service}
	group.GET("/transfers", handler.merchantList)
	group.POST("/transfers", handler.merchantCreate)
	group.GET("/transfers/:id", handler.merchantGet)
}

// RegisterAdmin mounts the payout review endpoints. The group is authenticated
// and administrator only.
func RegisterAdmin(group *gin.RouterGroup, service *Service) {
	if group == nil || service == nil {
		return
	}
	handler := &Handler{service: service}
	group.GET("/transfers", handler.adminList)
	group.GET("/transfers/:id", handler.adminGet)
	group.POST("/transfers/:id/process", handler.adminProcess)
}

type createRequest struct {
	OutTransferNo string `json:"out_transfer_no"`
	Amount        int64  `json:"amount"`
	Currency      string `json:"currency"`
	AccountName   string `json:"account_name"`
	AccountNo     string `json:"account_no"`
	BankName      string `json:"bank_name"`
	BranchName    string `json:"branch_name"`
}

type processRequest struct {
	Action          string `json:"action"`
	RejectionReason string `json:"rejection_reason"`
	Reason          string `json:"reason"`
	ProcessedBy     string `json:"processed_by"`
}

func (handler *Handler) merchantList(ctx *gin.Context) {
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	page, ok := pageFromQuery(ctx)
	if !ok {
		return
	}
	status, ok := statusFromQuery(ctx)
	if !ok {
		return
	}
	orders, total, err := handler.service.List(ctx.Request.Context(), Filter{MerchantID: merchantID, Status: status}, page)
	if err != nil {
		writeTransferError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": transferResponses(orders), "total": total, "page": page.Number, "page_size": page.Size})
}

func (handler *Handler) merchantCreate(ctx *gin.Context) {
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	var request createRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		invalidTransferRequest(ctx)
		return
	}
	order, created, err := handler.service.Create(ctx.Request.Context(), Request{
		MerchantID:    merchantID,
		OutTransferNo: request.OutTransferNo,
		Amount:        request.Amount,
		Currency:      request.Currency,
		AccountName:   request.AccountName,
		AccountNo:     request.AccountNo,
		BankName:      request.BankName,
		BranchName:    request.BranchName,
	})
	if err != nil {
		writeTransferError(ctx, err)
		return
	}
	code := http.StatusOK
	if created {
		code = http.StatusCreated
	}
	ctx.JSON(code, transferResponse(order))
}

func (handler *Handler) merchantGet(ctx *gin.Context) {
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	order, err := handler.service.Get(ctx.Request.Context(), merchantID, ctx.Param("id"))
	if err != nil {
		writeTransferError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, transferResponse(order))
}

func (handler *Handler) adminList(ctx *gin.Context) {
	page, ok := pageFromQuery(ctx)
	if !ok {
		return
	}
	status, ok := statusFromQuery(ctx)
	if !ok {
		return
	}
	orders, total, err := handler.service.List(ctx.Request.Context(), Filter{
		MerchantID: strings.TrimSpace(ctx.Query("merchant_id")),
		Status:     status,
	}, page)
	if err != nil {
		writeTransferError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": transferResponses(orders), "total": total, "page": page.Number, "page_size": page.Size})
}

func (handler *Handler) adminGet(ctx *gin.Context) {
	order, err := handler.service.Get(ctx.Request.Context(), "", ctx.Param("id"))
	if err != nil {
		writeTransferError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, transferResponse(order))
}

func (handler *Handler) adminProcess(ctx *gin.Context) {
	var request processRequest
	if err := ctx.ShouldBindJSON(&request); err != nil {
		invalidTransferRequest(ctx)
		return
	}
	processedBy := strings.TrimSpace(request.ProcessedBy)
	if processedBy == "" {
		processedBy = identityhttp.MustSession(ctx).PrincipalID
	}
	action := strings.ToLower(strings.TrimSpace(request.Action))
	reason := strings.TrimSpace(request.RejectionReason)
	if reason == "" {
		reason = strings.TrimSpace(request.Reason)
	}
	var (
		order Order
		err   error
	)
	switch action {
	case "approve":
		order, err = handler.service.Approve(ctx.Request.Context(), ctx.Param("id"), processedBy)
	case "reject":
		order, err = handler.service.Reject(ctx.Request.Context(), ctx.Param("id"), processedBy, reason)
	default:
		invalidTransferRequest(ctx)
		return
	}
	if err != nil {
		writeTransferError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, transferResponse(order))
}

func pageFromQuery(ctx *gin.Context) (Page, bool) {
	number := 1
	size := 20
	if raw := strings.TrimSpace(ctx.DefaultQuery("page", "1")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			invalidTransferRequest(ctx)
			return Page{}, false
		}
		number = parsed
	}
	if raw := strings.TrimSpace(ctx.DefaultQuery("page_size", "20")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			invalidTransferRequest(ctx)
			return Page{}, false
		}
		size = parsed
	}
	return Page{Number: number, Size: size}, true
}

func statusFromQuery(ctx *gin.Context) (Status, bool) {
	raw := strings.TrimSpace(ctx.Query("status"))
	if raw == "" {
		return "", true
	}
	status := Status(raw)
	if !ValidStatus(status) {
		invalidTransferRequest(ctx)
		return "", false
	}
	return status, true
}

func transferResponse(order Order) gin.H {
	return gin.H{
		"id":                    order.ID,
		"transfer_no":           order.TransferNo,
		"merchant_id":           order.MerchantID,
		"out_transfer_no":       order.OutTransferNo,
		"amount":                order.Amount,
		"currency":              order.Currency,
		"account_name":          order.AccountName,
		"account_no":            order.AccountNo,
		"bank_name":             order.BankName,
		"branch_name":           order.BranchName,
		"status":                order.Status,
		"ledger_transaction_id": order.LedgerTransactionID,
		"rejection_reason":      order.RejectionReason,
		"processed_by":          order.ProcessedBy,
		"processed_at":          order.ProcessedAt,
		"created_at":            order.CreatedAt,
		"updated_at":            order.UpdatedAt,
	}
}

func transferResponses(orders []Order) []gin.H {
	items := make([]gin.H, 0, len(orders))
	for _, order := range orders {
		items = append(items, transferResponse(order))
	}
	return items
}

func invalidTransferRequest(ctx *gin.Context) {
	ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "invalid request"})
}

func writeTransferError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		invalidTransferRequest(ctx)
	case errors.Is(err, ErrNotFound):
		ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "transfer not found"})
	case errors.Is(err, ErrInsufficientBalance):
		ctx.JSON(http.StatusConflict, gin.H{"code": "insufficient_balance", "message": "available balance is insufficient"})
	case errors.Is(err, ErrIdempotencyConflict):
		ctx.JSON(http.StatusConflict, gin.H{"code": "idempotency_conflict", "message": "transfer already exists with different parameters"})
	case errors.Is(err, ErrStateConflict):
		ctx.JSON(http.StatusConflict, gin.H{"code": "state_conflict", "message": "transfer state does not allow this action"})
	default:
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
	}
}
