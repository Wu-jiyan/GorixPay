package httpapi

import (
	"errors"
	"net/http"

	"gorixpay/internal/device"
	identityhttp "gorixpay/internal/identity/httpapi"

	"github.com/gin-gonic/gin"
)

// PrinterHandler is the merchant self-service side of 小票打印: a merchant manages
// its own printers and prints a receipt for one of its paid orders. The admin side
// (internal/device/http.go) keeps managing platform devices.
type PrinterHandler struct {
	devices *device.Service
}

// RegisterPrinters mounts the merchant printer routes. A nil service keeps them
// off, so a deployment without the device module does not advertise the button.
func RegisterPrinters(group *gin.RouterGroup, devices *device.Service) {
	if devices == nil {
		return
	}
	handler := &PrinterHandler{devices: devices}
	group.GET("/printers", handler.list)
	group.POST("/printers", handler.create)
	group.DELETE("/printers/:id", handler.remove)
	group.POST("/printers/:id/test", handler.test)
	group.POST("/printers/:id/reset-secret", handler.resetSecret)
	group.POST("/orders/:id/print", handler.printOrder)
}

func (handler *PrinterHandler) list(ctx *gin.Context) {
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	printers, err := handler.devices.ListPrinters(ctx.Request.Context(), merchantID)
	if err != nil {
		orderInternalError(ctx)
		return
	}
	items := make([]gin.H, 0, len(printers))
	for _, printer := range printers {
		items = append(items, gin.H{
			"id": printer.ID, "name": printer.Name, "status": printer.Status,
			"created_at": printer.CreatedAt, "updated_at": printer.UpdatedAt,
		})
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items})
}

func (handler *PrinterHandler) create(ctx *gin.Context) {
	var payload struct {
		Name string `json:"name" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		orderInvalidRequest(ctx)
		return
	}
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	printer, secret, err := handler.devices.CreatePrinter(ctx.Request.Context(), device.PrinterInput{
		MerchantID: merchantID, Name: payload.Name, Status: string(device.StatusActive),
	})
	if err != nil {
		orderInternalError(ctx)
		return
	}
	ctx.JSON(http.StatusCreated, gin.H{"printer": printer, "secret": secret})
}

func (handler *PrinterHandler) remove(ctx *gin.Context) {
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	printers, err := handler.devices.ListPrinters(ctx.Request.Context(), merchantID)
	if err != nil {
		orderInternalError(ctx)
		return
	}
	// Ownership check: DeletePrinter is platform-scoped, so the id has to be proven
	// to belong to this merchant first.
	owned := false
	for _, printer := range printers {
		if printer.ID == ctx.Param("id") {
			owned = true
			break
		}
	}
	if !owned {
		ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "打印机不存在"})
		return
	}
	if err := handler.devices.DeletePrinter(ctx.Request.Context(), ctx.Param("id")); err != nil {
		orderInternalError(ctx)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (handler *PrinterHandler) test(ctx *gin.Context) {
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	printers, err := handler.devices.ListPrinters(ctx.Request.Context(), merchantID)
	if err != nil {
		orderInternalError(ctx)
		return
	}
	owned := false
	for _, printer := range printers {
		if printer.ID == ctx.Param("id") {
			owned = true
			break
		}
	}
	if !owned {
		ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "打印机不存在"})
		return
	}
	job, err := handler.devices.PrinterTest(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		orderInternalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"job": job})
}

// resetSecret rotates the printer key so a merchant can recover from a lost
// secret without deleting the printer and re-registering the print agent.
func (handler *PrinterHandler) resetSecret(ctx *gin.Context) {
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	printers, err := handler.devices.ListPrinters(ctx.Request.Context(), merchantID)
	if err != nil {
		orderInternalError(ctx)
		return
	}
	owned := false
	for _, printer := range printers {
		if printer.ID == ctx.Param("id") {
			owned = true
			break
		}
	}
	if !owned {
		ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "打印机不存在"})
		return
	}
	secret, err := handler.devices.ResetPrinterSecret(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		orderInternalError(ctx)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"secret": secret})
}

func (handler *PrinterHandler) printOrder(ctx *gin.Context) {
	merchantID := identityhttp.MustSession(ctx).PrincipalID
	job, err := handler.devices.PrintOrder(ctx.Request.Context(), merchantID, ctx.Param("id"))
	if err != nil {
		switch {
		case errors.Is(err, device.ErrNotFound):
			ctx.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "订单不存在"})
		case errors.Is(err, device.ErrOrderNotPrintable):
			ctx.JSON(http.StatusConflict, gin.H{"code": "order_not_printable", "message": "只有已支付的订单可以打印小票"})
		case errors.Is(err, device.ErrNoPrinter):
			ctx.JSON(http.StatusConflict, gin.H{"code": "printer_missing", "message": "还没有可用的打印机，请先在「小票打印机」里添加"})
		default:
			orderInternalError(ctx)
		}
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"job": job})
}
