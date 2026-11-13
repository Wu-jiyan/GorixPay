package epay

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorixpay/internal/identity/password"
)

// Fixture totals, kept in one place so a change to the fixture is a single edit.
const (
	fixtureOrderAmountCents    = 3249  // 10.00 + 20.50 + 1.99
	fixtureBalanceCents        = 15629 // 135.79 + 20.50
	fixtureRefundAmountCents   = 100
	fixtureSettleAmountCents   = 1000
	fixtureTransferAmountCents = 500
	fixtureConfigKeys          = 7
)

func fixtureOptions(sourceDSN, targetDSN string, dryRun bool) Options {
	return Options{
		Source: SourceConfig{Driver: "sqlite", DSN: sourceDSN},
		Target: TargetConfig{Driver: "sqlite", DSN: targetDSN},
		DryRun: dryRun,
		// Small batch size exercises the chunked write path.
		BatchSize: 2,
	}
}

func mustRun(t *testing.T, options Options) Report {
	t.Helper()
	migrator, err := New(options)
	if err != nil {
		t.Fatalf("构造迁移器失败: %v", err)
	}
	report, err := migrator.Run(context.Background())
	if err != nil {
		t.Fatalf("执行迁移失败: %v", err)
	}
	return report
}

func tableByTarget(t *testing.T, report Report, target string) TableReport {
	t.Helper()
	for _, table := range report.Tables {
		if table.TargetTable == target {
			return table
		}
	}
	t.Fatalf("报告缺少目标表 %s：%+v", target, report.Tables)
	return TableReport{}
}

func blockingCodes(report Report) map[string]int64 {
	codes := map[string]int64{}
	for _, item := range report.Blocking {
		codes[item.Code]++
	}
	return codes
}

func newTargetDSN(t *testing.T) string {
	t.Helper()
	return filepath.ToSlash(filepath.Join(t.TempDir(), "gorixpay.db"))
}

func TestDryRunDoesNotWriteTarget(t *testing.T) {
	sourceDSN := newSourceFixture(t)
	targetPath := filepath.Join(t.TempDir(), "gorixpay.db")
	targetDSN := filepath.ToSlash(targetPath)

	report := mustRun(t, fixtureOptions(sourceDSN, targetDSN, true))

	if report.Mode != "dry-run" {
		t.Fatalf("mode = %q, want dry-run", report.Mode)
	}
	if report.ExitCode != exitSuccess {
		t.Fatalf("exit code = %d, want 0, blocking=%+v", report.ExitCode, report.Blocking)
	}
	if len(report.Blocking) != 0 {
		t.Fatalf("dry run reported blocking items: %+v", report.Blocking)
	}
	if report.Target.Opened {
		t.Fatal("dry run opened a target database that does not exist")
	}
	if report.Target.Note == "" {
		t.Fatal("dry run should explain why the target was not opened")
	}
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("dry run created the target database: %v", err)
	}

	if table := tableByTarget(t, report, "merchants"); table.Source != 2 || table.Inserted != 2 {
		t.Fatalf("merchants dry run = %+v", table)
	}
	orders := tableByTarget(t, report, "payment_orders")
	if orders.Source != 3 || orders.Inserted != 3 || orders.SourceAmountCents != fixtureOrderAmountCents {
		t.Fatalf("payment_orders dry run = %+v", orders)
	}
	if report.Balances.Posted != 2 || report.Balances.SourceTotalCents != fixtureBalanceCents {
		t.Fatalf("balances dry run = %+v", report.Balances)
	}
	if report.Balances.TargetTotalCents != fixtureBalanceCents {
		t.Fatalf("dry run should project the opening balance, got %d", report.Balances.TargetTotalCents)
	}
}

