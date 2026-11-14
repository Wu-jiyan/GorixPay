package transfer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	identitymodel "gorixpay/internal/identity/model"

	"github.com/gin-gonic/gin"
)

func TestCreateAdminManualTransferIsIdempotent(t *testing.T) {
	service, db, merchantID := transferFixture(t, "transfer-admin-manual")
	request := AdminRequest{MerchantID: merchantID, Amount: 1000, Destination: "6222000000000011", Remark: "manual payout", IdempotencyKey: "key-1"}
	order, created, err := service.CreateAdmin(context.Background(), request)
	if err != nil || !created || order.Kind != KindManual || order.Status != StatusPending {
		t.Fatalf("order=%+v created=%v err=%v", order, created, err)
	}
	if order.AccountNo != "6222000000000011" || order.Remark == nil || *order.Remark != "manual payout" {
		t.Fatalf("order=%+v", order)
	}
	replay, created, err := service.CreateAdmin(context.Background(), request)
	if err != nil || created || replay.ID != order.ID {
		t.Fatalf("replay=%+v created=%v err=%v", replay, created, err)
	}
	changed := request
	changed.Amount = 2000
	if _, _, err := service.CreateAdmin(context.Background(), changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflict err=%v", err)
	}
	balance, err := service.Balance(context.Background(), merchantID)
	if err != nil || balance.Frozen != 1000 || balance.Available != 4000 {
		t.Fatalf("balance=%+v err=%v", balance, err)
	}
	if _, _, err := service.CreateAdmin(context.Background(), AdminRequest{MerchantID: "missing", Amount: 100, Destination: "x"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing merchant err=%v", err)
	}
	var count int64
	if err := db.Model(&Order{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("orders=%d err=%v", count, err)
	}
}

func TestCreateAdminBatchRollsBackOnFailure(t *testing.T) {
	service, db, merchantID := transferFixture(t, "transfer-admin-batch")
	result, err := service.CreateAdminBatch(context.Background(), AdminBatchRequest{
		MerchantID: merchantID,
		Items: []AdminBatchItem{
			{Amount: 1000, Destination: "A-1"},
			{Amount: 2000, Destination: "A-2"},
		},
		Remark: "batch payout", IdempotencyKey: "batch-1",
	})
	if err != nil || result.Created != 2 || result.BatchNo == "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	orders, total, err := service.List(context.Background(), Filter{MerchantID: merchantID, Kind: KindManual}, Page{Number: 1, Size: 10})
	if err != nil || total != 2 {
		t.Fatalf("total=%d err=%v", total, err)
	}
	for _, order := range orders {
		if order.Kind != KindManual || order.BatchNo == nil || *order.BatchNo != result.BatchNo {
			t.Fatalf("order=%+v", order)
		}
	}
	balance, _ := service.Balance(context.Background(), merchantID)
	if balance.Frozen != 3000 || balance.Available != 2000 {
		t.Fatalf("balance=%+v", balance)
	}

	// Second item exceeds the remaining balance, so the whole batch compensates.
	_, err = service.CreateAdminBatch(context.Background(), AdminBatchRequest{
		MerchantID: merchantID,
		Items: []AdminBatchItem{
			{Amount: 100, Destination: "B-1"},
			{Amount: 9000, Destination: "B-2"},
		},
		IdempotencyKey: "batch-2",
	})
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("err=%v", err)
	}
	var count int64
	if err := db.Model(&Order{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("orders after rollback=%d err=%v", count, err)
	}
	balance, _ = service.Balance(context.Background(), merchantID)
	if balance.Frozen != 3000 || balance.Available != 2000 {
		t.Fatalf("balance after rollback=%+v", balance)
	}
}

func TestAdminTransferHTTPEndpoints(t *testing.T) {
	service, _, merchantID := transferFixture(t, "transfer-admin-http")
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterAdmin(router.Group("/api/v1/admin", stubSession(identitymodel.PrincipalAdministrator, "admin-1")), service)

	body := `{"merchant_id":"` + merchantID + `","amount_cents":1000,"destination":"6222000000000012","remark":"manual","idempotency_key":"http-1"}`
	response := performJSON(router, http.MethodPost, "/api/v1/admin/transfers", body)
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"kind":"manual"`) {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	var order map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &order); err != nil {
		t.Fatal(err)
	}
	if order["amount"] != float64(1000) || order["status"] != "pending" {
		t.Fatalf("order=%v", order)
	}
	replay := performJSON(router, http.MethodPost, "/api/v1/admin/transfers", body)
	if replay.Code != http.StatusCreated || !strings.Contains(replay.Body.String(), order["id"].(string)) {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
	}

	batch := `{"merchant_id":"` + merchantID + `","items":[{"amount_cents":100,"destination":"C-1"},{"amount_cents":200,"destination":"C-2"}],"remark":"batch","idempotency_key":"http-batch"}`
	batchResponse := performJSON(router, http.MethodPost, "/api/v1/admin/transfers/batch", batch)
	if batchResponse.Code != http.StatusCreated || !strings.Contains(batchResponse.Body.String(), `"created":2`) || !strings.Contains(batchResponse.Body.String(), `"batch_no":"`) {
		t.Fatalf("batch status=%d body=%s", batchResponse.Code, batchResponse.Body.String())
	}
	if response := performJSON(router, http.MethodPost, "/api/v1/admin/transfers", `{"merchant_id":"missing","amount_cents":100,"destination":"x"}`); response.Code != http.StatusBadRequest {
		t.Fatalf("missing merchant status=%d", response.Code)
	}
	if response := performJSON(router, http.MethodPost, "/api/v1/admin/transfers/batch", `{"merchant_id":"`+merchantID+`","items":[]}`); response.Code != http.StatusBadRequest {
		t.Fatalf("empty batch status=%d", response.Code)
	}
	if response := performJSON(router, http.MethodPost, "/api/v1/admin/transfers", `{"merchant_id":"`+merchantID+`","amount_cents":0,"destination":"x"}`); response.Code != http.StatusBadRequest {
		t.Fatalf("zero amount status=%d", response.Code)
	}
}
