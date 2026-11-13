package epay

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorixpay/internal/platform/secret"
)

// Migrator runs the Epay → GorixPay data migration.
type Migrator struct {
	options Options
}

// New validates the options and returns a migrator. Nothing touches a database
// before Run is called.
func New(options Options) (*Migrator, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	return &Migrator{options: options}, nil
}

// Run performs preflight and, unless blocked or dry-running, the migration. A
// returned error means the run itself broke (exit code 1); data problems are
// expressed inside the report (exit code 2).
func (m *Migrator) Run(ctx context.Context) (Report, error) {
	started := time.Now()
	report := Report{
		Tool:             ToolVersion,
		GeneratedAt:      started.UTC(),
		Mode:             "apply",
		DryRun:           m.options.DryRun,
		KnownLimitations: KnownLimitations,
		Warnings:         []string{},
		Blocking:         []BlockingItem{},
		Tables:           []TableReport{},
	}
	if m.options.DryRun {
		report.Mode = "dry-run"
	}
	report.Source = SourceReport{
		Driver: normalizeDriver(m.options.Source.Driver),
		DSN:    redactDSN(m.options.Source.Driver, m.options.Source.DSN),
	}
	report.Target = TargetReport{
		Driver:   normalizeDriver(m.options.Target.Driver),
		DSN:      redactDSN(m.options.Target.Driver, m.options.Target.DSN),
		Writable: !m.options.DryRun,
	}

	if err := m.validateMasterKey(); err != nil {
		return report, err
	}

	source, err := openSource(ctx, m.options.Source)
	if err != nil {
		return report, err
	}
	defer source.Close()
	report.Source.Tables = source.tableNames()
	report.Source.MissingTables = missingSourceTables(source)

	sourceCounts, err := m.sourceCounts(ctx, source)
	if err != nil {
		return report, err
	}
	m.logf("源库已连接（%s），共 %d 张表", report.Source.Driver, len(report.Source.Tables))

	preflight, blocking, warnings := m.preflight(ctx, source)
	report.Preflight = preflight
	report.Blocking = append(report.Blocking, blocking...)
	report.Warnings = append(report.Warnings, warnings...)
	if !preflight.OK {
		report.Target.Note = "预检阻断，未连接目标库（未写入任何数据）"
		m.logf("预检发现 %d 个阻断项，迁移未执行", len(blocking))
		m.finish(&report, started)
		return report, nil
	}
	m.logf("预检通过（必需表 %d/%d）", countTrue(preflight.RequiredTables), len(preflight.RequiredTables))

	target, err := m.prepareTarget(ctx, &report)
	if err != nil {
		return report, err
	}
	defer target.Close()

	merchants, merchantReport, err := m.migrateMerchants(ctx, source, target)
	if err != nil {
		return report, err
	}
	report.Tables = append(report.Tables, merchantReport)

	orders, orderReport, err := m.migrateOrders(ctx, source, target, merchants)
	if err != nil {
		return report, err
	}
	report.Tables = append(report.Tables, orderReport)

	refundReport, err := m.migrateRefunds(ctx, source, target, merchants, orders)
	if err != nil {
		return report, err
	}
	report.Tables = append(report.Tables, refundReport)

	settlementReport, err := m.migrateSettlements(ctx, source, target, merchants)
	if err != nil {
		return report, err
	}
	report.Tables = append(report.Tables, settlementReport)

	transferReport, err := m.migrateTransfers(ctx, source, target, merchants)
	if err != nil {
		return report, err
	}
	report.Tables = append(report.Tables, transferReport)

	domainReport, err := m.migrateDomains(ctx, source, target, merchants)
	if err != nil {
		return report, err
	}
	report.Tables = append(report.Tables, domainReport)

	blacklistReport, err := m.migrateBlacklist(ctx, source, target)
	if err != nil {
		return report, err
	}
	report.Tables = append(report.Tables, blacklistReport)

	settingsReport, err := m.migrateSettings(ctx, source, target)
	if err != nil {
		return report, err
	}
	report.Tables = append(report.Tables, settingsReport)

	balances, err := m.migrateBalances(ctx, source, target, merchants)
	if err != nil {
		return report, err
	}
	report.Balances = balances

	for _, table := range report.Tables {
		if table.Failed > 0 {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s 有 %d 行失败，见失败原因", table.TargetTable, table.Failed))
		}
		if table.Source != sourceCounts[table.SourceTable] {
			report.Warnings = append(report.Warnings,
				fmt.Sprintf("%s 源记录数 %d 与预检时的 %d 不一致（源库在迁移过程中被修改？）",
					table.SourceTable, table.Source, sourceCounts[table.SourceTable]))
		}
	}
	sort.Strings(report.Warnings)
	m.finish(&report, started)
	m.logf("运行完成：新增/跳过/失败 见报告，退出码 %d", report.ExitCode)
	return report, nil
}

