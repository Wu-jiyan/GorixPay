package settlement

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	financemodel "gorixpay/internal/finance/model"
	financeservice "gorixpay/internal/finance/service"
	identitymodel "gorixpay/internal/identity/model"
	ledgermodel "gorixpay/internal/ledger/model"
	transferservice "gorixpay/internal/transfer"

	"gorm.io/gorm"
)

// FeeResolver returns the merchant fee rate in basis points (100 = 1%). M1
// provides it from the merchant group; returning false means "no rate known".
type FeeResolver func(ctx context.Context, merchantID string) (int, bool)

// Service runs automated settlement rules against the finance and transfer
// services. It never writes ledger rows itself.
type Service struct {
	db          *gorm.DB
	finance     *financeservice.Service
	transfers   *transferservice.Service
	feeResolver FeeResolver
	now         func() time.Time
	running     sync.Map
}

func New(db *gorm.DB, finance *financeservice.Service, transfers *transferservice.Service, feeResolver FeeResolver, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, finance: finance, transfers: transfers, feeResolver: feeResolver, now: now}
}

// Start launches the scheduled loop. It runs once immediately, then every
// interval, and re-entry is blocked by an in-process guard. Multiple replicas
// coordinate through the settle_rules.last_run_at conditional update.
func (service *Service) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		service.runScheduled(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				service.runScheduled(ctx)
			}
		}
	}()
}

func (service *Service) runScheduled(ctx context.Context) {
	if _, loaded := service.running.LoadOrStore("auto-settle", struct{}{}); loaded {
		return
	}
	defer service.running.Delete("auto-settle")
	runContext, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	_, _ = service.RunAutoSettle(runContext, service.now())
}

// RunAutoSettle evaluates every active rule once and returns the number of
// settlement orders it created. A rule is claimed per calendar day, so two
// replicas racing the same day only let one of them run.
func (service *Service) RunAutoSettle(ctx context.Context, now time.Time) (int, error) {
	if service.finance == nil {
		return 0, ErrInvalidRequest
	}
	var rules []Rule
	if err := service.db.WithContext(ctx).Where("status = ?", RuleActive).Order("created_at ASC").Find(&rules).Error; err != nil {
		return 0, err
	}
	generated := 0
	var failures []string
	for _, rule := range rules {
		claimed, err := service.claim(ctx, rule.ID, now)
		if err != nil {
			failures = append(failures, fmt.Sprintf("rule %s: %v", rule.ID, err))
			continue
		}
		if !claimed {
			continue
		}
		count, runErr := service.runRule(ctx, rule, now)
		generated += count
		if runErr != nil {
			failures = append(failures, fmt.Sprintf("rule %s: %v", rule.ID, runErr))
			// Release the day claim so a later tick can retry the failed rule.
			_ = service.db.WithContext(ctx).Model(&Rule{}).
				Where("id = ? AND last_run_at = ?", rule.ID, now.UTC()).
				Updates(map[string]any{"last_run_at": nil, "updated_at": now.UTC()}).Error
		}
	}
	if len(failures) > 0 {
		return generated, errors.New(strings.Join(failures, "; "))
	}
	return generated, nil
}

// RunRule executes one rule immediately, bypassing the daily claim. The
// per-merchant settlement number keeps the run idempotent for the same day.
func (service *Service) RunRule(ctx context.Context, ruleID string, now time.Time) (int, error) {
	rule, err := service.GetRule(ctx, ruleID)
	if err != nil {
		return 0, err
	}
	if rule.Status != RuleActive {
		return 0, ErrInvalidRequest
	}
	count, err := service.runRule(ctx, rule, now)
	if err != nil {
		return count, err
	}
	_ = service.db.WithContext(ctx).Model(&Rule{}).Where("id = ?", rule.ID).Updates(map[string]any{"last_run_at": now.UTC(), "updated_at": now.UTC()}).Error
	return count, nil
}

