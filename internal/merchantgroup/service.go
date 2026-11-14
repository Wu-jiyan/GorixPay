package merchantgroup

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Bounds of the administrator editable fields. rate_bp is "万分比": 10000 = 100%,
// which is the highest fee an Epay style group can carry.
const (
	MinRateBp   = 0
	MaxRateBp   = 10000
	maxNameRune = 64
	maxPayTypes = 32
	maxPayType  = 32
)

// Service owns merchant groups and the merchant → group assignment.
type Service struct {
	db  *gorm.DB
	now func() time.Time
}

// New builds the service. now is injectable for tests.
func New(db *gorm.DB, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, now: now}
}

// Input is the editable part of a group.
type Input struct {
	Name       string
	RateBp     int
	PayTypes   []string
	SettleMode string
}

// Filter narrows the administrator list.
type Filter struct {
	Status  string
	Keyword string
}

// Page is the shared pagination shape (page/page_size).
type Page struct {
	Number int
	Size   int
}

// GroupView is a group plus how many merchants currently sit in it.
type GroupView struct {
	Group
	MerchantCount int64 `json:"merchant_count"`
}

// Assignment is the result of moving a merchant in or out of a group.
type Assignment struct {
	MerchantID string     `json:"merchant_id"`
	GroupID    *string    `json:"group_id"`
	SettleMode SettleMode `json:"settle_mode"`
}

// List returns one page of groups with their member counts.
func (service *Service) List(ctx context.Context, filter Filter, page Page) ([]GroupView, int64, error) {
	if page.Number < 1 {
		page.Number = 1
	}
	if page.Size < 1 || page.Size > 100 {
		page.Size = 20
	}
	query := service.db.WithContext(ctx).Model(&Group{})
	if status := strings.TrimSpace(filter.Status); status != "" {
		normalized := Status(strings.ToLower(status))
		if !ValidStatus(normalized) {
			return nil, 0, ErrInvalidRequest
		}
		query = query.Where("status = ?", normalized)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		query = query.Where("name LIKE ? ESCAPE '\\'", likePattern(keyword))
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var groups []Group
	if err := query.Order("created_at DESC, id DESC").
		Offset((page.Number - 1) * page.Size).Limit(page.Size).Find(&groups).Error; err != nil {
		return nil, 0, err
	}
	counts, err := service.countMerchants(ctx, groups)
	if err != nil {
		return nil, 0, err
	}
	views := make([]GroupView, 0, len(groups))
	for _, group := range groups {
		views = append(views, GroupView{Group: group, MerchantCount: counts[group.ID]})
	}
	return views, total, nil
}

// Create adds a group. Names are unique across groups.
func (service *Service) Create(ctx context.Context, input Input) (GroupView, error) {
	normalized, err := normalizeInput(input)
	if err != nil {
		return GroupView{}, err
	}
	var clashes int64
	if err := service.db.WithContext(ctx).Model(&Group{}).Where("name = ?", normalized.Name).Count(&clashes).Error; err != nil {
		return GroupView{}, err
	}
	if clashes > 0 {
		return GroupView{}, ErrDuplicateName
	}
	now := service.now().UTC()
	normalized.ID = uuid.NewString()
	normalized.Status = StatusActive
	normalized.CreatedAt = now
	normalized.UpdatedAt = now
	if err := service.db.WithContext(ctx).Create(&normalized).Error; err != nil {
		return GroupView{}, writeError(err)
	}
	return GroupView{Group: normalized}, nil
}

// Update replaces name, rate, pay types and settle mode. Status is changed
// separately so a fee edit cannot accidentally re-enable a retired group.
//
// Settle mode belongs to the group but is denormalised onto the merchant row, so
// changing it here keeps the members in sync; that column is what settlement
// reads when it decides whether a merchant is paid through the platform.
func (service *Service) Update(ctx context.Context, id string, input Input) (GroupView, error) {
	id = strings.TrimSpace(id)
	normalized, err := normalizeInput(input)
	if err != nil {
		return GroupView{}, err
	}
	if id == "" {
		return GroupView{}, ErrInvalidRequest
	}
	var group Group
	err = service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&group, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if group.Name != normalized.Name {
			var clashes int64
			if err := tx.Model(&Group{}).Where("name = ? AND id <> ?", normalized.Name, id).Count(&clashes).Error; err != nil {
				return err
			}
			if clashes > 0 {
				return ErrDuplicateName
			}
		}
		now := service.now().UTC()
		if err := tx.Model(&Group{}).Where("id = ?", id).Updates(map[string]any{
			"name":        normalized.Name,
			"rate_bp":     normalized.RateBp,
			"pay_types":   normalized.PayTypes,
			"settle_mode": string(normalized.SettleMode),
			"updated_at":  now,
		}).Error; err != nil {
			return writeError(err)
		}
		if group.SettleMode != normalized.SettleMode {
			if err := tx.Model(&merchantRow{}).Where("group_id = ?", id).
				Update("settle_mode", string(normalized.SettleMode)).Error; err != nil {
				return err
			}
		}
		group.Name = normalized.Name
		group.RateBp = normalized.RateBp
		group.PayTypes = normalized.PayTypes
		group.SettleMode = normalized.SettleMode
		group.UpdatedAt = now
		return nil
	})
	if err != nil {
		return GroupView{}, err
	}
	counts, err := service.countMerchants(ctx, []Group{group})
	if err != nil {
		return GroupView{}, err
	}
	return GroupView{Group: group, MerchantCount: counts[group.ID]}, nil
}

