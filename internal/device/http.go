package device

import (
	"errors"
	"net/http"
	"strings"
	"time"

	identityhttp "gorixpay/internal/identity/httpapi"
	paymentservice "gorixpay/internal/payment/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Deps carries the optional payment wiring used by the channel test order.
type Deps struct {
	Orders  *paymentservice.Service
	Starter PaymentStarter
}

type heartbeatPayload struct {
	DeviceNo string `json:"device_no"`
	Secret   string `json:"secret"`
}

type ackPayload struct {
	DeviceNo  string   `json:"device_no"`
	PrinterNo string   `json:"printer_no"`
	Secret    string   `json:"secret"`
	IDs       []string `json:"ids"`
}

type pollPayload struct {
	PrinterNo string `json:"printer_no"`
	Secret    string `json:"secret"`
}

type voiceDevicePayload struct {
	MerchantID *string `json:"merchant_id"`
	DeviceNo   string  `json:"device_no"`
	Name       string  `json:"name"`
	Status     string  `json:"status"`
}

type printerPayload struct {
	MerchantID string `json:"merchant_id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
}

type testVoicePayload struct {
	Text string `json:"text"`
}

type cleanupPayload struct {
	Targets []string `json:"targets"`
	Before  string   `json:"before"`
	DryRun  *bool    `json:"dry_run"`
	Confirm string   `json:"confirm"`
}

type testOrderPayload struct {
	MerchantID  string `json:"merchant_id"`
	PaymentType string `json:"payment_type"`
	AmountCents int64  `json:"amount_cents"`
}

// Register mounts the device gateway under /device (no session, device
// credentials only) and the administration endpoints under /admin (session +
// administrator).
// Register returns the service so callers can also mount PaymentHook on the
// payment callback chain.
func Register(group *gin.RouterGroup, db *gorm.DB, sessions gin.HandlerFunc, deps Deps) *Service {
	service := New(db, time.Now).WithPayments(deps.Orders, deps.Starter)
	if group == nil || db == nil || sessions == nil {
		return service
	}

	device := group.Group("/device")
	device.POST("/voice/heartbeat", service.voiceHeartbeat)
	device.POST("/voice/ack", service.voiceAck)
	device.POST("/printer/poll", service.printerPoll)
	device.POST("/printer/ack", service.printerAck)

	admin := group.Group("/admin", sessions, identityhttp.RequireAdministrator())
	admin.GET("/voice-devices", service.listVoiceDevices)
	admin.POST("/voice-devices", service.createVoiceDevice)
	admin.PUT("/voice-devices/:id", service.updateVoiceDevice)
	admin.DELETE("/voice-devices/:id", service.deleteVoiceDevice)
	admin.POST("/voice-devices/:id/test", service.testVoiceDevice)
	admin.POST("/voice-devices/:id/reset-secret", service.resetVoiceDeviceSecret)
	admin.GET("/printers", service.listPrinters)
	admin.POST("/printers", service.createPrinter)
	admin.PUT("/printers/:id", service.updatePrinter)
	admin.DELETE("/printers/:id", service.deletePrinter)
	admin.POST("/printers/:id/test", service.testPrinter)
	admin.POST("/printers/:id/reset-secret", service.resetPrinterSecret)
	admin.POST("/maintenance/cleanup", service.cleanup)
	admin.POST("/channels/:id/test-order", service.testOrder)
	return service
}

func (service *Service) voiceHeartbeat(ctx *gin.Context) {
	var payload heartbeatPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx)
		return
	}
	broadcasts, err := service.VoiceHeartbeat(ctx.Request.Context(), payload.DeviceNo, payload.Secret)
	if err != nil {
		writeError(ctx, err)
		return
	}
	items := make([]gin.H, 0, len(broadcasts))
	for _, broadcast := range broadcasts {
		items = append(items, gin.H{"id": broadcast.ID, "text": broadcast.Text})
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "broadcasts": items})
}

func (service *Service) voiceAck(ctx *gin.Context) {
	var payload ackPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx)
		return
	}
	acked, err := service.VoiceAck(ctx.Request.Context(), payload.DeviceNo, payload.Secret, payload.IDs)
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "acked": acked})
}

func (service *Service) printerPoll(ctx *gin.Context) {
	var payload pollPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx)
		return
	}
	jobs, err := service.PrinterPoll(ctx.Request.Context(), payload.PrinterNo, payload.Secret)
	if err != nil {
		writeError(ctx, err)
		return
	}
	items := make([]gin.H, 0, len(jobs))
	for _, job := range jobs {
		items = append(items, gin.H{"id": job.ID, "content": job.Content})
	}
	ctx.JSON(http.StatusOK, gin.H{"jobs": items})
}

func (service *Service) printerAck(ctx *gin.Context) {
	var payload ackPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx)
		return
	}
	acked, err := service.PrinterAck(ctx.Request.Context(), payload.PrinterNo, payload.Secret, payload.IDs)
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "acked": acked})
}

func (service *Service) listVoiceDevices(ctx *gin.Context) {
	devices, err := service.ListVoiceDevices(ctx.Request.Context(), ctx.Query("merchant_id"))
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": devices})
}

func (service *Service) createVoiceDevice(ctx *gin.Context) {
	var payload voiceDevicePayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx)
		return
	}
	device, secret, err := service.CreateVoiceDevice(ctx.Request.Context(), VoiceDeviceInput{
		MerchantID: payload.MerchantID, DeviceNo: payload.DeviceNo, Name: payload.Name, Status: payload.Status,
	})
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, gin.H{"device": device, "secret": secret})
}

func (service *Service) updateVoiceDevice(ctx *gin.Context) {
	var payload voiceDevicePayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx)
		return
	}
	device, err := service.UpdateVoiceDevice(ctx.Request.Context(), ctx.Param("id"), VoiceDeviceInput{
		MerchantID: payload.MerchantID, Name: payload.Name, Status: payload.Status,
	})
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, device)
}

func (service *Service) deleteVoiceDevice(ctx *gin.Context) {
	if err := service.DeleteVoiceDevice(ctx.Request.Context(), ctx.Param("id")); err != nil {
		writeError(ctx, err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (service *Service) testVoiceDevice(ctx *gin.Context) {
	var payload testVoicePayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx)
		return
	}
	broadcast, err := service.VoiceTest(ctx.Request.Context(), ctx.Param("id"), payload.Text)
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"queued": true, "broadcast": broadcast})
}

func (service *Service) resetVoiceDeviceSecret(ctx *gin.Context) {
	secret, err := service.ResetVoiceSecret(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"secret": secret})
}

func (service *Service) listPrinters(ctx *gin.Context) {
	printers, err := service.ListPrinters(ctx.Request.Context(), ctx.Query("merchant_id"))
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": printers})
}

func (service *Service) createPrinter(ctx *gin.Context) {
	var payload printerPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx)
		return
	}
	printer, secret, err := service.CreatePrinter(ctx.Request.Context(), PrinterInput{
		MerchantID: payload.MerchantID, Name: payload.Name, Status: payload.Status,
	})
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusCreated, gin.H{"printer": printer, "secret": secret})
}

func (service *Service) updatePrinter(ctx *gin.Context) {
	var payload printerPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx)
		return
	}
	printer, err := service.UpdatePrinter(ctx.Request.Context(), ctx.Param("id"), PrinterInput{
		MerchantID: payload.MerchantID, Name: payload.Name, Status: payload.Status,
	})
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, printer)
}

func (service *Service) deletePrinter(ctx *gin.Context) {
	if err := service.DeletePrinter(ctx.Request.Context(), ctx.Param("id")); err != nil {
		writeError(ctx, err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (service *Service) testPrinter(ctx *gin.Context) {
	job, err := service.PrinterTest(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"queued": true, "job": job})
}

func (service *Service) resetPrinterSecret(ctx *gin.Context) {
	secret, err := service.ResetPrinterSecret(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"secret": secret})
}

func (service *Service) cleanup(ctx *gin.Context) {
	var payload cleanupPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx)
		return
	}
	before, err := parseBefore(payload.Before)
	if err != nil {
		invalidRequest(ctx)
		return
	}
	dryRun := true
	if payload.DryRun != nil {
		dryRun = *payload.DryRun
	}
	if !dryRun && payload.Confirm != "DELETE" {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": "confirmation_required", "message": "confirm must be DELETE"})
		return
	}
	items, err := service.Cleanup(ctx.Request.Context(), payload.Targets, before, dryRun)
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items, "dry_run": dryRun, "before": before.UTC().Format("2006-01-02")})
}

func (service *Service) testOrder(ctx *gin.Context) {
	var payload testOrderPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		invalidRequest(ctx)
		return
	}
	result, err := service.CreateTestOrder(ctx.Request.Context(), ctx.Param("id"), payload.MerchantID, payload.PaymentType, payload.AmountCents)
	if err != nil {
		writeError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, result)
}

func parseBefore(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, ErrInvalidRequest
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, ErrInvalidRequest
}

func writeError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrUnauthorized):
		ctx.JSON(http.StatusUnauthorized, gin.H{"code": "unauthorized", "message": "device authentication failed"})
	case errors.Is(err, ErrInvalidRequest):
		invalidRequest(ctx)
	case errors.Is(err, ErrNotFound):
		ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "device not found"})
	case errors.Is(err, ErrDuplicate):
		ctx.JSON(http.StatusConflict, gin.H{"code": "duplicate", "message": "device already registered"})
	case errors.Is(err, ErrUnavailable):
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": "unavailable", "message": "test order service unavailable"})
	default:
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
	}
}

func invalidRequest(ctx *gin.Context) {
	ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "invalid request"})
}
