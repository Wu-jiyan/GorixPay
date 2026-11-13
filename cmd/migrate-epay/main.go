// Command migrate-epay migrates a legacy Epay (彩虹易支付) database into GorixPay.
//
//	go run ./cmd/migrate-epay --source-driver mysql --source-dsn "user:pass@tcp(127.0.0.1:3306)/epay" \
//	  --target-driver sqlite --target-dsn gorixpay.db --master-key-file master.key \
//	  --dry-run --report migrate-report --batch-size 500
//
// Exit codes: 0 success (or a clean dry run), 2 blocked by preflight/amount
// differences, 1 runtime error.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"gorixpay/internal/migration/epay"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("migrate-epay", flag.ContinueOnError)
	flags.SetOutput(stderr)
	sourceDriver := flags.String("source-driver", "sqlite", "源库类型：mysql 或 sqlite")
	sourceDSN := flags.String("source-dsn", "", "源库 DSN（MySQL: user:pass@tcp(host:3306)/epay；SQLite: 文件路径）")
	targetDriver := flags.String("target-driver", "sqlite", "目标库类型：sqlite、mysql 或 postgres")
	targetDSN := flags.String("target-dsn", "gorixpay.db", "目标库 DSN（先执行现有 migrations.Runner）")
	masterKeyFile := flags.String("master-key-file", "", "主密钥文件（与 gorixpay 的 --master-key-file 一致，仅校验；本期不写加密字段）")
	dryRun := flags.Bool("dry-run", false, "只做预检与只读对比，不写目标库")
	reportPrefix := flags.String("report", "", "报告文件前缀，输出 <prefix>.json 与 <prefix>.md")
	batchSize := flags.Int("batch-size", 500, "每批写入行数（1-10000）")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "用法：migrate-epay --source-driver mysql|sqlite --source-dsn \"...\" [选项]")
		fmt.Fprintln(flags.Output(), "")
		fmt.Fprintln(flags.Output(), "示例：")
		fmt.Fprintln(flags.Output(), "  go run ./cmd/migrate-epay --source-driver sqlite --source-dsn epay.db \\")
		fmt.Fprintln(flags.Output(), "    --target-driver sqlite --target-dsn gorixpay.db --dry-run --report migrate-report")
		fmt.Fprintln(flags.Output(), "")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(stderr, "参数解析失败:", err)
		return 1
	}
	if flags.NArg() > 0 {
		fmt.Fprintln(stderr, "存在多余参数:", strings.Join(flags.Args(), " "))
		return 1
	}

	migrator, err := epay.New(epay.Options{
		Source:        epay.SourceConfig{Driver: *sourceDriver, DSN: *sourceDSN},
		Target:        epay.TargetConfig{Driver: *targetDriver, DSN: *targetDSN},
		DryRun:        *dryRun,
		BatchSize:     *batchSize,
		MasterKeyFile: *masterKeyFile,
		ReportPrefix:  *reportPrefix,
		Logf: func(format string, arguments ...any) {
			fmt.Fprintf(stderr, "[migrate-epay] "+format+"\n", arguments...)
		},
	})
	if err != nil {
		fmt.Fprintln(stderr, "参数错误:", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintln(stdout, "GorixPay Epay 迁移工具")
	report, err := migrator.Run(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "迁移失败:", err)
		return 1
	}

	paths, err := epay.WriteReports(*reportPrefix, report)
	if err != nil {
		fmt.Fprintln(stderr, "写报告失败:", err)
		return 1
	}
	fmt.Fprint(stdout, report.Summary())
	if len(paths) > 0 {
		fmt.Fprintln(stdout, "报告文件："+strings.Join(paths, "，"))
	} else {
		fmt.Fprintln(stdout, "报告文件：未生成（--report 为空）")
	}
	return report.ExitCode
}