// SetStatus enables or disables a group.
func (service *Service) SetStatus(ctx context.Context, id, status string) (GroupView, error) {
	id = strings.TrimSpace(id)
	next := Status(strings.ToLower(strings.TrimSpace(status)))
	if id == "" || !ValidStatus(next) {
		return GroupView{}, ErrInvalidRequest
	}
	result := service.db.WithContext(ctx).Model(&Group{}).Where("id = ?", id).
		Updates(map[string]any{"status": next, "updated_at": service.now().UTC()})
	if result.Error != nil {
		return GroupView{}, result.Error
	}
	var group Group
	if err := service.db.WithContext(ctx).First(&group, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return GroupView{}, ErrNotFound
		}
		return GroupView{}, err
	}
	counts, err := service.countMerchants(ctx, []Group{group})
	if err != nil {
		return GroupView{}, err
	}
	return GroupView{Group: group, MerchantCount: counts[group.ID]}, nil
}

// Delete removes an empty group. A group that still has merchants is a conflict,
// because deleting it would silently move those merchants to the default fee.
func (service *Service) Delete(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrInvalidRequest
	}
	return service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var group Group
		if err := tx.First(&group, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		var references int64
		if err := tx.Model(&merchantRow{}).Where("group_id = ?", id).Count(&references).Error; err != nil {
			return err
		}
		if references > 0 {
			return ErrGroupInUse
		}
		return tx.Delete(&Group{}, "id = ?", id).Error
	})
}

// AssignMerchant moves a merchant into a group, or out of every group when
// groupID is nil. The group settle mode is copied onto the merchant row.
//
// A disabled group cannot take new members: it is retired, and silently handing a
// merchant a retired fee would hide the mistake. Merchants already in it keep
// their rate until they are moved out.
func (service *Service) AssignMerchant(ctx context.Context, merchantID string, groupID *string) (Assignment, error) {
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return Assignment{}, ErrInvalidRequest
	}
	var target *string
	if groupID != nil {
		if trimmed := strings.TrimSpace(*groupID); trimmed != "" {
			target = &trimmed
		}
	}
	assignment := Assignment{MerchantID: merchantID, GroupID: target, SettleMode: SettleModeNormal}
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var merchant merchantRow
		if err := tx.First(&merchant, "id = ?", merchantID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrMerchantNotFound
			}
			return err
		}
		if target != nil {
			var group Group
			if err := tx.First(&group, "id = ?", *target).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrNotFound
				}
				return err
			}
			if group.Status != StatusActive {
				return ErrGroupDisabled
			}
			assignment.SettleMode = group.SettleMode
		}
		return tx.Model(&merchantRow{}).Where("id = ?", merchantID).Updates(map[string]any{
			"group_id":    target,
			"settle_mode": string(assignment.SettleMode),
		}).Error
	})
	return assignment, err
}

