package epay

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	paymentmodel "gorixpay/internal/payment/model"
)

// defaultEpayPaymentTypes mirrors the pre_type seed rows of install.sql. They are
// used when a fork dropped the pre_type table, and overridden by it when present.
var defaultEpayPaymentTypes = map[int64]string{
	1: "alipay",
	2: "wxpay",
	3: "qqpay",
	4: "bank",
	5: "jdpay",
	6: "paypal",
	7: "douyinpay",
}

// orderIndex is what later tables need from the migrated orders.
type orderIndex struct {
	ids      map[string]string // trade_no -> payment_orders.id
	amounts  map[string]int64  // trade_no -> amount in cents
	byOutlet map[string]string // merchant_id|out_trade_no -> trade_no
}

func newOrderIndex() orderIndex {
	return orderIndex{ids: map[string]string{}, amounts: map[string]int64{}, byOutlet: map[string]string{}}
}

// orderExpiryOffset is the lifetime given to a migrated order. Epay has no
// expiry column, so a paid order gets its paid time and an open order 24h.
const orderExpiryOffset = 24 * time.Hour

const insertOrderStatement = `INSERT INTO payment_orders
    (id, trade_no, merchant_id, out_trade_no, payment_type, name, amount, currency, status,
     notify_url, return_url, client_ip, attach, channel_trade_no, paid_at, expires_at, created_at, updated_at)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// migrateOrders maps pre_order onto payment_orders.
//
// Status mapping (Epay user/order.php): 0 未支付 → pending, 1 已支付 → paid,
// 2 已退款/已关闭 → closed, 3 冻结 → paid (the money was already collected, the
// freeze only affected the balance, which the opening balance reflects).
func (m *Migrator) migrateOrders(ctx context.Context, source *sourceDB, target *targetStore, merchants merchantIndex) (orderIndex, TableReport, error) {
	index := newOrderIndex()
	counts := newCounters()
	if !source.hasTable("pre_order") {
		return index, skippedTable("payment_orders", "pre_order", "payment_orders", "源表缺失"), nil
	}
	comparable := !m.options.DryRun
	if target != nil && target.hasTable("payment_orders") {
		rows, err := target.db.WithContext(ctx).Raw("SELECT id, trade_no, merchant_id, out_trade_no, amount FROM payment_orders").Rows()
		if err != nil {
			return index, TableReport{}, fmt.Errorf("读取目标 payment_orders 失败: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var id, tradeNo, merchantID, outTradeNo string
			var amount sql.NullInt64
			if err := rows.Scan(&id, &tradeNo, &merchantID, &outTradeNo, &amount); err != nil {
				return index, TableReport{}, err
			}
			index.ids[tradeNo] = id
			index.amounts[tradeNo] = intValue(amount)
			index.byOutlet[merchantID+"|"+outTradeNo] = tradeNo
		}
		if err := rows.Err(); err != nil {
			return index, TableReport{}, err
		}
	}

	paymentTypes, err := m.loadPaymentTypes(ctx, source)
	if err != nil {
		return index, TableReport{}, err
	}
	unmapped := map[int64]int64{}

	statement := source.selectStatement(ctx, "pre_order",
		[]string{"trade_no", "out_trade_no", "uid", "type", "name", "money", "addtime", "status"},
		[]string{"api_trade_no", "notify_url", "return_url", "param", "endtime", "ip"},
	)
	rows, err := source.db.QueryContext(ctx, statement)
	if err != nil {
		return index, TableReport{}, fmt.Errorf("读取 pre_order 失败: %w", err)
	}
	defer rows.Close()

	now := m.options.clock()
	var (
		sourceRows    int64
		sourceAmount  int64
		targetAmount  int64
		frozenOrders  int64
		unknownStatus int64
		pending       []pendingInsert
		amountByKey   = map[string]int64{}
	)
	for rows.Next() {
		var (
			tradeNo, outTradeNo, name, money     sql.NullString
			apiTradeNo, notifyURL, returnURL     sql.NullString
			param, addtimeRaw, endtimeRaw, ipRaw sql.NullString
			uid, typeID, status                  sql.NullInt64
		)
		if err := rows.Scan(&tradeNo, &outTradeNo, &uid, &typeID, &name, &money, &addtimeRaw, &status,
			&apiTradeNo, &notifyURL, &returnURL, &param, &endtimeRaw, &ipRaw); err != nil {
			return index, TableReport{}, err
		}
		sourceRows++
		cents, parseErr := ParseMoneyCents(money.String)
		if parseErr != nil {
			counts.fail(moneyReason(parseErr), fmt.Sprintf("trade_no=%s money=%q", tradeNo.String, money.String))
			continue
		}
		if cents < 0 {
			cents = 0
		}
		sourceAmount += cents
		if _, ok := index.ids[tradeNo.String]; ok {
			counts.skip("existing_trade_no")
			targetAmount += index.amounts[tradeNo.String]
			continue
		}
		merchantID, ok := merchants.id(intValue(uid))
		if !ok {
			counts.fail("merchant_missing", fmt.Sprintf("trade_no=%s uid=%d", tradeNo.String, intValue(uid)))
			continue
		}
		outletKey := merchantID + "|" + outTradeNo.String
		if holder, ok := index.byOutlet[outletKey]; ok && holder != tradeNo.String {
			counts.skip("out_trade_no_conflict")
			continue
		}
		orderStatus, statusNote := legacyOrderStatus(intValue(status))
		switch statusNote {
		case "frozen":
			frozenOrders++
		case "unknown":
			unknownStatus++
		}
		paymentType, mapped := paymentTypes[intValue(typeID)]
		if !mapped {
			paymentType = "epay-type-" + strconv.FormatInt(intValue(typeID), 10)
			unmapped[intValue(typeID)]++
		}
		createdAt := timeOr(addtimeRaw.String, now)
		updatedAt := createdAt
		var paidAt any
		if endtime, ok := parseLegacyTime(endtimeRaw.String); ok {
			updatedAt = laterOf(createdAt, endtime)
			if orderStatus == paymentmodel.StatusPaid {
				value := endtime
				paidAt = value
			}
		} else if orderStatus == paymentmodel.StatusPaid {
			paidAt = createdAt
		}
		expiresAt := createdAt.Add(orderExpiryOffset)
		id := deterministicID("order", tradeNo.String)
		index.ids[tradeNo.String] = id
		index.amounts[tradeNo.String] = cents
		index.byOutlet[outletKey] = tradeNo.String
		targetAmount += cents
		amountByKey[tradeNo.String] = cents
		displayName := strings.TrimSpace(name.String)
		if displayName == "" {
			displayName = strings.TrimSpace(outTradeNo.String)
		}
		pending = append(pending, pendingInsert{
			key: tradeNo.String,
			args: []any{
				id,
				tradeNo.String,
				merchantID,
				trimUTF8(outTradeNo.String, maxOutTradeNoLength),
				paymentType,
				trimUTF8(displayName, maxOrderNameLength),
				cents,
				"CNY",
				string(orderStatus),
				strings.TrimSpace(notifyURL.String),
				nullString(returnURL.String),
				nullableIP(ipRaw.String),
				nullString(param.String),
				nullableTrimmed(apiTradeNo.String, 128),
				paidAt,
				expiresAt,
				createdAt,
				updatedAt,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return index, TableReport{}, err
	}
	if err := m.writeInserts(ctx, target, insertOrderStatement, pending, counts); err != nil {
		return index, TableReport{}, err
	}
	// Rows that failed to write are not part of the target, so they must not be
	// counted there: that keeps the amount reconciliation honest.
	for key := range counts.failureKeys {
		targetAmount -= amountByKey[key]
		delete(index.ids, key)
		delete(index.amounts, key)
	}

	report := finishTable("payment_orders", "pre_order", "payment_orders", sourceRows, counts, sourceAmount, targetAmount, comparable)
	notes := []string{}
	if len(unmapped) > 0 {
		ids := make([]string, 0, len(unmapped))
		for typeID := range unmapped {
			ids = append(ids, strconv.FormatInt(typeID, 10))
		}
		notes = append(notes, fmt.Sprintf("未映射的支付类型 id: %s（写入 payment_type=epay-type-<id>）", strings.Join(ids, ",")))
	}
	if frozenOrders > 0 {
		notes = append(notes, fmt.Sprintf("%d 笔冻结订单按 paid 迁移", frozenOrders))
	}
	if unknownStatus > 0 {
		notes = append(notes, fmt.Sprintf("%d 笔未知状态按 closed 迁移", unknownStatus))
	}
	report.Note = strings.Join(notes, "；")
	return index, report, nil
}

// legacyOrderStatus maps the Epay status column onto the payment order lifecycle.
func legacyOrderStatus(legacy int64) (paymentmodel.Status, string) {
	switch legacy {
	case 0:
		return paymentmodel.StatusPending, ""
	case 1:
		return paymentmodel.StatusPaid, ""
	case 2:
		return paymentmodel.StatusClosed, ""
	case 3:
		return paymentmodel.StatusPaid, "frozen"
	default:
		return paymentmodel.StatusClosed, "unknown"
	}
}

// loadPaymentTypes resolves pre_order.type through pre_type, falling back to the
// install.sql seed rows when the table or row is absent.
func (m *Migrator) loadPaymentTypes(ctx context.Context, source *sourceDB) (map[int64]string, error) {
	types := make(map[int64]string, len(defaultEpayPaymentTypes))
	for id, name := range defaultEpayPaymentTypes {
		types[id] = name
	}
	if source.hasTable("pre_type") {
		rows, err := source.db.QueryContext(ctx, "SELECT id, name FROM pre_type")
		if err != nil {
			return nil, fmt.Errorf("读取 pre_type 失败: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var id sql.NullInt64
			var name sql.NullString
			if err := rows.Scan(&id, &name); err != nil {
				return nil, err
			}
			if normalized := normalizePaymentType(name.String); normalized != "" {
				types[intValue(id)] = normalized
			}
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return types, nil
}

func normalizePaymentType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return ""
	}
	known := map[string]string{
		"alipay": "alipay", "支付宝": "alipay",
		"wxpay": "wxpay", "wechat": "wxpay", "weixin": "wxpay", "微信": "wxpay", "微信支付": "wxpay",
		"qqpay": "qqpay", "qq钱包": "qqpay",
		"bank": "bank", "网银": "bank", "网银支付": "bank",
		"jdpay": "jdpay", "paypal": "paypal", "douyinpay": "douyinpay",
	}
	if mapped, ok := known[normalized]; ok {
		return mapped
	}
	return trimUTF8(normalized, 32)
}

// nullableTrimmed shortens an optional value and maps the empty string to NULL.
func nullableTrimmed(value string, limit int) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return trimUTF8(trimmed, limit)
}

// nullableIP keeps client_ip inside VARCHAR(45) or drops it.
func nullableIP(value string) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(trimmed) > 45 {
		return nil
	}
	return trimmed
}
