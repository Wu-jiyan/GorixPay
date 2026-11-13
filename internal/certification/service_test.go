package certification

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	identitymodel "gorixpay/internal/identity/model"
	ledgermodel "gorixpay/internal/ledger/model"
	ledgerservice "gorixpay/internal/ledger/service"
	paymentservice "gorixpay/internal/payment/service"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
	"gorixpay/internal/platform/secret"

	"gorm.io/gorm"
)

type settingRow struct {
	Key       string    `gorm:"column:setting_key;primaryKey"`
	Value     string    `gorm:"column:setting_value;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
}

func (settingRow) TableName() string { return "system_settings" }

func TestSubmitReviewFlowAndGuard(t *testing.T) {
	service, db, _, merchantID := certificationFixture(t, "cert-flow")
	submit := SubmitRequest{MerchantID: merchantID, CertType: "person", RealName: "张三", IDNo: "110101199001011234"}
	item, err := service.Submit(context.Background(), submit)
	if err != nil || item.Status != StatusPending {
		t.Fatalf("submit: %+v err=%v", item, err)
	}
	if _, err := service.Submit(context.Background(), submit); !errors.Is(err, ErrPendingReview) {
		t.Fatalf("duplicate submit err=%v", err)
	}

	guard := service.Guard()
	command := paymentservice.CreateCommand{MerchantID: merchantID, Amount: 100, NotifyURL: "https://merchant.example/notify", Name: "test"}
	if err := guard.Check(context.Background(), command); err != nil {
		t.Fatalf("guard without requirement should pass: %v", err)
	}
	writeSetting(t, db, "certification.required", "true")
	var rejection paymentservice.Rejection
	if err := guard.Check(context.Background(), command); !errors.As(err, &rejection) || rejection.Reason != "请先完成实名认证" {
		t.Fatalf("guard must reject unapproved merchant: %v", err)
	}

	reviewed, err := service.Review(context.Background(), ReviewRequest{MerchantID: merchantID, Action: "approve", ReviewerID: "admin-1"})
	if err != nil || reviewed.Status != StatusApproved {
		t.Fatalf("review: %+v err=%v", reviewed, err)
	}
	if err := guard.Check(context.Background(), command); err != nil {
		t.Fatalf("approved merchant rejected: %v", err)
	}
	if _, err := service.Submit(context.Background(), submit); !errors.Is(err, ErrAlreadyApproved) {
		t.Fatalf("resubmit after approval err=%v", err)
	}
}

func TestApprovalChargesCertificationFee(t *testing.T) {
	service, db, ledger, merchantID := certificationFixture(t, "cert-fee")
	writeSetting(t, db, "certification.fee_cents", "500")
	if _, err := service.Submit(context.Background(), SubmitRequest{
		MerchantID: merchantID, CertType: "corp", RealName: "李四", IDNo: "11010119900101567X",
		CorpName: "示例科技有限公司", CorpNo: "91310000MA1FL0000X",
	}); err != nil {
		t.Fatal(err)
	}
	approved, err := service.Review(context.Background(), ReviewRequest{MerchantID: merchantID, Action: "approve", ReviewerID: "admin-1"})
	if err != nil || approved.Status != StatusApproved || approved.FeeCents != 500 {
		t.Fatalf("approve: %+v err=%v", approved, err)
	}
	balance := merchantBalance(t, ledger, db, merchantID)
	if balance != 500 {
		t.Fatalf("balance after fee = %d", balance)
	}
	replay, err := service.Review(context.Background(), ReviewRequest{MerchantID: merchantID, Action: "approve", ReviewerID: "admin-1"})
	if err != nil || replay.Status != StatusApproved {
		t.Fatalf("idempotent approve: %+v err=%v", replay, err)
	}
	if got := merchantBalance(t, ledger, db, merchantID); got != 500 {
		t.Fatalf("fee charged twice: %d", got)
	}
}

func TestApprovalFailsWhenFeeExceedsBalance(t *testing.T) {
	service, db, _, merchantID := certificationFixture(t, "cert-fee-insufficient")
	writeSetting(t, db, "certification.fee_cents", "5000")
	if _, err := service.Submit(context.Background(), SubmitRequest{
		MerchantID: merchantID, CertType: "person", RealName: "王五", IDNo: "110101199001012345",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Review(context.Background(), ReviewRequest{MerchantID: merchantID, Action: "approve", ReviewerID: "admin-1"}); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("err=%v", err)
	}
	current, err := service.Current(context.Background(), merchantID)
	if err != nil || current.Status != StatusPending {
		t.Fatalf("status changed after failed fee: %+v err=%v", current, err)
	}
}

func TestSubmitValidatesIDNumberAndCorpFields(t *testing.T) {
	service, _, _, merchantID := certificationFixture(t, "cert-validation")
	if _, err := service.Submit(context.Background(), SubmitRequest{MerchantID: merchantID, CertType: "person", RealName: "张三", IDNo: "123"}); !errors.Is(err, ErrInvalidIDNo) {
		t.Fatalf("short id err=%v", err)
	}
	if _, err := service.Submit(context.Background(), SubmitRequest{MerchantID: merchantID, CertType: "person", RealName: "张三", IDNo: "11010119900101123Y"}); !errors.Is(err, ErrInvalidIDNo) {
		t.Fatalf("bad last char err=%v", err)
	}
	if _, err := service.Submit(context.Background(), SubmitRequest{MerchantID: merchantID, CertType: "corp", RealName: "张三", IDNo: "110101199001011234"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing corp fields err=%v", err)
	}
}

func TestMaskedValuesHideSensitiveParts(t *testing.T) {
	if got := MaskedName("张三"); got != "张*" {
		t.Fatalf("masked name = %q", got)
	}
	if got := MaskedIDNo("110101199001011234"); got != "1101**********1234" {
		t.Fatalf("masked id = %q", got)
	}
}

func certificationFixture(t *testing.T, name string) (*Service, *gorm.DB, *ledgerservice.Service, string) {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Certification{}, &identitymodel.Merchant{}, &settingRow{},
		&ledgermodel.Account{}, &ledgermodel.Balance{}, &ledgermodel.Transaction{}, &ledgermodel.Entry{}); err != nil {
		t.Fatal(err)
	}
	merchant := identitymodel.Merchant{ID: "merchant-1", DisplayName: "merchant", Status: identitymodel.StatusActive}
	if err := db.Create(&merchant).Error; err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC) }
	ledger := ledgerservice.New(db, now)
	merchantAccount, err := ledger.CreateAccount(context.Background(), ledgerservice.AccountRequest{
		OwnerType: "merchant", OwnerID: merchant.ID, Purpose: "balance", Category: ledgermodel.CategoryLiability, Currency: "CNY",
	})
	if err != nil {
		t.Fatal(err)
	}
	platformAccount, err := ledger.CreateAccount(context.Background(), ledgerservice.AccountRequest{
		OwnerType: "platform", OwnerID: "platform", Purpose: "cash", Category: ledgermodel.CategoryAsset, Currency: "CNY",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.Post(context.Background(), ledgerservice.TransactionRequest{
		IdempotencyKey: "seed:" + merchant.ID, ReferenceType: "seed", ReferenceID: merchant.ID, Description: "seed",
		Postings: []ledgerservice.Posting{
			{AccountID: platformAccount.ID, Direction: ledgermodel.DirectionDebit, Bucket: ledgermodel.BucketAvailable, Amount: 1000},
			{AccountID: merchantAccount.ID, Direction: ledgermodel.DirectionCredit, Bucket: ledgermodel.BucketAvailable, Amount: 1000},
		},
	}); err != nil {
		t.Fatal(err)
	}
	cipher, err := secret.NewCipher(bytes.Repeat([]byte{0x71}, 32), "v1")
	if err != nil {
		t.Fatal(err)
	}
	return New(db, cipher, ledger, now), db, ledger, merchant.ID
}

func writeSetting(t *testing.T, db *gorm.DB, key, value string) {
	t.Helper()
	row := settingRow{Key: key, Value: value, UpdatedAt: time.Now().UTC()}
	if err := db.Save(&row).Error; err != nil {
		t.Fatal(err)
	}
}

func merchantBalance(t *testing.T, ledger *ledgerservice.Service, db *gorm.DB, merchantID string) int64 {
	t.Helper()
	var account ledgermodel.Account
	if err := db.First(&account, "owner_type = ? AND owner_id = ? AND purpose = ?", "merchant", merchantID, "balance").Error; err != nil {
		t.Fatal(err)
	}
	var balance ledgermodel.Balance
	if err := db.First(&balance, "account_id = ?", account.ID).Error; err != nil {
		t.Fatal(err)
	}
	return balance.Available
}
