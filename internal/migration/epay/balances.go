package epay

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	ledgermodel "gorixpay/internal/ledger/model"
	ledgerservice "gorixpay/internal/ledger/service"
)

// openingReferenceType tags the ledger transactions created by this migration so
// a rerun can find them without scanning descriptions.
const openingReferenceType = "merchant-opening-balance"

// balanceOwnerPlatform is the platform owner id used by the payment callback
// service for its cash account; the opening balance mirrors that posting.
const (
	balanceOwnerTypePlatform = "platform"
	balanceOwnerIDPlatform   = "platform"
	balancePurposePlatform   = "cash"
	balancePurposeMerchant   = "balance"
)

// migrateBalances turns pre_user.money into a ledger opening balance.
//
// Each merchant with a non-zero balance gets one balanced transaction
// "opening-balance:{uid}": the merchant's liability/balance account is credited
// and the platform cash account is debited — the same shape the payment callback
// uses for real income, so every downstream balance query keeps working.
func (m *Migrator) migrateBalances(ctx context.Context, source *sourceDB, target *targetStore, merchants merchantIndex) (BalanceReport, error) {
	report := BalanceReport{
		SkipReasons:    map[string]int64{},
		FailureReasons: map[string]int64{},
		Comparable:     !m.options.DryRun,
	}
	if !source.hasTable("pre_user") {
		return report, nil
	}
	existing, err := m.existingOpeningBalances(ctx, target)
	if err != nil {
		return report, err
	}

	rows, err := source.db.QueryContext(ctx, "SELECT uid, money, addtime FROM pre_user")
	if err != nil {
		return report, fmt.Errorf("读取 pre_user.money 失败: %w", err)
	}
	defer rows.Close()

	var ledger *ledgerservice.Service
	platformAccountID := ""
	if target != nil && target.writable {
		ledger = ledgerservice.New(target.db, m.options.Now)
		platformAccount, err := ledger.CreateAccount(ctx, ledgerservice.AccountRequest{
			OwnerType: balanceOwnerTypePlatform,
			OwnerID:   balanceOwnerIDPlatform,
			Purpose:   balancePurposePlatform,
			Category:  ledgermodel.CategoryAsset,
			Currency:  "CNY",
		})
		if err != nil {
			return report, fmt.Errorf("创建平台账户: %w", err)
		}
		platformAccountID = platformAccount.ID
	}
	total := int64(0)
	for rows.Next() {
		var uid sql.NullInt64
		var money, addtimeRaw sql.NullString
		if err := rows.Scan(&uid, &money, &addtimeRaw); err != nil {
			return report, err
		}
		legacyUID := intValue(uid)
		cents, parseErr := ParseMoneyCents(money.String)
		if parseErr != nil {
			report.fail(moneyReason(parseErr), fmt.Sprintf("uid=%d money=%q", legacyUID, money.String))
			continue
		}
		report.Merchants++
		if cents == 0 {
			report.skip("zero_balance")
			continue
		}
		if cents < 0 {
			report.fail("negative_amount", fmt.Sprintf("uid=%d money=%q", legacyUID, money.String))
			continue
		}
		total += cents
		merchantID, ok := merchants.id(legacyUID)
		if !ok {
			report.fail("merchant_missing", fmt.Sprintf("uid=%d", legacyUID))
			continue
		}
		if posted, ok := existing[merchantID]; ok {
			report.skip("existing_opening_balance")
			report.TargetTotalCents += posted
			continue
		}
		if ledger == nil {
			// Dry run without a readable target: the posting is what would happen.
			report.Posted++
			report.TargetTotalCents += cents
			continue
		}
		posted, err := m.postOpeningBalance(ctx, ledger, platformAccountID, legacyUID, merchantID, cents, money.String)
		if err != nil {
			report.fail("ledger_error", "uid="+strconv.FormatInt(legacyUID, 10)+" "+err.Error())
			continue
		}
		if posted {
			report.Posted++
		} else {
			report.skip("existing_opening_balance")
		}
		report.TargetTotalCents += cents
	}
	if err := rows.Err(); err != nil {
		return report, err
	}
	report.SourceTotalCents = total
	report.DiffCents = report.TargetTotalCents - report.SourceTotalCents
	if len(report.SkipReasons) == 0 {
		report.SkipReasons = nil
	}
	if len(report.FailureReasons) == 0 {
		report.FailureReasons = nil
	}
	return report, nil
}

