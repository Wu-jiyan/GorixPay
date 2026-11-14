package complaint

import (
	"context"
	"errors"
	"testing"
	"time"

	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"

	"gorm.io/gorm"
)

func TestCreateDuplicateReturnsConflict(t *testing.T) {
	service, _ := complaintFixture(t, "complaint-duplicate")
	input := CreateInput{MerchantID: "merchant-1", Channel: "alipay", ComplaintNo: "C-1", Reason: "买家投诉未到账", AmountCents: 1000}
	first, err := service.Create(context.Background(), input)
	if err != nil || first.Status != StatusPending {
		t.Fatalf("create=%+v err=%v", first, err)
	}
	if _, err := service.Create(context.Background(), input); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate err=%v", err)
	}
	// Same complaint number on another channel is a different complaint.
	other := input
	other.Channel = "wechat"
	if _, err := service.Create(context.Background(), other); err != nil {
		t.Fatalf("other channel err=%v", err)
	}
}

func TestReplyMovesPendingToRepliedAndCloseBlocksFurtherReplies(t *testing.T) {
	service, db := complaintFixture(t, "complaint-flow")
	created, err := service.Create(context.Background(), CreateInput{MerchantID: "merchant-1", Channel: "alipay", ComplaintNo: "C-2", Reason: "未收到货", AmountCents: 500})
	if err != nil {
		t.Fatal(err)
	}
	replied, err := service.Reply(context.Background(), created.ID, "已联系商户处理", "admin-1")
	if err != nil || replied.Status != StatusReplied {
		t.Fatalf("reply=%+v err=%v", replied, err)
	}
	messages, err := service.Messages(context.Background(), created.ID)
	if err != nil || len(messages) != 1 || messages[0].Direction != DirectionOut || messages[0].Content != "已联系商户处理" {
		t.Fatalf("messages=%+v err=%v", messages, err)
	}
	if messages[0].Operator == nil || *messages[0].Operator != "admin-1" {
		t.Fatalf("operator=%v", messages[0].Operator)
	}
	closed, err := service.Close(context.Background(), created.ID, "已处理")
	if err != nil || closed.Status != StatusClosed || closed.ClosedAt == nil {
		t.Fatalf("closed=%+v err=%v", closed, err)
	}
	if _, err := service.Reply(context.Background(), created.ID, "再来一条", "admin-1"); !errors.Is(err, ErrClosed) {
		t.Fatalf("reply after close err=%v", err)
	}
	again, err := service.Close(context.Background(), created.ID, "重复关闭")
	if err != nil || again.Status != StatusClosed {
		t.Fatalf("close again=%+v err=%v", again, err)
	}
	var count int64
	if err := db.Model(&Message{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("messages in db=%d err=%v", count, err)
	}
}

func TestOverdueIsDerivedOnlyForPending(t *testing.T) {
	service, _ := complaintFixture(t, "complaint-overdue")
	past := service.now().Add(-time.Hour)
	overdue, err := service.Create(context.Background(), CreateInput{
		MerchantID: "merchant-1", Channel: "alipay", ComplaintNo: "C-3", Reason: "超时投诉", DeadlineAt: &past,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := ViewStatus(overdue, service.now()); got != StatusOverdue {
		t.Fatalf("derived status=%q", got)
	}
	listed, total, err := service.List(context.Background(), Filter{Status: string(StatusOverdue)}, Page{Number: 1, Size: 10})
	if err != nil || total != 1 || len(listed) != 1 || listed[0].ID != overdue.ID {
		t.Fatalf("overdue list total=%d listed=%+v err=%v", total, listed, err)
	}
	replied, err := service.Reply(context.Background(), overdue.ID, "正在处理", "admin-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := ViewStatus(replied, service.now()); got != StatusReplied {
		t.Fatalf("replied derived status=%q", got)
	}
	closed, err := service.Close(context.Background(), overdue.ID, "处理完成")
	if err != nil {
		t.Fatal(err)
	}
	if got := ViewStatus(closed, service.now()); got != StatusClosed {
		t.Fatalf("closed derived status=%q", got)
	}
	future := service.now().Add(48 * time.Hour)
	pending, err := service.Create(context.Background(), CreateInput{
		MerchantID: "merchant-1", Channel: "alipay", ComplaintNo: "C-4", Reason: "未到期", DeadlineAt: &future,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := ViewStatus(pending, service.now()); got != StatusPending {
		t.Fatalf("pending derived status=%q", got)
	}
}

func TestMerchantIsolation(t *testing.T) {
	service, _ := complaintFixture(t, "complaint-isolation")
	owned, err := service.Create(context.Background(), CreateInput{MerchantID: "merchant-1", Channel: "alipay", ComplaintNo: "C-5", Reason: "only mine"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(context.Background(), owned.ID, "merchant-2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross merchant get err=%v", err)
	}
	if _, err := service.Get(context.Background(), owned.ID, "merchant-1"); err != nil {
		t.Fatalf("owner get err=%v", err)
	}
	other, err := service.Create(context.Background(), CreateInput{MerchantID: "merchant-2", Channel: "wxpay", ComplaintNo: "C-6", Reason: "other merchant"})
	if err != nil {
		t.Fatal(err)
	}
	items, total, err := service.List(context.Background(), Filter{MerchantID: "merchant-1"}, Page{Number: 1, Size: 10})
	if err != nil || total != 1 || items[0].ID == other.ID {
		t.Fatalf("merchant list total=%d items=%+v err=%v", total, items, err)
	}
}

func TestPaginationAndFilters(t *testing.T) {
	service, _ := complaintFixture(t, "complaint-filters")
	seeds := []CreateInput{
		{MerchantID: "merchant-1", Channel: "alipay", ComplaintNo: "F-1", Reason: "r1"},
		{MerchantID: "merchant-1", Channel: "alipay", ComplaintNo: "F-2", Reason: "r2"},
		{MerchantID: "merchant-1", Channel: "wxpay", ComplaintNo: "F-3", Reason: "r3"},
		{MerchantID: "merchant-2", Channel: "alipay", ComplaintNo: "F-4", Reason: "r4"},
		{MerchantID: "merchant-2", Channel: "wxpay", ComplaintNo: "F-5", Reason: "r5"},
	}
	var ids []string
	for _, seed := range seeds {
		created, err := service.Create(context.Background(), seed)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, created.ID)
	}
	if _, err := service.Reply(context.Background(), ids[0], "收到", "admin-1"); err != nil {
		t.Fatal(err)
	}

	page, total, err := service.List(context.Background(), Filter{}, Page{Number: 1, Size: 2})
	if err != nil || total != 5 || len(page) != 2 {
		t.Fatalf("page total=%d len=%d err=%v", total, len(page), err)
	}
	page, total, err = service.List(context.Background(), Filter{}, Page{Number: 3, Size: 2})
	if err != nil || total != 5 || len(page) != 1 {
		t.Fatalf("last page total=%d len=%d err=%v", total, len(page), err)
	}
	_, total, err = service.List(context.Background(), Filter{Channel: "wxpay"}, Page{Number: 1, Size: 10})
	if err != nil || total != 2 {
		t.Fatalf("channel filter total=%d err=%v", total, err)
	}
	_, total, err = service.List(context.Background(), Filter{Status: string(StatusReplied)}, Page{Number: 1, Size: 10})
	if err != nil || total != 1 {
		t.Fatalf("status filter total=%d err=%v", total, err)
	}
	_, total, err = service.List(context.Background(), Filter{MerchantID: "merchant-2", Channel: "alipay"}, Page{Number: 1, Size: 10})
	if err != nil || total != 1 {
		t.Fatalf("merchant+channel filter total=%d err=%v", total, err)
	}
	if _, _, err := service.List(context.Background(), Filter{Status: "unknown"}, Page{Number: 1, Size: 10}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid status err=%v", err)
	}
}

// pullFake proves the Puller hook shape without shipping a production puller.
type pullFake struct{ rows []Complaint }

func (fake pullFake) Pull(context.Context, time.Time) ([]Complaint, error) { return fake.rows, nil }

func TestPullerInterfaceShape(t *testing.T) {
	var puller Puller = pullFake{}
	rows, err := puller.Pull(context.Background(), time.Now())
	if err != nil || len(rows) != 0 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}

type complaintEnv struct {
	db      *gorm.DB
	service *Service
}

func complaintFixture(t *testing.T, name string) (*Service, *gorm.DB) {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Complaint{}, &Message{}, &AutoRule{}, &identitymodel.Merchant{}); err != nil {
		t.Fatal(err)
	}
	for _, merchant := range []identitymodel.Merchant{
		{ID: "merchant-1", DisplayName: "merchant one", Status: identitymodel.StatusActive},
		{ID: "merchant-2", DisplayName: "merchant two", Status: identitymodel.StatusActive},
	} {
		if err := db.Create(&merchant).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	return New(db, func() time.Time { return now }), db
}
