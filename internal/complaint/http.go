package complaint

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	identityhttp "gorixpay/internal/identity/httpapi"
	identitymodel "gorixpay/internal/identity/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type createPayload struct {
	MerchantID  string  `json:"merchant_id"`
	OrderID     string  `json:"order_id"`
	Channel     string  `json:"channel"`
	ComplaintNo string  `json:"complaint_no"`
	Reason      string  `json:"reason"`
	AmountCents int64   `json:"amount_cents"`
	DeadlineAt  *string `json:"deadline_at"`
}

type messagePayload struct {
	Content string `json:"content"`
}

type closePayload struct {
	Reason string `json:"reason"`
}

// Register mounts the complaint endpoints on the /api/v1 group: /admin for the
// administrator console and /merchant scoped to the session merchant.
func Register(group *gin.RouterGroup, db *gorm.DB, sessions gin.HandlerFunc) {
	if group == nil || db == nil || sessions == nil {
		return
	}
	service := New(db, time.Now)
	admin := group.Group("/admin", sessions, identityhttp.RequireAdministrator())
	admin.GET("/complaints", service.adminList)
	admin.GET("/complaints/:id", service.adminDetail)
	admin.POST("/complaints", service.adminCreate)
	admin.POST("/complaints/:id/messages", service.adminReply)
	admin.POST("/complaints/:id/close", service.adminClose)

	merchant := group.Group("/merchant", sessions, requireMerchant())
	merchant.GET("/complaints", service.merchantList)
	merchant.GET("/complaints/:id", service.merchantDetail)
}

func requireMerchant() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if identityhttp.MustSession(ctx).PrincipalType != identitymodel.PrincipalMerchant {
			ctx.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": "forbidden", "message": "merchant access required"})
			return
		}
		ctx.Next()
	}
}

func (service *Service) adminList(ctx *gin.Context) {
	page, size, ok := pagination(ctx)
	if !ok {
		return
	}
	items, total, err := service.List(ctx.Request.Context(), Filter{
		MerchantID: ctx.Query("merchant_id"),
		Channel:    ctx.Query("channel"),
		Status:     ctx.Query("status"),
	}, Page{Number: page, Size: size})
	if err != nil {
		writeError(ctx, err)
		return
	}
	views := make([]complaintResponse, 0, len(items))
	for _, item := range items {
		views = append(views, service.view(item, nil))
	}
	ctx.JSON(http.StatusOK, gin.H{"items": views, "total": total, "page": page, "page_size": size})
}

func (service *Service) adminDetail(ctx *gin.Context) {
	complaint, err := service.Get(ctx.Request.Context(), ctx.Param("id"), "")
	if err != nil {
		writeError(ctx, err)
		return
	}
	messages, err := service.Messages(ctx.Request.Context(), complaint.ID)
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, service.view(complaint, messages))
}

func (service *Service) adminCreate(ctx *gin.Context) {
	var payload createPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx)
		return
	}
	deadline, err := parseDeadline(payload.DeadlineAt)
	if err != nil {
		invalidRequest(ctx)
		return
	}
	complaint, err := service.Create(ctx.Request.Context(), CreateInput{
		MerchantID:  payload.MerchantID,
		OrderID:     payload.OrderID,
		Channel:     payload.Channel,
		ComplaintNo: payload.ComplaintNo,
		Reason:      payload.Reason,
		AmountCents: payload.AmountCents,
		DeadlineAt:  deadline,
	})
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, service.view(complaint, nil))
}

func (service *Service) adminReply(ctx *gin.Context) {
	var payload messagePayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx)
		return
	}
	operator := identityhttp.MustSession(ctx).PrincipalID
	complaint, err := service.Reply(ctx.Request.Context(), ctx.Param("id"), payload.Content, operator)
	if err != nil {
		writeError(ctx, err)
		return
	}
	messages, err := service.Messages(ctx.Request.Context(), complaint.ID)
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"complaint": service.view(complaint, messages)})
}

func (service *Service) adminClose(ctx *gin.Context) {
	var payload closePayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx)
		return
	}
	complaint, err := service.Close(ctx.Request.Context(), ctx.Param("id"), payload.Reason)
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, service.view(complaint, nil))
}

func (service *Service) merchantList(ctx *gin.Context) {
	page, size, ok := pagination(ctx)
	if !ok {
		return
	}
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	items, total, err := service.List(ctx.Request.Context(), Filter{MerchantID: merchantID}, Page{Number: page, Size: size})
	if err != nil {
		writeError(ctx, err)
		return
	}
	views := make([]complaintResponse, 0, len(items))
	for _, item := range items {
		views = append(views, service.view(item, nil))
	}
	ctx.JSON(http.StatusOK, gin.H{"items": views, "total": total, "page": page, "page_size": size})
}

func (service *Service) merchantDetail(ctx *gin.Context) {
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	complaint, err := service.Get(ctx.Request.Context(), ctx.Param("id"), merchantID)
	if err != nil {
		writeError(ctx, err)
		return
	}
	messages, err := service.Messages(ctx.Request.Context(), complaint.ID)
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, service.view(complaint, messages))
}

type complaintResponse struct {
	ID          string     `json:"id"`
	MerchantID  string     `json:"merchant_id"`
	OrderID     *string    `json:"order_id"`
	Channel     string     `json:"channel"`
	ComplaintNo string     `json:"complaint_no"`
	Status      Status     `json:"status"`
	BaseStatus  Status     `json:"base_status"`
	Reason      string     `json:"reason"`
	AmountCents int64      `json:"amount_cents"`
	DeadlineAt  *time.Time `json:"deadline_at"`
	ClosedAt    *time.Time `json:"closed_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Messages    []Message  `json:"messages,omitempty"`
}

func (service *Service) view(complaint Complaint, messages []Message) complaintResponse {
	return complaintResponse{
		ID:          complaint.ID,
		MerchantID:  complaint.MerchantID,
		OrderID:     complaint.OrderID,
		Channel:     complaint.Channel,
		ComplaintNo: complaint.ComplaintNo,
		Status:      ViewStatus(complaint, service.now().UTC()),
		BaseStatus:  complaint.Status,
		Reason:      complaint.Reason,
		AmountCents: complaint.AmountCents,
		DeadlineAt:  complaint.DeadlineAt,
		ClosedAt:    complaint.ClosedAt,
		CreatedAt:   complaint.CreatedAt,
		UpdatedAt:   complaint.UpdatedAt,
		Messages:    messages,
	}
}

func parseDeadline(raw *string) (*time.Time, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*raw))
	if err != nil {
		return nil, ErrInvalidRequest
	}
	utc := parsed.UTC()
	return &utc, nil
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
	case errors.Is(err, ErrInvalidRequest), errors.Is(err, ErrMerchant):
		invalidRequest(ctx)
	case errors.Is(err, ErrNotFound):
		ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "complaint not found"})
	case errors.Is(err, ErrDuplicate):
		ctx.JSON(http.StatusConflict, gin.H{"code": "duplicate", "message": "complaint already registered"})
	case errors.Is(err, ErrClosed):
		ctx.JSON(http.StatusConflict, gin.H{"code": "complaint_closed", "message": "complaint is closed"})
	default:
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
	}
}

func invalidRequest(ctx *gin.Context) {
	ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "invalid request"})
}
