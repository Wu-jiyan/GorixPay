package outbox

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gorixpay/internal/platform/config"
	"gorixpay/internal/platform/database"

	"gorm.io/gorm"
)

func purgeFixture(t *testing.T, name string, now time.Time, options Options) (*gorm.DB, *Worker) {
	t.Helper()
	db, _, err := database.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: "file:" + name + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Message{}); err != nil {
		t.Fatal(err)
	}
	worker := NewWorker(db, handlerFunc(func(context.Context, Message) error { return nil }), func() time.Time { return now }, options)
	return db, worker
}

func seedMessages(t *testing.T, db *gorm.DB, prefix string, status Status, updatedAt time.Time, count int) {
	t.Helper()
	messages := make([]Message, 0, count)
	for index := 0; index < count; index++ {
		messages = append(messages, Message{
			ID:             fmt.Sprintf("%s-%04d", prefix, index),
			Topic:          "test",
			AggregateID:    "order-1",
			IdempotencyKey: fmt.Sprintf("%s:%04d", prefix, index),
			Payload:        `{}`,
			Status:         status,
			AvailableAt:    updatedAt,
			CreatedAt:      updatedAt,
			UpdatedAt:      updatedAt,
		})
	}
	if err := db.CreateInBatches(messages, 500).Error; err != nil {
		t.Fatal(err)
	}
}

func countMessages(t *testing.T, db *gorm.DB, status Status) int64 {
	t.Helper()
	var total int64
	if err := db.Model(&Message{}).Where("status = ?", status).Count(&total).Error; err != nil {
		t.Fatal(err)
	}
	return total
}

func TestWorkerPurgeDefaults(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	_, worker := purgeFixture(t, "outbox-purge-defaults", now, Options{})
	if worker.options.RetentionDays != 7 {
		t.Fatalf("retention days=%d want 7", worker.options.RetentionDays)
	}
	if worker.options.PurgeBatchSize != 500 {
		t.Fatalf("purge batch=%d want 500", worker.options.PurgeBatchSize)
	}
	if worker.options.PurgeInterval != 5*time.Minute {
		t.Fatalf("purge interval=%s want 5m", worker.options.PurgeInterval)
	}
	if worker.options.PurgeMaxBatches != 10 {
		t.Fatalf("purge max batches=%d want 10", worker.options.PurgeMaxBatches)
	}
}

func TestPurgeDeletesOnlyExpiredCompleted(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	db, worker := purgeFixture(t, "outbox-purge-retention", now, Options{})
	expired := now.Add(-8 * 24 * time.Hour)
	fresh := now.Add(-time.Hour)
	seedMessages(t, db, "completed-old", StatusCompleted, expired, 700)
	seedMessages(t, db, "completed-new", StatusCompleted, fresh, 500)
	seedMessages(t, db, "dead-old", StatusDead, expired, 5)
	seedMessages(t, db, "pending-old", StatusPending, expired, 5)
	// Retention boundary: one hour past the seven day cutoff is deleted, one
	// hour inside it is kept.
	seedMessages(t, db, "boundary-old", StatusCompleted, now.Add(-7*24*time.Hour-time.Hour), 1)
	seedMessages(t, db, "boundary-new", StatusCompleted, now.Add(-7*24*time.Hour+time.Hour), 1)

	deleted, err := worker.Purge(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 701 {
		t.Fatalf("deleted=%d want 701", deleted)
	}
	if remaining := countMessages(t, db, StatusCompleted); remaining != 501 {
		t.Fatalf("completed remaining=%d want 501", remaining)
	}
	if dead := countMessages(t, db, StatusDead); dead != 5 {
		t.Fatalf("dead=%d, dead messages must be kept", dead)
	}
	if pending := countMessages(t, db, StatusPending); pending != 5 {
		t.Fatalf("pending=%d", pending)
	}
	var freshKept int64
	if err := db.Model(&Message{}).Where("id LIKE ?", "completed-new-%").Count(&freshKept).Error; err != nil {
		t.Fatal(err)
	}
	if freshKept != 500 {
		t.Fatalf("fresh completed=%d want 500", freshKept)
	}
	if again, err := worker.Purge(context.Background()); err != nil || again != 0 {
		t.Fatalf("second purge deleted=%d err=%v", again, err)
	}
}

func TestPurgeRespectsBatchAndCycleCap(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	db, worker := purgeFixture(t, "outbox-purge-cap", now, Options{
		PurgeBatchSize: 100, PurgeMaxBatches: 2,
	})
	expired := now.Add(-30 * 24 * time.Hour)
	seedMessages(t, db, "completed", StatusCompleted, expired, 1200)

	first, err := worker.Purge(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first != 200 {
		t.Fatalf("first cycle deleted=%d want 200", first)
	}
	if remaining := countMessages(t, db, StatusCompleted); remaining != 1000 {
		t.Fatalf("remaining=%d want 1000", remaining)
	}
	second, err := worker.Purge(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second != 200 {
		t.Fatalf("second cycle deleted=%d want 200", second)
	}
}

func TestRunPurgesOnIntervalWithoutBlockingPolling(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	db, worker := purgeFixture(t, "outbox-purge-run", now, Options{
		PollInterval: 5 * time.Millisecond, PurgeInterval: time.Minute,
	})
	seedMessages(t, db, "completed", StatusCompleted, now.Add(-8*24*time.Hour), 3)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	deadline := time.Now().Add(3 * time.Second)
	for {
		if countMessages(t, db, StatusCompleted) == 0 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("worker did not purge completed messages")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop after cancellation")
	}
}
