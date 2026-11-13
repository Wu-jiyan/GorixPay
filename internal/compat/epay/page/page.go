// Package page serves the browser facing payment entry points that upstream Epay
// exposes next to the API: /pay/submit/{trade_no}/, /pay/checkpay/{trade_no}/,
// /pay/return/{trade_no}/ and /pay/ok/{trade_no}/.
//
// The protocol surfaces (submit.php, mapi.php, api.php, /api/*) never render
// these URLs themselves except as a payurl fallback, but every merchant SDK that
// completes a payment in a browser ends up on /pay/return/{trade_no}/ and
// verifies $_GET with the merchant key (EpayCore::verifyReturn). Dropping the
// signed query string there makes verifyReturn() fail, so the redirect is part of
// the compatibility contract, not a convenience.
package page

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"

	"gorixpay/internal/payment/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Signer builds the signed parameter set handed to the merchant. The epay
// notification sender implements it, so the browser return and the asynchronous
// notification always carry the same signature format.
type Signer interface {
	NotificationValues(ctx context.Context, order model.Order) (url.Values, error)
}

type Handler struct {
	db      *gorm.DB
	signer  Signer
	siteURL string
}

// Register mounts the browser payment pages. siteURL is the public base URL used
// to build the cashier link; it is trimmed of a trailing slash.
func Register(router *gin.Engine, db *gorm.DB, siteURL string, signer Signer) {
	handler := &Handler{db: db, signer: signer, siteURL: strings.TrimRight(siteURL, "/")}
	router.GET("/pay/submit/:trade_no/", handler.submit)
	router.GET("/pay/checkpay/:trade_no/", handler.checkpay)
	router.GET("/pay/return/:trade_no/", handler.returnToMerchant)
	router.GET("/pay/ok/:trade_no/", handler.ok)
	router.GET("/submit2.php", handler.submit2)
}

// submit2 is the retry page upstream links from a failed channel attempt
// (Epay/submit2.php). It re-opens an unpaid order: a missing order and a paid
// order both answer with the upstream sysmsg page, anything else goes back to the
// cashier, which re-runs the channel selection.
func (handler *Handler) submit2(ctx *gin.Context) {
	tradeNo := strings.TrimSpace(ctx.Query("trade_no"))
	if tradeNo == "" {
		handler.sysMessage(ctx, "该订单号不存在，请返回来源地重新发起请求！")
		return
	}
	var order model.Order
	if err := handler.db.WithContext(ctx.Request.Context()).Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		handler.sysMessage(ctx, "该订单号不存在，请返回来源地重新发起请求！")
		return
	}
	if order.Status != model.StatusPending && order.Status != model.StatusProcessing {
		// Epay/submit2.php:9-10 answers a finished order with this exact text.
		handler.sysMessage(ctx, "该订单("+order.OutTradeNo+")已完成支付，请勿重复发起支付")
		return
	}
	ctx.Redirect(http.StatusFound, handler.cashierPath(order.TradeNo))
}

// sysMessage renders the sysmsg page of Epay/includes/functions.php:382-401.
func (handler *Handler) sysMessage(ctx *gin.Context, message string) {
	ctx.Header("Content-Type", "text/html; charset=UTF-8")
	ctx.String(http.StatusOK, `<!DOCTYPE html><html lang="zh-CN"><head><meta http-equiv="Content-Type" content="text/html; charset=utf-8" /><title>站点提示信息</title></head><body id="error-page"><h3>站点提示信息</h3>`+html.EscapeString(message)+`</body></html>`)
}

// submit hands the browser to the cashier, which is where GorixPay performs the
// channel selection that upstream does inside lib\Channel::submit.
func (handler *Handler) submit(ctx *gin.Context) {
	order, err := handler.load(ctx)
	if err != nil {
		handler.notFound(ctx)
		return
	}
	if order.Status == model.StatusPaid {
		ctx.Redirect(http.StatusFound, handler.returnPath(order.TradeNo))
		return
	}
	ctx.Redirect(http.StatusFound, handler.cashierPath(order.TradeNo))
}

