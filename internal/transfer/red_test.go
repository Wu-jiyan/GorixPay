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

func TestCreateRedSplitsTotalAndFreezesBalance(t *testing.T) {
	service, db, merchantID := transferFixture(t, "transfer-red-create")
	batch, created, err := service.CreateRed(context.Background(), RedRequest{
		MerchantID: merchantID, TotalAmount: 1000, Count: 4, Remark: "新年",
	})
	if err != nil || !created {
		t.Fatalf("create red: %+v created=%v err=%v", batch, created, err)
	}
	if batch.Count != 4 || batch.Amount != 1000 || batch.ClaimedCount != 0 || batch.Status != StatusPending || batch.BatchNo == "" {
		t.Fatalf("batch=%+v", batch)
	}
	var orders []Order
	if err := db.Where("merchant_id = ? AND kind = ?", merchantID, KindRed).Order("out_transfer_no ASC").Find(&orders).Error; err != nil {
		t.Fatal(err)
	}
	if len(orders) != 4 {
		t.Fatalf("children=%d", len(orders))
	}
	var total int64
	for index, order := range orders {
		if order.Kind != KindRed || order.Status != StatusPending || order.BatchNo == nil || *order.BatchNo != batch.BatchNo {
			t.Fatalf("child=%+v", order)
		}
		if order.Amount < 1 {
			t.Fatalf("child amount=%d", order.Amount)
		}
		wantOutNo := batch.BatchNo + "-" + "0" + string(rune('1'+index))
		if order.OutTransferNo != wantOutNo {
			t.Fatalf("out transfer no=%q want %q", order.OutTransferNo, wantOutNo)
		}
		if order.Remark == nil || *order.Remark != "新年" {
			t.Fatalf("remark=%v", order.Remark)
		}
		total += order.Amount
	}
	if total != 1000 {
		t.Fatalf("children sum=%d", total)
	}
	balance, err := service.Balance(context.Background(), merchantID)
	if err != nil || balance.Available != 4000 || balance.Frozen != 1000 {
		t.Fatalf("balance=%+v err=%v", balance, err)
	}
}

func TestCreateRedRejectsInvalidBatches(t *testing.T) {
	service, db, merchantID := transferFixture(t, "transfer-red-invalid")
	for name, request := range map[string]RedRequest{
		"no-merchant":    {TotalAmount: 100, Count: 1},
		"zero-total":     {MerchantID: merchantID, Count: 1},
		"zero-count":     {MerchantID: merchantID, TotalAmount: 100},
		"count-too-big":  {MerchantID: merchantID, TotalAmount: 3, Count: 5},
		"count-over-100": {MerchantID: merchantID, TotalAmount: 1000, Count: MaxRedCount + 1},
		"remark-too-long": {MerchantID: merchantID, TotalAmount: 100, Count: 1,
			Remark: strings.Repeat("x", 192)},
		"bad-out-no": {MerchantID: merchantID, TotalAmount: 100, Count: 1, OutTransferNo: "bad no!"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := service.CreateRed(context.Background(), request); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("err=%v", err)
			}
		})
	}
	var count int64
	if err := db.Model(&Order{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("orders=%d err=%v", count, err)
	}
}

func TestCreateRedRejectsInsufficientBalanceAtomically(t *testing.T) {
	service, db, merchantID := transferFixture(t, "transfer-red-insufficient")
	_, _, err := service.CreateRed(context.Background(), RedRequest{MerchantID: merchantID, TotalAmount: 6000, Count: 3})
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("err=%v", err)
	}
	var count int64
	if err := db.Model(&Order{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("orders=%d err=%v", count, err)
	}
	balance, err := service.Balance(context.Background(), merchantID)
	if err != nil || balance.Available != 5000 || balance.Frozen != 0 {
		t.Fatalf("balance=%+v err=%v", balance, err)
	}
}

