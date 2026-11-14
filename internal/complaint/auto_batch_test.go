package complaint

import (
	"context"
	"testing"
	"time"
)

func seedOverdueComplaints(t *testing.T, service *Service, channel string, count int) {
	t.Helper()
	ctx := context.Background()
	deadline := service.now().Add(-time.Hour)
	for index := 0; index < count; index++ {
		if _, err := service.Create(ctx, CreateInput{
			MerchantID: "merchant-1", Channel: channel,
			ComplaintNo: "BULK-" + channel + "-" + padNumber(index),
			Reason:      "批量测试", DeadlineAt: &deadline,
		}); err != nil {
			t.Fatalf("seed %d: %v", index, err)
		}
	}
}

func padNumber(value int) string {
	digits := []byte("0000")
	text := ""
	number := value
	for count := 0; count < 4; count++ {
		digits[3-count] = byte('0' + number%10)
		number /= 10
	}
	return text + string(digits)
}

func countStatus(t *testing.T, service *Service, status Status) int64 {
	t.Helper()
	var total int64
	if err := service.db.Model(&Complaint{}).Where("status = ?", status).Count(&total).Error; err != nil {
		t.Fatal(err)
	}
	return total
}

// TestRunAutoRulesSkipsWithoutRules keeps the empty-rule case from scanning the
// whole overdue backlog every minute.
func TestRunAutoRulesSkipsWithoutRules(t *testing.T) {
	service, _ := complaintFixture(t, "complaint-auto-batch-none")
	seedOverdueComplaints(t, service, "wxpay", 250)
	started := time.Now()
	replied, closed, err := service.RunAutoRules(context.Background(), service.now())
	if err != nil {
		t.Fatal(err)
	}
	if replied != 0 || closed != 0 {
		t.Fatalf("replied=%d closed=%d without rules", replied, closed)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("empty run took %s, it should skip the scan", elapsed)
	}
	if pending := countStatus(t, service, StatusPending); pending != 250 {
		t.Fatalf("pending=%d want 250", pending)
	}
}

// TestRunAutoRulesRespectsBatchLimit covers the 250 overdue complaints with a
// wildcard rule: each run may handle at most maxAutoRulesBatch rows and the
// backlog drains over several runs.
func TestRunAutoRulesRespectsBatchLimit(t *testing.T) {
	service, _ := complaintFixture(t, "complaint-auto-batch-limit")
	ctx := context.Background()
	if _, err := service.CreateAutoRule(ctx, AutoRuleInput{Channel: RuleWildcard, ReplyText: "已受理", CloseAfterHours: 0}); err != nil {
		t.Fatal(err)
	}
	seedOverdueComplaints(t, service, "wxpay", 250)
	now := service.now()
	first, _, err := service.RunAutoRules(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if first != maxAutoRulesBatch {
		t.Fatalf("first run replied=%d want %d", first, maxAutoRulesBatch)
	}
	if replied := countStatus(t, service, StatusReplied); replied != int64(maxAutoRulesBatch) {
		t.Fatalf("replied rows=%d want %d", replied, maxAutoRulesBatch)
	}
	second, _, err := service.RunAutoRules(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if second != maxAutoRulesBatch {
		t.Fatalf("second run replied=%d want %d", second, maxAutoRulesBatch)
	}
	third, _, err := service.RunAutoRules(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if third != 50 {
		t.Fatalf("third run replied=%d want 50", third)
	}
	if replied := countStatus(t, service, StatusReplied); replied != 250 {
		t.Fatalf("replied rows=%d want 250", replied)
	}
	if extra, _, err := service.RunAutoRules(ctx, now); err != nil || extra != 0 {
		t.Fatalf("idle run replied=%d err=%v", extra, err)
	}
}

// TestRunAutoRulesIgnoresChannelsWithoutRules makes sure complaints from a
// channel that has no rule are not dragged into every scan.
func TestRunAutoRulesIgnoresChannelsWithoutRules(t *testing.T) {
	service, _ := complaintFixture(t, "complaint-auto-batch-channel")
	ctx := context.Background()
	if _, err := service.CreateAutoRule(ctx, AutoRuleInput{Channel: "alipay", ReplyText: "已受理"}); err != nil {
		t.Fatal(err)
	}
	seedOverdueComplaints(t, service, "wxpay", 250)
	replied, _, err := service.RunAutoRules(ctx, service.now())
	if err != nil {
		t.Fatal(err)
	}
	if replied != 0 {
		t.Fatalf("replied=%d, wxpay has no rule and must not be touched", replied)
	}
	if pending := countStatus(t, service, StatusPending); pending != 250 {
		t.Fatalf("pending=%d want 250", pending)
	}
}
