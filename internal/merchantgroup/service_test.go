package merchantgroup

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCreateValidatesInput(t *testing.T) {
	service, _ := groupFixture(t, "group-validate")
	ctx := context.Background()

	cases := []struct {
		name  string
		input Input
		want  error
	}{
		{name: "empty name", input: Input{Name: "  "}, want: ErrInvalidRequest},
		{name: "name too long", input: Input{Name: strings.Repeat("组", 65)}, want: ErrInvalidRequest},
		{name: "negative rate", input: Input{Name: "g", RateBp: -1}, want: ErrInvalidRequest},
		{name: "rate over 100%", input: Input{Name: "g", RateBp: 10001}, want: ErrInvalidRequest},
		{name: "unknown settle mode", input: Input{Name: "g", SettleMode: "weekly"}, want: ErrInvalidRequest},
	}
	for _, testCase := range cases {
		if _, err := service.Create(ctx, testCase.input); !errors.Is(err, testCase.want) {
			t.Fatalf("%s: err = %v, want %v", testCase.name, err, testCase.want)
		}
	}

	created, err := service.Create(ctx, Input{Name: "  标准组  ", RateBp: 60, PayTypes: []string{"Alipay", " alipay ", "wxpay", ""}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Name != "标准组" || created.RateBp != 60 {
		t.Fatalf("created = %+v", created.Group)
	}
	if created.Status != StatusActive || created.SettleMode != SettleModeNormal {
		t.Fatalf("defaults = status %q settle %q, want active/normal", created.Status, created.SettleMode)
	}
	if len(created.PayTypes) != 2 || created.PayTypes[0] != "alipay" || created.PayTypes[1] != "wxpay" {
		t.Fatalf("pay types = %v, want deduplicated lowercase list", created.PayTypes)
	}
	if !created.CreatedAt.Equal(fixedNow) || !created.UpdatedAt.Equal(fixedNow) {
		t.Fatalf("timestamps = %v / %v", created.CreatedAt, created.UpdatedAt)
	}

	if _, err := service.Create(ctx, Input{Name: "标准组"}); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("duplicate name err = %v", err)
	}
	if _, err := service.Create(ctx, Input{Name: "标准组2", RateBp: MaxRateBp}); err != nil {
		t.Fatalf("100%% rate should be accepted: %v", err)
	}
}

func TestUpdateValidatesAndPropagatesSettleMode(t *testing.T) {
	service, db := groupFixture(t, "group-update")
	ctx := context.Background()
	seedMerchant(t, db, "merchant-1")

	group, err := service.Create(ctx, Input{Name: "直清组", RateBp: 25, SettleMode: "direct"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := service.AssignMerchant(ctx, "merchant-1", &group.ID); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if mode := merchantSettleMode(t, db, "merchant-1"); mode != string(SettleModeDirect) {
		t.Fatalf("merchant settle_mode = %q after assigning a direct group", mode)
	}

	updated, err := service.Update(ctx, group.ID, Input{Name: "直清组", RateBp: 30, SettleMode: "normal", PayTypes: []string{"wxpay"}})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.RateBp != 30 || updated.SettleMode != SettleModeNormal || len(updated.PayTypes) != 1 {
		t.Fatalf("updated = %+v", updated.Group)
	}
	if mode := merchantSettleMode(t, db, "merchant-1"); mode != string(SettleModeNormal) {
		t.Fatalf("merchant settle_mode = %q, want the group change to propagate", mode)
	}
	if rate, ok := service.RateBp(ctx, "merchant-1"); !ok || rate != 30 {
		t.Fatalf("RateBp after update = (%d,%v), want (30,true)", rate, ok)
	}

	if _, err := service.Update(ctx, group.ID, Input{Name: "标准组", RateBp: 30}); err != nil {
		t.Fatalf("rename: %v", err)
	}
	other, err := service.Create(ctx, Input{Name: "另一个组", RateBp: 10})
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	if _, err := service.Update(ctx, group.ID, Input{Name: other.Name, RateBp: 30}); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("rename onto an existing name err = %v", err)
	}
	if _, err := service.Update(ctx, "missing", Input{Name: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing err = %v", err)
	}
	if _, err := service.Update(ctx, group.ID, Input{Name: "x", RateBp: 20000}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid rate err = %v", err)
	}
}

func TestAssignMerchantAndRateBp(t *testing.T) {
	service, db := groupFixture(t, "group-assign")
	ctx := context.Background()
	seedMerchant(t, db, "merchant-1")

	if rate, ok := service.RateBp(ctx, "merchant-1"); ok || rate != 0 {
		t.Fatalf("ungrouped RateBp = (%d,%v), want (0,false)", rate, ok)
	}
	if rate, ok := service.RateBp(ctx, "missing"); ok || rate != 0 {
		t.Fatalf("unknown merchant RateBp = (%d,%v), want (0,false)", rate, ok)
	}

	group, err := service.Create(ctx, Input{Name: "VIP", RateBp: 35})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	assigned, err := service.AssignMerchant(ctx, "merchant-1", &group.ID)
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	if assigned.GroupID == nil || *assigned.GroupID != group.ID || assigned.SettleMode != SettleModeNormal {
		t.Fatalf("assignment = %+v", assigned)
	}
	// The 万分比 rate is what settlement charges: 35 bp of 10000.
	if rate, ok := service.RateBp(ctx, "merchant-1"); !ok || rate != 35 {
		t.Fatalf("RateBp = (%d,%v), want (35,true)", rate, ok)
	}

	empty := "   "
	if _, err := service.AssignMerchant(ctx, "merchant-1", &empty); err != nil {
		t.Fatalf("assign with blank id: %v", err)
	}
	if id := merchantGroupID(t, db, "merchant-1"); id != nil {
		t.Fatalf("blank group id should remove the assignment, got %q", *id)
	}
	if rate, ok := service.RateBp(ctx, "merchant-1"); ok {
		t.Fatalf("removed merchant RateBp = (%d,%v), want false", rate, ok)
	}

	if _, err := service.AssignMerchant(ctx, "merchant-2", &group.ID); !errors.Is(err, ErrMerchantNotFound) {
		t.Fatalf("unknown merchant err = %v", err)
	}
	unknown := "no-such-group"
	if _, err := service.AssignMerchant(ctx, "merchant-1", &unknown); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown group err = %v", err)
	}
	if _, err := service.AssignMerchant(ctx, "", &group.ID); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty merchant err = %v", err)
	}
}

func TestDeleteRejectsGroupsWithMerchants(t *testing.T) {
	service, db := groupFixture(t, "group-delete")
	ctx := context.Background()
	seedMerchant(t, db, "merchant-1")

	group, err := service.Create(ctx, Input{Name: "待删组"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := service.AssignMerchant(ctx, "merchant-1", &group.ID); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if err := service.Delete(ctx, group.ID); !errors.Is(err, ErrGroupInUse) {
		t.Fatalf("delete with members err = %v, want ErrGroupInUse", err)
	}
	if _, err := service.AssignMerchant(ctx, "merchant-1", nil); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	if err := service.Delete(ctx, group.ID); err != nil {
		t.Fatalf("delete empty group: %v", err)
	}
	if err := service.Delete(ctx, group.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete err = %v", err)
	}
}

func TestDisabledGroupRejectsNewMembers(t *testing.T) {
	service, db := groupFixture(t, "group-disabled")
	ctx := context.Background()
	seedMerchant(t, db, "merchant-1")
	seedMerchant(t, db, "merchant-2")

	group, err := service.Create(ctx, Input{Name: "已停用组", RateBp: 70})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := service.AssignMerchant(ctx, "merchant-1", &group.ID); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if _, err := service.SetStatus(ctx, group.ID, "disabled"); err != nil {
		t.Fatalf("disable: %v", err)
	}

	if _, err := service.AssignMerchant(ctx, "merchant-2", &group.ID); !errors.Is(err, ErrGroupDisabled) {
		t.Fatalf("assign to disabled group err = %v, want ErrGroupDisabled", err)
	}
	// The existing member keeps its rate until somebody moves it out.
	if rate, ok := service.RateBp(ctx, "merchant-1"); !ok || rate != 70 {
		t.Fatalf("RateBp of an existing member = (%d,%v)", rate, ok)
	}
	if _, err := service.AssignMerchant(ctx, "merchant-1", nil); err != nil {
		t.Fatalf("unassign from disabled group: %v", err)
	}
	if err := service.Delete(ctx, group.ID); err != nil {
		t.Fatalf("delete retired group: %v", err)
	}
}

func TestStatusAndListFilters(t *testing.T) {
	service, db := groupFixture(t, "group-list")
	ctx := context.Background()
	seedMerchant(t, db, "merchant-1")
	seedMerchant(t, db, "merchant-2")

	standard, err := service.Create(ctx, Input{Name: "标准组", RateBp: 60})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	vip, err := service.Create(ctx, Input{Name: "VIP 商户", RateBp: 30})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := service.AssignMerchant(ctx, "merchant-1", &vip.ID); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if _, err := service.AssignMerchant(ctx, "merchant-2", &vip.ID); err != nil {
		t.Fatalf("assign: %v", err)
	}

	views, total, err := service.List(ctx, Filter{}, Page{Number: 1, Size: 20})
	if err != nil || total != 2 {
		t.Fatalf("list: total=%d err=%v", total, err)
	}
	counts := map[string]int64{}
	for _, view := range views {
		counts[view.Name] = view.MerchantCount
	}
	if counts["VIP 商户"] != 2 || counts["标准组"] != 0 {
		t.Fatalf("merchant counts = %v", counts)
	}

	disabled, err := service.SetStatus(ctx, standard.ID, "disabled")
	if err != nil {
		t.Fatalf("set status: %v", err)
	}
	if disabled.Status != StatusDisabled {
		t.Fatalf("status = %q, want disabled", disabled.Status)
	}
	if _, err := service.SetStatus(ctx, standard.ID, "paused"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid status err = %v", err)
	}
	if _, err := service.SetStatus(ctx, "missing", "active"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing group status err = %v", err)
	}

	views, total, err = service.List(ctx, Filter{Status: "disabled"}, Page{Number: 1, Size: 20})
	if err != nil || total != 1 || views[0].ID != standard.ID {
		t.Fatalf("disabled filter: total=%d err=%v", total, err)
	}
	views, total, err = service.List(ctx, Filter{Keyword: "vip"}, Page{Number: 1, Size: 20})
	if err != nil || total != 1 || views[0].ID != vip.ID {
		t.Fatalf("keyword filter: total=%d err=%v", total, err)
	}
	views, total, err = service.List(ctx, Filter{Keyword: "%"}, Page{Number: 1, Size: 20})
	if err != nil || total != 0 {
		t.Fatalf("literal keyword filter: total=%d err=%v", total, err)
	}
	if _, _, err := service.List(ctx, Filter{Status: "weird"}, Page{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid status filter err = %v", err)
	}
	if _, _, err := service.List(ctx, Filter{}, Page{Number: 1, Size: 500}); err != nil {
		t.Fatalf("page size should be clamped, not rejected: %v", err)
	}

	// A disabled group keeps paying its own rate: settlement must not silently
	// fall back to the default fee when a group is merely retired.
	if rate, ok := service.RateBp(ctx, "merchant-1"); !ok || rate != 30 {
		t.Fatalf("RateBp on disabled-member group = (%d,%v)", rate, ok)
	}
}
