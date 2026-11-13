package domainauth

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	identityhttp "gorixpay/internal/identity/httpapi"

	"github.com/gin-gonic/gin"
)

type submitPayload struct {
	Domain string `json:"domain"`
}

type reviewPayload struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
}

// RegisterMerchant mounts the merchant domain endpoints on an authenticated
// merchant group (/api/v1/merchant).
func RegisterMerchant(group *gin.RouterGroup, service *Service) {
	if group == nil || service == nil {
		return
	}
	group.GET("/domains", service.listMine)
	group.POST("/domains", service.submit)
	group.DELETE("/domains/:id", service.remove)
}

// RegisterAdmin mounts the domain review endpoints on an authenticated
// administrator group (/api/v1/admin).
func RegisterAdmin(group *gin.RouterGroup, service *Service) {
	if group == nil || service == nil {
		return
	}
	group.GET("/domains", service.listAll)
	group.POST("/domains/:id/review", service.review)
}

func (service *Service) listMine(ctx *gin.Context) {
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	domains, err := service.List(ctx.Request.Context(), merchantID)
	if err != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": domainResponses(domains)})
}

func (service *Service) submit(ctx *gin.Context) {
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	var payload submitPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx, "invalid request")
		return
	}
	domain, err := service.Submit(ctx.Request.Context(), merchantID, payload.Domain)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidDomain):
			invalidRequest(ctx, "域名格式不正确")
		case errors.Is(err, ErrDuplicate):
			ctx.JSON(http.StatusConflict, gin.H{"code": "duplicate", "message": "domain already submitted"})
		default:
			invalidRequest(ctx, "invalid request")
		}
		return
	}
	ctx.JSON(http.StatusCreated, domainResponse(domain))
}

func (service *Service) remove(ctx *gin.Context) {
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	if err := service.Delete(ctx.Request.Context(), merchantID, ctx.Param("id")); err != nil {
		if errors.Is(err, ErrNotFound) {
			ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "domain not found"})
			return
		}
		internalError(ctx)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (service *Service) listAll(ctx *gin.Context) {
	page, size, ok := pagination(ctx)
	if !ok {
		return
	}
	status := Status(strings.TrimSpace(ctx.Query("status")))
	if status != "" && !ValidStatus(status) {
		invalidRequest(ctx, "invalid status")
		return
	}
	domains, total, err := service.ListAll(ctx.Request.Context(), Filter{
		MerchantID: identityParam(ctx, "merchant_id"),
		Status:     status,
	}, Page{Number: page, Size: size})
	if err != nil {
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": domainResponses(domains), "total": total, "page": page, "page_size": size})
}

func (service *Service) review(ctx *gin.Context) {
	var payload reviewPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx, "invalid request")
		return
	}
	reviewerID := identityhttp.MustSession(ctx).PrincipalID
	domain, err := service.Review(ctx.Request.Context(), ctx.Param("id"), reviewerID, payload.Action, payload.Reason)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "domain not found"})
		case errors.Is(err, ErrInvalidRequest):
			invalidRequest(ctx, "invalid request")
		case errors.Is(err, ErrStateConflict):
			ctx.JSON(http.StatusConflict, gin.H{"code": "state_conflict", "message": "domain state conflict"})
		default:
			internalError(ctx)
		}
		return
	}
	ctx.JSON(http.StatusOK, domainResponse(domain))
}

func domainResponse(domain Domain) gin.H {
	return gin.H{
		"id":               domain.ID,
		"merchant_id":      domain.MerchantID,
		"domain":           domain.Domain,
		"status":           domain.Status,
		"rejection_reason": domain.RejectionReason,
		"created_at":       domain.CreatedAt,
		"reviewed_at":      domain.ReviewedAt,
	}
}

func domainResponses(domains []Domain) []gin.H {
	items := make([]gin.H, 0, len(domains))
	for _, domain := range domains {
		items = append(items, domainResponse(domain))
	}
	return items
}

func pagination(ctx *gin.Context) (int, int, bool) {
	page := 1
	size := 20
	if raw := strings.TrimSpace(ctx.DefaultQuery("page", "1")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			invalidRequest(ctx, "invalid page")
			return 0, 0, false
		}
		page = parsed
	}
	if raw := strings.TrimSpace(ctx.DefaultQuery("page_size", "20")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			invalidRequest(ctx, "invalid page size")
			return 0, 0, false
		}
		size = parsed
	}
	return page, size, true
}

func identityParam(ctx *gin.Context, name string) string {
	return strings.TrimSpace(ctx.Query(name))
}

func invalidRequest(ctx *gin.Context, message string) {
	ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": message})
}

func internalError(ctx *gin.Context) {
	ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
}
