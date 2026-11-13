package epay

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	identitymodel "gorixpay/internal/identity/model"
	"gorixpay/internal/identity/password"
)

// merchantIndex maps a legacy uid to the GorixPay merchant id and is the
// reference every other table needs.
type merchantIndex struct {
	ids map[int64]string
}

func newMerchantIndex() merchantIndex {
	return merchantIndex{ids: map[int64]string{}}
}

func (index merchantIndex) id(legacyUID int64) (string, bool) {
	id, ok := index.ids[legacyUID]
	return id, ok
}

const insertMerchantStatement = `INSERT INTO merchants
    (id, legacy_uid, email, phone, password_hash, display_name, status, created_at, updated_at)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

// migrateMerchants maps pre_user onto merchants. The Epay password column already
// holds md5(md5(pwd) . md5('1277180438' . uid)), which is exactly what
// password.VerifyLegacyMerchant recomputes, so it is copied verbatim and the
// normal login flow upgrades it to Argon2id on first use.
func (m *Migrator) migrateMerchants(ctx context.Context, source *sourceDB, target *targetStore) (merchantIndex, TableReport, error) {
	index := newMerchantIndex()
	counts := newCounters()
	if !source.hasTable("pre_user") {
		return index, skippedTable("merchants", "pre_user", "merchants", "源表缺失"), nil
	}
	if target != nil && target.hasTable("merchants") {
		rows, err := target.db.WithContext(ctx).Raw("SELECT id, legacy_uid FROM merchants WHERE legacy_uid IS NOT NULL").Rows()
		if err != nil {
			return index, TableReport{}, fmt.Errorf("读取目标 merchants 失败: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			var legacyUID sql.NullInt64
			if err := rows.Scan(&id, &legacyUID); err != nil {
				return index, TableReport{}, err
			}
			index.ids[intValue(legacyUID)] = id
		}
		if err := rows.Err(); err != nil {
			return index, TableReport{}, err
		}
	}

	statement := source.selectStatement(ctx, "pre_user",
		[]string{"uid", "pwd", "status", "addtime"},
		[]string{"username", "account", "email", "phone"},
	)
	rows, err := source.db.QueryContext(ctx, statement)
	if err != nil {
		return index, TableReport{}, fmt.Errorf("读取 pre_user 失败: %w", err)
	}
	defer rows.Close()

	now := m.options.clock()
	var sourceRows int64
	var unusablePasswords int64
	var pending []pendingInsert
	for rows.Next() {
		var (
			uid                  sql.NullInt64
			pwdValue, addtimeRaw sql.NullString
			username, account    sql.NullString
			email, phone         sql.NullString
			status               sql.NullInt64
		)
		if err := rows.Scan(&uid, &pwdValue, &status, &addtimeRaw, &username, &account, &email, &phone); err != nil {
			return index, TableReport{}, err
		}
		sourceRows++
		legacyUID := intValue(uid)
		if _, ok := index.id(legacyUID); ok {
			counts.skip("existing_legacy_uid")
			continue
		}
		createdAt := timeOr(addtimeRaw.String, now)
		merchantStatus := identitymodel.StatusDisabled
		if intValue(status) == 1 {
			merchantStatus = identitymodel.StatusActive
		}
		var passwordHash any
		trimmedPassword := strings.TrimSpace(pwdValue.String)
		if password.IsLegacyMD5(trimmedPassword) {
			passwordHash = strings.ToLower(trimmedPassword)
		} else if trimmedPassword != "" {
			unusablePasswords++
		}
		displayName := merchantDisplayName(username.String, account.String, email.String, phone.String, legacyUID)
		id := deterministicID("merchant", strconv.FormatInt(legacyUID, 10))
		pending = append(pending, pendingInsert{
			key: id,
			args: []any{
				id,
				legacyUID,
				nullString(email.String),
				nullString(phone.String),
				passwordHash,
				displayName,
				string(merchantStatus),
				createdAt,
				createdAt,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return index, TableReport{}, err
	}

	// Resolve the ids first so a failed insert only removes that merchant.
	for _, row := range pending {
		index.ids[row.args[1].(int64)] = row.args[0].(string)
	}
	if err := m.writeInserts(ctx, target, insertMerchantStatement, pending, counts); err != nil {
		return index, TableReport{}, err
	}

	report := finishTable("merchants", "pre_user", "merchants", sourceRows, counts, 0, 0, false)
	if unusablePasswords > 0 {
		report.Note = fmt.Sprintf("%d 个商户的 pwd 不是 32 位 MD5，password_hash 置空（需重置密码）", unusablePasswords)
	}
	return index, report, nil
}

// merchantDisplayName follows the plan: username first, then email/phone when the
// username is empty, then the Epay account name, and finally a synthetic name.
func merchantDisplayName(username, account, email, phone string, legacyUID int64) string {
	for _, candidate := range []string{username, email, phone, account} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimUTF8(trimmed, 128)
		}
	}
	return "epay-" + strconv.FormatInt(legacyUID, 10)
}
