# Epay 迁移 fixture

`epay_fixture.sql` 是 `Epay/install/install.sql` 的 **SQLite 子集**，用来给
`internal/migration/epay` 与 `cmd/migrate-epay` 的测试提供一份可重复的“历史易支付库”。

## 内容

| 表 | 行数 | 说明 |
| --- | ---: | --- |
| `pre_config` | 7 | 含 `version=2055`、`sitename`，以及一个不应被迁移的 `mail_pwd` |
| `pre_type` | 2 | `alipay` / `wxpay`，用于 `pre_order.type` 映射 |
| `pre_user` | 2 | uid 1001（status=1，余额 135.79，有 username）、uid 1002（status=0，余额 20.50，username 为空） |
| `pre_order` | 3 | 已支付 10.00 + 未支付 20.50 + 已关闭 1.99（合计 32.49） |
| `pre_refundorder` | 1 | 关联订单 1，退款 1.00，status=1 |
| `pre_settle` | 1 | id=7，结算 10.00，status=1 |
| `pre_transfer` | 1 | 代付 5.00，status=1 |
| `pre_domain` | 1 | `shop.example.com`，status=1 |
| `pre_blacklist` | 1 | type=1（IP）`203.0.113.7` |

## 方言差异（有意为之）

- `install.sql` 是 MySQL 方言；SQLite 没有 `AUTO_INCREMENT`，主键用 `INTEGER PRIMARY KEY`。
- 金额列在 `install.sql` 是 `DECIMAL(10,2)`。MySQL 的 DECIMAL 经 `database/sql`
  读出就是字符串；SQLite 若用 `NUMERIC` 会存成 REAL 并引入二进制浮点误差，
  因此这里声明为 `TEXT`，让金额保持“精确十进制字符串”这一与 MySQL 相同的行为。
- 时间列保持 `DATETIME`，SQLite 以 `TEXT` 存 `YYYY-MM-DD HH:MM:SS`。

## 密码

Epay 的 `pwd = md5(md5(明文) . md5('1277180438' . uid))`（`includes/common.php::getMd5Pwd`）。

- uid 1001 → 明文 `merchant1001` → `e120f39b04b9814151ec35e721d94ed1`
- uid 1002 → 明文 `merchant1002` → `cca810cdf3f9de23807c56e16a100e20`

迁移后 `password.VerifyLegacyMerchant` 会用同一公式校验，并在首次登录时升级为 Argon2id。

## 用法

测试里直接执行整个脚本（按 `;\n` 拆分语句）建库：

```
sql.Open("sqlite", dsn) → 逐条 Exec → 该文件即模拟出的源库
```

要复现“负数金额阻断”等场景，测试会在建库后追加 `UPDATE/DROP` 破坏数据，
而不是再维护第二份 fixture。

## 样例报告

`sample-report.md` / `sample-report.json` 是用这份 fixture 真跑一次（`--batch-size 2`）
产出的报告样例，可以直接对照 `internal/migration/epay` 的报告结构：

```
go run ./cmd/migrate-epay --source-driver sqlite --source-dsn epay-source.db \
  --target-driver sqlite --target-dsn gorixpay.db --report sample-report --batch-size 2
```

其中的 DSN 是相对路径，金额单位为分，`pre_config` 只出现白名单键（`sitename` → `site.name`），
`mail_pwd` 这类敏感键不会进入报告。
