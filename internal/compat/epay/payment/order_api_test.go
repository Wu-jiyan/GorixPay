package payment

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	epaysigning "gorixpay/internal/compat/epay/signing"
)

func TestSignedQueryAndCloseEndpoints(t *testing.T) {
	router, key, _ := testRouter(t, "query-close-api")
	created := validForm(key)
	createResponse := performForm(router, http.MethodPost, "/mapi.php", created)
	tradeNo := jsonString(t, createResponse.Body.String(), "trade_no")

	query := signedOrderForm(key, tradeNo)
	response := performForm(router, http.MethodPost, "/api/pay/query", query)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"code":0`) ||
		!strings.Contains(response.Body.String(), `"status":0`) || !strings.Contains(response.Body.String(), `"money":"1.00"`) {
		t.Fatalf("query: %d %s", response.Code, response.Body.String())
	}

	closeForm := signedOrderForm(key, tradeNo)
	response = performForm(router, http.MethodPost, "/api/pay/close", closeForm)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"code":0`) {
		t.Fatalf("close: %d %s", response.Code, response.Body.String())
	}
	response = performForm(router, http.MethodPost, "/api/pay/query", query)
	if !strings.Contains(response.Body.String(), `"status":2`) {
		t.Fatalf("closed query: %s", response.Body.String())
	}
}

func TestQueryCannotReadAnotherMerchantOrder(t *testing.T) {
	router, key, _ := testRouter(t, "query-isolation")
	created := validForm(key)
	tradeNo := jsonString(t, performForm(router, http.MethodPost, "/mapi.php", created).Body.String(), "trade_no")
	query := signedOrderForm(key, tradeNo+"9")
	response := performForm(router, http.MethodPost, "/api/pay/query", query)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"code":-1`) {
		t.Fatalf("missing query: %d %s", response.Code, response.Body.String())
	}
}

func TestCashierOrderEndpointOnlyReturnsPublicFields(t *testing.T) {
	router, key, _ := testRouter(t, "cashier-api")
	form := validForm(key)
	form.Set("param", "private-value")
	resign(form, key)
	tradeNo := jsonString(t, performForm(router, http.MethodPost, "/mapi.php", form).Body.String(), "trade_no")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/cashier/orders/"+tradeNo, nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"amount":100`) ||
		strings.Contains(response.Body.String(), "private-value") || strings.Contains(response.Body.String(), "notify_url") {
		t.Fatalf("cashier response: %d %s", response.Code, response.Body.String())
	}
}

func signedOrderForm(key, tradeNo string) url.Values {
	form := url.Values{"pid": {"1001"}, "trade_no": {tradeNo}, "sign_type": {"MD5"}}
	params := map[string]any{"pid": "1001", "trade_no": tradeNo, "sign_type": "MD5"}
	form.Set("sign", epaysigning.SignMD5(params, key))
	return form
}

func jsonString(t *testing.T, body, key string) string {
	t.Helper()
	needle := `"` + key + `":"`
	start := strings.Index(body, needle)
	if start < 0 {
		t.Fatalf("key %s missing from %s", key, body)
	}
	start += len(needle)
	end := strings.IndexByte(body[start:], '"')
	if end < 0 {
		t.Fatalf("unterminated key %s in %s", key, body)
	}
	return body[start : start+end]
}