// RateBp returns the fee rate (in basis points of 10000) of the group the
// merchant belongs to. It is the only entry point other modules may use: the
// settlement module asks for the rate, everything else stays inside this package.
//
// An ungrouped merchant, an unknown merchant and a merchant pointing at a deleted
// group all report (0, false), which the caller renders as "no group rate".
func (service *Service) RateBp(ctx context.Context, merchantID string) (int, bool) {
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return 0, false
	}
	var rateBp int
	row := service.db.WithContext(ctx).Raw(
		"SELECT g.rate_bp FROM merchants m JOIN merchant_groups g ON g.id = m.group_id WHERE m.id = ?", merchantID).Row()
	if err := row.Scan(&rateBp); err != nil {
		return 0, false
	}
	return rateBp, true
}

// countMerchants counts the members of the given groups in one round trip.
func (service *Service) countMerchants(ctx context.Context, groups []Group) (map[string]int64, error) {
	counts := make(map[string]int64, len(groups))
	if len(groups) == 0 {
		return counts, nil
	}
	ids := make([]string, 0, len(groups))
	for _, group := range groups {
		ids = append(ids, group.ID)
	}
	type row struct {
		GroupID string `gorm:"column:group_id"`
		Total   int64  `gorm:"column:total"`
	}
	var rows []row
	if err := service.db.WithContext(ctx).Table("merchants").
		Select("group_id, COUNT(*) AS total").
		Where("group_id IN ?", ids).Group("group_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, entry := range rows {
		counts[entry.GroupID] = entry.Total
	}
	return counts, nil
}

// normalizeInput validates and canonicalises the administrator input.
func normalizeInput(input Input) (Group, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || utf8.RuneCountInString(name) > maxNameRune {
		return Group{}, ErrInvalidRequest
	}
	if input.RateBp < MinRateBp || input.RateBp > MaxRateBp {
		return Group{}, ErrInvalidRequest
	}
	settleMode := SettleMode(strings.ToLower(strings.TrimSpace(input.SettleMode)))
	if settleMode == "" {
		settleMode = SettleModeNormal
	}
	if !ValidSettleMode(settleMode) {
		return Group{}, ErrInvalidRequest
	}
	payTypes, err := normalizePayTypes(input.PayTypes)
	if err != nil {
		return Group{}, err
	}
	return Group{Name: name, RateBp: input.RateBp, PayTypes: payTypes, SettleMode: settleMode}, nil
}

func normalizePayTypes(values []string) (PayTypes, error) {
	seen := make(map[string]bool, len(values))
	result := make(PayTypes, 0, len(values))
	for _, value := range values {
		trimmed := strings.ToLower(strings.TrimSpace(value))
		if trimmed == "" || seen[trimmed] {
			continue
		}
		if utf8.RuneCountInString(trimmed) > maxPayType {
			return nil, ErrInvalidRequest
		}
		seen[trimmed] = true
		result = append(result, trimmed)
	}
	if len(result) > maxPayTypes {
		return nil, ErrInvalidRequest
	}
	return result, nil
}

// likePattern escapes the LIKE wildcards so a keyword is matched literally.
func likePattern(keyword string) string {
	replacer := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)
	return "%" + replacer.Replace(keyword) + "%"
}

// writeError turns a driver unique violation into the domain error, so the HTTP
// layer can answer 409 instead of 500 for a race that slipped past the pre-check.
func writeError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "unique") || strings.Contains(message, "duplicate") {
		return ErrDuplicateName
	}
	return err
}
