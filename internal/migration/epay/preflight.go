package epay

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// requiredSourceTables must exist before anything is migrated; without them the
// mapping cannot be complete and the run stops with exit code 2.
var requiredSourceTables = []string{
	"pre_user",
	"pre_order",
	"pre_refundorder",
	"pre_settle",
	"pre_config",
	"pre_domain",
	"pre_blacklist",
}

// optionalSourceTables vary between Epay releases and forks. A missing optional
// table downgrades the corresponding mapping to "skipped", never to a failure.
var optionalSourceTables = []string{
	"pre_transfer",
	"pre_type",
}

// requiredConfigKeys is the minimum pre_config content: the schema version tells
// us which Epay shape we are reading and the site name is the one setting that
// maps onto GorixPay's system_settings.
var requiredConfigKeys = []string{"version", "sitename"}

// field limits of the target schema that a legacy value can overflow.
const (
	maxOutTradeNoLength = 64
	maxOrderNameLength  = 255
)

// preflight checks everything that can be verified without writing, then returns
// the blocking list and the warnings.
func (m *Migrator) preflight(ctx context.Context, source *sourceDB) (PreflightReport, []BlockingItem, []string) {
	report := PreflightReport{
		OK:             true,
		RequiredTables: map[string]bool{},
		OptionalTables: map[string]bool{},
		ConfigKeys:     map[string]string{},
	}
	blocking := []BlockingItem{}
	warnings := []string{}
	for _, table := range requiredSourceTables {
		present := source.hasTable(table)
		report.RequiredTables[table] = present
		if !present {
			report.MissingTables = append(report.MissingTables, table)
			blocking = append(blocking, BlockingItem{
				Code:   "missing_table",
				Table:  table,
				Detail: "必需表不存在，无法完整映射",
			})
		}
	}
	for _, table := range optionalSourceTables {
		present := source.hasTable(table)
		report.OptionalTables[table] = present
		if !present {
			warnings = append(warnings, fmt.Sprintf("可选表 %s 不存在，相关映射跳过", table))
		}
	}

	if source.hasTable("pre_config") {
		values, err := m.readConfig(ctx, source)
		if err != nil {
			blocking = append(blocking, BlockingItem{Code: "config_read_failed", Table: "pre_config", Detail: err.Error()})
		} else {
			// Only the keys the mapping actually consumes are reported: pre_config
			// also holds passwords and API keys that must never reach a report.
			for _, key := range requiredConfigKeys {
				if value, ok := values[key]; ok {
					report.ConfigKeys[key] = value
					continue
				}
				blocking = append(blocking, BlockingItem{
					Code:   "missing_config_key",
					Table:  "pre_config",
					Detail: fmt.Sprintf("关键配置项 %s 缺失", key),
					Count:  1,
				})
			}
			if version, ok := values["version"]; ok {
				if _, err := strconv.Atoi(strings.TrimSpace(version)); err != nil {
					warnings = append(warnings, fmt.Sprintf("pre_config.version=%q 不是数字，按未知版本继续", version))
				}
			}
		}
	}

	if source.hasTable("pre_order") {
		duplicates, samples, err := m.duplicateTradeNumbers(ctx, source)
		if err != nil {
			blocking = append(blocking, BlockingItem{Code: "preflight_query_failed", Table: "pre_order", Detail: err.Error()})
		} else if duplicates > 0 {
			blocking = append(blocking, BlockingItem{
				Code:    "duplicate_trade_no",
				Table:   "pre_order",
				Detail:  "pre_order.trade_no 重复，目标表 trade_no 唯一",
				Count:   duplicates,
				Samples: samples,
			})
		}
		duplicates, samples, err = m.duplicateOutTradeNumbers(ctx, source)
		if err != nil {
			blocking = append(blocking, BlockingItem{Code: "preflight_query_failed", Table: "pre_order", Detail: err.Error()})
		} else if duplicates > 0 {
			blocking = append(blocking, BlockingItem{
				Code:    "duplicate_out_trade_no",
				Table:   "pre_order",
				Detail:  "同一商户下 out_trade_no 重复（含空串），目标表 (merchant_id, out_trade_no) 唯一",
				Count:   duplicates,
				Samples: samples,
			})
		}
		if count, samples, err := m.oversizedOrderFields(ctx, source); err != nil {
			blocking = append(blocking, BlockingItem{Code: "preflight_query_failed", Table: "pre_order", Detail: err.Error()})
		} else if count > 0 {
			blocking = append(blocking, BlockingItem{
				Code:    "unmappable_field",
				Table:   "pre_order",
				Detail:  fmt.Sprintf("out_trade_no 超过 %d 字节或 name 超过 %d 字节，无法写入目标列", maxOutTradeNoLength, maxOrderNameLength),
				Count:   count,
				Samples: samples,
			})
		}
	}

	if source.hasTable("pre_user") {
		if duplicates, samples, err := m.duplicateMerchantIdentity(ctx, source); err != nil {
			blocking = append(blocking, BlockingItem{Code: "preflight_query_failed", Table: "pre_user", Detail: err.Error()})
		} else if duplicates > 0 {
			blocking = append(blocking, BlockingItem{
				Code:    "duplicate_merchant_identity",
				Table:   "pre_user",
				Detail:  "email 或 phone 在多个商户间重复，目标表为唯一列",
				Count:   duplicates,
				Samples: samples,
			})
		}
	}

	moneyBlocking := m.checkMoney(ctx, source)
	blocking = append(blocking, moneyBlocking...)

	report.BlockingCount = len(blocking)
	report.WarningCount = len(warnings)
	report.OK = len(blocking) == 0
	return report, blocking, warnings
}

