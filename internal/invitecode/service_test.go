package invitecode

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	identityauth "gorixpay/internal/identity/auth"
	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/identity/session"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"

	"gorm.io/gorm"
)

func newService(t *testing.T, name string) (*Service, *gorm.DB) {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("PRAGMA busy_timeout = 5000").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&InviteCode{}, &identitymodel.Merchant{}, &identitymodel.Session{}); err != nil {
		t.Fatal(err)
	}
	return New(db), db
}

func TestCreateGeneratesUniqueReadableCodes(t *testing.T) {
	service, _ := newService(t, "invite-create")
	ctx := context.Background()
	seen := map[string]bool{}
	for index := 0; index < 20; index++ {
		created, err := service.Create(ctx, CreateInput{MaxUses: 0})
		if err != nil {
			t.Fatal(err)
		}
		if len(created.Code) != codeLength || created.Status != StatusActive || created.UsedCount != 0 {
			t.Fatalf("created=%+v", created)
		}
		for _, char := range created.Code {
			if !strings.ContainsRune(codeAlphabet, char) {
				t.Fatalf("code %q contains %q", created.Code, char)
			}
		}
		if seen[created.Code] {
			t.Fatalf("duplicate code %q", created.Code)
		}
		seen[created.Code] = true
	}
	if _, err := service.Create(ctx, CreateInput{Code: "DUPCODE1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(ctx, CreateInput{Code: "dupcode1"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate err=%v", err)
	}
}

func TestConsumeHonoursLimits(t *testing.T) {
	service, _ := newService(t, "invite-limits")
	ctx := context.Background()
	limited, err := service.Create(ctx, CreateInput{MaxUses: 2})
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := service.ConsumeRegistrationCode(ctx, limited.Code); err != nil {
			t.Fatalf("attempt %d err=%v", attempt, err)
		}
	}
	if err := service.ConsumeRegistrationCode(ctx, limited.Code); !errors.Is(err, ErrInvalid) {
		t.Fatalf("exhausted err=%v", err)
	}
	unlimited, err := service.Create(ctx, CreateInput{MaxUses: 0})
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 5; attempt++ {
		if err := service.ConsumeRegistrationCode(ctx, unlimited.Code); err != nil {
			t.Fatalf("unlimited attempt %d err=%v", attempt, err)
		}
	}
	past := time.Now().UTC().Add(-time.Hour)
	expired, err := service.Create(ctx, CreateInput{ExpiresAt: &past})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ConsumeRegistrationCode(ctx, expired.Code); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expired err=%v", err)
	}
	disabled, err := service.Create(ctx, CreateInput{Status: StatusDisabled})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ConsumeRegistrationCode(ctx, disabled.Code); !errors.Is(err, ErrInvalid) {
		t.Fatalf("disabled err=%v", err)
	}
	if err := service.ConsumeRegistrationCode(ctx, "NOSUCHCODE"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown err=%v", err)
	}
	if err := service.ConsumeRegistrationCode(ctx, "  "); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty err=%v", err)
	}
}

func TestConsumeDoesNotOversellUnderConcurrency(t *testing.T) {
	service, _ := newService(t, "invite-concurrent")
	ctx := context.Background()
	code, err := service.Create(ctx, CreateInput{MaxUses: 5})
	if err != nil {
		t.Fatal(err)
	}
	const workers = 20
	var wait sync.WaitGroup
	results := make(chan error, workers)
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			results <- service.ConsumeRegistrationCode(ctx, code.Code)
		}()
	}
	wait.Wait()
	close(results)
	success := 0
	rejected := 0
	for err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrInvalid):
			rejected++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if success != 5 || rejected != workers-5 {
		t.Fatalf("success=%d rejected=%d", success, rejected)
	}
	stored, err := service.Get(ctx, code.ID)
	if err != nil || stored.UsedCount != 5 {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
}

func TestReleaseRefundsOneUse(t *testing.T) {
	service, _ := newService(t, "invite-release")
	ctx := context.Background()
	code, err := service.Create(ctx, CreateInput{MaxUses: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ConsumeRegistrationCode(ctx, code.Code); err != nil {
		t.Fatal(err)
	}
	if err := service.ReleaseRegistrationCode(ctx, code.Code); err != nil {
		t.Fatal(err)
	}
	if err := service.ConsumeRegistrationCode(ctx, code.Code); err != nil {
		t.Fatalf("after release err=%v", err)
	}
	if err := service.ReleaseRegistrationCode(ctx, "NOSUCHCODE"); err != nil {
		t.Fatalf("release unknown err=%v", err)
	}
}

func TestUpdateListDelete(t *testing.T) {
	service, _ := newService(t, "invite-crud")
	ctx := context.Background()
	created, err := service.Create(ctx, CreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	limit := 3
	updated, err := service.Update(ctx, created.ID, UpdateInput{MaxUses: &limit, Status: StatusDisabled})
	if err != nil || updated.MaxUses != 3 || updated.Status != StatusDisabled {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	items, total, err := service.List(ctx, Filter{Status: string(StatusDisabled)}, Page{Number: 1, Size: 10})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("items=%d total=%d err=%v", len(items), total, err)
	}
	if _, _, err := service.List(ctx, Filter{Status: "bogus"}, Page{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("bad filter err=%v", err)
	}
	if _, err := service.Update(ctx, "missing", UpdateInput{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing err=%v", err)
	}
	if err := service.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete again err=%v", err)
	}
}

func TestRegistrationConsumesCode(t *testing.T) {
	service, db := newService(t, "invite-registration")
	ctx := context.Background()
	code, err := service.Create(ctx, CreateInput{MaxUses: 2})
	if err != nil {
		t.Fatal(err)
	}
	sessions := session.NewService(db, time.Now)
	authService := identityauth.NewService(db, sessions).WithInviteCodes(service)

	if _, err := authService.RegisterMerchantWithCodes(ctx, "a@example.test", "SecretPass1", "", "NOSUCHCODE", identityauth.LoginMetadata{}); !errors.Is(err, identityauth.ErrInviteCodeInvalid) {
		t.Fatalf("invalid code err=%v", err)
	}
	var merchants int64
	if err := db.Model(&identitymodel.Merchant{}).Count(&merchants).Error; err != nil || merchants != 0 {
		t.Fatalf("merchants=%d err=%v", merchants, err)
	}
	registered, err := authService.RegisterMerchantWithCodes(ctx, "a@example.test", "SecretPass1", "", code.Code, identityauth.LoginMetadata{})
	if err != nil || registered.Token == "" {
		t.Fatalf("register result=%+v err=%v", registered, err)
	}
	stored, err := service.Get(ctx, code.ID)
	if err != nil || stored.UsedCount != 1 {
		t.Fatalf("used_count=%d err=%v", stored.UsedCount, err)
	}
	// A duplicate identifier fails and refunds the use.
	if _, err := authService.RegisterMerchantWithCodes(ctx, "a@example.test", "SecretPass1", "", code.Code, identityauth.LoginMetadata{}); !errors.Is(err, identityauth.ErrMerchantExists) {
		t.Fatalf("duplicate err=%v", err)
	}
	stored, err = service.Get(ctx, code.ID)
	if err != nil || stored.UsedCount != 1 {
		t.Fatalf("used_count after failure=%d err=%v", stored.UsedCount, err)
	}
	// Registration without a code keeps working.
	if _, err := authService.RegisterMerchantWithCodes(ctx, "b@example.test", "SecretPass1", "", "", identityauth.LoginMetadata{}); err != nil {
		t.Fatalf("no-code registration err=%v", err)
	}
}
