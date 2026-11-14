package transfer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	identitymodel "gorixpay/internal/identity/model"

	"github.com/gin-gonic/gin"
)

func TestCancelRedBatchReleasesFreezeAndIsIdempotent(t *testing.T) {
	service, db, merchantID := transferFixture(t, "transfer-red-cancel")
	ctx := context.Background()
	batch, _, err := service.CreateRed(ctx, RedRequest{MerchantID: merchantID, TotalAmount: 900, Count: 3})
	if err != nil {
		t.Fatal(err)
	}
	balance, err := service.Balance(ctx, merchantID)
	if err != nil || balance.Available != 4100 || balance.Frozen != 900 {
		t.Fatalf("balance before cancel=%+v err=%v", balance, err)
	}
	cancelled, err := service.CancelRedBatch(ctx, batch.BatchNo, "运营撤销")
	if err != nil || cancelled != 3 {
		t.Fatalf("cancel=%d err=%v", cancelled, err)
	}
	balance, err = service.Balance(ctx, merchantID)
	if err != nil || balance.Available != 5000 || balance.Frozen != 0 {
		t.Fatalf("balance after cancel=%+v err=%v", balance, err)
	}
	var children []Order
	if err := db.Where("batch_no = ?", batch.BatchNo).Find(&children).Error; err != nil {
		t.Fatal(err)
	}
	for _, child := range children {
		if child.Status != StatusRejected || child.RejectionReason == nil || *child.RejectionReason != "运营撤销" {
			t.Fatalf("child=%+v", child)
		}
	}
	// Repeating the cancel is a no-op instead of an error.
	again, err := service.CancelRedBatch(ctx, batch.BatchNo, "")
	if err != nil || again != 0 {
		t.Fatalf("second cancel=%d err=%v", again, err)
	}
	if balance, err = service.Balance(ctx, merchantID); err != nil || balance.Available != 5000 {
		t.Fatalf("balance after second cancel=%+v err=%v", balance, err)
	}
	if _, err := service.CancelRedBatch(ctx, "missing-batch", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing batch err=%v", err)
	}
	if _, err := service.CancelRedBatch(ctx, "", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty batch err=%v", err)
	}
}

func TestCancelRedBatchRejectsReviewedPackets(t *testing.T) {
	service, db, merchantID := transferFixture(t, "transfer-red-cancel-approved")
	ctx := context.Background()
	batch, _, err := service.CreateRed(ctx, RedRequest{MerchantID: merchantID, TotalAmount: 600, Count: 2})
	if err != nil {
		t.Fatal(err)
	}
	var children []Order
	if err := db.Where("batch_no = ?", batch.BatchNo).Order("out_transfer_no ASC").Find(&children).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(ctx, children[0].ID, "admin-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CancelRedBatch(ctx, batch.BatchNo, "撤销"); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("cancel with approved packet err=%v", err)
	}
	var pending Order
	if err := db.First(&pending, "id = ?", children[1].ID).Error; err != nil {
		t.Fatal(err)
	}
	if pending.Status != StatusPending {
		t.Fatalf("pending packet was touched: %+v", pending)
	}
	// The approved packet's freeze is already gone; the still-pending one stays.
	balance, err := service.Balance(ctx, merchantID)
	if err != nil || balance.Available != 4400 || balance.Frozen != pending.Amount {
		t.Fatalf("balance=%+v pending=%d err=%v", balance, pending.Amount, err)
	}
}