func TestCreateRedReplayAndConflict(t *testing.T) {
	service, db, merchantID := transferFixture(t, "transfer-red-replay")
	request := RedRequest{MerchantID: merchantID, TotalAmount: 600, Count: 3, OutTransferNo: "BATCH-1"}
	first, created, err := service.CreateRed(context.Background(), request)
	if err != nil || !created {
		t.Fatalf("first: %+v created=%v err=%v", first, created, err)
	}
	replay, created, err := service.CreateRed(context.Background(), request)
	if err != nil || created || replay.BatchNo != first.BatchNo || replay.Count != 3 || replay.Amount != 600 {
		t.Fatalf("replay: %+v created=%v err=%v", replay, created, err)
	}
	conflicting := request
	conflicting.Count = 2
	if _, _, err := service.CreateRed(context.Background(), conflicting); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflict err=%v", err)
	}
	var count int64
	if err := db.Model(&Order{}).Count(&count).Error; err != nil || count != 3 {
		t.Fatalf("orders=%d err=%v", count, err)
	}
}

func TestRedBatchesAggregateAndFollowChildStatus(t *testing.T) {
	service, _, merchantID := transferFixture(t, "transfer-red-batches")
	current := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return current }
	first, _, err := service.CreateRed(context.Background(), RedRequest{MerchantID: merchantID, TotalAmount: 1000, Count: 4, Remark: "A"})
	if err != nil {
		t.Fatal(err)
	}
	current = current.Add(time.Minute)
	second, _, err := service.CreateRed(context.Background(), RedRequest{MerchantID: merchantID, TotalAmount: 500, Count: 2, Remark: "B"})
	if err != nil {
		t.Fatal(err)
	}
	batches, total, err := service.RedBatches(context.Background(), Filter{MerchantID: merchantID}, Page{Number: 1, Size: 10})
	if err != nil || total != 2 || len(batches) != 2 {
		t.Fatalf("batches=%+v total=%d err=%v", batches, total, err)
	}
	if batches[0].BatchNo != second.BatchNo || batches[0].Amount != 500 || batches[0].Count != 2 || batches[0].ClaimedCount != 0 {
		t.Fatalf("newest batch=%+v", batches[0])
	}
	if batches[1].BatchNo != first.BatchNo || batches[1].Amount != 1000 || batches[1].Remark != "A" {
		t.Fatalf("oldest batch=%+v", batches[1])
	}

	// Approving one child keeps the batch pending; approving the rest completes it.
	var children []Order
	if err := service.db.Where("batch_no = ?", first.BatchNo).Order("out_transfer_no ASC").Find(&children).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(context.Background(), children[0].ID, "admin-1"); err != nil {
		t.Fatal(err)
	}
	batches, _, err = service.RedBatches(context.Background(), Filter{MerchantID: merchantID}, Page{Number: 1, Size: 10})
	if err != nil || batches[0].Status != StatusPending || batches[1].Status != StatusPending {
		t.Fatalf("after one approval: %+v err=%v", batches, err)
	}
	for _, child := range children[1:] {
		if _, err := service.Approve(context.Background(), child.ID, "admin-1"); err != nil {
			t.Fatal(err)
		}
	}
	batches, _, err = service.RedBatches(context.Background(), Filter{MerchantID: merchantID}, Page{Number: 1, Size: 10})
	if err != nil || batches[1].Status != StatusApproved {
		t.Fatalf("after all approvals: %+v err=%v", batches, err)
	}
	// The admin listing (no merchant scope) sees both batches too.
	all, total, err := service.RedBatches(context.Background(), Filter{}, Page{Number: 1, Size: 10})
	if err != nil || total != 2 || len(all) != 2 {
		t.Fatalf("unscoped batches=%+v total=%d err=%v", all, total, err)
	}
}