func (service *Service) claim(ctx context.Context, ruleID string, now time.Time) (bool, error) {
	dayStart := startOfDay(now)
	result := service.db.WithContext(ctx).Model(&Rule{}).
		Where("id = ? AND status = ? AND (last_run_at IS NULL OR last_run_at < ?)", ruleID, RuleActive, dayStart).
		Updates(map[string]any{"last_run_at": now.UTC(), "updated_at": now.UTC()})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func (service *Service) runRule(ctx context.Context, rule Rule, now time.Time) (int, error) {
	merchants, err := service.ruleMerchants(ctx, rule)
	if err != nil {
		return 0, err
	}
	generated := 0
	var failures []string
	for _, merchant := range merchants {
		amount := service.withdrawable(ctx, merchant.ID)
		if amount <= 0 || amount < rule.MinAmountCents {
			continue
		}
		fee := service.feeFor(ctx, rule, merchant.ID, amount)
		outSettlementNo := autoOutSettlementNo(rule.ID, merchant.ID, now)
		settlement, created, err := service.finance.CreateSettlement(ctx, financeservice.CreateSettlementRequest{
			MerchantID:      merchant.ID,
			OutSettlementNo: outSettlementNo,
			Amount:          amount,
			Currency:        "CNY",
			Destination:     strings.TrimSpace(stringValue(merchant.SettleAccount)),
		})
		if err != nil {
			failures = append(failures, fmt.Sprintf("merchant %s: %v", merchant.ID, err))
			continue
		}
		if !created {
			continue
		}
		if err := service.db.WithContext(ctx).Model(&settlementRow{}).Where("id = ?", settlement.ID).
			Updates(map[string]any{"source": SourceAuto, "fee_cents": fee, "transfer_status": TransferNone}).Error; err != nil {
			failures = append(failures, fmt.Sprintf("merchant %s: %v", merchant.ID, err))
			continue
		}
		generated++
	}
	if len(failures) > 0 {
		return generated, errors.New(strings.Join(failures, "; "))
	}
	return generated, nil
}

func (service *Service) ruleMerchants(ctx context.Context, rule Rule) ([]identitymodel.Merchant, error) {
	if rule.MerchantID != nil && strings.TrimSpace(*rule.MerchantID) != "" {
		var merchant identitymodel.Merchant
		if err := service.db.WithContext(ctx).First(&merchant, "id = ? AND status = ?", *rule.MerchantID, identitymodel.StatusActive).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, err
		}
		if strings.TrimSpace(stringValue(merchant.SettleAccount)) == "" {
			return nil, nil
		}
		return []identitymodel.Merchant{merchant}, nil
	}
	var merchants []identitymodel.Merchant
	err := service.db.WithContext(ctx).
		Where("status = ? AND settle_account IS NOT NULL AND settle_account <> ''", identitymodel.StatusActive).
		Find(&merchants).Error
	return merchants, err
}

// withdrawable is the available ledger bucket minus frozen funds and minus
// settlements that are still pending approval, so repeated runs cannot promise
// the same balance twice.
func (service *Service) withdrawable(ctx context.Context, merchantID string) int64 {
	var account ledgermodel.Account
	err := service.db.WithContext(ctx).
		Where("owner_type = ? AND owner_id = ? AND purpose = ? AND currency = ?", "merchant", merchantID, "balance", "CNY").
		First(&account).Error
	if err != nil {
		return 0
	}
	var balance ledgermodel.Balance
	if err := service.db.WithContext(ctx).First(&balance, "account_id = ?", account.ID).Error; err != nil {
		return 0
	}
	var pending int64
	_ = service.db.WithContext(ctx).Model(&settlementRow{}).
		Where("merchant_id = ? AND status = ?", merchantID, string(financemodel.StatusPending)).
		Select("COALESCE(SUM(amount), 0)").Scan(&pending).Error
	amount := balance.Available - balance.Frozen - pending
	if amount < 0 {
		return 0
	}
	return amount
}

func (service *Service) feeFor(ctx context.Context, rule Rule, merchantID string, amount int64) int64 {
	rate := rule.FeeBP
	if rate <= 0 && service.feeResolver != nil {
		if resolved, ok := service.feeResolver(ctx, merchantID); ok {
			rate = resolved
		}
	}
	if rate <= 0 {
		return 0
	}
	return amount * int64(rate) / 10000
}

// TransferResult reports what AutoTransfer did for one settlement.
type TransferResult struct {
	Skipped        bool           `json:"skipped"`
	Reason         string         `json:"reason,omitempty"`
	TransferNo     string         `json:"transfer_no,omitempty"`
	TransferStatus TransferStatus `json:"transfer_status"`
}

// AutoTransfer creates the payout order for an approved settlement when an
// active rule for the merchant enables auto transfer.
func (service *Service) AutoTransfer(ctx context.Context, settlementID string) (TransferResult, error) {
	var settlement settlementRow
	if err := service.db.WithContext(ctx).First(&settlement, "id = ?", strings.TrimSpace(settlementID)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return TransferResult{}, ErrSettlementNotFound
		}
		return TransferResult{}, err
	}
	if settlement.Status != string(financemodel.StatusApproved) {
		return TransferResult{Skipped: true, Reason: "settlement is not approved", TransferStatus: settlement.TransferStatus}, ErrSettlementNotReady
	}
	if settlement.TransferStatus == TransferDone {
		return TransferResult{Skipped: true, Reason: "settlement already transferred", TransferStatus: TransferDone}, nil
	}
	rule, err := service.transferRule(ctx, settlement.MerchantID)
	if err != nil {
		return TransferResult{}, err
	}
	if rule == nil {
		return TransferResult{Skipped: true, Reason: "no active auto transfer rule", TransferStatus: settlement.TransferStatus}, nil
	}
	if service.transfers == nil {
		return service.failTransfer(ctx, &settlement, "transfer service unavailable")
	}
	var merchant identitymodel.Merchant
	if err := service.db.WithContext(ctx).First(&merchant, "id = ?", settlement.MerchantID).Error; err != nil {
		return service.failTransfer(ctx, &settlement, "merchant not found")
	}
	accountNo := strings.TrimSpace(stringValue(merchant.SettleAccount))
	if accountNo == "" {
		return service.failTransfer(ctx, &settlement, ErrNoSettlementProfile.Error())
	}
	if err := service.updateTransfer(ctx, settlement.ID, TransferPending, nil, ""); err != nil {
		return TransferResult{}, err
	}
	outTransferNo := autoOutTransferNo(settlement.SettlementNo)
	order, _, err := service.transfers.Create(ctx, transferservice.Request{
		MerchantID:    settlement.MerchantID,
		OutTransferNo: outTransferNo,
		Amount:        settlement.Amount,
		Currency:      settlement.Currency,
		AccountName:   merchant.DisplayName,
		AccountNo:     accountNo,
	})
	if err != nil {
		return service.failTransfer(ctx, &settlement, err.Error())
	}
	now := service.now().UTC()
	if err := service.updateTransfer(ctx, settlement.ID, TransferDone, &now, ""); err != nil {
		return TransferResult{}, err
	}
	return TransferResult{TransferNo: order.TransferNo, TransferStatus: TransferDone}, nil
}