func TestExpireRedBatchesOnlyTouchesOldPendingBatches(t *testing.T) {
	service, db, merchantID := transferFixture(t, "transfer-red-expire")
	ctx := context.Background()
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	old, _, err := service.CreateRed(ctx, RedRequest{MerchantID: merchantID, TotalAmount: 300, Count: 2})
	if err != nil {
		t.Fatal(err)
	}
	fresh, _, err := service.CreateRed(ctx, RedRequest{MerchantID: merchantID, TotalAmount: 200, Count: 2})
	if err != nil {
		t.Fatal(err)
	}
	reviewed, _, err := service.CreateRed(ctx, RedRequest{MerchantID: merchantID, TotalAmount: 100, Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	// Two batches are older than the cutoff; the reviewed one counts as handled.
	stale := now.Add(-3 * time.Hour).UTC()
	if err := db.Model(&Order{}).Where("batch_no IN ?", []string{old.BatchNo, reviewed.BatchNo}).
		Update("created_at", stale).Error; err != nil {
		t.Fatal(err)
	}
	var reviewedChildren []Order
	if err := db.Where("batch_no = ?", reviewed.BatchNo).Find(&reviewedChildren).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(ctx, reviewedChildren[0].ID, "admin-1"); err != nil {
		t.Fatal(err)
	}

	count, err := service.ExpireRedBatches(ctx, now, 2*time.Hour)
	if err != nil || count != 1 {
		t.Fatalf("expire count=%d err=%v", count, err)
	}
	var expired []Order
	if err := db.Where("batch_no = ?", old.BatchNo).Find(&expired).Error; err != nil {
		t.Fatal(err)
	}
	for _, child := range expired {
		if child.Status != StatusRejected || child.RejectionReason == nil || !strings.Contains(*child.RejectionReason, "超时") {
			t.Fatalf("expired child=%+v", child)
		}
	}
	var freshChildren []Order
	if err := db.Where("batch_no = ?", fresh.BatchNo).Find(&freshChildren).Error; err != nil {
		t.Fatal(err)
	}
	for _, child := range freshChildren {
		if child.Status != StatusPending {
			t.Fatalf("fresh batch touched: %+v", child)
		}
	}
	// The expired batch (300) is released; the fresh batch and the reviewed
	// packet keep their freezes.
	balance, err := service.Balance(ctx, merchantID)
	if err != nil || balance.Available != 4700 || balance.Frozen != 200 {
		t.Fatalf("balance=%+v err=%v", balance, err)
	}
	if count, err := service.ExpireRedBatches(ctx, now, 2*time.Hour); err != nil || count != 0 {
		t.Fatalf("second expire count=%d err=%v", count, err)
	}
	if _, err := service.ExpireRedBatches(ctx, now, 0); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("zero window err=%v", err)
	}
}

func TestCancelRedBatchEndpoint(t *testing.T) {
	service, _, merchantID := transferFixture(t, "transfer-red-cancel-http")
	ctx := context.Background()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterAdmin(router.Group("/api/v1/admin", stubSession(identitymodel.PrincipalAdministrator, "admin-1")), service)

	batch, _, err := service.CreateRed(ctx, RedRequest{MerchantID: merchantID, TotalAmount: 300, Count: 2})
	if err != nil {
		t.Fatal(err)
	}
	response := performJSON(router, http.MethodPost, "/api/v1/admin/transfers/red/"+batch.BatchNo+"/cancel", `{"reason":"运营撤销"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("cancel: %d %s", response.Code, response.Body.String())
	}
	var body struct {
		BatchNo   string `json:"batch_no"`
		Cancelled int    `json:"cancelled"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.BatchNo != batch.BatchNo || body.Cancelled != 2 {
		t.Fatalf("body=%+v", body)
	}
	if response := performJSON(router, http.MethodPost, "/api/v1/admin/transfers/red/missing/cancel", ""); response.Code != http.StatusNotFound {
		t.Fatalf("missing batch: %d %s", response.Code, response.Body.String())
	}

	reviewed, _, err := service.CreateRed(ctx, RedRequest{MerchantID: merchantID, TotalAmount: 100, Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	var children []Order
	if err := service.db.Where("batch_no = ?", reviewed.BatchNo).Find(&children).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(ctx, children[0].ID, "admin-1"); err != nil {
		t.Fatal(err)
	}
	if response := performJSON(router, http.MethodPost, "/api/v1/admin/transfers/red/"+reviewed.BatchNo+"/cancel", ""); response.Code != http.StatusConflict {
		t.Fatalf("reviewed batch: %d %s", response.Code, response.Body.String())
	}
}
