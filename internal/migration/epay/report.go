package epay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ToolVersion identifies the report format and the mapping rules that produced it.
const ToolVersion = "1.0"

// Report is the single artefact of a run: it drives the exit code, the console
// summary and the JSON/Markdown files.
type Report struct {
	Tool             string          `json:"tool"`
	GeneratedAt      time.Time       `json:"generated_at"`
	DurationMS       int64           `json:"duration_ms"`
	Mode             string          `json:"mode"`
	DryRun           bool            `json:"dry_run"`
	Source           SourceReport    `json:"source"`
	Target           TargetReport    `json:"target"`
	Preflight        PreflightReport `json:"preflight"`
	Tables           []TableReport   `json:"tables"`
	Balances         BalanceReport   `json:"balances"`
	Warnings         []string        `json:"warnings"`
	Blocking         []BlockingItem  `json:"blocking"`
	AmountsEqual     bool            `json:"amounts_equal"`
	ExitCode         int             `json:"exit_code"`
	KnownLimitations []string        `json:"known_limitations"`
}

// SourceReport describes the legacy side and which of the expected tables exist.
type SourceReport struct {
	Driver        string   `json:"driver"`
	DSN           string   `json:"dsn"`
	Tables        []string `json:"tables"`
	MissingTables []string `json:"missing_tables"`
}

// TargetReport describes the GorixPay side. Opened is false when a dry run found
// nothing to inspect; Writable is false whenever the run must not write.
type TargetReport struct {
	Driver            string `json:"driver"`
	DSN               string `json:"dsn"`
	Opened            bool   `json:"opened"`
	Writable          bool   `json:"writable"`
	MigrationsApplied int    `json:"migrations_applied"`
	Note              string `json:"note,omitempty"`
}

// PreflightReport summarises the checks that run before any write.
type PreflightReport struct {
	OK             bool              `json:"ok"`
	RequiredTables map[string]bool   `json:"required_tables"`
	MissingTables  []string          `json:"missing_tables"`
	OptionalTables map[string]bool   `json:"optional_tables"`
	ConfigKeys     map[string]string `json:"config_keys"`
	BlockingCount  int               `json:"blocking_count"`
	WarningCount   int               `json:"warning_count"`
}

// TableReport is the per-table outcome. SourceAmountCents and TargetAmountCents
// are compared in apply mode; a diff (or a comparable pair that does not match)
// makes the run exit with code 2.
type TableReport struct {
	Name              string           `json:"name"`
	SourceTable       string           `json:"source_table"`
	TargetTable       string           `json:"target_table"`
	Source            int64            `json:"source"`
	Inserted          int64            `json:"inserted"`
	Skipped           int64            `json:"skipped"`
	Failed            int64            `json:"failed"`
	SkipReasons       map[string]int64 `json:"skip_reasons,omitempty"`
	FailureReasons    map[string]int64 `json:"failure_reasons,omitempty"`
	FailureSamples    []string         `json:"failure_samples,omitempty"`
	SourceAmountCents int64            `json:"source_amount_cents"`
	TargetAmountCents int64            `json:"target_amount_cents"`
	AmountDiffCents   int64            `json:"amount_diff_cents"`
	AmountComparable  bool             `json:"amount_comparable"`
	Note              string           `json:"note,omitempty"`
}

// BalanceReport covers the opening ledger balance built from pre_user.money.
type BalanceReport struct {
	Merchants        int64            `json:"merchants"`
	Posted           int64            `json:"posted"`
	Skipped          int64            `json:"skipped"`
	Failed           int64            `json:"failed"`
	SkipReasons      map[string]int64 `json:"skip_reasons,omitempty"`
	FailureReasons   map[string]int64 `json:"failure_reasons,omitempty"`
	FailureSamples   []string         `json:"failure_samples,omitempty"`
	SourceTotalCents int64            `json:"source_total_cents"`
	TargetTotalCents int64            `json:"target_total_cents"`
	DiffCents        int64            `json:"diff_cents"`
	Comparable       bool             `json:"comparable"`
}

// BlockingItem is a problem that stops the migration; any entry forces exit 2.
type BlockingItem struct {
	Code    string   `json:"code"`
	Table   string   `json:"table,omitempty"`
	Detail  string   `json:"detail"`
	Count   int64    `json:"count,omitempty"`
	Samples []string `json:"samples,omitempty"`
}

// KnownLimitations is repeated verbatim in every report so the operator knows
// what was intentionally left behind.
var KnownLimitations = []string{
	"pre_plugin / pre_channel / pre_wx* 的插件与渠道配置不迁移（含密钥），需在运营后台重新配置。",
	"商户 API 密钥（pre_user.key）与 RSA 公钥不迁移，上线后由商户重新签发。",
	"pre_user 的邀请关系、消息模板、打印配置，以及 pre_order 的分润、子单、退款流水不做映射。",
	"pre_user.deposit（保证金）不迁移，只迁移 money 作为可用余额期初。",
	"源库时间无时区，按 UTC 写入；通知重试状态（pre_order.notify）不迁移。",
	"pre_config 只迁移安全白名单键（sitename → site.name），其余键在报告中列为跳过。",
}

