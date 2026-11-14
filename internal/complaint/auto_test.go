package complaint

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAutoRuleCRUDAndValidation(t *testing.T) {
	service, _ := complaintFixture(t, "complaint-auto-crud")
	ctx := context.Background()
	rule, err := service.CreateAutoRule(ctx, AutoRuleInput{Channel: "alipay", ReplyText: "已收到，会尽快处理", CloseAfterHours: 24})
	if err != nil || rule.Status != RuleActive || rule.Channel != "alipay" {
		t.Fatalf("rule=%+v err=%v", rule, err)
	}
	rules, err := service.ListAutoRules(ctx)
	if err != nil || len(rules) != 1 || rules[0].ID != rule.ID {
		t.Fatalf("rules=%+v err=%v", rules, err)
	}
	updated, err := service.UpdateAutoRule(ctx, rule.ID, AutoRuleInput{
		Channel: "alipay", ReplyText: "已处理", CloseAfterHours: 6, Status: string(RuleDisabled),
	})
	if err != nil || updated.Status != RuleDisabled || updated.CloseAfterHours != 6 || updated.ReplyText != "已处理" {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	for _, input := range []AutoRuleInput{
		{Channel: "", ReplyText: "x"},
		{Channel: "alipay", ReplyText: ""},
		{Channel: "alipay", ReplyText: "x", CloseAfterHours: -1},
		{Channel: "alipay", ReplyText: "x", Status: "paused"},
	} {
		if _, err := service.CreateAutoRule(ctx, input); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("input=%+v err=%v", input, err)
		}
	}
	if _, err := service.UpdateAutoRule(ctx, "missing", AutoRuleInput{Channel: "alipay", ReplyText: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing err=%v", err)
	}
	if err := service.DeleteAutoRule(ctx, rule.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteAutoRule(ctx, rule.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing err=%v", err)
	}
}

func TestRunAutoRulesRepliesOnlyMatchingChannels(t *testing.T) {
	service, db := complaintFixture(t, "complaint-auto-reply")
	ctx := context.Background()
	if _, err := service.CreateAutoRule(ctx, AutoRuleInput{Channel: "alipay", ReplyText: "支付宝投诉已受理", CloseAfterHours: 5}); err != nil {
		t.Fatal(err)
	}
	past := service.now().Add(-time.Hour)
	matched, err := service.Create(ctx, CreateInput{MerchantID: "merchant-1", Channel: "alipay", ComplaintNo: "A-1", Reason: "未到账", DeadlineAt: &past})
	if err != nil {
		t.Fatal(err)
	}
	unmatched, err := service.Create(ctx, CreateInput{MerchantID: "merchant-1", Channel: "wxpay", ComplaintNo: "A-2", Reason: "未到账", DeadlineAt: &past})
	if err != nil {
		t.Fatal(err)
	}
	future := service.now().Add(24 * time.Hour)
	pending, err := service.Create(ctx, CreateInput{MerchantID: "merchant-1", Channel: "alipay", ComplaintNo: "A-3", Reason: "未到期", DeadlineAt: &future})
	if err != nil {
		t.Fatal(err)
	}

	replied, closed, err := service.RunAutoRules(ctx, service.now())
	if err != nil || replied != 1 || closed != 0 {
		t.Fatalf("replied=%d closed=%d err=%v", replied, closed, err)
	}
	stored, err := service.Get(ctx, matched.ID, "")
	if err != nil || stored.Status != StatusReplied {
		t.Fatalf("matched=%+v err=%v", stored, err)
	}
	messages, err := service.Messages(ctx, matched.ID)
	if err != nil || len(messages) != 1 || messages[0].Direction != DirectionOut || messages[0].Content != "支付宝投诉已受理" {
		t.Fatalf("messages=%+v err=%v", messages, err)
	}
	if messages[0].Operator == nil || *messages[0].Operator != SystemOperator {
		t.Fatalf("operator=%v", messages[0].Operator)
	}
	untouched, err := service.Get(ctx, unmatched.ID, "")
	if err != nil || untouched.Status != StatusPending {
		t.Fatalf("unmatched=%+v err=%v", untouched, err)
	}
	stillPending, err := service.Get(ctx, pending.ID, "")
	if err != nil || stillPending.Status != StatusPending {
		t.Fatalf("future deadline=%+v err=%v", stillPending, err)
	}

	// A second run must not reply again.
	replied, closed, err = service.RunAutoRules(ctx, service.now())
	if err != nil || replied != 0 || closed != 0 {
		t.Fatalf("second run replied=%d closed=%d err=%v", replied, closed, err)
	}
	var outbound int64
	envCount := db.Model(&Message{}).Where("complaint_id = ?", matched.ID).Count(&outbound)
	if envCount.Error != nil || outbound != 1 {
		t.Fatalf("messages after replay=%d err=%v", outbound, envCount.Error)
	}
}

func TestRunAutoRulesClosesAfterTimeout(t *testing.T) {
	service, _ := complaintFixture(t, "complaint-auto-close")
	ctx := context.Background()
	if _, err := service.CreateAutoRule(ctx, AutoRuleInput{Channel: "alipay", ReplyText: "已受理", CloseAfterHours: 5}); err != nil {
		t.Fatal(err)
	}
	past := service.now().Add(-time.Hour)
	created, err := service.Create(ctx, CreateInput{MerchantID: "merchant-1", Channel: "alipay", ComplaintNo: "T-1", Reason: "超时", DeadlineAt: &past})
	if err != nil {
		t.Fatal(err)
	}
	replied, closed, err := service.RunAutoRules(ctx, service.now())
	if err != nil || replied != 1 || closed != 0 {
		t.Fatalf("first run replied=%d closed=%d err=%v", replied, closed, err)
	}
	// Five hours after the deadline the rule closes the complaint.
	replied, closed, err = service.RunAutoRules(ctx, service.now().Add(5*time.Hour))
	if err != nil || replied != 0 || closed != 1 {
		t.Fatalf("close run replied=%d closed=%d err=%v", replied, closed, err)
	}
	stored, err := service.Get(ctx, created.ID, "")
	if err != nil || stored.Status != StatusClosed || stored.ClosedAt == nil {
		t.Fatalf("closed=%+v err=%v", stored, err)
	}
	messages, err := service.Messages(ctx, created.ID)
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages=%+v err=%v", messages, err)
	}
	if messages[1].Content != AutoCloseReason || messages[1].Operator == nil || *messages[1].Operator != SystemOperator {
		t.Fatalf("close message=%+v", messages[1])
	}
	replied, closed, err = service.RunAutoRules(ctx, service.now().Add(10*time.Hour))
	if err != nil || replied != 0 || closed != 0 {
		t.Fatalf("idempotent run replied=%d closed=%d err=%v", replied, closed, err)
	}
}

func TestAutoRuleWildcardAndDisabledRules(t *testing.T) {
	service, _ := complaintFixture(t, "complaint-auto-wildcard")
	ctx := context.Background()
	past := service.now().Add(-time.Hour)
	// A disabled rule must not reply; without an active rule the wildcard is the
	// only fallback, so this first run leaves the complaint pending.
	if _, err := service.CreateAutoRule(ctx, AutoRuleInput{Channel: "wxpay", ReplyText: "不会生效", Status: string(RuleDisabled)}); err != nil {
		t.Fatal(err)
	}
	disabledChannel, err := service.Create(ctx, CreateInput{MerchantID: "merchant-1", Channel: "wxpay", ComplaintNo: "W-1", Reason: "禁用规则", DeadlineAt: &past})
	if err != nil {
		t.Fatal(err)
	}
	replied, closed, err := service.RunAutoRules(ctx, service.now())
	if err != nil || replied != 0 || closed != 0 {
		t.Fatalf("disabled-only run replied=%d closed=%d err=%v", replied, closed, err)
	}
	skipped, _ := service.Get(ctx, disabledChannel.ID, "")
	if skipped.Status != StatusPending {
		t.Fatalf("disabled rule applied: %+v", skipped)
	}

	if _, err := service.CreateAutoRule(ctx, AutoRuleInput{Channel: RuleWildcard, ReplyText: "通用回复"}); err != nil {
		t.Fatal(err)
	}
	fallback, err := service.Create(ctx, CreateInput{MerchantID: "merchant-1", Channel: "qqpay", ComplaintNo: "W-2", Reason: "无专属规则", DeadlineAt: &past})
	if err != nil {
		t.Fatal(err)
	}
	replied, closed, err = service.RunAutoRules(ctx, service.now())
	if err != nil || replied != 1 || closed != 0 {
		t.Fatalf("wildcard run replied=%d closed=%d err=%v", replied, closed, err)
	}
	matched, _ := service.Get(ctx, fallback.ID, "")
	if matched.Status != StatusReplied {
		t.Fatalf("wildcard complaint=%+v", matched)
	}
}

func TestAutoRulesOverHTTP(t *testing.T) {
	_, db := complaintFixture(t, "complaint-auto-http")
	router := complaintRouter(t, db)
	created := performComplaint(t, router, http.MethodPost, "/api/v1/admin/complaint-auto-rules",
		`{"channel":"alipay","reply_text":"已收到","close_after_hours":12}`)
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"channel":"alipay"`) {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var rule AutoRule
	if err := json.Unmarshal(created.Body.Bytes(), &rule); err != nil {
		t.Fatal(err)
	}
	listed := performComplaint(t, router, http.MethodGet, "/api/v1/admin/complaint-auto-rules", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), rule.ID) {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	updated := performComplaint(t, router, http.MethodPut, "/api/v1/admin/complaint-auto-rules/"+rule.ID,
		`{"channel":"alipay","reply_text":"已处理","close_after_hours":6,"status":"disabled"}`)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"status":"disabled"`) {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
	if response := performComplaint(t, router, http.MethodPost, "/api/v1/admin/complaint-auto-rules", `{"channel":"","reply_text":""}`); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid create status=%d", response.Code)
	}
	if response := performComplaint(t, router, http.MethodPut, "/api/v1/admin/complaint-auto-rules/missing", `{"channel":"alipay","reply_text":"x"}`); response.Code != http.StatusNotFound {
		t.Fatalf("missing update status=%d", response.Code)
	}
	if response := performComplaint(t, router, http.MethodDelete, "/api/v1/admin/complaint-auto-rules/"+rule.ID, ""); response.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d", response.Code)
	}
}