// postOpeningBalance writes one balanced opening transaction, idempotent on
// "opening-balance:{uid}". posted is false when the transaction already existed.
func (m *Migrator) postOpeningBalance(ctx context.Context, ledger *ledgerservice.Service, platformAccountID string, legacyUID int64, merchantID string, cents int64, legacyMoney string) (bool, error) {
	merchantAccount, err := ledger.CreateAccount(ctx, ledgerservice.AccountRequest{
		OwnerType: "merchant",
		OwnerID:   merchantID,
		Purpose:   balancePurposeMerchant,
		Category:  ledgermodel.CategoryLiability,
		Currency:  "CNY",
	})
	if err != nil {
		return false, fmt.Errorf("创建商户余额账户: %w", err)
	}
	metadata := fmt.Sprintf(`{"source":"epay","legacy_uid":%d,"legacy_money":%q}`, legacyUID, legacyMoney)
	result, err := ledger.Post(ctx, ledgerservice.TransactionRequest{
		IdempotencyKey: "opening-balance:" + strconv.FormatInt(legacyUID, 10),
		ReferenceType:  openingReferenceType,
		ReferenceID:    merchantID,
		Description:    "Epay 期初余额",
		Metadata:       metadata,
		Postings: []ledgerservice.Posting{
			{AccountID: merchantAccount.ID, Direction: ledgermodel.DirectionCredit, Bucket: ledgermodel.BucketAvailable, Amount: cents},
			{AccountID: platformAccountID, Direction: ledgermodel.DirectionDebit, Bucket: ledgermodel.BucketAvailable, Amount: cents},
		},
	})
	if err != nil {
		return false, err
	}
	return !result.Replayed, nil
}

// existingOpeningBalances returns the already posted opening amount per merchant
// id, so a rerun reports the previous result instead of reposting it.
func (m *Migrator) existingOpeningBalances(ctx context.Context, target *targetStore) (map[string]int64, error) {
	posted := map[string]int64{}
	if target == nil || !target.hasTable("ledger_transactions") {
		return posted, nil
	}
	if !target.hasTable("ledger_entries") || !target.hasTable("ledger_accounts") {
		return posted, nil
	}
	rows, err := target.db.WithContext(ctx).Raw(`
        SELECT t.reference_id, SUM(e.amount)
          FROM ledger_transactions t
          JOIN ledger_entries e ON e.transaction_id = t.id
          JOIN ledger_accounts a ON a.id = e.account_id
         WHERE t.reference_type = ? AND a.owner_type = 'merchant' AND e.direction = 'credit'
         GROUP BY t.reference_id`, openingReferenceType).Rows()
	if err != nil {
		return nil, fmt.Errorf("读取目标期初余额交易失败: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var merchantID string
		var amount sql.NullInt64
		if err := rows.Scan(&merchantID, &amount); err != nil {
			return nil, err
		}
		posted[merchantID] = intValue(amount)
	}
	return posted, rows.Err()
}

func (report *BalanceReport) skip(reason string) {
	report.Skipped++
	report.SkipReasons[reason]++
}

func (report *BalanceReport) fail(reason, detail string) {
	report.Failed++
	report.FailureReasons[reason]++
	report.appendSample(reason + ": " + detail)
}

func (report *BalanceReport) appendSample(sample string) {
	if len(report.FailureSamples) >= failureSampleLimit {
		return
	}
	report.FailureSamples = append(report.FailureSamples, sample)
}
