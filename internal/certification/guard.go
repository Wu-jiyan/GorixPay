package certification

import (
	"context"

	paymentservice "gorixpay/internal/payment/service"
)

// Guard blocks order creation when real-name certification is required and the
// merchant has not been approved yet.
type Guard struct {
	service *Service
}

// Guard returns the payment guard for the certification policy.
func (service *Service) Guard() paymentservice.Guard {
	return Guard{service: service}
}

func (guard Guard) Check(ctx context.Context, command paymentservice.CreateCommand) error {
	if guard.service == nil {
		return nil
	}
	if !guard.service.Required(ctx) {
		return nil
	}
	item, err := guard.service.Current(ctx, command.MerchantID)
	if err != nil || item.Status != StatusApproved {
		return paymentservice.Rejection{Reason: "请先完成实名认证"}
	}
	return nil
}
