package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	channelconfig "gorixpay/internal/channel/config"
	channelrouter "gorixpay/internal/channel/router"
	pluginpayment "gorixpay/internal/plugin/payment"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// defaultTestAmount is the amount used when the caller does not pass one (1.00).
const defaultTestAmount int64 = 100

// PluginHandler exposes the payment plugin registry and a channel smoke test.
type PluginHandler struct {
	db      *gorm.DB
	configs *channelconfig.Service
	plugins *pluginpayment.Registry
	now     func() time.Time
}

// RegisterPlugins mounts the plugin endpoints on an already authenticated admin
// group, mirroring RegisterExtras.
func RegisterPlugins(group *gin.RouterGroup, db *gorm.DB, configs *channelconfig.Service, plugins *pluginpayment.Registry) {
	handler := &PluginHandler{db: db, configs: configs, plugins: plugins, now: time.Now}
	group.GET("/plugins", handler.list)
	group.POST("/channels/:id/test", handler.testChannel)
}

type pluginItem struct {
	Code         string                      `json:"code"`
	Name         string                      `json:"name"`
	Capabilities []string                    `json:"capabilities"`
	ConfigFields []pluginpayment.ConfigField `json:"config_fields"`
}

// list returns the registered plugins with their configuration schema. Secrets
// never leave the channel record, so only field descriptors are exposed.
func (handler *PluginHandler) list(ctx *gin.Context) {
	items := make([]pluginItem, 0)
	if handler.plugins != nil {
		for _, metadata := range handler.plugins.List() {
			capabilities := make([]string, 0, len(metadata.Capabilities))
			for _, capability := range metadata.Capabilities {
				capabilities = append(capabilities, string(capability))
			}
			fields := handler.plugins.ConfigFields(metadata.Code)
			if fields == nil {
				fields = []pluginpayment.ConfigField{}
			}
			items = append(items, pluginItem{Code: metadata.Code, Name: metadata.Name, Capabilities: capabilities, ConfigFields: fields})
		}
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items})
}

type channelTestRequest struct {
	PaymentType string `json:"payment_type"`
	Amount      int64  `json:"amount"`
	PluginCode  string `json:"plugin_code"`
}

// testChannel asks the channel's plugin to create a throwaway payment so an
// operator can validate credentials. No order is written and no state changes;
// whether the call reaches the upstream depends on the configured endpoint
// (tests point the channel at an httptest server).
func (handler *PluginHandler) testChannel(ctx *gin.Context) {
	var request channelTestRequest
	if ctx.ShouldBindJSON(&request) != nil || request.Amount < 0 {
		invalidRequest(ctx)
		return
	}
	if handler.plugins == nil {
		ctx.JSON(http.StatusOK, gin.H{"ok": false, "error": "payment plugins are not configured"})
		return
	}
	var channel channelrouter.Channel
	if err := handler.db.WithContext(ctx.Request.Context()).First(&channel, "id = ?", ctx.Param("id")).Error; err != nil {
		notFound(ctx)
		return
	}
	if request.PluginCode != "" && request.PluginCode != channel.PluginCode {
		invalidRequest(ctx)
		return
	}
	plugin, exists := handler.plugins.Lookup(channel.PluginCode)
	if !exists {
		ctx.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("plugin %q is not registered", channel.PluginCode)})
		return
	}
	configuration := make(map[string]string)
	if handler.configs != nil && strings.TrimSpace(channel.ConfigEncrypted) != "" {
		decoded, err := handler.configs.Decrypt(channel.ID, channel.ConfigEncrypted)
		if err != nil {
			ctx.JSON(http.StatusOK, gin.H{"ok": false, "error": "channel configuration cannot be decrypted"})
			return
		}
		configuration = decoded.Values()
	}
	paymentType := strings.TrimSpace(request.PaymentType)
	if paymentType == "" {
		paymentType = channel.PaymentType
	}
	amount := request.Amount
	if amount == 0 {
		amount = defaultTestAmount
	}
	siteURL := requestSiteURL(ctx)
	tradeNo := fmt.Sprintf("TEST%d", handler.now().UnixNano())
	action, err := plugin.CreatePayment(ctx.Request.Context(), pluginpayment.CreateRequest{
		TradeNo:       tradeNo,
		Amount:        amount,
		Currency:      "CNY",
		Description:   "渠道连通性测试",
		PaymentType:   paymentType,
		NotifyURL:     siteURL + "/api/v1/payment/callback/" + channel.PluginCode + "/" + channel.ID,
		ReturnURL:     siteURL + "/cashier",
		ClientIP:      ctx.ClientIP(),
		Configuration: configuration,
		Extra:         map[string]string{"site_url": siteURL},
	})
	if err != nil {
		ctx.JSON(http.StatusOK, gin.H{"ok": false, "pay_type": "", "pay_info": "", "error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "pay_type": string(action.Type), "pay_info": actionPayInfo(action), "error": ""})
}

// actionPayInfo flattens a plugin action into the string the cashier would use.
func actionPayInfo(action pluginpayment.Action) string {
	switch action.Type {
	case pluginpayment.ActionJump, pluginpayment.ActionQRCode, pluginpayment.ActionURLScheme:
		return action.URL
	}
	switch data := action.Data.(type) {
	case nil:
		return ""
	case string:
		return data
	default:
		encoded, err := json.Marshal(data)
		if err != nil {
			return fmt.Sprint(data)
		}
		return string(encoded)
	}
}

// requestSiteURL derives the public origin of the current request so a test call
// carries an absolute notify URL.
func requestSiteURL(ctx *gin.Context) string {
	scheme := "https"
	if ctx.Request.TLS == nil {
		scheme = ctx.Request.Header.Get("X-Forwarded-Proto")
		if scheme == "" {
			scheme = "http"
		}
	}
	host := ctx.Request.Host
	if forwarded := ctx.Request.Header.Get("X-Forwarded-Host"); forwarded != "" {
		host = forwarded
	}
	if host == "" {
		host = "localhost"
	}
	return scheme + "://" + strings.TrimRight(host, "/")
}