// finish computes the derived report fields (amount comparison, exit code, duration).
func (m *Migrator) finish(report *Report, started time.Time) {
	report.AmountsEqual = report.AmountsMatch()
	report.DurationMS = time.Since(started).Milliseconds()
	report.ExitCode = exitSuccess
	if len(report.Blocking) > 0 {
		report.ExitCode = exitBlocked
		return
	}
	if !report.AmountsEqual {
		report.ExitCode = exitBlocked
	}
}

// Exit codes shared with the CLI.
const (
	exitSuccess = 0
	exitBlocked = 2
)

func (m *Migrator) logf(format string, arguments ...any) {
	if m.options.Logf == nil {
		return
	}
	m.options.Logf(format, arguments...)
}

func (m *Migrator) validateMasterKey() error {
	path := strings.TrimSpace(m.options.MasterKeyFile)
	if path == "" {
		return nil
	}
	key, err := secret.LoadKeyFile(path)
	if err != nil {
		return fmt.Errorf("读取 master key 失败: %w", err)
	}
	for index := range key {
		key[index] = 0
	}
	m.logf("master key 已校验（%s）", path)
	return nil
}

// prepareTarget opens the target for a dry run (read only, may be absent) or for
// a real run (migrations applied, auxiliary tables ensured).
func (m *Migrator) prepareTarget(ctx context.Context, report *Report) (*targetStore, error) {
	if m.options.DryRun {
		target, note := probeTarget(ctx, m.options.Target)
		report.Target.Note = note
		report.Target.Opened = target != nil
		if target != nil {
			m.logf("目标库只读打开用于对比（不写入）")
		} else if note != "" {
			m.logf("目标库未打开：%s", note)
		}
		return target, nil
	}
	target, err := openTargetStore(ctx, m.options.Target, true)
	if err != nil {
		return nil, err
	}
	if err := target.applyMigrations(ctx); err != nil {
		target.Close()
		return nil, err
	}
	if err := target.ensureAuxTables(ctx); err != nil {
		target.Close()
		return nil, err
	}
	report.Target.Opened = true
	report.Target.MigrationsApplied = target.applied
	m.logf("目标库已就绪（新应用 migration %d 个）", target.applied)
	return target, nil
}

// sourceCounts snapshots the row count of every mapped table so the report can
// flag a source that changed under us during the run.
func (m *Migrator) sourceCounts(ctx context.Context, source *sourceDB) (map[string]int64, error) {
	counts := map[string]int64{}
	for _, table := range append(append([]string{}, requiredSourceTables...), optionalSourceTables...) {
		if !source.hasTable(table) {
			counts[table] = 0
			continue
		}
		count, err := source.count(ctx, table)
		if err != nil {
			return nil, fmt.Errorf("统计 %s 行数失败: %w", table, err)
		}
		counts[table] = count
	}
	return counts, nil
}

func missingSourceTables(source *sourceDB) []string {
	missing := []string{}
	for _, table := range append(append([]string{}, requiredSourceTables...), optionalSourceTables...) {
		if !source.hasTable(table) {
			missing = append(missing, table)
		}
	}
	return missing
}
