package epay

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	financemodel "gorixpay/internal/finance/model"
	transfermodel "gorixpay/internal/transfer"
)

const insertRefundStatement = `INSERT INTO refund_orders
    (id, refund_no, merchant_id, payment_order_id, out_refund_no, amount, currency, reason, status,
     created_at, updated_at, processed_at)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

const insertSettlementStatement = `INSERT INTO settlement_orders
    (id, settlement_no, merchant_id, out_settlement_no, amount, currency, destination, status,
     rejection_reason, created_at, updated_at, processed_at)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

const insertTransferStatement = `INSERT INTO transfer_orders
    (id, transfer_no, merchant_id, out_transfer_no, amount, currency, account_name, account_no,
     bank_name, branch_name, status, rejection_reason, created_at, updated_at, processed_at)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// settleTypeNames is the Epay settle.type legend (user/settle.php).
var settleTypeNames = map[int64]string{
	1: "支付宝",
	2: "微信",
	3: "QQ钱包",
	4: "银行卡",
	5: "支付机构",
}

// migrateRefunds maps pre_refundorder onto refund_orders. Epay refund status
// 1 means the refund completed (lib/Order::refund writes status=1), 0 is still
// in flight.
func (m *Migrator) migrateRefunds(ctx context.Context, source *sourceDB, target *targetStore, merchants merchantIndex, orders orderIndex) (TableReport, error) {
	counts := newCounters()
	if !source.hasTable("pre_refundorder") {
		return skippedTable("refund_orders", "pre_refundorder", "refund_orders", "源表缺失"), nil
	}
	comparable := !m.options.DryRun
	existingAmounts := map[string]int64{}
	existingOutlets := map[string]bool{}
	if target != nil && target.hasTable("refund_orders") {
		rows, err := target.db.WithContext(ctx).Raw("SELECT refund_no, merchant_id, out_refund_no, amount FROM refund_orders").Rows()
		if err != nil {
			return TableReport{}, fmt.Errorf("读取目标 refund_orders 失败: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var refundNo, merchantID, outRefundNo string
			var amount sql.NullInt64
			if err := rows.Scan(&refundNo, &merchantID, &outRefundNo, &amount); err != nil {
				return TableReport{}, err
			}
			existingAmounts[refundNo] = intValue(amount)
			existingOutlets[merchantID+"|"+outRefundNo] = true
		}
		if err := rows.Err(); err != nil {
			return TableReport{}, err
		}
	}

	statement := source.selectStatement(ctx, "pre_refundorder",
		[]string{"refund_no", "out_refund_no", "trade_no", "uid", "money", "status", "addtime"},
		[]string{"endtime"},
	)
	rows, err := source.db.QueryContext(ctx, statement)
	if err != nil {
		return TableReport{}, fmt.Errorf("读取 pre_refundorder 失败: %w", err)
	}
	defer rows.Close()

	now := m.options.clock()
	var (
		sourceRows   int64
		sourceAmount int64
		targetAmount int64
		pending      []pendingInsert
		amountByKey  = map[string]int64{}
	)
	for rows.Next() {
		var refundNo, outRefundNo, tradeNo, money, addtimeRaw, endtimeRaw sql.NullString
		var uid, status sql.NullInt64
		if err := rows.Scan(&refundNo, &outRefundNo, &tradeNo, &uid, &money, &status, &addtimeRaw, &endtimeRaw); err != nil {
			return TableReport{}, err
		}
		sourceRows++
		cents, parseErr := ParseMoneyCents(money.String)
		if parseErr != nil {
			counts.fail(moneyReason(parseErr), fmt.Sprintf("refund_no=%s money=%q", refundNo.String, money.String))
			continue
		}
		if cents <= 0 {
			counts.skip("non_positive_amount")
			continue
		}
		sourceAmount += cents
		if existing, ok := existingAmounts[refundNo.String]; ok {
			counts.skip("existing_refund_no")
			targetAmount += existing
			continue
		}
		merchantID, ok := merchants.id(intValue(uid))
		if !ok {
			counts.fail("merchant_missing", fmt.Sprintf("refund_no=%s uid=%d", refundNo.String, intValue(uid)))
			continue
		}
		orderID, ok := orders.ids[tradeNo.String]
		if !ok {
			counts.fail("payment_order_missing", fmt.Sprintf("refund_no=%s trade_no=%s", refundNo.String, tradeNo.String))
			continue
		}
		outletNo := strings.TrimSpace(outRefundNo.String)
		if outletNo == "" {
			outletNo = refundNo.String
		}
		if existingOutlets[merchantID+"|"+outletNo] {
			counts.skip("out_refund_no_conflict")
			continue
		}
		existingOutlets[merchantID+"|"+outletNo] = true
		refundStatus := financemodel.StatusPending
		if intValue(status) == 1 {
			refundStatus = financemodel.StatusApproved
		}
		createdAt := timeOr(addtimeRaw.String, now)
		updatedAt := createdAt
		var processedAt any
		if endtime, ok := parseLegacyTime(endtimeRaw.String); ok {
			updatedAt = laterOf(createdAt, endtime)
			if refundStatus == financemodel.StatusApproved {
				value := endtime
				processedAt = value
			}
		}
		if refundStatus == financemodel.StatusApproved && processedAt == nil {
			value := updatedAt
			processedAt = value
		}
		id := deterministicID("refund", refundNo.String)
		existingAmounts[refundNo.String] = cents
		targetAmount += cents
		amountByKey[refundNo.String] = cents
		pending = append(pending, pendingInsert{
			key: refundNo.String,
			args: []any{
				id,
				refundNo.String,
				merchantID,
				orderID,
				trimUTF8(outletNo, 64),
				cents,
				"CNY",
				trimUTF8("历史退款迁移（Epay refund_no="+refundNo.String+"）", 255),
				string(refundStatus),
				createdAt,
				updatedAt,
				processedAt,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return TableReport{}, err
	}
	if err := m.writeInserts(ctx, target, insertRefundStatement, pending, counts); err != nil {
		return TableReport{}, err
	}
	for key := range counts.failureKeys {
		targetAmount -= amountByKey[key]
	}
	return finishTable("refund_orders", "pre_refundorder", "refund_orders", sourceRows, counts, sourceAmount, targetAmount, comparable), nil
}

// migrateSettlements maps pre_settle onto settlement_orders. Epay status:
// 0 待结算 → pending, 1 已完成 → approved, 2 正在结算 → pending, 3 结算失败 → rejected.
func (m *Migrator) migrateSettlements(ctx context.Context, source *sourceDB, target *targetStore, merchants merchantIndex) (TableReport, error) {
	counts := newCounters()
	if !source.hasTable("pre_settle") {
		return skippedTable("settlement_orders", "pre_settle", "settlement_orders", "源表缺失"), nil
	}
	comparable := !m.options.DryRun
	existingAmounts := map[string]int64{}
	if target != nil && target.hasTable("settlement_orders") {
		rows, err := target.db.WithContext(ctx).Raw("SELECT settlement_no, amount FROM settlement_orders").Rows()
		if err != nil {
			return TableReport{}, fmt.Errorf("读取目标 settlement_orders 失败: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var settlementNo string
			var amount sql.NullInt64
			if err := rows.Scan(&settlementNo, &amount); err != nil {
				return TableReport{}, err
			}
			existingAmounts[settlementNo] = intValue(amount)
		}
		if err := rows.Err(); err != nil {
			return TableReport{}, err
		}
	}

	statement := source.selectStatement(ctx, "pre_settle",
		[]string{"id", "uid", "money", "status"},
		[]string{"batch", "type", "account", "username", "realmoney", "addtime", "endtime", "result"},
	)
	rows, err := source.db.QueryContext(ctx, statement)
	if err != nil {
		return TableReport{}, fmt.Errorf("读取 pre_settle 失败: %w", err)
	}
	defer rows.Close()

	now := m.options.clock()
	var (
		sourceRows   int64
		sourceAmount int64
		targetAmount int64
		pending      []pendingInsert
		amountByKey  = map[string]int64{}
	)
	for rows.Next() {
		var (
			batch, account, username, money, realmoney sql.NullString
			addtimeRaw, endtimeRaw, result             sql.NullString
			id, uid, settleType, status                sql.NullInt64
		)
		if err := rows.Scan(&id, &uid, &money, &status, &batch, &settleType, &account, &username, &realmoney, &addtimeRaw, &endtimeRaw, &result); err != nil {
			return TableReport{}, err
		}
		sourceRows++
		cents, parseErr := ParseMoneyCents(money.String)
		if parseErr != nil {
			counts.fail(moneyReason(parseErr), fmt.Sprintf("settle id=%d money=%q", intValue(id), money.String))
			continue
		}
		if cents <= 0 {
			counts.skip("non_positive_amount")
			continue
		}
		sourceAmount += cents
		settlementNo := "epay-settle-" + strconv.FormatInt(intValue(id), 10)
		if existing, ok := existingAmounts[settlementNo]; ok {
			counts.skip("existing_settlement_no")
			targetAmount += existing
			continue
		}
		merchantID, ok := merchants.id(intValue(uid))
		if !ok {
			counts.fail("merchant_missing", fmt.Sprintf("settle id=%d uid=%d", intValue(id), intValue(uid)))
			continue
		}
		outSettlementNo := settlementNo
		if trimmedBatch := strings.TrimSpace(batch.String); trimmedBatch != "" {
			outSettlementNo = trimUTF8(trimmedBatch+"-"+strconv.FormatInt(intValue(id), 10), 64)
		}
		settleStatus, rejection := legacySettlementStatus(intValue(status), result.String)
		createdAt := timeOr(addtimeRaw.String, now)
		updatedAt := createdAt
		var processedAt any
		if endtime, ok := parseLegacyTime(endtimeRaw.String); ok {
			updatedAt = laterOf(createdAt, endtime)
			if settleStatus != financemodel.StatusPending {
				value := endtime
				processedAt = value
			}
		}
		pending = append(pending, pendingInsert{
			key: settlementNo,
			args: []any{
				deterministicID("settlement", strconv.FormatInt(intValue(id), 10)),
				settlementNo,
				merchantID,
				outSettlementNo,
				cents,
				"CNY",
				trimUTF8(settlementDestination(intValue(settleType), account.String, username.String), 255),
				string(settleStatus),
				rejection,
				createdAt,
				updatedAt,
				processedAt,
			},
		})
		existingAmounts[settlementNo] = cents
		targetAmount += cents
		amountByKey[settlementNo] = cents
	}
	if err := rows.Err(); err != nil {
		return TableReport{}, err
	}
	if err := m.writeInserts(ctx, target, insertSettlementStatement, pending, counts); err != nil {
		return TableReport{}, err
	}
	for key := range counts.failureKeys {
		targetAmount -= amountByKey[key]
	}
	return finishTable("settlement_orders", "pre_settle", "settlement_orders", sourceRows, counts, sourceAmount, targetAmount, comparable), nil
}

// migrateTransfers maps pre_transfer onto transfer_orders. Epay status:
// 0 正在处理 / 3 待处理 → pending, 1 转账成功 → approved, 2 转账失败 → rejected.
func (m *Migrator) migrateTransfers(ctx context.Context, source *sourceDB, target *targetStore, merchants merchantIndex) (TableReport, error) {
	counts := newCounters()
	if !source.hasTable("pre_transfer") {
		return skippedTable("transfer_orders", "pre_transfer", "transfer_orders", "源表缺失"), nil
	}
	comparable := !m.options.DryRun
	existingAmounts := map[string]int64{}
	if target != nil && target.hasTable("transfer_orders") {
		rows, err := target.db.WithContext(ctx).Raw("SELECT merchant_id, out_transfer_no, amount FROM transfer_orders").Rows()
		if err != nil {
			return TableReport{}, fmt.Errorf("读取目标 transfer_orders 失败: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var merchantID, outTransferNo string
			var amount sql.NullInt64
			if err := rows.Scan(&merchantID, &outTransferNo, &amount); err != nil {
				return TableReport{}, err
			}
			existingAmounts[merchantID+"|"+outTransferNo] = intValue(amount)
		}
		if err := rows.Err(); err != nil {
			return TableReport{}, err
		}
	}

	statement := source.selectStatement(ctx, "pre_transfer",
		[]string{"biz_no", "out_biz_no", "uid", "type", "account", "username", "money", "status"},
		[]string{"addtime", "paytime", "result"},
	)
	rows, err := source.db.QueryContext(ctx, statement)
	if err != nil {
		return TableReport{}, fmt.Errorf("读取 pre_transfer 失败: %w", err)
	}
	defer rows.Close()

	now := m.options.clock()
	var (
		sourceRows   int64
		sourceAmount int64
		targetAmount int64
		pending      []pendingInsert
		amountByKey  = map[string]int64{}
	)
	for rows.Next() {
		var bizNo, outBizNo, transferType, account, username, money sql.NullString
		var addtimeRaw, paytimeRaw, result sql.NullString
		var uid, status sql.NullInt64
		if err := rows.Scan(&bizNo, &outBizNo, &uid, &transferType, &account, &username, &money, &status,
			&addtimeRaw, &paytimeRaw, &result); err != nil {
			return TableReport{}, err
		}
		sourceRows++
		cents, parseErr := ParseMoneyCents(money.String)
		if parseErr != nil {
			counts.fail(moneyReason(parseErr), fmt.Sprintf("biz_no=%s money=%q", bizNo.String, money.String))
			continue
		}
		if cents <= 0 {
			counts.skip("non_positive_amount")
			continue
		}
		sourceAmount += cents
		transferNo := trimUTF8(strings.TrimSpace(bizNo.String), 32)
		if transferNo == "" {
			transferNo = "epay-transfer-" + strconv.FormatInt(intValue(uid), 10)
		}
		outTransferNo := trimUTF8(strings.TrimSpace(outBizNo.String), 64)
		if outTransferNo == "" {
			outTransferNo = transferNo
		}
		merchantID, ok := merchants.id(intValue(uid))
		if !ok {
			counts.fail("merchant_missing", fmt.Sprintf("biz_no=%s uid=%d", bizNo.String, intValue(uid)))
			continue
		}
		outletKey := merchantID + "|" + outTransferNo
		if existing, ok := existingAmounts[outletKey]; ok {
			counts.skip("existing_out_transfer_no")
			targetAmount += existing
			continue
		}
		existingAmounts[outletKey] = cents
		transferStatus, rejection := legacyTransferStatus(intValue(status), result.String)
		createdAt := timeOr(addtimeRaw.String, now)
		updatedAt := createdAt
		var processedAt any
		if paytime, ok := parseLegacyTime(paytimeRaw.String); ok {
			updatedAt = laterOf(createdAt, paytime)
			if transferStatus != transfermodel.StatusPending {
				value := paytime
				processedAt = value
			}
		}
		pending = append(pending, pendingInsert{
			key: transferNo,
			args: []any{
				deterministicID("transfer", transferNo),
				transferNo,
				merchantID,
				outTransferNo,
				cents,
				"CNY",
				trimUTF8(strings.TrimSpace(username.String), 128),
				trimUTF8(strings.TrimSpace(account.String), 64),
				trimUTF8(strings.TrimSpace(transferType.String), 128),
				nil,
				string(transferStatus),
				rejection,
				createdAt,
				updatedAt,
				processedAt,
			},
		})
		targetAmount += cents
		amountByKey[transferNo] = cents
	}
	if err := rows.Err(); err != nil {
		return TableReport{}, err
	}
	if err := m.writeInserts(ctx, target, insertTransferStatement, pending, counts); err != nil {
		return TableReport{}, err
	}
	for key := range counts.failureKeys {
		targetAmount -= amountByKey[key]
	}
	return finishTable("transfer_orders", "pre_transfer", "transfer_orders", sourceRows, counts, sourceAmount, targetAmount, comparable), nil
}

func legacySettlementStatus(legacy int64, result string) (financemodel.Status, any) {
	switch legacy {
	case 1:
		return financemodel.StatusApproved, nil
	case 3:
		return financemodel.StatusRejected, nullableTrimmed(result, 255)
	default:
		return financemodel.StatusPending, nil
	}
}

func legacyTransferStatus(legacy int64, result string) (transfermodel.Status, any) {
	switch legacy {
	case 1:
		return transfermodel.StatusApproved, nil
	case 2:
		return transfermodel.StatusRejected, nullableTrimmed(result, 255)
	default:
		return transfermodel.StatusPending, nil
	}
}

// settlementDestination renders the payout target the way the operator sees it
// in Epay: settlement channel, account and account holder.
func settlementDestination(settleType int64, account, username string) string {
	parts := []string{}
	if name, ok := settleTypeNames[settleType]; ok {
		parts = append(parts, name)
	}
	for _, candidate := range []string{account, username} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	if len(parts) == 0 {
		return "unknown"
	}
	return strings.Join(parts, " ")
}
