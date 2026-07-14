package callback

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"strings"

	channelconfig "gorixpay/internal/channel/config"
	channelrouter "gorixpay/internal/channel/router"
	"gorixpay/internal/compat/epay/signing"
	paymentmodel "gorixpay/internal/payment/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var callbackAmountPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

type EpayHandler struct {
	db      *gorm.DB
	configs *channelconfig.Service
	service *Service
}

func RegisterEpay(router *gin.Engine, db *gorm.DB, configs *channelconfig.Service, service *Service) {
	handler := &EpayHandler{db: db, configs: configs, service: service}
	router.POST("/api/v1/payment/callback/epay/:channel_id", handler.callback)
}

func (handler *EpayHandler) callback(ctx *gin.Context) {
	if err := ctx.Request.ParseForm(); err != nil {
		ctx.String(http.StatusBadRequest, "fail")
		return
	}
	var channel channelrouter.Channel
	err := handler.db.WithContext(ctx.Request.Context()).
		Where("id = ? AND plugin_code = ? AND status = ?", ctx.Param("channel_id"), "epay", channelrouter.StatusActive).
		First(&channel).Error
	if err != nil {
		ctx.String(http.StatusNotFound, "fail")
		return
	}
	config, err := handler.configs.Decrypt(channel.ID, channel.ConfigEncrypted)
	if err != nil {
		ctx.String(http.StatusInternalServerError, "fail")
		return
	}
	params := make(map[string]any, len(ctx.Request.Form))
	for name := range ctx.Request.Form {
		params[name] = ctx.Request.Form.Get(name)
	}
	if ctx.Request.Form.Get("pid") != config.PID || ctx.Request.Form.Get("trade_status") != "TRADE_SUCCESS" ||
		!signing.VerifyMD5(params, config.Key, ctx.Request.Form.Get("sign")) {
		ctx.String(http.StatusUnauthorized, "fail")
		return
	}
	amount, err := parseCallbackAmount(ctx.Request.Form.Get("money"))
	if err != nil {
		ctx.String(http.StatusBadRequest, "fail")
		return
	}
	var order paymentmodel.Order
	if err := handler.db.WithContext(ctx.Request.Context()).Where("trade_no = ?", ctx.Request.Form.Get("out_trade_no")).First(&order).Error; err != nil {
		ctx.String(http.StatusNotFound, "fail")
		return
	}
	channelTradeNo := strings.TrimSpace(ctx.Request.Form.Get("trade_no"))
	if channelTradeNo == "" {
		ctx.String(http.StatusBadRequest, "fail")
		return
	}
	digestSource := channel.ID + "\n" + signing.Canonical(params)
	digestBytes := sha256.Sum256([]byte(digestSource))
	_, _, err = handler.service.Complete(ctx.Request.Context(), Completion{
		OrderID: order.ID, ChannelID: channel.ID, ChannelTradeNo: channelTradeNo,
		Amount: amount, Digest: hex.EncodeToString(digestBytes[:]),
	})
	if err != nil {
		ctx.String(http.StatusConflict, "fail")
		return
	}
	ctx.String(http.StatusOK, "success")
}

func parseCallbackAmount(value string) (int64, error) {
	if !callbackAmountPattern.MatchString(value) {
		return 0, errors.New("invalid callback amount")
	}
	rational, ok := new(big.Rat).SetString(value)
	if !ok || rational.Sign() <= 0 {
		return 0, errors.New("invalid callback amount")
	}
	scaled := new(big.Rat).Mul(rational, big.NewRat(100, 1))
	quotient, remainder := new(big.Int).QuoRem(scaled.Num(), scaled.Denom(), new(big.Int))
	if new(big.Int).Lsh(remainder, 1).Cmp(scaled.Denom()) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() || quotient.Sign() <= 0 {
		return 0, errors.New("invalid callback amount")
	}
	return quotient.Int64(), nil
}

func formatCallbackAmount(amount int64) string {
	return fmt.Sprintf("%d.%02d", amount/100, amount%100)
}
