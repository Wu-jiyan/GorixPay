package service

import (
	"context"
	"net/url"
	"strings"

	"gorixpay/internal/platform/netguard"
)

// URLGuard rejects orders whose notify_url or return_url would make the
// platform issue a request into its own network. It runs before any order row
// is written, so a rejected order leaves nothing behind.
//
// The dialer in netguard enforces the same rule at request time; this guard
// exists so the merchant gets an immediate, actionable error instead of an
// order that silently never notifies.
type URLGuard struct {
	// AllowPrivate permits loopback and private (RFC 1918 / ULA) targets for
	// self-hosted deployments whose merchant site runs on the same host or LAN.
	// Link-local and cloud metadata addresses stay rejected.
	AllowPrivate bool
}

// URLGuardReason is the merchant facing message for a rejected callback host.
const URLGuardReason = "通知地址或跳转地址不允许指向本机或内网地址"

func (guard URLGuard) Check(_ context.Context, command CreateCommand) error {
	for _, raw := range []string{command.NotifyURL, command.ReturnURL} {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		parsed, err := url.Parse(raw)
		if err != nil {
			return Rejection{Reason: URLGuardReason}
		}
		if err := netguard.ValidateURLWith(parsed, guard.AllowPrivate); err != nil {
			return Rejection{Reason: URLGuardReason}
		}
	}
	return nil
}