func TestApplyMigratesEverything(t *testing.T) {
	sourceDSN := newSourceFixture(t)
	targetDSN := newTargetDSN(t)

	report := mustRun(t, fixtureOptions(sourceDSN, targetDSN, false))
	if report.ExitCode != exitSuccess {
		t.Fatalf("exit code = %d, want 0, blocking=%+v", report.ExitCode, report.Blocking)
	}
	if !report.AmountsEqual {
		t.Fatalf("amounts differ: %+v", report.Tables)
	}
	if !report.Target.Opened || !report.Target.Writable {
		t.Fatalf("target report = %+v", report.Target)
	}

	merchants := tableByTarget(t, report, "merchants")
	if merchants.Source != 2 || merchants.Inserted != 2 || merchants.Skipped != 0 || merchants.Failed != 0 {
		t.Fatalf("merchants = %+v", merchants)
	}
	orders := tableByTarget(t, report, "payment_orders")
	if orders.Source != 3 || orders.Inserted != 3 || orders.Failed != 0 {
		t.Fatalf("payment_orders = %+v", orders)
	}
	if orders.SourceAmountCents != fixtureOrderAmountCents || orders.TargetAmountCents != fixtureOrderAmountCents || orders.AmountDiffCents != 0 {
		t.Fatalf("payment_orders amount = %+v", orders)
	}
	refunds := tableByTarget(t, report, "refund_orders")
	if refunds.Inserted != 1 || refunds.SourceAmountCents != fixtureRefundAmountCents || refunds.TargetAmountCents != fixtureRefundAmountCents {
		t.Fatalf("refund_orders = %+v", refunds)
	}
	settlements := tableByTarget(t, report, "settlement_orders")
	if settlements.Inserted != 1 || settlements.SourceAmountCents != fixtureSettleAmountCents || settlements.AmountDiffCents != 0 {
		t.Fatalf("settlement_orders = %+v", settlements)
	}
	transfers := tableByTarget(t, report, "transfer_orders")
	if transfers.Inserted != 1 || transfers.SourceAmountCents != fixtureTransferAmountCents || transfers.AmountDiffCents != 0 {
		t.Fatalf("transfer_orders = %+v", transfers)
	}
	if domains := tableByTarget(t, report, "merchant_domains"); domains.Inserted != 1 || domains.Failed != 0 {
		t.Fatalf("merchant_domains = %+v", domains)
	}
	if blacklist := tableByTarget(t, report, "blacklist_entries"); blacklist.Inserted != 1 || blacklist.Failed != 0 {
		t.Fatalf("blacklist_entries = %+v", blacklist)
	}
	settings := tableByTarget(t, report, "system_settings")
	if settings.Source != fixtureConfigKeys || settings.Inserted != 1 || settings.Skipped != fixtureConfigKeys-1 {
		t.Fatalf("system_settings = %+v", settings)
	}
	if report.Balances.Posted != 2 || report.Balances.SourceTotalCents != fixtureBalanceCents ||
		report.Balances.TargetTotalCents != fixtureBalanceCents || report.Balances.DiffCents != 0 {
		t.Fatalf("balances = %+v", report.Balances)
	}

	db := mustOpenDB(t, targetDSN)
	defer db.Close()

	// merchants: uid → legacy_uid, display_name fallback, status, legacy password.
	if got := queryString(t, db, `SELECT display_name FROM merchants WHERE legacy_uid = 1001`); got != "示例商户A" {
		t.Fatalf("display_name(1001) = %q", got)
	}
	if got := queryString(t, db, `SELECT display_name FROM merchants WHERE legacy_uid = 1002`); got != "second@example.com" {
		t.Fatalf("display_name(1002) = %q, want the email fallback", got)
	}
	if got := queryString(t, db, `SELECT status FROM merchants WHERE legacy_uid = 1001`); got != "active" {
		t.Fatalf("status(1001) = %q", got)
	}
	if got := queryString(t, db, `SELECT status FROM merchants WHERE legacy_uid = 1002`); got != "disabled" {
		t.Fatalf("status(1002) = %q", got)
	}
	if got := queryString(t, db, `SELECT email FROM merchants WHERE legacy_uid = 1001`); got != "shop@example.com" {
		t.Fatalf("email(1001) = %q", got)
	}
	legacyHash := queryString(t, db, `SELECT password_hash FROM merchants WHERE legacy_uid = 1001`)
	if legacyHash != "e120f39b04b9814151ec35e721d94ed1" {
		t.Fatalf("password_hash(1001) = %q, want the preserved Epay md5", legacyHash)
	}
	matched, upgrade, err := password.VerifyLegacyMerchant("merchant1001", legacyHash, "1001")
	if err != nil || !matched || !upgrade {
		t.Fatalf("legacy password verify = (%t, %t, %v), want a verified match that upgrades", matched, upgrade, err)
	}
	if matched, _, _ := password.VerifyLegacyMerchant("wrong-password", legacyHash, "1001"); matched {
		t.Fatal("legacy verification accepted a wrong password")
	}

	// payment orders.
	if got := queryInt(t, db, `SELECT amount FROM payment_orders WHERE trade_no = ?`, "20250102030405001"); got != 1000 {
		t.Fatalf("order 1 amount = %d", got)
	}
	if got := queryString(t, db, `SELECT status FROM payment_orders WHERE trade_no = ?`, "20250102030405001"); got != "paid" {
		t.Fatalf("order 1 status = %q", got)
	}
	if got := queryString(t, db, `SELECT status FROM payment_orders WHERE trade_no = ?`, "20250102030405002"); got != "pending" {
		t.Fatalf("order 2 status = %q", got)
	}
	if got := queryString(t, db, `SELECT status FROM payment_orders WHERE trade_no = ?`, "20250102030405003"); got != "closed" {
		t.Fatalf("order 3 status = %q", got)
	}
	if got := queryString(t, db, `SELECT payment_type FROM payment_orders WHERE trade_no = ?`, "20250102030405002"); got != "wxpay" {
		t.Fatalf("order 2 payment_type = %q", got)
	}
	if got := queryString(t, db, `SELECT channel_trade_no FROM payment_orders WHERE trade_no = ?`, "20250102030405001"); got != "ALI-9990001" {
		t.Fatalf("order 1 channel_trade_no = %q", got)
	}
	if got := queryString(t, db, `SELECT attach FROM payment_orders WHERE trade_no = ?`, "20250102030405001"); got != "uid=42" {
		t.Fatalf("order 1 attach = %q", got)
	}
	if got := queryInt(t, db, `SELECT paid_at IS NOT NULL FROM payment_orders WHERE trade_no = ?`, "20250102030405001"); got != 1 {
		t.Fatal("paid order has no paid_at")
	}
	if got := queryInt(t, db, `SELECT paid_at IS NULL FROM payment_orders WHERE trade_no = ?`, "20250102030405002"); got != 1 {
		t.Fatal("pending order should not have paid_at")
	}

	// refunds point at the migrated payment order.
	orderID := queryString(t, db, `SELECT id FROM payment_orders WHERE trade_no = ?`, "20250102030405001")
	if got := queryString(t, db, `SELECT payment_order_id FROM refund_orders WHERE refund_no = ?`, "R20250102030405001"); got != orderID {
		t.Fatalf("refund payment_order_id = %q, want %q", got, orderID)
	}
	if got := queryInt(t, db, `SELECT amount FROM refund_orders WHERE refund_no = ?`, "R20250102030405001"); got != fixtureRefundAmountCents {
		t.Fatalf("refund amount = %d", got)
	}
	if got := queryString(t, db, `SELECT status FROM refund_orders WHERE refund_no = ?`, "R20250102030405001"); got != "approved" {
		t.Fatalf("refund status = %q", got)
	}

	// settlements and transfers keep the Epay batch/id identity.
	if got := queryString(t, db, `SELECT settlement_no FROM settlement_orders`); got != "epay-settle-7" {
		t.Fatalf("settlement_no = %q", got)
	}
	if got := queryString(t, db, `SELECT out_settlement_no FROM settlement_orders`); got != "20250103010000-7" {
		t.Fatalf("out_settlement_no = %q", got)
	}
	if got := queryString(t, db, `SELECT destination FROM settlement_orders`); got != "支付宝 shop@example.com 示例商户A" {
		t.Fatalf("settlement destination = %q", got)
	}
	if got := queryString(t, db, `SELECT status FROM settlement_orders`); got != "approved" {
		t.Fatalf("settlement status = %q", got)
	}
	if got := queryString(t, db, `SELECT transfer_no FROM transfer_orders`); got != "T20250103010000001" {
		t.Fatalf("transfer_no = %q", got)
	}
	if got := queryString(t, db, `SELECT status FROM transfer_orders`); got != "approved" {
		t.Fatalf("transfer status = %q", got)
	}

	// domains and blacklist.
	if got := queryString(t, db, `SELECT status FROM merchant_domains WHERE domain = ?`, "shop.example.com"); got != "approved" {
		t.Fatalf("domain status = %q", got)
	}
	if got := queryString(t, db, `SELECT entry_type FROM blacklist_entries WHERE value = ?`, "203.0.113.7"); got != "ip" {
		t.Fatalf("blacklist entry_type = %q", got)
	}

	// settings: only the whitelisted key.
	if got := countRows(t, db, "system_settings", ""); got != 1 {
		t.Fatalf("system_settings rows = %d, want only the whitelisted key", got)
	}
	if got := queryString(t, db, `SELECT setting_value FROM system_settings WHERE setting_key = ?`, "site.name"); got != "Epay Fixture 支付平台" {
		t.Fatalf("site.name = %q", got)
	}

	// opening balance.
	if got := countRows(t, db, "ledger_transactions", "reference_type = ?", "merchant-opening-balance"); got != 2 {
		t.Fatalf("opening balance transactions = %d", got)
	}
	merchantID := queryString(t, db, `SELECT id FROM merchants WHERE legacy_uid = 1001`)
	balance := queryInt(t, db, `SELECT COALESCE(SUM(b.available), 0)
        FROM ledger_balances b JOIN ledger_accounts a ON a.id = b.account_id
        WHERE a.owner_type = 'merchant' AND a.purpose = 'balance' AND a.owner_id = ?`, merchantID)
	if balance != 13579 {
		t.Fatalf("merchant 1001 balance = %d, want 13579", balance)
	}
	if got := queryInt(t, db, `SELECT COALESCE(SUM(b.available), 0)
        FROM ledger_balances b JOIN ledger_accounts a ON a.id = b.account_id
        WHERE a.owner_type = 'merchant' AND a.purpose = 'balance'`); got != fixtureBalanceCents {
		t.Fatalf("total merchant balance = %d, want %d", got, fixtureBalanceCents)
	}
	if got := queryInt(t, db, `SELECT COUNT(*) FROM ledger_accounts WHERE owner_type = 'platform' AND purpose = 'cash'`); got != 1 {
		t.Fatalf("platform cash accounts = %d, want 1", got)
	}
	if got := queryInt(t, db, `SELECT COUNT(*) FROM ledger_entries e
        JOIN ledger_transactions t ON t.id = e.transaction_id
        WHERE t.idempotency_key = ?`, "opening-balance:1001"); got != 2 {
		t.Fatalf("opening-balance:1001 entries = %d, want merchant credit + platform debit", got)
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	sourceDSN := newSourceFixture(t)
	targetDSN := newTargetDSN(t)

	first := mustRun(t, fixtureOptions(sourceDSN, targetDSN, false))
	if first.ExitCode != exitSuccess {
		t.Fatalf("first run exit code = %d", first.ExitCode)
	}
	db := mustOpenDB(t, targetDSN)
	defer db.Close()
	tables := []string{"merchants", "payment_orders", "refund_orders", "settlement_orders",
		"transfer_orders", "merchant_domains", "blacklist_entries", "system_settings",
		"ledger_transactions", "ledger_entries"}
	before := map[string]int64{}
	for _, table := range tables {
		before[table] = countRows(t, db, table, "")
	}

	second := mustRun(t, fixtureOptions(sourceDSN, targetDSN, false))
	if second.ExitCode != exitSuccess {
		t.Fatalf("second run exit code = %d, blocking=%+v", second.ExitCode, second.Blocking)
	}
	if !second.AmountsEqual {
		t.Fatalf("second run amounts differ: %+v", second.Tables)
	}
	for _, table := range second.Tables {
		if table.Inserted != 0 {
			t.Fatalf("second run inserted %d rows into %s, want 0", table.Inserted, table.TargetTable)
		}
		if table.Failed != 0 {
			t.Fatalf("second run failed %d rows in %s: %+v", table.Failed, table.TargetTable, table.FailureReasons)
		}
	}
	if second.Balances.Posted != 0 || second.Balances.Skipped != 2 {
		t.Fatalf("second run balances = %+v, want all skipped", second.Balances)
	}
	for _, table := range tables {
		if after := countRows(t, db, table, ""); after != before[table] {
			t.Fatalf("%s row count changed on the second run: %d → %d", table, before[table], after)
		}
	}

	// The skipped counts must account for every source row.
	expectations := map[string]int64{
		"merchants":         2,
		"payment_orders":    3,
		"refund_orders":     1,
		"settlement_orders": 1,
		"transfer_orders":   1,
		"merchant_domains":  1,
		"blacklist_entries": 1,
	}
	for targetTable, want := range expectations {
		table := tableByTarget(t, second, targetTable)
		if table.Skipped != want || table.Source != want {
			t.Fatalf("%s second run = %+v, want %d skipped of %d", targetTable, table, want, want)
		}
	}
}

func TestNegativeAmountBlocksPreflight(t *testing.T) {
	sourceDSN := newSourceFixture(t)
	execOn(t, sourceDSN, `UPDATE pre_order SET money = '-10.00' WHERE trade_no = '20250102030405003'`)
	targetPath := filepath.Join(t.TempDir(), "gorixpay.db")
	targetDSN := filepath.ToSlash(targetPath)

	report := mustRun(t, fixtureOptions(sourceDSN, targetDSN, true))
	if report.ExitCode != exitBlocked {
		t.Fatalf("exit code = %d, want 2", report.ExitCode)
	}
	codes := blockingCodes(report)
	if codes["negative_amount"] == 0 {
		t.Fatalf("blocking codes = %+v, want negative_amount", codes)
	}
	if report.Preflight.OK {
		t.Fatal("preflight should not be OK")
	}
	if len(report.Tables) != 0 {
		t.Fatalf("blocked run must not migrate anything: %+v", report.Tables)
	}

	// A blocked apply must not create or touch the target either.
	applyReport := mustRun(t, fixtureOptions(sourceDSN, targetDSN, false))
	if applyReport.ExitCode != exitBlocked {
		t.Fatalf("apply exit code = %d, want 2", applyReport.ExitCode)
	}
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("blocked apply created the target database: %v", err)
	}
}

