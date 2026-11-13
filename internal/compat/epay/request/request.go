// Package request parses the payment hints that the V1 mapi.php and the V2
// pay/create endpoints share. Upstream keeps them in the request scope while it
// starts the channel (Epay/includes/lib/api/Pay.php:296-310); both protocol
// generations must interpret them identically, so the parsing lives in one place.
package request

import (
	"strings"

	paymentorchestrator "gorixpay/internal/payment/orchestrator"
)

// Getter reads one request parameter. The V1 handler passes url.Values.Get and
// the V2 handler passes its PostForm map lookup, so both share this parser.
type Getter func(name string) string

// Options normalises device and method the way Pay.php:296-310 does: the device
// defaults to pc, a jump device forces the jump method, and the
// qq/wechat/alipay/app devices are mobile clients.
func Options(get Getter) paymentorchestrator.StartOptions {
	device := strings.ToLower(strings.TrimSpace(get("device")))
	if device == "" {
		device = "pc"
	}
	method := strings.ToLower(strings.TrimSpace(get("method")))
	if device == "jump" {
		method = "jump"
	}
	switch device {
	case "qq", "wechat", "alipay", "app":
		device = "mobile"
	}
	return paymentorchestrator.StartOptions{
		Method:    method,
		Device:    device,
		SubOpenID: strings.TrimSpace(get("sub_openid")),
		SubAppID:  strings.TrimSpace(get("sub_appid")),
		AuthCode:  strings.TrimSpace(get("auth_code")),
		IsApplet:  isApplet(get("is_applet")),
	}
}

func isApplet(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	// Pay.php:301 uses intval(), so "1", "01" and "1abc" are all truthy.
	digits := 0
	for digits < len(trimmed) && trimmed[digits] >= '0' && trimmed[digits] <= '9' {
		digits++
	}
	if digits == 0 {
		return false
	}
	return strings.TrimLeft(trimmed[:digits], "0") != ""
}

// ScanPayType reproduces getScanPayType (Epay/includes/functions.php:553-575):
// the first two digits of the payment barcode identify the wallet.
func ScanPayType(authCode string) string {
	if len(authCode) < 2 {
		return ""
	}
	switch prefix := authCode[:2]; prefix {
	case "25", "26", "27", "28", "29", "30":
		return "alipay"
	case "10", "11", "12", "13", "14", "15":
		return "wxpay"
	case "91":
		return "qqpay"
	case "62":
		return "bank"
	case "01":
		return "ecny"
	default:
		return ""
	}
}
