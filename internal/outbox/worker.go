package outbox

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"gorm.io/gorm"
)

type Handler interface {
	Handle(context.Context, Message) error
}

type Options struct {
	LeaseDuration time.Duration
	MaxAttempts   int
	Backoff       func(int) time.Duration
	PollInterval  time.Duration
	// RetentionDays keeps completed messages for this many days before the
	// worker deletes them. Zero means the default of 7 days.
	RetentionDays int
	// PurgeBatchSize caps one purge batch. Zero means 500 rows.
	PurgeBatchSize int
	// PurgeInterval is how often a purge cycle may run. Zero means 5 minutes.
	PurgeInterval time.Duration
	// PurgeMaxBatches caps the batches per cycle so a large backlog cannot park
	// the worker for minutes. Zero means 10 batches.
	PurgeMaxBatches int
}

const (
	defaultRetentionDays = 7
	defaultPurgeBatch    = 500
	defaultPurgeInterval = 5 * time.Minute
	defaultPurgeBatches  = 10
)

type Worker struct {
	db        *gorm.DB
	handler   Handler
	now       func() time.Time
	options   Options
	lastPurge time.Time
}

func NewWorker(db *gorm.DB, handler Handler, now func() time.Time, options Options) *Worker {
	if now == nil {
		now = time.Now
	}
	if options.LeaseDuration <= 0 {
		options.LeaseDuration = time.Minute
	}
	if options.MaxAttempts <= 0 {
		options.MaxAttempts = 5
	}
	if options.Backoff == nil {
		options.Backoff = defaultBackoff
	}
	if options.PollInterval <= 0 {
		options.PollInterval = time.Second
	}
	if options.RetentionDays <= 0 {
		options.RetentionDays = defaultRetentionDays
	}
	if options.PurgeBatchSize <= 0 {
		options.PurgeBatchSize = defaultPurgeBatch
	}
	if options.PurgeInterval <= 0 {
		options.PurgeInterval = defaultPurgeInterval
	}
	if options.PurgeMaxBatches <= 0 {
		options.PurgeMaxBatches = defaultPurgeBatches
	}
	return &Worker{db: db, handler: handler, now: now, options: options}
}

func (worker *Worker) Run(ctx context.Context) error {
	for {
		if now := worker.now().UTC(); worker.purgeDue(now) {
			worker.lastPurge = now
			if _, err := worker.Purge(ctx); err != nil && ctx.Err() == nil {
				slog.Warn("outbox purge failed", "error", err)
			}
		}
		processed, err := worker.ProcessOne(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err == nil && processed {
			continue
		}
		timer := time.NewTimer(worker.options.PollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-timer.C:
		}
	}
}

func (worker *Worker) purgeDue(now time.Time) bool {
	return worker.lastPurge.IsZero() || now.Sub(worker.lastPurge) >= worker.options.PurgeInterval
}

// Purge deletes completed messages older than the retention window. Dead
// messages are deliberately kept: they are the operational record of failed
// deliveries. The delete is done by id because DELETE ... LIMIT is not portable
// across the three supported dialects.
func (worker *Worker) Purge(ctx context.Context) (int64, error) {
	cutoff := worker.now().UTC().Add(-time.Duration(worker.options.RetentionDays) * 24 * time.Hour)
	deleted := int64(0)
	for batch := 0; batch < worker.options.PurgeMaxBatches; batch++ {
		var ids []string
		if err := worker.db.WithContext(ctx).Model(&Message{}).
			Where("status = ? AND updated_at < ?", StatusCompleted, cutoff).
			Order("updated_at ASC, id ASC").Limit(worker.options.PurgeBatchSize).
			Pluck("id", &ids).Error; err != nil {
			return deleted, err
		}
		if len(ids) == 0 {
			break
		}
		result := worker.db.WithContext(ctx).Where("id IN ?", ids).Delete(&Message{})
		if result.Error != nil {
			return deleted, result.Error
		}
		deleted += result.RowsAffected
		if len(ids) < worker.options.PurgeBatchSize {
			break
		}
	}
	if deleted > 0 {
		slog.Debug("outbox purged completed messages", "deleted", deleted,
			"retention_days", worker.options.RetentionDays)
	}
	return deleted, nil
}

func (worker *Worker) ProcessOne(ctx context.Context) (bool, error) {
	message, claimed, err := worker.claim(ctx)
	if err != nil || !claimed {
		return claimed, err
	}
	if err := worker.handler.Handle(ctx, message); err != nil {
		if updateErr := worker.fail(ctx, message, err); updateErr != nil {
			return true, updateErr
		}
		return true, err
	}
	return true, worker.complete(ctx, message)
}

func (worker *Worker) claim(ctx context.Context) (Message, bool, error) {
	now := worker.now().UTC()
	leaseUntil := now.Add(worker.options.LeaseDuration)
	var claimed Message
	err := worker.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var candidate Message
		err := tx.Where(
			"(status = ? AND available_at <= ?) OR (status = ? AND lease_until <= ?)",
			StatusPending, now, StatusProcessing, now,
		).Order("available_at ASC, created_at ASC").First(&candidate).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		update := tx.Model(&Message{}).
			Where("id = ? AND ((status = ? AND available_at <= ?) OR (status = ? AND lease_until <= ?))",
				candidate.ID, StatusPending, now, StatusProcessing, now).
			Updates(map[string]any{
				"status": StatusProcessing, "lease_until": leaseUntil,
				"attempts": gorm.Expr("attempts + 1"), "updated_at": now,
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected == 0 {
			return nil
		}
		return tx.First(&claimed, "id = ?", candidate.ID).Error
	})
	if err != nil {
		return Message{}, false, err
	}
	return claimed, claimed.ID != "", nil
}

func (worker *Worker) complete(ctx context.Context, message Message) error {
	now := worker.now().UTC()
	result := worker.db.WithContext(ctx).Model(&Message{}).
		Where("id = ? AND status = ? AND lease_until = ?", message.ID, StatusProcessing, message.LeaseUntil).
		Updates(map[string]any{
			"status": StatusCompleted, "lease_until": nil, "last_error": nil, "updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("outbox lease lost")
	}
	return nil
}

func (worker *Worker) fail(ctx context.Context, message Message, processingError error) error {
	now := worker.now().UTC()
	status := StatusPending
	availableAt := now.Add(worker.options.Backoff(message.Attempts))
	if message.Attempts >= worker.options.MaxAttempts {
		status = StatusDead
		availableAt = now
	}
	errorText := processingError.Error()
	if len(errorText) > 500 {
		errorText = errorText[:500]
	}
	result := worker.db.WithContext(ctx).Model(&Message{}).
		Where("id = ? AND status = ? AND lease_until = ?", message.ID, StatusProcessing, message.LeaseUntil).
		Updates(map[string]any{
			"status": status, "available_at": availableAt, "lease_until": nil,
			"last_error": errorText, "updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("outbox lease lost")
	}
	return nil
}

func defaultBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 10 {
		attempt = 10
	}
	return time.Minute * time.Duration(1<<(attempt-1))
}