func TestAmountPrecisionBlocksPreflight(t *testing.T) {
	sourceDSN := newSourceFixture(t)
	execOn(t, sourceDSN, `UPDATE pre_user SET money = '1.005' WHERE uid = 1001`)
	targetDSN := newTargetDSN(t)

	report := mustRun(t, fixtureOptions(sourceDSN, targetDSN, true))
	if report.ExitCode != exitBlocked {
		t.Fatalf("exit code = %d, want 2", report.ExitCode)
	}
	if blockingCodes(report)["amount_precision"] == 0 {
		t.Fatalf("blocking = %+v, want amount_precision", report.Blocking)
	}
}

func TestDuplicateTradeNoBlocksPreflight(t *testing.T) {
	sourceDSN := newSourceFixture(t)
	// Rebuild pre_order without its primary key, then insert a duplicate trade_no,
	// which is what a fork without the constraint would look like.
	execOn(t, sourceDSN,
		`CREATE TABLE pre_order_dup AS SELECT * FROM pre_order`,
		`DROP TABLE pre_order`,
		`ALTER TABLE pre_order_dup RENAME TO pre_order`,
		`INSERT INTO pre_order (trade_no, out_trade_no, uid, type, name, money, addtime, status)
         VALUES ('20250102030405001', 'DUP-0001', 1001, 1, '重复单', '1.00', '2025-01-05 00:00:00', 1)`,
	)
	targetDSN := newTargetDSN(t)

	report := mustRun(t, fixtureOptions(sourceDSN, targetDSN, true))
	if report.ExitCode != exitBlocked {
		t.Fatalf("exit code = %d, want 2", report.ExitCode)
	}
	if blockingCodes(report)["duplicate_trade_no"] == 0 {
		t.Fatalf("blocking = %+v, want duplicate_trade_no", report.Blocking)
	}
}

