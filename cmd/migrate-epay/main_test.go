package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureSource materialises tests/fixtures/epay/epay_fixture.sql in a temporary
// SQLite database and returns its DSN.
func fixtureSource(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "epay", "epay_fixture.sql"))
	if err != nil {
		t.Fatalf("读取 fixture 失败: %v", err)
	}
	dsn := filepath.ToSlash(filepath.Join(t.TempDir(), "epay-source.db"))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("打开 fixture 失败: %v", err)
	}
	defer db.Close()
	for _, statement := range splitStatements(string(content)) {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("执行 fixture 失败: %v\n%s", err, statement)
		}
	}
	return dsn
}

func splitStatements(script string) []string {
	statements := []string{}
	for _, chunk := range strings.Split(script, ";\n") {
		kept := make([]string, 0, 8)
		for _, line := range strings.Split(chunk, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "--") {
				continue
			}
			kept = append(kept, line)
		}
		statement := strings.Join(kept, "\n")
		if strings.TrimSpace(statement) == "" {
			continue
		}
		statements = append(statements, statement)
	}
	return statements
}

func tempTarget(t *testing.T) (path, dsn string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "gorixpay.db")
	return path, filepath.ToSlash(path)
}

func TestRunDryRunWritesOnlyReports(t *testing.T) {
	source := fixtureSource(t)
	targetPath, targetDSN := tempTarget(t)
	prefix := filepath.Join(t.TempDir(), "migrate-report")

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--source-driver", "sqlite", "--source-dsn", source,
		"--target-driver", "sqlite", "--target-dsn", targetDSN,
		"--dry-run", "--report", prefix, "--batch-size", "2",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d\nstdout=%s\nstderr=%s", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("dry run created the target database: %v", err)
	}
	payload, err := os.ReadFile(prefix + ".json")
	if err != nil {
		t.Fatalf("dry run 未生成 json 报告: %v", err)
	}
	var report struct {
		Mode      string `json:"mode"`
		ExitCode  int    `json:"exit_code"`
		Blocking  []any  `json:"blocking"`
		Tables    []any  `json:"tables"`
		Preflight struct {
			OK bool `json:"ok"`
		} `json:"preflight"`
	}
	if err := json.Unmarshal(payload, &report); err != nil {
		t.Fatalf("json 报告无法解析: %v", err)
	}
	if report.Mode != "dry-run" || report.ExitCode != 0 || len(report.Blocking) != 0 || !report.Preflight.OK {
		t.Fatalf("dry-run 报告 = %+v", report)
	}
	if len(report.Tables) == 0 {
		t.Fatal("dry-run 报告没有表统计")
	}
	if _, err := os.Stat(prefix + ".md"); err != nil {
		t.Fatalf("dry run 未生成 md 报告: %v", err)
	}
	if !strings.Contains(stdout.String(), "dry-run") {
		t.Fatalf("stdout 缺少模式说明:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "报告文件") {
		t.Fatalf("stdout 缺少报告文件路径:\n%s", stdout.String())
	}
}

func TestRunApplyIsRepeatable(t *testing.T) {
	source := fixtureSource(t)
	_, targetDSN := tempTarget(t)
	prefix := filepath.Join(t.TempDir(), "report")
	arguments := []string{
		"--source-driver", "sqlite", "--source-dsn", source,
		"--target-driver", "sqlite", "--target-dsn", targetDSN,
		"--report", prefix,
	}

	var firstOut, firstErr bytes.Buffer
	if code := run(arguments, &firstOut, &firstErr); code != 0 {
		t.Fatalf("首次运行 exit = %d\nstderr=%s", code, firstErr.String())
	}
	if !strings.Contains(firstOut.String(), "金额一致：true") {
		t.Fatalf("首次运行金额不一致:\n%s", firstOut.String())
	}

	var secondOut, secondErr bytes.Buffer
	if code := run(arguments, &secondOut, &secondErr); code != 0 {
		t.Fatalf("二次运行 exit = %d\nstderr=%s", code, secondErr.String())
	}
	if !strings.Contains(secondOut.String(), "新增 0") {
		t.Fatalf("二次运行仍有新增:\n%s", secondOut.String())
	}
	if !strings.Contains(secondOut.String(), "金额一致：true") {
		t.Fatalf("二次运行金额不一致:\n%s", secondOut.String())
	}
}

func TestRunBlocksOnNegativeAmount(t *testing.T) {
	source := fixtureSource(t)
	db, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatalf("打开 fixture 失败: %v", err)
	}
	if _, err := db.Exec(`UPDATE pre_order SET money = '-1.00' WHERE trade_no = '20250102030405001'`); err != nil {
		t.Fatalf("篡改金额失败: %v", err)
	}
	db.Close()
	targetPath, targetDSN := tempTarget(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--source-driver", "sqlite", "--source-dsn", source,
		"--target-driver", "sqlite", "--target-dsn", targetDSN,
		"--dry-run", "--report", filepath.Join(t.TempDir(), "blocked"),
	}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\nstdout=%s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "negative_amount") {
		t.Fatalf("stdout 未列出阻断项:\n%s", stdout.String())
	}
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("阻断运行创建了目标库: %v", err)
	}
}

func TestRunRejectsBadArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--source-driver", "sqlite"}, &stdout, &stderr); code != 1 {
		t.Fatalf("缺少 --source-dsn 时 exit = %d, want 1", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{
		"--source-driver", "sqlite", "--source-dsn", "same.db",
		"--target-driver", "sqlite", "--target-dsn", "same.db",
	}, &stdout, &stderr); code != 1 {
		t.Fatalf("源目标相同时 exit = %d, want 1", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--source-driver", "oracle", "--source-dsn", "x", "--target-dsn", "y"}, &stdout, &stderr); code != 1 {
		t.Fatalf("不支持的源类型 exit = %d, want 1", code)
	}
}

func TestRunRejectsInvalidBatchSizeAndMasterKey(t *testing.T) {
	source := fixtureSource(t)
	_, targetDSN := tempTarget(t)
	base := []string{
		"--source-driver", "sqlite", "--source-dsn", source,
		"--target-driver", "sqlite", "--target-dsn", targetDSN, "--dry-run",
	}
	for _, batchSize := range []string{"-5", "999999"} {
		var stdout, stderr bytes.Buffer
		arguments := append(append([]string{}, base...), "--batch-size", batchSize)
		if code := run(arguments, &stdout, &stderr); code != 1 {
			t.Fatalf("--batch-size %s 时 exit = %d, want 1", batchSize, code)
		}
	}
	var stdout, stderr bytes.Buffer
	arguments := append(append([]string{}, base...), "--master-key-file", filepath.Join(t.TempDir(), "missing.key"))
	if code := run(arguments, &stdout, &stderr); code != 1 {
		t.Fatalf("master key 缺失时 exit = %d, want 1", code)
	}
}

func TestRunAcceptsMasterKeyFile(t *testing.T) {
	source := fixtureSource(t)
	_, targetDSN := tempTarget(t)
	keyPath := filepath.Join(t.TempDir(), "master.key")
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{'k'}, 32), 0o600); err != nil {
		t.Fatalf("写 master key 失败: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--source-driver", "sqlite", "--source-dsn", source,
		"--target-driver", "sqlite", "--target-dsn", targetDSN,
		"--dry-run", "--master-key-file", keyPath,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "master key 已校验") {
		t.Fatalf("stderr 缺少 master key 校验记录:\n%s", stderr.String())
	}
}
