package epay

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const insertPayTypeStatement = `INSERT INTO pay_types
    (id, code, name, icon, sort, status, created_at, updated_at)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

// migratePayTypes maps the legacy pre_type table (支付方式) onto pay_types.
//
// Epay keeps one row per payment type with a machine name (alipay, usdt.trc20)
// and a display name; GorixPay needs the same pair plus a stable sort. Rows are
// keyed by code, so a second run only fills what is still missing and never
// overwrites a name the operator already edited in the console.
func (m *Migrator) migratePayTypes(ctx context.Context, source *sourceDB, target *targetStore) (TableReport, error) {
	counts := newCounters()
	if !source.hasTable("pre_type") {
		return skippedTable("pay_types", "pre_type", "pay_types", "源表缺失"), nil
	}
	existing, err := existingPayTypeCodes(ctx, target)
	if err != nil {
		return TableReport{}, err
	}

	statement := source.selectStatement(ctx, "pre_type",
		[]string{"id", "name", "showname", "status"},
		[]string{"device"},
	)
	rows, err := source.queryContext(ctx, statement)
	if err != nil {
		return TableReport{}, fmt.Errorf("读取 pre_type 失败: %w", err)
	}
	defer rows.Close()

	now := m.options.clock()
	var (
		sourceRows int64
		pending    []pendingInsert
	)
	for rows.Next() {
		var name, showname, device sql.NullString
		var id, status sql.NullInt64
		if err := rows.Scan(&id, &name, &showname, &status, &device); err != nil {
			return TableReport{}, err
		}
		sourceRows++
		code := strings.ToLower(strings.TrimSpace(stringValue(name)))
		if code == "" {
			counts.skip("empty_code")
			continue
		}
		if len([]rune(code)) > 32 {
			counts.fail("code_too_long", fmt.Sprintf("type_id=%d code=%s", intValue(id), code))
			continue
		}
		if existing[code] {
			counts.skip("existing_code")
			continue
		}
		existing[code] = true
		display := strings.TrimSpace(stringValue(showname))
		if display == "" {
			display = code
		}
		typeStatus := "disabled"
		if intValue(status) == 1 {
			typeStatus = "active"
		}
		pending = append(pending, pendingInsert{
			key: code,
			args: []any{
				deterministicID("paytype", code),
				trimUTF8(code, 32),
				trimUTF8(display, 64),
				nil,
				intValue(id),
				typeStatus,
				now,
				now,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return TableReport{}, err
	}
	if err := m.writeInserts(ctx, target, insertPayTypeStatement, pending, counts); err != nil {
		return TableReport{}, err
	}
	report := finishTable("pay_types", "pre_type", "pay_types", sourceRows, counts, 0, 0, false)
	report.Note = "支付方式按 code 幂等写入：已存在的 code 不覆盖，停用的来源状态映射为 disabled"
	return report, nil
}

// existingPayTypeCodes reads the codes already present so the run is repeatable.
func existingPayTypeCodes(ctx context.Context, target *targetStore) (map[string]bool, error) {
	codes := map[string]bool{}
	if target == nil || !target.hasTable("pay_types") {
		return codes, nil
	}
	rows, err := target.db.WithContext(ctx).Raw("SELECT code FROM pay_types").Rows()
	if err != nil {
		return nil, fmt.Errorf("读取目标 pay_types 失败: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		codes[strings.ToLower(code)] = true
	}
	return codes, rows.Err()
}

// legacyPayTypeNames returns pre_type id → machine name, which is how a channel
// row points at the payment type it serves.
func legacyPayTypeNames(ctx context.Context, source *sourceDB) (map[int64]string, error) {
	names := map[int64]string{}
	if !source.hasTable("pre_type") {
		return names, nil
	}
	statement := source.selectStatement(ctx, "pre_type",
		[]string{"id", "name", "showname", "status"},
		[]string{"device"},
	)
	rows, err := source.queryContext(ctx, statement)
	if err != nil {
		return nil, fmt.Errorf("读取 pre_type 失败: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, showname, device sql.NullString
		var id, status sql.NullInt64
		if err := rows.Scan(&id, &name, &showname, &status, &device); err != nil {
			return nil, err
		}
		code := strings.ToLower(strings.TrimSpace(stringValue(name)))
		if code == "" {
			continue
		}
		names[intValue(id)] = code
	}
	return names, rows.Err()
}
