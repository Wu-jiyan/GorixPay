package outbox

import (
	"context"
	"errors"
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
}

type Worker struct {
	db      *gorm.DB
	handler Handler
	now     func() time.Time
	options Options
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
	return &Worker{db: db, handler: handler, now: now, options: options}
}

func (worker *Worker) Run(ctx context.Context) error {
	for {
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