// addBlocking appends a blocking item, keeping the first samples only.
func (report *Report) addBlocking(item BlockingItem) {
	if len(item.Samples) > maxReportSamples {
		item.Samples = item.Samples[:maxReportSamples]
	}
	report.Blocking = append(report.Blocking, item)
}

// AmountsMatch reports whether every comparable amount pair agrees.
func (report Report) AmountsMatch() bool {
	for _, table := range report.Tables {
		if table.AmountComparable && table.AmountDiffCents != 0 {
			return false
		}
	}
	if report.Balances.Comparable && report.Balances.DiffCents != 0 {
		return false
	}
	return true
}

// WriteReports writes "<prefix>.json" and "<prefix>.md". A prefix that already
// ends with one of those extensions is treated as the prefix without it, so
// "--report migrate-report.md" still yields both files.
func WriteReports(prefix string, report Report) ([]string, error) {
	trimmed := strings.TrimSpace(prefix)
	if trimmed == "" {
		return nil, nil
	}
	lowered := strings.ToLower(trimmed)
	switch {
	case strings.HasSuffix(lowered, ".json"):
		trimmed = trimmed[:len(trimmed)-len(".json")]
	case strings.HasSuffix(lowered, ".md"):
		trimmed = trimmed[:len(trimmed)-len(".md")]
	}
	if directory := filepath.Dir(trimmed); directory != "" && directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return nil, fmt.Errorf("create report directory: %w", err)
		}
	}
	jsonPath := trimmed + ".json"
	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode report: %w", err)
	}
	payload = append(payload, '\n')
	if err := os.WriteFile(jsonPath, payload, 0o644); err != nil {
		return nil, fmt.Errorf("write json report: %w", err)
	}
	markdownPath := trimmed + ".md"
	if err := os.WriteFile(markdownPath, []byte(report.Markdown()), 0o644); err != nil {
		return nil, fmt.Errorf("write markdown report: %w", err)
	}
	return []string{jsonPath, markdownPath}, nil
}

// Markdown renders the human readable report.
func (report Report) Markdown() string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# Epay → GorixPay 迁移报告\n\n")
	fmt.Fprintf(&builder, "- 工具版本：%s\n", report.Tool)
	fmt.Fprintf(&builder, "- 生成时间：%s\n", report.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&builder, "- 模式：%s\n", report.Mode)
	fmt.Fprintf(&builder, "- 耗时：%d ms\n", report.DurationMS)
	fmt.Fprintf(&builder, "- 退出码：%d\n", report.ExitCode)
	fmt.Fprintf(&builder, "- 源库：%s `%s`\n", report.Source.Driver, report.Source.DSN)
	fmt.Fprintf(&builder, "- 目标库：%s `%s`（已打开：%t，可写：%t，已应用 migration：%d）\n",
		report.Target.Driver, report.Target.DSN, report.Target.Opened, report.Target.Writable, report.Target.MigrationsApplied)
	if report.Target.Note != "" {
		fmt.Fprintf(&builder, "- 目标库说明：%s\n", report.Target.Note)
	}
	builder.WriteString("\n## 预检\n\n")
	fmt.Fprintf(&builder, "- 结论：%s\n", ternary(report.Preflight.OK, "通过", "存在阻断项"))
	fmt.Fprintf(&builder, "- 必需表：%d/%d 存在\n", countTrue(report.Preflight.RequiredTables), len(report.Preflight.RequiredTables))
	for _, table := range report.Preflight.MissingTables {
		fmt.Fprintf(&builder, "  - 缺失：`%s`\n", table)
	}
	if len(report.Preflight.ConfigKeys) > 0 {
		builder.WriteString("- pre_config 关键项：")
		keys := make([]string, 0, len(report.Preflight.ConfigKeys))
		for key := range report.Preflight.ConfigKeys {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for index, key := range keys {
			if index > 0 {
				builder.WriteString("，")
			}
			fmt.Fprintf(&builder, "`%s=%s`", key, report.Preflight.ConfigKeys[key])
		}
		builder.WriteString("\n")
	}
	builder.WriteString("\n## 迁移计数\n\n")
	builder.WriteString("| 源表 | 目标表 | 源记录 | 新增 | 跳过 | 失败 | 源金额(分) | 目标金额(分) | 差额(分) |\n")
	builder.WriteString("| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, table := range report.Tables {
		fmt.Fprintf(&builder, "| `%s` | `%s` | %d | %d | %d | %d | %d | %d | %d |\n",
			table.SourceTable, table.TargetTable, table.Source, table.Inserted, table.Skipped, table.Failed,
			table.SourceAmountCents, table.TargetAmountCents, table.AmountDiffCents)
	}
	builder.WriteString("\n## 商户余额期初\n\n")
	fmt.Fprintf(&builder, "- 商户账户：%d 个，写入 %d 笔，跳过 %d 笔，失败 %d 笔\n",
		report.Balances.Merchants, report.Balances.Posted, report.Balances.Skipped, report.Balances.Failed)
	fmt.Fprintf(&builder, "- 金额合计：源 %d 分 / 目标 %d 分（差额 %d 分）\n",
		report.Balances.SourceTotalCents, report.Balances.TargetTotalCents, report.Balances.DiffCents)
	builder.WriteString("\n## 跳过原因\n\n")
	wroteSkip := false
	for _, table := range report.Tables {
		if len(table.SkipReasons) == 0 {
			continue
		}
		wroteSkip = true
		fmt.Fprintf(&builder, "- `%s`：%s\n", table.TargetTable, formatReasons(table.SkipReasons))
	}
	if len(report.Balances.SkipReasons) > 0 {
		wroteSkip = true
		fmt.Fprintf(&builder, "- 期初余额：%s\n", formatReasons(report.Balances.SkipReasons))
	}
	if !wroteSkip {
		builder.WriteString("- 无\n")
	}
	builder.WriteString("\n## 阻断项\n\n")
	if len(report.Blocking) == 0 {
		builder.WriteString("- 无\n")
	}
	for _, item := range report.Blocking {
		fmt.Fprintf(&builder, "- `%s`", item.Code)
		if item.Table != "" {
			fmt.Fprintf(&builder, "（%s）", item.Table)
		}
		fmt.Fprintf(&builder, "：%s", item.Detail)
		if item.Count > 0 {
			fmt.Fprintf(&builder, "，共 %d 行", item.Count)
		}
		builder.WriteString("\n")
		for _, sample := range item.Samples {
			fmt.Fprintf(&builder, "  - `%s`\n", sample)
		}
	}
	builder.WriteString("\n## 警告\n\n")
	if len(report.Warnings) == 0 {
		builder.WriteString("- 无\n")
	}
	for _, warning := range report.Warnings {
		fmt.Fprintf(&builder, "- %s\n", warning)
	}
	builder.WriteString("\n## 已知限制\n\n")
	for _, limitation := range report.KnownLimitations {
		fmt.Fprintf(&builder, "- %s\n", limitation)
	}
	return builder.String()
}