// checkpay mirrors upstream's polling page: a paid order moves on to the return
// redirect, anything else stays on the cashier.
func (handler *Handler) checkpay(ctx *gin.Context) {
	order, err := handler.load(ctx)
	if err != nil {
		handler.notFound(ctx)
		return
	}
	if order.Status == model.StatusPaid {
		ctx.Redirect(http.StatusFound, handler.returnPath(order.TradeNo))
		return
	}
	ctx.Redirect(http.StatusFound, handler.cashierPath(order.TradeNo))
}

// returnToMerchant sends the browser to the merchant return_url with the same
// signed parameter set the asynchronous notification carries. Upstream builds it
// with creat_callback and appends http_build_query(...) to return_url
// (Epay/includes/functions.php:519-531).
func (handler *Handler) returnToMerchant(ctx *gin.Context) {
	order, err := handler.load(ctx)
	if err != nil {
		handler.notFound(ctx)
		return
	}
	if order.Status == model.StatusPending || order.Status == model.StatusProcessing {
		ctx.Redirect(http.StatusFound, handler.cashierPath(order.TradeNo))
		return
	}
	if order.ReturnURL == nil || strings.TrimSpace(*order.ReturnURL) == "" {
		ctx.Redirect(http.StatusFound, handler.siteURL+"/pay/ok/"+url.PathEscape(order.TradeNo)+"/")
		return
	}
	target := *order.ReturnURL
	if handler.signer != nil && order.Status == model.StatusPaid {
		values, signErr := handler.signer.NotificationValues(ctx.Request.Context(), order)
		if signErr != nil {
			handler.serverError(ctx)
			return
		}
		target = appendQuery(target, values)
	}
	ctx.Redirect(http.StatusFound, target)
}

func (handler *Handler) ok(ctx *gin.Context) {
	order, err := handler.load(ctx)
	if err != nil {
		handler.notFound(ctx)
		return
	}
	status := "支付成功"
	if order.Status != model.StatusPaid {
		status = "订单状态：" + string(order.Status)
	}
	ctx.Header("Content-Type", "text/html; charset=UTF-8")
	ctx.String(http.StatusOK, `<!doctype html><html lang="zh-CN"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title></head><body style="font-family:system-ui;text-align:center;padding:15vh 1rem"><h1>%s</h1><p>订单号：%s</p><p>金额：%s 元</p></body></html>`,
		html.EscapeString(status), html.EscapeString(status), html.EscapeString(order.TradeNo), formatAmount(order.Amount))
}

func (handler *Handler) load(ctx *gin.Context) (model.Order, error) {
	tradeNo := strings.TrimSpace(ctx.Param("trade_no"))
	if tradeNo == "" {
		return model.Order{}, errors.New("missing trade number")
	}
	var order model.Order
	err := handler.db.WithContext(ctx.Request.Context()).Where("trade_no = ?", tradeNo).First(&order).Error
	return order, err
}

func (handler *Handler) cashierPath(tradeNo string) string {
	return handler.siteURL + "/cashier.php?trade_no=" + url.QueryEscape(tradeNo)
}

func (handler *Handler) returnPath(tradeNo string) string {
	return "/pay/return/" + url.PathEscape(tradeNo) + "/"
}

func (handler *Handler) notFound(ctx *gin.Context) {
	ctx.Header("Content-Type", "text/html; charset=UTF-8")
	ctx.String(http.StatusNotFound, `<!doctype html><html><body>订单不存在</body></html>`)
}

func (handler *Handler) serverError(ctx *gin.Context) {
	ctx.Header("Content-Type", "text/html; charset=UTF-8")
	ctx.String(http.StatusInternalServerError, `<!doctype html><html><body>签名生成失败</body></html>`)
}

func appendQuery(target string, values url.Values) string {
	if len(values) == 0 {
		return target
	}
	separator := "?"
	if strings.Contains(target, "?") {
		separator = "&"
	}
	return target + separator + values.Encode()
}

func formatAmount(amount int64) string {
	sign := ""
	if amount < 0 {
		sign = "-"
		amount = -amount
	}
	return fmt.Sprintf("%s%d.%02d", sign, amount/100, amount%100)
}
