package complaint

import (
	"context"
	"errors"
	"strings"
	"time"

	identitymodel "gorixpay/internal/identity/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const maxMessageLength = 4000

type Service struct {
	db  *gorm.DB
	now func() time.Time
}

func New(db *gorm.DB, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, now: now}
}

type CreateInput struct {
	MerchantID  string
	OrderID     string
	Channel     string
	ComplaintNo string
	Reason      string
	AmountCents int64
	DeadlineAt  *time.Time
}

type Filter struct {
	MerchantID string
	Channel    string
	Status     string
}

type Page struct {
	Number int
	Size   int
}

// Create registers a complaint manually. The (channel, complaint_no) pair is the
// business key, so a second registration returns ErrDuplicate.
func (service *Service) Create(ctx context.Context, input CreateInput) (Complaint, error) {
	input.MerchantID = strings.TrimSpace(input.MerchantID)
	input.OrderID = strings.TrimSpace(input.OrderID)
	input.Channel = strings.ToLower(strings.TrimSpace(input.Channel))
	input.ComplaintNo = strings.TrimSpace(input.ComplaintNo)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.MerchantID == "" || input.Channel == "" || len(input.Channel) > 20 ||
		input.ComplaintNo == "" || len(input.ComplaintNo) > 64 ||
		input.Reason == "" || len(input.Reason) > 255 || input.AmountCents < 0 {
		return Complaint{}, ErrInvalidRequest
	}
	var merchant identitymodel.Merchant
	if err := service.db.WithContext(ctx).First(&merchant, "id = ?", input.MerchantID).Error; err != nil {
		return Complaint{}, ErrMerchant
	}
	var existing Complaint
	err := service.db.WithContext(ctx).Where("channel = ? AND complaint_no = ?", input.Channel, input.ComplaintNo).First(&existing).Error
	if err == nil {
		return Complaint{}, ErrDuplicate
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return Complaint{}, err
	}
	now := service.now().UTC()
	complaint := Complaint{
		ID:          uuid.NewString(),
		MerchantID:  input.MerchantID,
		OrderID:     optional(input.OrderID),
		Channel:     input.Channel,
		ComplaintNo: input.ComplaintNo,
		Status:      StatusPending,
		Reason:      input.Reason,
		AmountCents: input.AmountCents,
		DeadlineAt:  optionalTime(input.DeadlineAt),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := service.db.WithContext(ctx).Create(&complaint).Error; err != nil {
		var duplicate Complaint
		if lookupErr := service.db.WithContext(ctx).Where("channel = ? AND complaint_no = ?", input.Channel, input.ComplaintNo).First(&duplicate).Error; lookupErr == nil {
			return Complaint{}, ErrDuplicate
		}
		return Complaint{}, err
	}
	return complaint, nil
}

// List returns complaints for the admin console or one merchant.
func (service *Service) List(ctx context.Context, filter Filter, page Page) ([]Complaint, int64, error) {
	if page.Number < 1 {
		page.Number = 1
	}
	if page.Size < 1 || page.Size > 100 {
		page.Size = 20
	}
	query := service.db.WithContext(ctx).Model(&Complaint{})
	if merchantID := strings.TrimSpace(filter.MerchantID); merchantID != "" {
		query = query.Where("merchant_id = ?", merchantID)
	}
	if channel := strings.ToLower(strings.TrimSpace(filter.Channel)); channel != "" {
		query = query.Where("channel = ?", channel)
	}
	switch status := strings.ToLower(strings.TrimSpace(filter.Status)); status {
	case "":
	case string(StatusOverdue):
		query = query.Where("status = ? AND deadline_at IS NOT NULL AND deadline_at < ?", StatusPending, service.now().UTC())
	case string(StatusPending), string(StatusReplied), string(StatusClosed):
		query = query.Where("status = ?", status)
	default:
		return nil, 0, ErrInvalidRequest
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []Complaint
	err := query.Order("created_at DESC").Offset((page.Number - 1) * page.Size).Limit(page.Size).Find(&items).Error
	return items, total, err
}

// Get returns one complaint. An empty merchantID means admin access; otherwise
// the complaint must belong to that merchant.
func (service *Service) Get(ctx context.Context, id, merchantID string) (Complaint, error) {
	var complaint Complaint
	query := service.db.WithContext(ctx).Where("id = ?", strings.TrimSpace(id))
	if merchantID = strings.TrimSpace(merchantID); merchantID != "" {
		query = query.Where("merchant_id = ?", merchantID)
	}
	if err := query.First(&complaint).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Complaint{}, ErrNotFound
		}
		return Complaint{}, err
	}
	return complaint, nil
}

// Messages lists the conversation of one complaint in chronological order.
func (service *Service) Messages(ctx context.Context, complaintID string) ([]Message, error) {
	var messages []Message
	err := service.db.WithContext(ctx).Where("complaint_id = ?", strings.TrimSpace(complaintID)).Order("created_at ASC, id ASC").Find(&messages).Error
	return messages, err
}

// Reply appends an outbound message and moves a pending complaint to replied.
func (service *Service) Reply(ctx context.Context, id, content, operator string) (Complaint, error) {
	complaint, err := service.Get(ctx, id, "")
	if err != nil {
		return Complaint{}, err
	}
	if complaint.Status == StatusClosed {
		return Complaint{}, ErrClosed
	}
	content = strings.TrimSpace(content)
	operator = strings.TrimSpace(operator)
	if content == "" || len(content) > maxMessageLength || operator == "" {
		return Complaint{}, ErrInvalidRequest
	}
	now := service.now().UTC()
	message := Message{ID: uuid.NewString(), ComplaintID: complaint.ID, Direction: DirectionOut, Content: content, Operator: optional(operator), CreatedAt: now}
	err = service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&message).Error; err != nil {
			return err
		}
		updates := map[string]any{"updated_at": now}
		if complaint.Status == StatusPending {
			updates["status"] = StatusReplied
		}
		return tx.Model(&Complaint{}).Where("id = ? AND status <> ?", complaint.ID, StatusClosed).Updates(updates).Error
	})
	if err != nil {
		return Complaint{}, err
	}
	return service.Get(ctx, complaint.ID, "")
}

// Close closes a complaint. Closing twice returns the already closed row.
func (service *Service) Close(ctx context.Context, id, reason string) (Complaint, error) {
	complaint, err := service.Get(ctx, id, "")
	if err != nil {
		return Complaint{}, err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 255 {
		return Complaint{}, ErrInvalidRequest
	}
	if complaint.Status == StatusClosed {
		return complaint, nil
	}
	now := service.now().UTC()
	if err := service.db.WithContext(ctx).Model(&Complaint{}).
		Where("id = ? AND status <> ?", complaint.ID, StatusClosed).
		Updates(map[string]any{"status": StatusClosed, "closed_at": now, "updated_at": now}).Error; err != nil {
		return Complaint{}, err
	}
	return service.Get(ctx, complaint.ID, "")
}

func optional(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func optionalTime(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}