// Summary renders the console summary printed by the CLI.
func (report Report) Summary() string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "模式：%s\n", report.Mode)
	fmt.Fprintf(&builder, "源库：%s %s\n", report.Source.Driver, report.Source.DSN)
	fmt.Fprintf(&builder, "目标库：%s %s（已打开=%t，可写=%t，新应用 migration=%d）\n",
		report.Target.Driver, report.Target.DSN, report.Target.Opened, report.Target.Writable, report.Target.MigrationsApplied)
	if report.Target.Note != "" {
		fmt.Fprintf(&builder, "目标库说明：%s\n", report.Target.Note)
	}
	if report.Preflight.OK {
		fmt.Fprintf(&builder, "预检：通过（必需表 %d/%d）\n", countTrue(report.Preflight.RequiredTables), len(report.Preflight.RequiredTables))
	} else {
		fmt.Fprintf(&builder, "预检：存在 %d 个阻断项\n", len(report.Blocking))
	}
	for _, table := range report.Tables {
		fmt.Fprintf(&builder, "  %-18s → %-20s 源 %-5d 新增 %-5d 跳过 %-5d 失败 %-5d 金额 %d/%d 分\n",
			table.SourceTable, table.TargetTable, table.Source, table.Inserted, table.Skipped, table.Failed,
			table.SourceAmountCents, table.TargetAmountCents)
	}
	fmt.Fprintf(&builder, "余额期初：商户 %d，写入 %d，跳过 %d，失败 %d，金额 %d/%d 分\n",
		report.Balances.Merchants, report.Balances.Posted, report.Balances.Skipped, report.Balances.Failed,
		report.Balances.SourceTotalCents, report.Balances.TargetTotalCents)
	if report.DryRun {
		fmt.Fprintf(&builder, "金额一致：dry-run 未写入目标库，不做目标金额比对，退出码：%d\n", report.ExitCode)
	} else {
		fmt.Fprintf(&builder, "金额一致：%t，退出码：%d\n", report.AmountsEqual, report.ExitCode)
	}
	for _, item := range report.Blocking {
		fmt.Fprintf(&builder, "阻断：%s %s %s（%d 行）\n", item.Code, item.Table, item.Detail, item.Count)
	}
	for _, warning := range report.Warnings {
		fmt.Fprintf(&builder, "警告：%s\n", warning)
	}
	return builder.String()
}

func formatReasons(reasons map[string]int64) string {
	keys := make([]string, 0, len(reasons))
	for key := range reasons {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, reasons[key]))
	}
	return strings.Join(parts, "，")
}

func countTrue(values map[string]bool) int {
	total := 0
	for _, value := range values {
		if value {
			total++
		}
	}
	return total
}

func ternary(condition bool, whenTrue, whenFalse string) string {
	if condition {
		return whenTrue
	}
	return whenFalse
}
