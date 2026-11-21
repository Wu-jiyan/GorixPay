package announcement

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestSiteReportsCaptchaRequired pins the login-page contract: the public site
// payload has to say whether the platform asks for a captcha. Without it the login
// pages show a captcha box even when the switch is off, and hide it when the
// switch is on — which locks the operator out of their own console.
func TestSiteReportsCaptchaRequired(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&systemSetting{}, &Announcement{}); err != nil {
		t.Fatal(err)
	}
	service := New(db)

	site, err := service.Site(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if site.CaptchaRequired {
		t.Fatal("captcha_required should default to false")
	}

	// The admin console writes a real JSON boolean.
	if err := db.Create(&systemSetting{Key: "security.captcha_required", Value: "true"}).Error; err != nil {
		t.Fatal(err)
	}
	site, err = service.Site(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !site.CaptchaRequired {
		t.Fatal("captcha_required should follow the setting")
	}
}
