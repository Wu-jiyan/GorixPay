package settlement

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	identityhttp "gorixpay/internal/identity/httpapi"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	financeservice "gorixpay/internal/finance/service"
	transferservice "gorixpay/internal/transfer"
)

type rulePayload struct {
	Name           string  `json:"name"`
	MerchantID     *string `json:"merchant_id"`
	MinAmountCents int64   `json:"min_amount_cents"`
	FeeBP          int     `json:"fee_bp"`
	Period         string  `json:"period"`
	AutoTransfer   bool    `json:"auto_transfer"`
	Status         string  `json:"status"`
}

type batchPayload struct {
	IDs    []string `json:"ids"`
	Action string   `json:"action"`
	Reason string   `json:"reason"`
}

// Register mounts the admin settlement automation endpoints on an already
// authenticated group. When group does not end in /admin the routes are nested
// under it, so both router.Group("/api/v1") and router.Group("/api/v1/admin")
// work. The sessions handler must be the identity authenticator; the admin
// principal check is added here.
func Register(group *gin.RouterGroup, db *gorm.DB, sessions gin.HandlerFunc, finance *financeservice.Service, transfers *transferservice.Service, feeResolver FeeResolver) {
	if group == nil || db == nil || finance == nil || sessions == nil {
		return
	}
	service := New(db, finance, transfers, feeResolver, time.Now)
	prefix := group
	if !strings.HasSuffix(strings.TrimRight(group.BasePath(), "/"), "/admin") {
		prefix = group.Group("/admin")
	}
	secured := prefix.Group("", sessions, identityhttp.RequireAdministrator())
	secured.GET("/settle-rules", service.listRules)
	secured.POST("/settle-rules", service.createRule)
	secured.PUT("/settle-rules/:id", service.updateRule)
	secured.DELETE("/settle-rules/:id", service.deleteRule)
	secured.POST("/settle-rules/:id/run", service.runRuleHandler)
	secured.POST("/settlements/batch-process", service.batchProcess)
	secured.POST("/settlements/:id/auto-transfer", service.autoTransfer)
}

func (service *Service) listRules(ctx *gin.Context) {
	page, size, ok := pagination(ctx)
	if !ok {
		return
	}
	rules, total, err := service.ListRules(ctx.Request.Context(), ctx.Query("merchant_id"), ctx.Query("status"), Page{Number: page, Size: size})
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": rules, "total": total, "page": page, "page_size": size})
}

func (service *Service) createRule(ctx *gin.Context) {
	var payload rulePayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx)
		return
	}
	rule, err := service.CreateRule(ctx.Request.Context(), payload.input())
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, rule)
}

func (service *Service) updateRule(ctx *gin.Context) {
	var payload rulePayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx)
		return
	}
	rule, err := service.UpdateRule(ctx.Request.Context(), ctx.Param("id"), payload.input())
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, rule)
}

func (service *Service) deleteRule(ctx *gin.Context) {
	if err := service.DeleteRule(ctx.Request.Context(), ctx.Param("id")); err != nil {
		writeError(ctx, err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (service *Service) runRuleHandler(ctx *gin.Context) {
	generated, err := service.RunRule(ctx.Request.Context(), ctx.Param("id"), time.Now())
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"generated": generated})
}

func (service *Service) batchProcess(ctx *gin.Context) {
	var payload batchPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx)
		return
	}
	processedBy := identityhttp.MustSession(ctx).PrincipalID
	results, err := service.BatchProcess(ctx.Request.Context(), payload.IDs, payload.Action, payload.Reason, processedBy)
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": results, "processed": len(results)})
}

func (service *Service) autoTransfer(ctx *gin.Context) {
	result, err := service.AutoTransfer(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, result)
}

func (payload rulePayload) input() RuleInput {
	return RuleInput{
		Name:           payload.Name,
		MerchantID:     payload.MerchantID,
		MinAmountCents: payload.MinAmountCents,
		FeeBP:          payload.FeeBP,
		Period:         payload.Period,
		AutoTransfer:   payload.AutoTransfer,
		Status:         payload.Status,
	}
}

func pagination(ctx *gin.Context) (int, int, bool) {
	page := 1
	size := 20
	if raw := strings.TrimSpace(ctx.DefaultQuery("page", "1")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			invalidRequest(ctx)
			return 0, 0, false
		}
		page = parsed
	}
	if raw := strings.TrimSpace(ctx.DefaultQuery("page_size", "20")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			invalidRequest(ctx)
			return 0, 0, false
		}
		size = parsed
	}
	return page, size, true
}

func writeError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		invalidRequest(ctx)
	case errors.Is(err, ErrRuleNotFound), errors.Is(err, ErrSettlementNotFound):
		ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": err.Error()})
	case errors.Is(err, ErrSettlementNotReady):
		ctx.JSON(http.StatusConflict, gin.H{"code": "settlement_not_ready", "message": err.Error()})
	default:
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
	}
}

func invalidRequest(ctx *gin.Context) {
	ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "invalid request"})
}
