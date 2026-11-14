package transfer

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	identitymodel "gorixpay/internal/identity/model"

	"github.com/gin-gonic/gin"
)

// TestMerchantCanCancelOwnRedBatchEndpoint covers the merchant self service route.
// It only exists because the first version shipped the admin route alone, and the
// merchant portal reported the feature as unavailable.
func TestMerchantCanCancelOwnRedBatchEndpoint(t *testing.T) {
	service, _, merchantID := transferFixture(t, "transfer-red-cancel-merchant")
	ctx := context.Background()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterMerchant(router.Group("/api/v1/merchant", stubSession(identitymodel.PrincipalMerchant, merchantID)), service)

	batch, _, err := service.CreateRed(ctx, RedRequest{MerchantID: merchantID, TotalAmount: 300, Count: 2})
	if err != nil {
		t.Fatal(err)
	}
	response := performJSON(router, http.MethodPost, "/api/v1/merchant/transfers/red/"+batch.BatchNo+"/cancel", `{"reason":"活动取消"}`)
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
	// Freeze released: nothing stays frozen after the whole batch is withdrawn.
	balance, err := service.Balance(ctx, merchantID)
	if err != nil {
		t.Fatal(err)
	}
	if balance.Frozen != 0 {
		t.Fatalf("frozen = %d, want 0 after cancelling the batch", balance.Frozen)
	}
}

// TestMerchantCannotCancelAnotherMerchantsBatch keeps one merchant's batches
// invisible to another: the answer is the same 404 as a batch that does not exist.
func TestMerchantCannotCancelAnotherMerchantsBatch(t *testing.T) {
	service, _, merchantID := transferFixture(t, "transfer-red-cancel-foreign")
	ctx := context.Background()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterMerchant(router.Group("/api/v1/merchant", stubSession(identitymodel.PrincipalMerchant, "merchant-2")), service)

	batch, _, err := service.CreateRed(ctx, RedRequest{MerchantID: merchantID, TotalAmount: 200, Count: 2})
	if err != nil {
		t.Fatal(err)
	}
	response := performJSON(router, http.MethodPost, "/api/v1/merchant/transfers/red/"+batch.BatchNo+"/cancel", `{"reason":"试试"}`)
	if response.Code != http.StatusNotFound {
		t.Fatalf("foreign batch cancel: %d %s", response.Code, response.Body.String())
	}
	// The batch is untouched.
	balance, err := service.Balance(ctx, merchantID)
	if err != nil {
		t.Fatal(err)
	}
	if balance.Frozen != 200 {
		t.Fatalf("frozen = %d, want the batch still pending", balance.Frozen)
	}
	if response := performJSON(router, http.MethodPost, "/api/v1/merchant/transfers/red/missing/cancel", ""); response.Code != http.StatusNotFound {
		t.Fatalf("missing batch: %d %s", response.Code, response.Body.String())
	}
}