func TestMissingTableBlocksPreflight(t *testing.T) {
	sourceDSN := newSourceFixture(t)
	execOn(t, sourceDSN, `DROP TABLE pre_domain`)
	targetDSN := newTargetDSN(t)

	report := mustRun(t, fixtureOptions(sourceDSN, targetDSN, true))
	if report.ExitCode != exitBlocked {
		t.Fatalf("exit code = %d, want 2", report.ExitCode)
	}
	if blockingCodes(report)["missing_table"] == 0 {
		t.Fatalf("blocking = %+v, want missing_table", report.Blocking)
	}
	if report.Preflight.RequiredTables["pre_domain"] {
		t.Fatal("pre_domain should be reported missing")
	}
}

func TestDryRunOnMigratedTargetWritesNothing(t *testing.T) {
	sourceDSN := newSourceFixture(t)
	targetDSN := newTargetDSN(t)

	if report := mustRun(t, fixtureOptions(sourceDSN, targetDSN, false)); report.ExitCode != exitSuccess {
		t.Fatalf("首次实跑 exit = %d", report.ExitCode)
	}
	db := mustOpenDB(t, targetDSN)
	defer db.Close()
	before := countRows(t, db, "payment_orders", "")
	entriesBefore := countRows(t, db, "ledger_entries", "")

	report := mustRun(t, fixtureOptions(sourceDSN, targetDSN, true))
	if report.ExitCode != exitSuccess {
		t.Fatalf("dry-run exit = %d, blocking=%+v", report.ExitCode, report.Blocking)
	}
	if !report.Target.Opened || report.Target.Writable {
		t.Fatalf("dry-run on an existing target = %+v, want opened and read only", report.Target)
	}
	for _, table := range report.Tables {
		if table.Inserted != 0 || table.Failed != 0 {
			t.Fatalf("dry-run inserted rows into %s: %+v", table.TargetTable, table)
		}
	}
	if after := countRows(t, db, "payment_orders", ""); after != before {
		t.Fatalf("dry-run changed payment_orders: %d → %d", before, after)
	}
	if after := countRows(t, db, "ledger_entries", ""); after != entriesBefore {
		t.Fatalf("dry-run changed ledger_entries: %d → %d", entriesBefore, after)
	}
}

