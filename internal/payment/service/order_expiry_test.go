package service

import (
	"context"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestOrderExpiryFollowsTheConfiguredWindow pins the order lifetime: the console
// can set payment.order_ttl_minutes, a missing or broken value falls back to 30
// minutes, and 0 means "never expires". It used to be a hardcoded 5 minutes, which
// expired real buyers mid-payment (pick a channel, jump to the app, type a
// password) and turned a payable order into「订单已过期」.
func TestOrderExpiryFollowsTheConfiguredWindow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE system_settings (setting_key TEXT PRIMARY KEY, setting_value TEXT NOT NULL)`).Error; err != nil {
		t.Fatal(err)
	}
	service := New(db, time.Now)
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)

	if got := service.orderExpiry(context.Background(), now); !got.Equal(now.Add(30 * time.Minute)) {
		t.Fatalf("默认有效期 = %v，应为 30 分钟", got.Sub(now))
	}

	set := func(value string) {
		if err := db.Exec(`INSERT INTO system_settings (setting_key, setting_value) VALUES (?, ?)
			ON CONFLICT(setting_key) DO UPDATE SET setting_value = excluded.setting_value`,
			settingOrderTTL, value).Error; err != nil {
			t.Fatal(err)
		}
	}

	set(`60`)
	if got := service.orderExpiry(context.Background(), now); !got.Equal(now.Add(60 * time.Minute)) {
		t.Fatalf("配置 60 分钟时 = %v", got.Sub(now))
	}

	set(`"45"`) // 控制台写的是 JSON 字符串
	if got := service.orderExpiry(context.Background(), now); !got.Equal(now.Add(45 * time.Minute)) {
		t.Fatalf("JSON 字符串形式未生效：%v", got.Sub(now))
	}

	set(`0`)
	if got := service.orderExpiry(context.Background(), now); !got.IsZero() {
		t.Fatalf("0 应表示不过期，得到 %v", got)
	}

	set(`abc`)
	if got := service.orderExpiry(context.Background(), now); !got.Equal(now.Add(30 * time.Minute)) {
		t.Fatalf("非法值应回落到 30 分钟，得到 %v", got.Sub(now))
	}
}