func (m *Migrator) readConfig(ctx context.Context, source *sourceDB) (map[string]string, error) {
	rows, err := source.db.QueryContext(ctx, "SELECT k, v FROM pre_config")
	if err != nil {
		return nil, fmt.Errorf("读取 pre_config 失败: %w", err)
	}
	defer rows.Close()
	values := map[string]string{}
	for rows.Next() {
		var key, value sql.NullString
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		values[key.String] = value.String
	}
	return values, rows.Err()
}

// duplicateTradeNumbers counts pre_order rows whose trade_no repeats.
func (m *Migrator) duplicateTradeNumbers(ctx context.Context, source *sourceDB) (int64, []string, error) {
	rows, err := source.db.QueryContext(ctx, "SELECT trade_no FROM pre_order")
	if err != nil {
		return 0, nil, fmt.Errorf("读取 pre_order.trade_no 失败: %w", err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	var duplicates int64
	samples := []string{}
	for rows.Next() {
		var tradeNo sql.NullString
		if err := rows.Scan(&tradeNo); err != nil {
			return 0, nil, err
		}
		if seen[tradeNo.String] {
			duplicates++
			if len(samples) < maxReportSamples {
				samples = append(samples, tradeNo.String)
			}
			continue
		}
		seen[tradeNo.String] = true
	}
	return duplicates, samples, rows.Err()
}

// duplicateOutTradeNumbers counts pre_order rows whose (uid, out_trade_no) pair
// repeats, which the target's UNIQUE (merchant_id, out_trade_no) would reject.
func (m *Migrator) duplicateOutTradeNumbers(ctx context.Context, source *sourceDB) (int64, []string, error) {
	rows, err := source.db.QueryContext(ctx, "SELECT uid, out_trade_no FROM pre_order")
	if err != nil {
		return 0, nil, fmt.Errorf("读取 pre_order.out_trade_no 失败: %w", err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	var duplicates int64
	samples := []string{}
	for rows.Next() {
		var uid sql.NullInt64
		var outTradeNo sql.NullString
		if err := rows.Scan(&uid, &outTradeNo); err != nil {
			return 0, nil, err
		}
		key := strconv.FormatInt(intValue(uid), 10) + "|" + outTradeNo.String
		if seen[key] {
			duplicates++
			if len(samples) < maxReportSamples {
				samples = append(samples, fmt.Sprintf("%d/%s", intValue(uid), outTradeNo.String))
			}
			continue
		}
		seen[key] = true
	}
	return duplicates, samples, rows.Err()
}

// duplicateMerchantIdentity counts duplicate non-empty email/phone values.
func (m *Migrator) duplicateMerchantIdentity(ctx context.Context, source *sourceDB) (int64, []string, error) {
	statement := source.selectStatement(ctx, "pre_user", []string{"uid"}, []string{"email", "phone"})
	rows, err := source.db.QueryContext(ctx, statement)
	if err != nil {
		return 0, nil, fmt.Errorf("读取 pre_user 身份列失败: %w", err)
	}
	defer rows.Close()
	identities := map[string]bool{}
	var duplicates int64
	samples := []string{}
	for rows.Next() {
		var uid sql.NullInt64
		var email, phone sql.NullString
		if err := rows.Scan(&uid, &email, &phone); err != nil {
			return 0, nil, err
		}
		candidates := []string{
			strings.ToLower(strings.TrimSpace(email.String)),
			strings.TrimSpace(phone.String),
		}
		for _, candidate := range candidates {
			if candidate == "" {
				continue
			}
			if identities[candidate] {
				duplicates++
				if len(samples) < maxReportSamples {
					samples = append(samples, fmt.Sprintf("uid=%d %s", intValue(uid), candidate))
				}
				continue
			}
			identities[candidate] = true
		}
	}
	return duplicates, samples, rows.Err()
}

// oversizedOrderFields counts orders whose out_trade_no or name cannot fit the
// target column, which would otherwise fail row by row at insert time.
func (m *Migrator) oversizedOrderFields(ctx context.Context, source *sourceDB) (int64, []string, error) {
	rows, err := source.db.QueryContext(ctx, "SELECT trade_no, out_trade_no, name FROM pre_order")
	if err != nil {
		return 0, nil, fmt.Errorf("读取 pre_order 字段长度失败: %w", err)
	}
	defer rows.Close()
	var count int64
	samples := []string{}
	for rows.Next() {
		var tradeNo, outTradeNo, name sql.NullString
		if err := rows.Scan(&tradeNo, &outTradeNo, &name); err != nil {
			return 0, nil, err
		}
		if len(outTradeNo.String) > maxOutTradeNoLength || len(name.String) > maxOrderNameLength {
			count++
			if len(samples) < maxReportSamples {
				samples = append(samples, fmt.Sprintf("trade_no=%s out_trade_no=%d 字节 name=%d 字节", tradeNo.String, len(outTradeNo.String), len(name.String)))
			}
		}
	}
	return count, samples, rows.Err()
}

// moneyTarget labels one legacy money column the mapping consumes.
type moneyTarget struct {
	table  string
	key    string
	column string
}

func (m *Migrator) moneyTargets(ctx context.Context, source *sourceDB) []moneyTarget {
	targets := []moneyTarget{}
	appendIf := func(table, key, column string) {
		if !source.hasTable(table) {
			return
		}
		ok, err := source.hasColumn(ctx, table, column)
		if err != nil || !ok {
			return
		}
		targets = append(targets, moneyTarget{table: table, key: key, column: column})
	}
	appendIf("pre_user", "uid", "money")
	appendIf("pre_user", "uid", "deposit")
	appendIf("pre_order", "trade_no", "money")
	appendIf("pre_refundorder", "refund_no", "money")
	appendIf("pre_settle", "id", "money")
	appendIf("pre_settle", "id", "realmoney")
	appendIf("pre_transfer", "biz_no", "money")
	return targets
}

type moneyProblem struct {
	table   string
	column  string
	reason  string
	count   int64
	samples []string
}

// checkMoney scans every money column in Go so the strict decimal parser (and not
// the source dialect) decides what is valid.
func (m *Migrator) checkMoney(ctx context.Context, source *sourceDB) []BlockingItem {
	problems := map[string]*moneyProblem{}
	order := []string{}
	record := func(table, column, reason, sample string) {
		key := table + "." + column + "|" + reason
		problem, ok := problems[key]
		if !ok {
			problem = &moneyProblem{table: table, column: column, reason: reason}
			problems[key] = problem
			order = append(order, key)
		}
		problem.count++
		if len(problem.samples) < maxReportSamples && sample != "" {
			problem.samples = append(problem.samples, sample)
		}
	}
	for _, target := range m.moneyTargets(ctx, source) {
		query := fmt.Sprintf("SELECT %s, %s FROM %s", target.key, target.column, target.table)
		rows, err := source.db.QueryContext(ctx, query)
		if err != nil {
			record(target.table, target.column, "query_failed", err.Error())
			continue
		}
		for rows.Next() {
			var keyValue, moneyValue sql.NullString
			if err := rows.Scan(&keyValue, &moneyValue); err != nil {
				break
			}
			if !moneyValue.Valid {
				// A NULL amount is "absent", not corrupt: optional columns such as
				// pre_user.deposit are NULL for most merchants.
				continue
			}
			if _, err := ParseMoneyCents(moneyValue.String); err != nil {
				record(target.table, target.column, moneyReason(err), fmt.Sprintf("%s=%s 值=%q", target.key, keyValue.String, moneyValue.String))
			}
		}
		rows.Close()
	}
	sort.Strings(order)
	blocking := make([]BlockingItem, 0, len(order))
	for _, key := range order {
		problem := problems[key]
		detail := map[string]string{
			"negative_amount":  "金额为负数，不能迁移",
			"amount_precision": "金额小数位超过两位，无法精确换算为分",
			"amount_syntax":    "金额不是合法十进制字符串",
			"amount_overflow":  "金额超出范围",
			"query_failed":     "读取金额列失败",
		}[problem.reason]
		if detail == "" {
			detail = "金额校验失败"
		}
		code := problem.reason
		if problem.reason == "query_failed" {
			code = "preflight_query_failed"
		}
		blocking = append(blocking, BlockingItem{
			Code:    code,
			Table:   problem.table,
			Detail:  problem.column + "：" + detail,
			Count:   problem.count,
			Samples: problem.samples,
		})
	}
	return blocking
}