func (service *Service) failTransfer(ctx context.Context, settlement *settlementRow, reason string) (TransferResult, error) {
	if err := service.updateTransfer(ctx, settlement.ID, TransferFailed, nil, "auto-transfer: "+reason); err != nil {
		return TransferResult{}, err
	}
	return TransferResult{Skipped: true, Reason: reason, TransferStatus: TransferFailed}, nil
}

func (service *Service) updateTransfer(ctx context.Context, settlementID string, status TransferStatus, transferAt *time.Time, reason string) error {
	updates := map[string]any{"transfer_status": status, "updated_at": service.now().UTC()}
	if transferAt != nil {
		updates["transfer_at"] = transferAt.UTC()
	}
	if reason != "" {
		updates["rejection_reason"] = reason
	}
	return service.db.WithContext(ctx).Model(&settlementRow{}).Where("id = ?", settlementID).Updates(updates).Error
}

func (service *Service) transferRule(ctx context.Context, merchantID string) (*Rule, error) {
	var rule Rule
	err := service.db.WithContext(ctx).
		Where("status = ? AND auto_transfer = ? AND merchant_id = ?", RuleActive, true, merchantID).
		Order("created_at ASC").First(&rule).Error
	if err == nil {
		return &rule, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	err = service.db.WithContext(ctx).
		Where("status = ? AND auto_transfer = ? AND merchant_id IS NULL", RuleActive, true).
		Order("created_at ASC").First(&rule).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// BatchResult reports one settlement processed through the batch endpoint.
type BatchResult struct {
	ID             string         `json:"id"`
	Status         string         `json:"status,omitempty"`
	TransferStatus TransferStatus `json:"transfer_status,omitempty"`
	Error          string         `json:"error,omitempty"`
}

// BatchProcess approves or rejects a list of settlements through the finance
// service. Approved settlements go through AutoTransfer when a rule asks for it.
func (service *Service) BatchProcess(ctx context.Context, ids []string, action, reason, processedBy string) ([]BatchResult, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	reason = strings.TrimSpace(reason)
	processedBy = strings.TrimSpace(processedBy)
	if len(ids) == 0 || len(ids) > 200 || processedBy == "" || (action != "approve" && action != "reject") || (action == "reject" && reason == "") {
		return nil, ErrInvalidRequest
	}
	results := make([]BatchResult, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			results = append(results, BatchResult{Error: "empty settlement id"})
			continue
		}
		settlement, err := service.finance.ProcessSettlement(ctx, financeservice.ProcessRequest{
			ID: id, Action: action, ProcessedBy: processedBy, RejectionReason: reason,
		})
		if err != nil {
			results = append(results, BatchResult{ID: id, Error: batchError(err)})
			continue
		}
		result := BatchResult{ID: id, Status: string(settlement.Status)}
		if action == "approve" {
			transfer, transferErr := service.AutoTransfer(ctx, id)
			result.TransferStatus = transfer.TransferStatus
			if transferErr != nil && !errors.Is(transferErr, ErrSettlementNotReady) {
				result.Error = batchError(transferErr)
			}
			if transfer.Skipped && transfer.Reason != "" && result.Error == "" {
				result.TransferStatus = transfer.TransferStatus
			}
		}
		results = append(results, result)
	}
	return results, nil
}

func batchError(err error) string {
	switch {
	case errors.Is(err, financeservice.ErrInvalidRequest):
		return "invalid request"
	case errors.Is(err, financeservice.ErrNotFound):
		return "settlement not found"
	case errors.Is(err, financeservice.ErrStateConflict):
		return "settlement state conflict"
	default:
		return err.Error()
	}
}

func autoOutSettlementNo(ruleID, merchantID string, now time.Time) string {
	key := fmt.Sprintf("auto-settle:%s:%s:%s", ruleID, merchantID, now.UTC().Format("20060102"))
	digest := sha256.Sum256([]byte(key))
	return "AS" + hex.EncodeToString(digest[:])[:30]
}

func autoOutTransferNo(settlementNo string) string {
	return "AT" + settlementNo
}

func startOfDay(now time.Time) time.Time {
	utc := now.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
