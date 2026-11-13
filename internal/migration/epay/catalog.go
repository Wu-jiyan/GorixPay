package epay

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

const insertSettingStatement = `INSERT INTO system_settings (setting_key, setting_value, updated_at)
    VALUES (?, ?, ?)`

const insertDomainStatement = `INSERT INTO merchant_domains
    (id, merchant_id, domain, status, rejection_reason, reviewer_id, created_at, reviewed_at)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

const insertBlacklistStatement = `INSERT INTO blacklist_entries
    (id, entry_type, value, remark, created_at)
    VALUES (?, ?, ?, ?, ?)`

// settingWhitelist is the only pre_config → system_settings mapping performed.
// Everything else in pre_config is either a secret, an Epay-only knob, or would
// silently change GorixPay behaviour, so it is reported as skipped instead.
var settingWhitelist = map[string]string{
	"sitename": "site.name",
}

// migrateSettings copies the whitelisted pre_config keys into system_settings.
// The source is re-read here (instead of reusing preflight output) so secret keys
// never have to be carried around in the report.
func (m *Migrator) migrateSettings(ctx context.Context, source *sourceDB, target *targetStore) (TableReport, error) {
	counts := newCounters()
	if !source.hasTable("pre_config") {
		return skippedTable("system_settings", "pre_config", "system_settings", "源表缺失"), nil
	}
	configKeys, err := m.readConfig(ctx, source)
	if err != nil {
		return TableReport{}, err
	}
	existing := map[string]bool{}
	if target != nil && target.hasTable("system_settings") {
		rows, err := target.db.WithContext(ctx).Raw("SELECT setting_key FROM system_settings").Rows()
		if err != nil {
			return TableReport{}, fmt.Errorf("读取目标 system_settings 失败: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err != nil {
				return TableReport{}, err
			}
			existing[key] = true
		}
		if err := rows.Err(); err != nil {
			return TableReport{}, err
		}
	}

	now := m.options.clock()
	var sourceRows int64
	var pending []pendingInsert
	for key, value := range configKeys {
		sourceRows++
		mapping, whitelisted := settingWhitelist[key]
		if !whitelisted {
			counts.skip("not_whitelisted")
			continue
		}
		if existing[mapping] {
			counts.skip("existing_setting")
			continue
		}
		pending = append(pending, pendingInsert{
			key:  mapping,
			args: []any{mapping, strings.TrimSpace(value), now},
		})
		existing[mapping] = true
	}
	if err := m.writeInserts(ctx, target, insertSettingStatement, pending, counts); err != nil {
		return TableReport{}, err
	}
	report := finishTable("system_settings", "pre_config", "system_settings", sourceRows, counts, 0, 0, false)
	report.Note = "只迁移安全白名单键：sitename → site.name；其余键跳过"
	return report, nil
}

// migrateDomains maps pre_domain onto merchant_domains. Epay status:
// 0 待审核 → pending, 1 正常 → approved, 2 拒绝 → rejected.
func (m *Migrator) migrateDomains(ctx context.Context, source *sourceDB, target *targetStore, merchants merchantIndex) (TableReport, error) {
	counts := newCounters()
	if !source.hasTable("pre_domain") {
		return skippedTable("merchant_domains", "pre_domain", "merchant_domains", "源表缺失"), nil
	}
	existing := map[string]bool{}
	if target != nil && target.hasTable("merchant_domains") {
		rows, err := target.db.WithContext(ctx).Raw("SELECT merchant_id, domain FROM merchant_domains").Rows()
		if err != nil {
			return TableReport{}, fmt.Errorf("读取目标 merchant_domains 失败: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var merchantID, domain string
			if err := rows.Scan(&merchantID, &domain); err != nil {
				return TableReport{}, err
			}
			existing[merchantID+"|"+strings.ToLower(domain)] = true
		}
		if err := rows.Err(); err != nil {
			return TableReport{}, err
		}
	}

	statement := source.selectStatement(ctx, "pre_domain",
		[]string{"id", "uid", "domain", "status", "addtime"},
		[]string{"endtime"},
	)
	rows, err := source.db.QueryContext(ctx, statement)
	if err != nil {
		return TableReport{}, fmt.Errorf("读取 pre_domain 失败: %w", err)
	}
	defer rows.Close()

	now := m.options.clock()
	var (
		sourceRows    int64
		unknownStatus int64
		pending       []pendingInsert
	)
	for rows.Next() {
		var domain, addtimeRaw, endtimeRaw sql.NullString
		var id, uid, status sql.NullInt64
		if err := rows.Scan(&id, &uid, &domain, &status, &addtimeRaw, &endtimeRaw); err != nil {
			return TableReport{}, err
		}
		sourceRows++
		trimmed := strings.ToLower(strings.TrimSpace(domain.String))
		if trimmed == "" {
			counts.skip("empty_domain")
			continue
		}
		merchantID, ok := merchants.id(intValue(uid))
		if !ok {
			counts.fail("merchant_missing", fmt.Sprintf("domain_id=%d uid=%d domain=%s", intValue(id), intValue(uid), trimmed))
			continue
		}
		key := merchantID + "|" + trimmed
		if existing[key] {
			counts.skip("duplicate_domain")
			continue
		}
		existing[key] = true
		domainStatus := "pending"
		switch intValue(status) {
		case 1:
			domainStatus = "approved"
		case 2:
			domainStatus = "rejected"
		default:
			if intValue(status) != 0 {
				unknownStatus++
			}
		}
		createdAt := timeOr(addtimeRaw.String, now)
		var reviewedAt any
		if endtime, ok := parseLegacyTime(endtimeRaw.String); ok && domainStatus != "pending" {
			value := endtime
			reviewedAt = value
		}
		pending = append(pending, pendingInsert{
			key: key,
			args: []any{
				deterministicID("domain", key),
				merchantID,
				trimUTF8(trimmed, 191),
				domainStatus,
				nil,
				nil,
				createdAt,
				reviewedAt,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return TableReport{}, err
	}
	if err := m.writeInserts(ctx, target, insertDomainStatement, pending, counts); err != nil {
		return TableReport{}, err
	}
	report := finishTable("merchant_domains", "pre_domain", "merchant_domains", sourceRows, counts, 0, 0, false)
	if unknownStatus > 0 {
		report.Note = fmt.Sprintf("%d 条未知状态按 pending 迁移", unknownStatus)
	}
	return report, nil
}

// legacyBlacklistTypes follows the plan: 1 IP, 2 域名, 3 关键词. The shipped Epay
// release only defines 0 (支付账号/buyer) and 1 (IP), so type 0 is reported as
// unsupported instead of being guessed into a wrong bucket.
var legacyBlacklistTypes = map[int64]string{
	1: "ip",
	2: "domain",
	3: "keyword",
}

// migrateBlacklist maps pre_blacklist onto blacklist_entries.
func (m *Migrator) migrateBlacklist(ctx context.Context, source *sourceDB, target *targetStore) (TableReport, error) {
	counts := newCounters()
	if !source.hasTable("pre_blacklist") {
		return skippedTable("blacklist_entries", "pre_blacklist", "blacklist_entries", "源表缺失"), nil
	}
	existing := map[string]bool{}
	if target != nil && target.hasTable("blacklist_entries") {
		rows, err := target.db.WithContext(ctx).Raw("SELECT entry_type, value FROM blacklist_entries").Rows()
		if err != nil {
			return TableReport{}, fmt.Errorf("读取目标 blacklist_entries 失败: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var entryType, value string
			if err := rows.Scan(&entryType, &value); err != nil {
				return TableReport{}, err
			}
			existing[entryType+"|"+value] = true
		}
		if err := rows.Err(); err != nil {
			return TableReport{}, err
		}
	}

	statement := source.selectStatement(ctx, "pre_blacklist",
		[]string{"id", "type", "content", "addtime"},
		[]string{"endtime", "remark"},
	)
	rows, err := source.db.QueryContext(ctx, statement)
	if err != nil {
		return TableReport{}, fmt.Errorf("读取 pre_blacklist 失败: %w", err)
	}
	defer rows.Close()

	now := m.options.clock()
	var (
		sourceRows   int64
		unsupported  int64
		expiredCount int64
		pending      []pendingInsert
	)
	for rows.Next() {
		var content, addtimeRaw, endtimeRaw, remark sql.NullString
		var id, entryType sql.NullInt64
		if err := rows.Scan(&id, &entryType, &content, &addtimeRaw, &endtimeRaw, &remark); err != nil {
			return TableReport{}, err
		}
		sourceRows++
		value := strings.TrimSpace(content.String)
		if value == "" {
			counts.skip("empty_value")
			continue
		}
		mappedType, ok := legacyBlacklistTypes[intValue(entryType)]
		if !ok {
			counts.skip("unsupported_type")
			unsupported++
			continue
		}
		key := mappedType + "|" + value
		if existing[key] {
			counts.skip("duplicate_entry")
			continue
		}
		existing[key] = true
		createdAt := timeOr(addtimeRaw.String, now)
		if endtime, ok := parseLegacyTime(endtimeRaw.String); ok && endtime.Before(now) {
			expiredCount++
		}
		pending = append(pending, pendingInsert{
			key: key,
			args: []any{
				deterministicID("blacklist", strconv.FormatInt(intValue(id), 10)+"|"+key),
				mappedType,
				trimUTF8(value, 191),
				nullableTrimmed(remark.String, 191),
				createdAt,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return TableReport{}, err
	}
	if err := m.writeInserts(ctx, target, insertBlacklistStatement, pending, counts); err != nil {
		return TableReport{}, err
	}
	report := finishTable("blacklist_entries", "pre_blacklist", "blacklist_entries", sourceRows, counts, 0, 0, false)
	notes := []string{}
	if unsupported > 0 {
		notes = append(notes, fmt.Sprintf("%d 条 type 无对应语义（Epay type=0 为支付账号），已跳过", unsupported))
	}
	if expiredCount > 0 {
		notes = append(notes, fmt.Sprintf("%d 条已过期条目仍按原样迁移", expiredCount))
	}
	report.Note = strings.Join(notes, "；")
	return report, nil
}
