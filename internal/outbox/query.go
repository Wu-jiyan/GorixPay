package outbox

import (
	"context"

	"gorm.io/gorm"
)

type QueryService struct {
	db *gorm.DB
}

func NewQuery(db *gorm.DB) *QueryService {
	return &QueryService{db: db}
}

type Page struct {
	Number int
	Size   int
}

type Stats struct {
	Pending    int64 `json:"pending"`
	Processing int64 `json:"processing"`
	Completed  int64 `json:"completed"`
	Dead       int64 `json:"dead"`
	Total      int64 `json:"total"`
}

func (qs *QueryService) Messages(ctx context.Context, status string, page Page) ([]Message, int64, error) {
	query := qs.db.WithContext(ctx).Model(&Message{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []Message
	if err := query.Order("created_at DESC").Offset((page.Number - 1) * page.Size).Limit(page.Size).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (qs *QueryService) Stats(ctx context.Context) (Stats, error) {
	var stats Stats
	type row struct {
		Status string
		Count  int64
	}
	var rows []row
	if err := qs.db.WithContext(ctx).Model(&Message{}).Select("status, COUNT(*) as count").Group("status").Scan(&rows).Error; err != nil {
		return stats, err
	}
	for _, r := range rows {
		switch Status(r.Status) {
		case StatusPending:
			stats.Pending = r.Count
		case StatusProcessing:
			stats.Processing = r.Count
		case StatusCompleted:
			stats.Completed = r.Count
		case StatusDead:
			stats.Dead = r.Count
		}
	}
	stats.Total = stats.Pending + stats.Processing + stats.Completed + stats.Dead
	return stats, nil
}

func (qs *QueryService) RequeueDead(ctx context.Context) (int64, error) {
	result := qs.db.WithContext(ctx).Model(&Message{}).Where("status = ?", StatusDead).Updates(map[string]any{
		"status":     StatusPending,
		"updated_at": qs.db.NowFunc(),
	})
	return result.RowsAffected, result.Error
}
