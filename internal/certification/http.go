package certification

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	identityhttp "gorixpay/internal/identity/httpapi"
	identitymodel "gorixpay/internal/identity/model"

	"github.com/gin-gonic/gin"
)

type submitPayload struct {
	CertType string `json:"cert_type"`
	RealName string `json:"real_name"`
	IDNo     string `json:"id_no"`
	CorpName string `json:"corp_name"`
	CorpNo   string `json:"corp_no"`
}

type reviewPayload struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
}

// RegisterMerchant mounts the merchant certification endpoints on an already
// authenticated merchant group (/api/v1/merchant).
func RegisterMerchant(group *gin.RouterGroup, service *Service) {
	if group == nil || service == nil {
		return
	}
	group.GET("/certification", service.current)
	group.POST("/certification", service.submit)
}

// RegisterAdmin mounts the certification review endpoints on an authenticated
// administrator group (/api/v1/admin).
func RegisterAdmin(group *gin.RouterGroup, service *Service) {
	if group == nil || service == nil {
		return
	}
	group.GET("/certifications", service.list)
	group.POST("/certifications/:merchant_id/review", service.review)
}

func (service *Service) current(ctx *gin.Context) {
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	item, err := service.Current(ctx.Request.Context(), merchantID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			ctx.JSON(http.StatusOK, gin.H{
				"status":       "none",
				"cert_type":    "",
				"fee_cents":    service.Fee(ctx.Request.Context()),
				"required":     service.Required(ctx.Request.Context()),
				"submitted_at": nil,
				"reviewed_at":  nil,
			})
			return
		}
		internalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, service.describe(ctx, item))
}

func (service *Service) submit(ctx *gin.Context) {
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	var payload submitPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx, "invalid request")
		return
	}
	item, err := service.Submit(ctx.Request.Context(), SubmitRequest{
		MerchantID: merchantID,
		CertType:   payload.CertType,
		RealName:   payload.RealName,
		IDNo:       payload.IDNo,
		CorpName:   payload.CorpName,
		CorpNo:     payload.CorpNo,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidIDNo):
			invalidRequest(ctx, "身份证号码格式不正确")
		case errors.Is(err, ErrInvalidRequest):
			invalidRequest(ctx, "提交参数不完整或格式不正确")
		case errors.Is(err, ErrAlreadyApproved):
			ctx.JSON(http.StatusConflict, gin.H{"code": "already_approved", "message": "certification already approved"})
		case errors.Is(err, ErrPendingReview):
			ctx.JSON(http.StatusConflict, gin.H{"code": "pending_review", "message": "certification pending review"})
		default:
			internalError(ctx)
		}
		return
	}
	ctx.JSON(http.StatusCreated, gin.H{"status": item.Status})
}

func (service *Service) list(ctx *gin.Context) {
	page, size, ok := pagination(ctx)
	if !ok {
		return
	}
	status := Status(strings.TrimSpace(ctx.Query("status")))
	if status != "" && !ValidStatus(status) {
		invalidRequest(ctx, "invalid status")
		return
	}
	items, total, err := service.List(ctx.Request.Context(), Filter{Status: status, Keyword: ctx.Query("keyword")}, Page{Number: page, Size: size})
	if err != nil {
		if errors.Is(err, ErrInvalidRequest) {
			invalidRequest(ctx, "invalid request")
			return
		}
		internalError(ctx)
		return
	}
	names, err := service.merchantNames(ctx, items)
	if err != nil {
		internalError(ctx)
		return
	}
	responses := make([]gin.H, 0, len(items))
	for _, item := range items {
		response := service.describe(ctx, item)
		response["merchant_id"] = item.MerchantID
		response["display_name"] = names[item.MerchantID]
		response["reviewer_id"] = item.ReviewerID
		responses = append(responses, response)
	}
	ctx.JSON(http.StatusOK, gin.H{"items": responses, "total": total, "page": page, "page_size": size})
}

func (service *Service) review(ctx *gin.Context) {
	var payload reviewPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx, "invalid request")
		return
	}
	reviewerID := identityhttp.MustSession(ctx).PrincipalID
	item, err := service.Review(ctx.Request.Context(), ReviewRequest{
		MerchantID: ctx.Param("merchant_id"),
		Action:     payload.Action,
		Reason:     payload.Reason,
		ReviewerID: reviewerID,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "certification not found"})
		case errors.Is(err, ErrInvalidRequest):
			invalidRequest(ctx, "invalid request")
		case errors.Is(err, ErrInsufficientBalance):
			invalidRequest(ctx, "商户可用余额不足，无法扣除认证费")
		case errors.Is(err, ErrStateConflict):
			ctx.JSON(http.StatusConflict, gin.H{"code": "state_conflict", "message": "certification state conflict"})
		default:
			internalError(ctx)
		}
		return
	}
	response := service.describe(ctx, item)
	response["merchant_id"] = item.MerchantID
	ctx.JSON(http.StatusOK, response)
}

func (service *Service) describe(ctx *gin.Context, item Certification) gin.H {
	realName, nameErr := service.RealName(item)
	idNo, idNoErr := service.IDNo(item)
	if nameErr != nil {
		realName = ""
	}
	if idNoErr != nil {
		idNo = ""
	}
	return gin.H{
		"status":           item.Status,
		"cert_type":        item.CertType,
		"real_name_masked": MaskedName(realName),
		"id_no_masked":     MaskedIDNo(idNo),
		"corp_name":        item.CorpName,
		"corp_no":          item.CorpNo,
		"fee_cents":        item.FeeCents,
		"required":         service.Required(ctx.Request.Context()),
		"submitted_at":     item.SubmittedAt,
		"reviewed_at":      item.ReviewedAt,
		"rejection_reason": item.RejectionReason,
	}
}

func (service *Service) merchantNames(ctx *gin.Context, items []Certification) (map[string]string, error) {
	names := make(map[string]string, len(items))
	if len(items) == 0 {
		return names, nil
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.MerchantID)
	}
	var merchants []identitymodel.Merchant
	if err := service.db.WithContext(ctx.Request.Context()).Where("id IN ?", ids).Find(&merchants).Error; err != nil {
		return nil, err
	}
	for _, merchant := range merchants {
		names[merchant.ID] = merchant.DisplayName
	}
	return names, nil
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

func invalidRequest(ctx *gin.Context, message string) {
	ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": message})
}

func internalError(ctx *gin.Context) {
	ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
}
