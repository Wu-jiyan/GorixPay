package auth

import (
	"context"
	"testing"
	"time"

	"gorixpay/internal/identity/model"
	"gorixpay/internal/identity/session"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
)

func TestRegisterMerchantWithInviteBindsInviterAndAllocatesCode(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:merchant-invite?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Merchant{}, &model.Session{}); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, session.NewService(db, time.Now))
	inviter, err := service.RegisterMerchantWithInvite(context.Background(), "inviter@example.test", "SecretPass1", "", LoginMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	if inviter.Merchant.InviteCode == nil || len(*inviter.Merchant.InviteCode) != 8 {
		t.Fatalf("invite code: %+v", inviter.Merchant.InviteCode)
	}
	invitee, err := service.RegisterMerchantWithInvite(context.Background(), "invitee@example.test", "SecretPass1", *inviter.Merchant.InviteCode, LoginMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	if invitee.Merchant.InvitedBy == nil || *invitee.Merchant.InvitedBy != inviter.Merchant.ID {
		t.Fatalf("invited_by: %+v", invitee.Merchant.InvitedBy)
	}
	if invitee.Merchant.InviteCode == nil || *invitee.Merchant.InviteCode == *inviter.Merchant.InviteCode {
		t.Fatal("invitee code must be unique")
	}
	unknown, err := service.RegisterMerchantWithInvite(context.Background(), "other@example.test", "SecretPass1", "ZZZZZZZZ", LoginMetadata{})
	if err != nil {
		t.Fatalf("unknown invite code must be ignored: %v", err)
	}
	if unknown.Merchant.InvitedBy != nil {
		t.Fatal("unknown invite code bound an inviter")
	}
	var stored model.Merchant
	if err := db.First(&stored, "id = ?", invitee.Merchant.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.InvitedBy == nil || *stored.InvitedBy != inviter.Merchant.ID {
		t.Fatalf("stored invited_by: %+v", stored.InvitedBy)
	}
}
