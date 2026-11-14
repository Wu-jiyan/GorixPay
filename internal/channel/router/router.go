package router

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"sort"
	"time"

	pluginpayment "gorixpay/internal/plugin/payment"

	"gorm.io/gorm"
)

var ErrNoAvailableChannel = errors.New("no available payment channel")

type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

type Channel struct {
	ID              string    `gorm:"type:varchar(36);primaryKey"`
	Name            string    `gorm:"type:varchar(100);not null"`
	PluginCode      string    `gorm:"type:varchar(64);not null;index"`
	PaymentType     string    `gorm:"type:varchar(32);not null;index"`
	Priority        int       `gorm:"not null;index"`
	Weight          int       `gorm:"not null"`
	Status          Status    `gorm:"type:varchar(20);not null;index"`
	ConfigEncrypted string    `gorm:"type:text"`
	CreatedAt       time.Time `gorm:"not null"`
	UpdatedAt       time.Time `gorm:"not null"`
}

func (Channel) TableName() string {
	return "payment_channels"
}

type RandomInt func(max int) (int, error)

// RollResolver returns the effective weight for a channel on a given day.
// Implementations come from the pay-roll table; ok=false keeps the configured
// weight, and a resolved zero weight means the channel sits out that day.
type RollResolver interface {
	RollWeight(ctx context.Context, channelID string, day time.Time) (int, bool)
}

type Router struct {
	db       *gorm.DB
	registry *pluginpayment.Registry
	random   RandomInt
	rolls    RollResolver
	now      func() time.Time
}

func New(db *gorm.DB, registry *pluginpayment.Registry, random RandomInt) *Router {
	if random == nil {
		random = cryptoRandomInt
	}
	return &Router{db: db, registry: registry, random: random, now: time.Now}
}

// WithRollResolver enables per-day weight rotation. Without it the router only
// uses payment_channels.weight.
func (router *Router) WithRollResolver(resolver RollResolver) *Router {
	router.rolls = resolver
	return router
}

// WithClock overrides the clock used for day-based rotation; tests use it.
func (router *Router) WithClock(now func() time.Time) *Router {
	if now != nil {
		router.now = now
	}
	return router
}

func (router *Router) Select(ctx context.Context, paymentType string) (Channel, pluginpayment.Plugin, error) {
	var channels []Channel
	if err := router.db.WithContext(ctx).
		Where("payment_type = ? AND status = ? AND weight > 0", paymentType, StatusActive).
		Order("priority ASC, id ASC").Find(&channels).Error; err != nil {
		return Channel{}, nil, err
	}
	type candidate struct {
		channel Channel
		plugin  pluginpayment.Plugin
		weight  int
	}
	var candidates []candidate
	bestPriority := 0
	for _, channel := range channels {
		plugin, err := router.registry.Resolve(channel.PluginCode, pluginpayment.CapabilityPayment)
		if err != nil {
			continue
		}
		if len(candidates) == 0 {
			bestPriority = channel.Priority
		}
		if channel.Priority != bestPriority {
			break
		}
		candidates = append(candidates, candidate{channel: channel, plugin: plugin, weight: channel.Weight})
	}
	if len(candidates) == 0 {
		return Channel{}, nil, ErrNoAvailableChannel
	}
	sort.SliceStable(candidates, func(left, right int) bool { return candidates[left].channel.ID < candidates[right].channel.ID })
	if router.rolls != nil {
		day := router.now()
		rotated := candidates[:0]
		for _, candidate := range candidates {
			if weight, ok := router.rolls.RollWeight(ctx, candidate.channel.ID, day); ok {
				candidate.weight = weight
			}
			if candidate.weight > 0 {
				rotated = append(rotated, candidate)
			}
		}
		candidates = rotated
		if len(candidates) == 0 {
			return Channel{}, nil, ErrNoAvailableChannel
		}
	}
	total := 0
	for _, candidate := range candidates {
		total += candidate.weight
	}
	selected, err := router.random(total)
	if err != nil || selected < 0 || selected >= total {
		if err == nil {
			err = errors.New("random source returned value outside range")
		}
		return Channel{}, nil, err
	}
	for _, candidate := range candidates {
		if selected < candidate.weight {
			return candidate.channel, candidate.plugin, nil
		}
		selected -= candidate.weight
	}
	return Channel{}, nil, ErrNoAvailableChannel
}

func cryptoRandomInt(max int) (int, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0, err
	}
	return int(value.Int64()), nil
}
