package groupshop

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	identityhttp "gorixpay/internal/identity/httpapi"
	ledgerservice "gorixpay/internal/ledger/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Handler exposes the group shop endpoints.
type Handler struct {
	service *Service
}

// Register mounts the merchant purchase routes and the administrator routes.
// group is the /api/v1 group; sessions must be the identity middleware.
func Register(group *gin.RouterGroup, db *gorm.DB, sessions gin.HandlerFunc, ledger *ledgerservice.Service) {
	if group == nil || db == nil || sessions == nil || ledger == nil {
		return
	}
	handler := &Handler{service: New(db, ledger, time.Now)}
	merchant := group.Group("/merchant", sessions)
	merchant.GET("/group-plans", handler.merchantPlans)
	merchant.POST("/group-purchases", handler.createPurchase)
	admin := group.Group("/admin", sessions, identityhttp.RequireAdministrator())
	admin.GET("/group-plans", handler.adminPlans)
	admin.GET("/group-purchases", handler.adminPurchases)
	admin.POST("/group-purchases/:id/refund", handler.refundPurchase)
	admin.PATCH("/groups/:id/price", handler.setPrice)
}

type purchaseRequest struct {
	GroupID string `json:"group_id"`
}

type priceRequest struct {
	PriceCents *int64 `json:"price_cents"`
}

type purchaseView struct {
	PurchaseID          string         `json:"purchase_id"`
	MerchantID          string         `json:"merchant_id"`
	GroupID             string         `json:"group_id"`
	PriceCents          int64          `json:"price_cents"`
	Status              PurchaseStatus `json:"status"`
	LedgerTransactionID *string        `json:"ledger_transaction_id"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
}

func viewOf(purchase Purchase) purchaseView {
	return purchaseView{
		PurchaseID: purchase.ID, MerchantID: purchase.MerchantID, GroupID: purchase.GroupID,
		PriceCents: purchase.PriceCents, Status: purchase.Status,
		LedgerTransactionID: purchase.LedgerTransactionID,
		CreatedAt:           purchase.CreatedAt, UpdatedAt: purchase.UpdatedAt,
	}
}

func (handler *Handler) merchantPlans(ctx *gin.Context) {
	plans, err := handler.service.Plans(ctx.Request.Context())
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": plans})
}

func (handler *Handler) adminPlans(ctx *gin.Context) {
	plans, err := handler.service.AdminPlans(ctx.Request.Context())
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": plans})
}

func (handler *Handler) createPurchase(ctx *gin.Context) {
	principal := identityhttp.MustSession(ctx)
	var request purchaseRequest
	if err := ctx.ShouldBindJSON(&request); err != nil || strings.TrimSpace(request.GroupID) == "" {
		invalid(ctx)
		return
	}
	purchase, created, err := handler.service.Purchase(ctx.Request.Context(), principal.PrincipalID, request.GroupID)
	if err != nil {
		writeError(ctx, err)
		return
	}
	code := http.StatusOK
	if created {
		code = http.StatusCreated
	}
	ctx.JSON(code, viewOf(purchase))
}

func (handler *Handler) adminPurchases(ctx *gin.Context) {
	page, ok := pageFromQuery(ctx)
	if !ok {
		return
	}
	filter := PurchaseFilter{
		MerchantID: strings.TrimSpace(ctx.Query("merchant_id")),
		Status:     PurchaseStatus(strings.TrimSpace(ctx.Query("status"))),
	}
	purchases, total, err := handler.service.Purchases(ctx.Request.Context(), filter, page)
	if err != nil {
		writeError(ctx, err)
		return
	}
	items := make([]purchaseView, 0, len(purchases))
	for _, purchase := range purchases {
		items = append(items, viewOf(purchase))
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page.Number, "page_size": page.Limit()})
}

func (handler *Handler) refundPurchase(ctx *gin.Context) {
	purchase, err := handler.service.Refund(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, viewOf(purchase))
}

func (handler *Handler) setPrice(ctx *gin.Context) {
	var request priceRequest
	if err := ctx.ShouldBindJSON(&request); err != nil || request.PriceCents == nil {
		invalid(ctx)
		return
	}
	plan, err := handler.service.SetPrice(ctx.Request.Context(), ctx.Param("id"), *request.PriceCents)
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, plan)
}

func pageFromQuery(ctx *gin.Context) (Page, bool) {
	page := Page{Number: 1, Size: 20}
	if raw := strings.TrimSpace(ctx.DefaultQuery("page", "1")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			invalid(ctx)
			return Page{}, false
		}
		page.Number = parsed
	}
	if raw := strings.TrimSpace(ctx.DefaultQuery("page_size", "20")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			invalid(ctx)
			return Page{}, false
		}
		page.Size = parsed
	}
	return page, true
}

func invalid(ctx *gin.Context) {
	ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "invalid request"})
}

func writeError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		invalid(ctx)
	case errors.Is(err, ErrNotFound):
		ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "group or purchase not found"})
	case errors.Is(err, ErrGroupNotForSale):
		ctx.JSON(http.StatusConflict, gin.H{"code": "group_not_for_sale", "message": "该用户组暂不可购买"})
	case errors.Is(err, ErrInsufficientBalance):
		ctx.JSON(http.StatusConflict, gin.H{"code": "insufficient_balance", "message": "可用余额不足，请先充值后再购买"})
	case errors.Is(err, ErrNotRefundable):
		ctx.JSON(http.StatusConflict, gin.H{"code": "not_refundable", "message": "该购买记录已退款，不能重复退款"})
	default:
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
	}
}