func TestMasterKeyFileIsValidated(t *testing.T) {
	sourceDSN := newSourceFixture(t)
	targetDSN := newTargetDSN(t)
	directory := t.TempDir()

	good := filepath.Join(directory, "master.key")
	if err := os.WriteFile(good, bytes.Repeat([]byte{'a'}, 32), 0o600); err != nil {
		t.Fatalf("写 master key 失败: %v", err)
	}
	options := fixtureOptions(sourceDSN, targetDSN, true)
	options.MasterKeyFile = good
	mustRun(t, options)

	short := filepath.Join(directory, "short.key")
	if err := os.WriteFile(short, []byte("too-short"), 0o600); err != nil {
		t.Fatalf("写 master key 失败: %v", err)
	}
	options.MasterKeyFile = short
	if migrator, err := New(options); err == nil {
		if _, runErr := migrator.Run(context.Background()); runErr == nil {
			t.Fatal("长度错误的 master key 未被拒绝")
		}
	}

	options.MasterKeyFile = filepath.Join(directory, "missing.key")
	if migrator, err := New(options); err == nil {
		if _, runErr := migrator.Run(context.Background()); runErr == nil {
			t.Fatal("缺失的 master key 文件未被拒绝")
		}
	}
}

func TestReportDoesNotLeakPreConfigSecrets(t *testing.T) {
	sourceDSN := newSourceFixture(t)
	targetDSN := newTargetDSN(t)

	report := mustRun(t, fixtureOptions(sourceDSN, targetDSN, true))
	if _, ok := report.Preflight.ConfigKeys["mail_pwd"]; ok {
		t.Fatal("preflight report contains a non-whitelisted pre_config key")
	}
	if got := report.Preflight.ConfigKeys["version"]; got != "2055" {
		t.Fatalf("preflight version = %q, want 2055", got)
	}
	payload, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("编码报告失败: %v", err)
	}
	if strings.Contains(string(payload), "smtp-secret-not-migrated") {
		t.Fatal("report leaked a pre_config secret value")
	}
}

