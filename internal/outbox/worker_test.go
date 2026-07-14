package outbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"
)

type handlerFunc func(context.Context, Message) error

func (function handlerFunc) Handle(ctx context.Context, message Message) error {
	return function(ctx, message)
}

func TestWorkerClaimsAndCompletesMessage(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:outbox-worker?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Message{}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	message := Message{ID: "message-1", Topic: "test", AggregateID: "order-1", IdempotencyKey: "test:1", Payload: `{}`, Status: StatusPending, AvailableAt: now, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&message).Error; err != nil {
		t.Fatal(err)
	}
	called := 0
	worker := NewWorker(db, handlerFunc(func(_ context.Context, claimed Message) error {
		called++
		if claimed.ID != message.ID || claimed.Status != StatusProcessing {
			t.Fatalf("claimed=%+v", claimed)
		}
		return nil
	}), func() time.Time { return now }, Options{LeaseDuration: time.Minute, MaxAttempts: 3})
	processed, err := worker.ProcessOne(context.Background())
	if err != nil || !processed || called != 1 {
		t.Fatalf("processed=%v called=%d err=%v", processed, called, err)
	}
	var completed Message
	if err := db.First(&completed, "id = ?", message.ID).Error; err != nil || completed.Status != StatusCompleted || completed.LeaseUntil != nil {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	processed, err = worker.ProcessOne(context.Background())
	if err != nil || processed {
		t.Fatalf("empty queue processed=%v err=%v", processed, err)
	}
}

func TestWorkerRetriesThenMovesMessageToDeadState(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:outbox-retry?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Message{}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	message := Message{ID: "message-1", Topic: "test", AggregateID: "order-1", IdempotencyKey: "test:1", Payload: `{}`, Status: StatusPending, AvailableAt: now, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&message).Error; err != nil {
		t.Fatal(err)
	}
	worker := NewWorker(db, handlerFunc(func(context.Context, Message) error { return errors.New("temporary failure") }), func() time.Time { return now }, Options{
		LeaseDuration: time.Minute,
		MaxAttempts:   2,
		Backoff:       func(attempt int) time.Duration { return time.Duration(attempt) * time.Minute },
	})
	if processed, err := worker.ProcessOne(context.Background()); err == nil || !processed {
		t.Fatalf("first attempt processed=%v err=%v", processed, err)
	}
	var retry Message
	if err := db.First(&retry, "id = ?", message.ID).Error; err != nil {
		t.Fatal(err)
	}
	if retry.Status != StatusPending || retry.Attempts != 1 || !retry.AvailableAt.Equal(now.Add(time.Minute)) || retry.LastError == nil {
		t.Fatalf("retry=%+v", retry)
	}
	now = now.Add(time.Minute)
	if processed, err := worker.ProcessOne(context.Background()); err == nil || !processed {
		t.Fatalf("second attempt processed=%v err=%v", processed, err)
	}
	var dead Message
	if err := db.First(&dead, "id = ?", message.ID).Error; err != nil {
		t.Fatal(err)
	}
	if dead.Status != StatusDead || dead.Attempts != 2 || dead.LeaseUntil != nil {
		t.Fatalf("dead=%+v", dead)
	}
}

func TestWorkerReclaimsExpiredLease(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:outbox-lease?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Message{}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	expired := now.Add(-time.Minute)
	message := Message{ID: "message-1", Topic: "test", AggregateID: "order-1", IdempotencyKey: "test:1", Payload: `{}`, Status: StatusProcessing, AvailableAt: now.Add(-time.Hour), LeaseUntil: &expired, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)}
	if err := db.Create(&message).Error; err != nil {
		t.Fatal(err)
	}
	worker := NewWorker(db, handlerFunc(func(context.Context, Message) error { return nil }), func() time.Time { return now }, Options{LeaseDuration: time.Minute, MaxAttempts: 3})
	processed, err := worker.ProcessOne(context.Background())
	if err != nil || !processed {
		t.Fatalf("processed=%v err=%v", processed, err)
	}
}

func TestWorkerRunsUntilContextCancellation(t *testing.T) {
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:outbox-run?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Message{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	message := Message{ID: "message-1", Topic: "test", AggregateID: "order-1", IdempotencyKey: "test:run", Payload: `{}`, Status: StatusPending, AvailableAt: now, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&message).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	called := make(chan struct{}, 1)
	worker := NewWorker(db, handlerFunc(func(context.Context, Message) error {
		called <- struct{}{}
		return nil
	}), time.Now, Options{PollInterval: time.Millisecond})
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	select {
	case <-called:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("worker did not process message")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after cancellation")
	}
}
