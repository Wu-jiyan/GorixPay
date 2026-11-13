package app

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorixpay/internal/compat/epay/signing"
	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/identity/password"
	ledgermodel "gorixpay/internal/ledger/model"
)

// ecosystemFixture boots an application and returns an admin-authenticated
// client plus a registered merchant (token, legacy uid, md5 key).
type ecosystemFixture struct {
	client        *e2eClient
	application   *App
	adminToken    string
	merchantID    string
	merchantToken string
	pid           int64
	key           string
	channelID     string
	cashierID     string
}

func newEcosystemFixture(t *testing.T) *ecosystemFixture {
	t.Helper()
	client, application := newE2EApplicationNamed(t, "app-ecosystem")
	now := time.Now().UTC()
	hash, err := password.Hash("admin12345")
	if err != nil {
		t.Fatal(err)
	}
	administrator := identitymodel.Administrator{
		ID: "00000000-0000-0000-0000-00000000ecos", Username: "admin", PasswordHash: hash,
		DisplayName: "Admin", Status: identitymodel.StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := application.DB().Create(&administrator).Error; err != nil {
		t.Fatal(err)
	}
	status, login := client.json(http.MethodPost, "/api/v1/auth/administrator/login", map[string]string{
		"identifier": "admin", "password": "admin12345",
	})
	if status != http.StatusOK {
		t.Fatalf("admin login = %d %v", status, login)
	}
	client.token = jsonString(t, login["token"])
	status, channel := client.json(http.MethodPost, "/api/v1/admin/channels", map[string]any{
		"name": "sandbox", "plugin_code": "sandbox", "payment_type": "alipay",
		"priority": 1, "weight": 1, "status": "active",
	})
	if status != http.StatusCreated {
		t.Fatalf("create channel = %d %v", status, channel)
	}
	status, cashierChannel := client.json(http.MethodPost, "/api/v1/admin/channels", map[string]any{
		"name": "sandbox-cashier", "plugin_code": "sandbox", "payment_type": "cashier",
		"priority": 1, "weight": 1, "status": "active",
	})
	if status != http.StatusCreated {
		t.Fatalf("create cashier channel = %d %v", status, cashierChannel)
	}
	status, register := client.json(http.MethodPost, "/api/v1/auth/register", map[string]string{
		"identifier": "ecosystem@example.com", "password": "e2e-pass-123",
	})
	if status != http.StatusCreated {
		t.Fatalf("register = %d %v", status, register)
	}
	merchant := register["merchant"].(map[string]any)
	client.token = jsonString(t, register["token"])
	status, rotated := client.json(http.MethodPost, "/api/v1/merchant/credentials/md5/rotate", nil)
	if status != http.StatusCreated {
		t.Fatalf("rotate md5 = %d %v", status, rotated)
	}
	return &ecosystemFixture{
		client: client, application: application,
		adminToken:    jsonString(t, login["token"]),
		merchantID:    jsonString(t, merchant["id"]),
		merchantToken: jsonString(t, register["token"]),
		pid:           jsonInt(t, merchant["legacy_uid"]),
		key:           jsonString(t, rotated["secret"]),
		channelID:     jsonString(t, channel["id"]),
		cashierID:     jsonString(t, cashierChannel["id"]),
	}
}

func (fixture *ecosystemFixture) setSetting(t *testing.T, key string, value any) {
	t.Helper()
	fixture.client.token = fixture.adminToken
	status, response := fixture.client.json(http.MethodPut, "/api/v1/admin/settings/"+key, map[string]any{"value": value})
	if status != http.StatusOK {
		t.Fatalf("set setting %s = %d %v", key, status, response)
	}
}

func (fixture *ecosystemFixture) v1Order(t *testing.T, outTradeNo, notifyURL, money string) map[string]any {
	t.Helper()
	fixture.client.token = ""
	values := url.Values{
		"pid": {strconv.FormatInt(fixture.pid, 10)}, "type": {"alipay"}, "out_trade_no": {outTradeNo},
		"notify_url": {notifyURL}, "return_url": {notifyURL}, "name": {"生态测试"},
		"money": {money}, "clientip": {"127.0.0.1"}, "sign_type": {"MD5"},
	}
	values.Set("sign", signing.SignMD5(formMap(values), fixture.key))
	_, response := fixture.client.form("/mapi.php", values)
	return response
}

func (fixture *ecosystemFixture) paidOrder(t *testing.T, outTradeNo, notifyURL, money string) string {
	t.Helper()
	response := fixture.v1Order(t, outTradeNo, notifyURL, money)
	if jsonInt(t, response["code"]) != 1 {
		t.Fatalf("create order %s = %v", outTradeNo, response)
	}
	tradeNo := jsonString(t, response["trade_no"])
	status, start := fixture.client.json(http.MethodPost, "/api/v1/cashier/orders/"+tradeNo+"/start", nil)
	if status != http.StatusOK {
		t.Fatalf("start %s = %d %v", tradeNo, status, start)
	}
	status, completed := fixture.client.json(http.MethodPost, "/api/v1/sandbox/orders/"+tradeNo+"/complete", nil)
	if status != http.StatusOK || completed["status"] != "paid" {
		t.Fatalf("complete %s = %d %v", tradeNo, status, completed)
	}
	return tradeNo
}

func TestEcosystemCertificationDomainsInviteRiskAndFunding(t *testing.T) {
	fixture := newEcosystemFixture(t)
	client := fixture.client

	// 0. Plugin registry and channel test endpoint.
	client.token = fixture.adminToken
	status, plugins := client.json(http.MethodGet, "/api/v1/admin/plugins", nil)
	if status != http.StatusOK {
		t.Fatalf("plugin list = %d %v", status, plugins)
	}
	pluginItems, _ := plugins["items"].([]any)
	if len(pluginItems) < 9 {
		t.Fatalf("plugin list = %v", plugins)
	}
	status, alipayChannel := client.json(http.MethodPost, "/api/v1/admin/channels", map[string]any{
		"name": "alipay-primary", "plugin_code": "alipay", "payment_type": "alipay",
		"priority": 2, "weight": 1, "status": "disabled",
		"config": map[string]string{
			"app_id": "2026000000000000", "private_key": "-----BEGIN PRIVATE KEY-----\nTEST\n-----END PRIVATE KEY-----",
			"alipay_public_key": "-----BEGIN PUBLIC KEY-----\nTEST\n-----END PUBLIC KEY-----",
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("create alipay channel = %d %v", status, alipayChannel)
	}
	status, channelTest := client.json(http.MethodPost, "/api/v1/admin/channels/"+fixture.channelID+"/test", map[string]any{
		"payment_type": "alipay", "amount": 100,
	})
	if status != http.StatusOK || channelTest["ok"] != true {
		t.Fatalf("channel test = %d %v", status, channelTest)
	}

	// 1. Seed balance with a paid sale, then require certification.
	fixture.paidOrder(t, "ECO-CERT-SEED", "https://merchant.example/notify", "10.00")
	fixture.setSetting(t, "certification.required", true)
	fixture.setSetting(t, "certification.fee_cents", 100)
	rejected := fixture.v1Order(t, "ECO-CERT-BLOCK", "https://merchant.example/notify", "10.00")
	if jsonInt(t, rejected["code"]) != -1 || !strings.Contains(jsonString(t, rejected["msg"]), "实名认证") {
		t.Fatalf("uncertified order = %v", rejected)
	}
	client.token = fixture.merchantToken
	status, submitted := client.json(http.MethodPost, "/api/v1/merchant/certification", map[string]string{
		"cert_type": "person", "real_name": "张三", "id_no": "110101199001011234",
	})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("submit certification = %d %v", status, submitted)
	}
	client.token = fixture.adminToken
	status, certifications := client.json(http.MethodGet, "/api/v1/admin/certifications?status=pending", nil)
	if status != http.StatusOK {
		t.Fatalf("list certifications = %d %v", status, certifications)
	}
	status, reviewed := client.json(http.MethodPost, "/api/v1/admin/certifications/"+fixture.merchantID+"/review", map[string]string{"action": "approve"})
	if status != http.StatusOK || reviewed["status"] != "approved" {
		t.Fatalf("approve certification = %d %v", status, reviewed)
	}
	if trade := fixture.v1Order(t, "ECO-CERT-PASS", "https://merchant.example/notify", "10.00"); jsonInt(t, trade["code"]) != 1 {
		t.Fatalf("certified order = %v", trade)
	}

	// 2. Domain whitelist blocks an unknown host until it is approved.
	fixture.setSetting(t, "payment.domain_whitelist_required", true)
	blocked := fixture.v1Order(t, "ECO-DOMAIN-BLOCK", "https://evil.example/notify", "10.00")
	if jsonInt(t, blocked["code"]) != -1 || !strings.Contains(jsonString(t, blocked["msg"]), "域名") {
		t.Fatalf("unauthorized domain order = %v", blocked)
	}
	client.token = fixture.merchantToken
	status, domain := client.json(http.MethodPost, "/api/v1/merchant/domains", map[string]string{"domain": "merchant.example"})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("add domain = %d %v", status, domain)
	}
	client.token = fixture.adminToken
	status, domains := client.json(http.MethodGet, "/api/v1/admin/domains?status=pending", nil)
	if status != http.StatusOK {
		t.Fatalf("list domains = %d %v", status, domains)
	}
	items, _ := domains["items"].([]any)
	if len(items) == 0 {
		t.Fatalf("pending domain missing: %v", domains)
	}
	domainID := jsonString(t, items[0].(map[string]any)["id"])
	status, reviewedDomain := client.json(http.MethodPost, "/api/v1/admin/domains/"+domainID+"/review", map[string]string{"action": "approve"})
	if status != http.StatusOK || reviewedDomain["status"] != "approved" {
		t.Fatalf("approve domain = %d %v", status, reviewedDomain)
	}
	if trade := fixture.v1Order(t, "ECO-DOMAIN-PASS", "https://merchant.example/notify", "10.00"); jsonInt(t, trade["code"]) != 1 {
		t.Fatalf("authorized domain order = %v", trade)
	}
	fixture.setSetting(t, "certification.fee_cents", 0)

	// 3. Invite rebate: inviter receives 5% of the invitee's paid order.
	fixture.setSetting(t, "invite.enabled", true)
	fixture.setSetting(t, "invite.rate_bp", 500)
	client.token = fixture.adminToken
	status, inviter := client.json(http.MethodPost, "/api/v1/auth/register", map[string]string{
		"identifier": "inviter@example.com", "password": "e2e-pass-123",
	})
	if status != http.StatusCreated {
		t.Fatalf("inviter register = %d %v", status, inviter)
	}
	inviterToken := jsonString(t, inviter["token"])
	client.token = inviterToken
	status, inviteInfo := client.json(http.MethodGet, "/api/v1/merchant/invite", nil)
	if status != http.StatusOK {
		t.Fatalf("invite info = %d %v", status, inviteInfo)
	}
	inviteCode := jsonString(t, inviteInfo["invite_code"])
	if inviteCode == "" {
		t.Fatalf("invite code missing: %v", inviteInfo)
	}
	status, invitee := client.json(http.MethodPost, "/api/v1/auth/register", map[string]string{
		"identifier": "invitee@example.com", "password": "e2e-pass-123", "invite": inviteCode,
	})
	if status != http.StatusCreated {
		t.Fatalf("invitee register = %d %v", status, invitee)
	}
	inviteeMerchant := invitee["merchant"].(map[string]any)
	inviteeToken := jsonString(t, invitee["token"])
	inviteePid := jsonInt(t, inviteeMerchant["legacy_uid"])
	client.token = inviteeToken
	status, inviteeRotated := client.json(http.MethodPost, "/api/v1/merchant/credentials/md5/rotate", nil)
	if status != http.StatusCreated {
		t.Fatalf("invitee md5 = %d %v", status, inviteeRotated)
	}
	inviteeKey := jsonString(t, inviteeRotated["secret"])
	// Both certification and domain whitelist are merchant scoped: turn them off
	// for the invitee so this section only exercises rebates.
	fixture.setSetting(t, "certification.required", false)
	fixture.setSetting(t, "payment.domain_whitelist_required", false)
	client.token = ""
	values := url.Values{
		"pid": {strconv.FormatInt(inviteePid, 10)}, "type": {"alipay"}, "out_trade_no": {"ECO-INVITE-PAY"},
		"notify_url": {"https://invitee.example/notify"}, "return_url": {"https://invitee.example/return"},
		"name": {"返佣测试"}, "money": {"10.00"}, "clientip": {"127.0.0.1"}, "sign_type": {"MD5"},
	}
	values.Set("sign", signing.SignMD5(formMap(values), inviteeKey))
	_, inviteeOrder := client.form("/mapi.php", values)
	if jsonInt(t, inviteeOrder["code"]) != 1 {
		t.Fatalf("invitee order = %v", inviteeOrder)
	}
	inviteeTrade := jsonString(t, inviteeOrder["trade_no"])
	client.json(http.MethodPost, "/api/v1/cashier/orders/"+inviteeTrade+"/start", nil)
	client.json(http.MethodPost, "/api/v1/sandbox/orders/"+inviteeTrade+"/complete", nil)
	client.token = inviterToken
	status, inviteAfter := client.json(http.MethodGet, "/api/v1/merchant/invite", nil)
	if status != http.StatusOK || jsonInt(t, inviteAfter["total_income"]) != 50 || jsonInt(t, inviteAfter["invited_count"]) != 1 {
		t.Fatalf("invite stats = %d %v", status, inviteAfter)
	}
	var inviterAccount ledgermodel.Account
	if err := fixture.application.DB().Where("owner_type = ? AND owner_id = ? AND purpose = ?", "merchant", jsonString(t, inviter["merchant"].(map[string]any)["id"]), "balance").First(&inviterAccount).Error; err != nil {
		t.Fatal(err)
	}
	var inviterBalance ledgermodel.Balance
	if err := fixture.application.DB().First(&inviterBalance, "account_id = ?", inviterAccount.ID).Error; err != nil {
		t.Fatal(err)
	}
	if inviterBalance.Available != 50 {
		t.Fatalf("inviter balance = %d, want 50", inviterBalance.Available)
	}

	// 4. Risk blacklist blocks order creation and records an event.
	client.token = fixture.adminToken
	status, entry := client.json(http.MethodPost, "/api/v1/admin/blacklist", map[string]string{
		"entry_type": "ip", "value": "127.0.0.1", "remark": "e2e",
	})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("add blacklist = %d %v", status, entry)
	}
	blocked = fixture.v1Order(t, "ECO-RISK-BLOCK", "https://merchant.example/notify", "1.00")
	if jsonInt(t, blocked["code"]) != -1 {
		t.Fatalf("blacklisted order = %v", blocked)
	}
	client.token = fixture.adminToken
	status, events := client.json(http.MethodGet, "/api/v1/admin/risk-events", nil)
	if status != http.StatusOK {
		t.Fatalf("risk events = %d %v", status, events)
	}
	eventItems, _ := events["items"].([]any)
	if len(eventItems) == 0 {
		t.Fatalf("risk event missing: %v", events)
	}
	status, blacklist := client.json(http.MethodGet, "/api/v1/admin/blacklist", nil)
	if status != http.StatusOK {
		t.Fatalf("blacklist = %d %v", status, blacklist)
	}
	blacklistItems, _ := blacklist["items"].([]any)
	if len(blacklistItems) == 0 {
		t.Fatalf("blacklist entry missing: %v", blacklist)
	}
	entryID := jsonString(t, blacklistItems[0].(map[string]any)["id"])
	client.json(http.MethodDelete, "/api/v1/admin/blacklist/"+entryID, nil)

	// 5. Recharge top-up and deposit accounting.
	client.token = fixture.merchantToken
	status, recharge := client.json(http.MethodPost, "/api/v1/merchant/recharge", map[string]any{"amount": 500})
	if status != http.StatusCreated {
		t.Fatalf("recharge = %d %v", status, recharge)
	}
	rechargeTrade := jsonString(t, recharge["trade_no"])
	client.json(http.MethodPost, "/api/v1/cashier/orders/"+rechargeTrade+"/start", nil)
	status, rechargePaid := client.json(http.MethodPost, "/api/v1/sandbox/orders/"+rechargeTrade+"/complete", nil)
	if status != http.StatusOK || rechargePaid["status"] != "paid" {
		t.Fatalf("recharge pay = %d %v", status, rechargePaid)
	}
	status, depositOrder := client.json(http.MethodPost, "/api/v1/merchant/deposit", map[string]any{"amount": 1000})
	if status != http.StatusCreated {
		t.Fatalf("deposit = %d %v", status, depositOrder)
	}
	// Guard: a configured deposit minimum blocks sales until it is reached.
	fixture.setSetting(t, "merchant.deposit_required_cents", 1000)
	blockedDeposit := fixture.v1Order(t, "ECO-DEPOSIT-BLOCK", "https://merchant.example/notify", "1.00")
	if jsonInt(t, blockedDeposit["code"]) != -1 || !strings.Contains(jsonString(t, blockedDeposit["msg"]), "保证金") {
		t.Fatalf("deposit guard = %v", blockedDeposit)
	}
	depositTrade := jsonString(t, depositOrder["trade_no"])
	client.token = ""
	client.json(http.MethodPost, "/api/v1/cashier/orders/"+depositTrade+"/start", nil)
	status, depositPaid := client.json(http.MethodPost, "/api/v1/sandbox/orders/"+depositTrade+"/complete", nil)
	if status != http.StatusOK || depositPaid["status"] != "paid" {
		t.Fatalf("deposit pay = %d %v", status, depositPaid)
	}
	client.token = fixture.merchantToken
	status, depositInfo := client.json(http.MethodGet, "/api/v1/merchant/deposit", nil)
	if status != http.StatusOK || jsonInt(t, depositInfo["deposit_balance"]) != 1000 {
		t.Fatalf("deposit info = %d %v", status, depositInfo)
	}
	if allowed := fixture.v1Order(t, "ECO-DEPOSIT-PASS", "https://merchant.example/notify", "1.00"); jsonInt(t, allowed["code"]) != 1 {
		t.Fatalf("sale after deposit = %v", allowed)
	}
	// Funding orders must not enqueue merchant notifications.
	client.token = fixture.adminToken
	status, outbox := client.json(http.MethodGet, "/api/v1/admin/outbox/messages?topic=merchant.payment.notify", nil)
	if status != http.StatusOK {
		t.Fatalf("outbox = %d %v", status, outbox)
	}
}
