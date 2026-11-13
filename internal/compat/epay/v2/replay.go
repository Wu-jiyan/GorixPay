package v2

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ReplayStore records a signed request once. Implementations backed by Redis or
// by the epay_request_nonces table must return (false, nil) for a replay.
type ReplayStore interface {
	Once(ctx context.Context, key string, ttl time.Duration) (bool, error)
}

// NonceTTL is how long a signed request nonce is remembered.
const NonceTTL = 10 * time.Minute

// RequestNonce is the database fallback used when no ReplayStore is provided.
type RequestNonce struct {
	ID         string    `gorm:"type:varchar(36);primaryKey"`
	MerchantID string    `gorm:"type:varchar(36);not null;index"`
	SignHash   string    `gorm:"type:varchar(64);not null;uniqueIndex"`
	Method     string    `gorm:"type:varchar(32);not null"`
	CreatedAt  time.Time `gorm:"not null"`
	ExpiresAt  time.Time `gorm:"not null;index"`
}

// TableName matches migrations/000010_epay_v2.sql.
func (RequestNonce) TableName() string { return "epay_request_nonces" }

// ReplayKey builds the store key for one signed request: merchant, method and
// the sha256 of the signature.
func ReplayKey(merchantID, method, signature string) string {
	digest := sha256.Sum256([]byte(signature))
	return merchantID + ":" + method + ":" + hex.EncodeToString(digest[:])
}

// DBReplayStore is the durable fallback when Redis is not configured.
type DBReplayStore struct {
	db  *gorm.DB
	now func() time.Time
}

func NewDBReplayStore(db *gorm.DB, now func() time.Time) *DBReplayStore {
	if now == nil {
		now = time.Now
	}
	return &DBReplayStore{db: db, now: now}
}

func (store *DBReplayStore) Once(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if store == nil || store.db == nil {
		return false, errors.New("replay store is unavailable")
	}
	if ttl <= 0 {
		ttl = NonceTTL
	}
	merchantID, method, hash := splitReplayKey(key)
	now := store.now().UTC()
	handler := store.db.WithContext(ctx)
	if err := handler.Where("expires_at < ?", now).Delete(&RequestNonce{}).Error; err != nil {
		return false, err
	}
	record := RequestNonce{
		ID:         uuid.NewString(),
		MerchantID: merchantID,
		SignHash:   hash,
		Method:     method,
		CreatedAt:  now,
		ExpiresAt:  now.Add(ttl),
	}
	result := handler.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "sign_hash"}}, DoNothing: true}).Create(&record)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func splitReplayKey(key string) (string, string, string) {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) != 3 {
		return "", "", key
	}
	method := parts[1]
	if len(method) > 32 {
		method = method[:32]
	}
	return parts[0], method, parts[2]
}