func TestReportsAreWritten(t *testing.T) {
	sourceDSN := newSourceFixture(t)
	targetDSN := newTargetDSN(t)
	prefix := filepath.Join(t.TempDir(), "migrate-report")

	options := fixtureOptions(sourceDSN, targetDSN, false)
	options.ReportPrefix = prefix
	report := mustRun(t, options)
	paths, err := WriteReports(prefix, report)
	if err != nil {
		t.Fatalf("写报告失败: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("报告文件 = %v", paths)
	}
	payload, err := os.ReadFile(prefix + ".json")
	if err != nil {
		t.Fatalf("读取 json 报告失败: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("json 报告无法解析: %v", err)
	}
	for _, key := range []string{"mode", "tables", "balances", "blocking", "exit_code", "preflight"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("json 报告缺少字段 %q", key)
		}
	}
	markdown, err := os.ReadFile(prefix + ".md")
	if err != nil {
		t.Fatalf("读取 md 报告失败: %v", err)
	}
	for _, want := range []string{"# Epay → GorixPay 迁移报告", "## 迁移计数", "## 商户余额期初", "## 已知限制"} {
		if !strings.Contains(string(markdown), want) {
			t.Fatalf("md 报告缺少 %q:\n%s", want, markdown)
		}
	}
}

func TestWriteReportsStripsKnownExtension(t *testing.T) {
	prefix := filepath.Join(t.TempDir(), "report")
	paths, err := WriteReports(prefix+".md", Report{Tool: "test", GeneratedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("写报告失败: %v", err)
	}
	for _, path := range paths {
		if filepath.Ext(path) != ".json" && filepath.Ext(path) != ".md" {
			t.Fatalf("意外扩展名: %s", path)
		}
		if strings.Count(filepath.Base(path), ".") != 1 {
			t.Fatalf("扩展名被重复追加: %s", path)
		}
	}
}