func TestRedPacketEndpoints(t *testing.T) {
	service, _, merchantID := transferFixture(t, "transfer-red-http")
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterMerchant(router.Group("/api/v1/merchant", stubSession(identitymodel.PrincipalMerchant, merchantID)), service)
	RegisterAdmin(router.Group("/api/v1/admin", stubSession(identitymodel.PrincipalAdministrator, "admin-1")), service)

	response := performJSON(router, http.MethodPost, "/api/v1/merchant/transfers/red", `{"amount":900,"count":3,"remark":"春节"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create red: %d %s", response.Code, response.Body.String())
	}
	var created RedBatch
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Amount != 900 || created.Count != 3 || created.ClaimedCount != 0 || created.BatchNo == "" || created.TransferNo == "" {
		t.Fatalf("created=%+v", created)
	}

	response = performJSON(router, http.MethodGet, "/api/v1/merchant/transfers/red?page=1&page_size=10", "")
	if response.Code != http.StatusOK {
		t.Fatalf("list red: %d %s", response.Code, response.Body.String())
	}
	var listing struct {
		Items []RedBatch `json:"items"`
		Total int64      `json:"total"`
		Page  int        `json:"page"`
		Size  int        `json:"page_size"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	if listing.Total != 1 || len(listing.Items) != 1 || listing.Items[0].BatchNo != created.BatchNo || listing.Items[0].ClaimedCount != 0 {
		t.Fatalf("listing=%+v", listing)
	}

	// Ordinary payouts stay out of the red packet listing.
	if response := performJSON(router, http.MethodPost, "/api/v1/merchant/transfers", `{"out_transfer_no":"NORMAL-1","amount":100,"account_no":"A1"}`); response.Code != http.StatusCreated {
		t.Fatalf("normal create: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(router, http.MethodGet, "/api/v1/merchant/transfers/red", ""); !strings.Contains(response.Body.String(), `"total":1`) {
		t.Fatalf("red list polluted: %s", response.Body.String())
	}

	if response := performJSON(router, http.MethodPost, "/api/v1/merchant/transfers/red", `{"amount":0,"count":1}`); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid body: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(router, http.MethodPost, "/api/v1/merchant/transfers/red", `{"amount":9000,"count":2}`); response.Code != http.StatusConflict {
		t.Fatalf("insufficient: %d %s", response.Code, response.Body.String())
	}

	// Admin creates a red packet for an explicit merchant.
	response = performJSON(router, http.MethodPost, "/api/v1/admin/transfers/red", `{"merchant_id":"merchant-1","amount":300,"count":2,"remark":"运营"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("admin create red: %d %s", response.Code, response.Body.String())
	}
	response = performJSON(router, http.MethodGet, "/api/v1/admin/transfers/red?merchant_id=merchant-1", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"total":2`) {
		t.Fatalf("admin list red: %d %s", response.Code, response.Body.String())
	}
	response = performJSON(router, http.MethodGet, "/api/v1/merchant/transfers?kind=red", "")
	if response.Code != http.StatusOK {
		t.Fatalf("kind filter: %d %s", response.Code, response.Body.String())
	}
	var plain struct {
		Items []map[string]any `json:"items"`
		Total int64            `json:"total"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &plain); err != nil {
		t.Fatal(err)
	}
	if plain.Total != 5 {
		t.Fatalf("kind=red children total=%d", plain.Total)
	}
}

// splitRedAmount is exercised directly because the random draw has no stable
// expectation beyond the invariants.
func TestSplitRedAmountInvariants(t *testing.T) {
	for _, total := range []int64{1, 2, 5, 100, 999, 100000} {
		for _, count := range []int{1, 2, 3, 7} {
			if int64(count) > total {
				continue
			}
			amounts, err := splitRedAmount(total, count)
			if err != nil {
				t.Fatalf("total=%d count=%d err=%v", total, count, err)
			}
			if len(amounts) != count {
				t.Fatalf("total=%d count=%d len=%d", total, count, len(amounts))
			}
			var sum int64
			for _, amount := range amounts {
				if amount < 1 {
					t.Fatalf("total=%d count=%d zero packet", total, count)
				}
				sum += amount
			}
			if sum != total {
				t.Fatalf("total=%d count=%d sum=%d", total, count, sum)
			}
		}
	}
	if _, err := splitRedAmount(3, 5); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("too many packets err=%v", err)
	}
}
