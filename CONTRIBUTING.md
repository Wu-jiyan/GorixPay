# 参与开发

仓库是 Go + Gin + GORM 的支付平台，前端 Vue 3 + Naive UI。协议细节不看这里，看 [docs/README.md](docs/README.md) 的索引。

## 环境

| 需要 | 版本 | 说明 |
| --- | --- | --- |
| Go | 1.21+ | `go.mod` 声明 `go 1.21`，CI 用 `go-version-file: go.mod` |
| Node.js | 20+ | 前端构建，CI 用 Node 20 |
| PHP | 7.4 或 8.3（可选） | `tests/conformance/epay` 的上游 oracle 与客户端 SDK 用例，没有就跳过；两个版本都实测跑通，CI 用 8.3 |
| Redis | 7（可选） | V2 防重放与收银台下单限流。不配置时防重放回落到 `epay_request_nonces` 表，限流不生效 |
| SQLite | 不需要装 | 默认数据库，`glebarez/sqlite` 是纯 Go 实现，不需要 cgo |
| MySQL | 5.7+（可选） | 生产部署 |
| PostgreSQL | 16（可选） | `deploy/compose.yaml` 用 `postgres:16-alpine` |

Windows、macOS、Linux 都能编译和跑测试。仓库里的 `.ps1` 脚本按 PowerShell 写，Windows 直接可用；其它系统把命令改成对应 shell 语法即可。

上游易支付源码不进仓库。想跑客户端 SDK 那几条用例，把它放到仓库外任意目录，用 `EPAY_UPSTREAM_DIR` 指过去；不指也行，那几条会 skip，其余全跑。

## 跑起来

最省事的一条路是 SQLite 加一个主密钥文件：

```powershell
# 主密钥：32 字节原始字节，或这 32 字节的 base64（internal/platform/secret.LoadKeyFile 只认这两种）
$bytes = New-Object byte[] 32
[System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($bytes)
[System.IO.File]::WriteAllBytes('master.key', $bytes)

go run ./cmd/gorixpay --http-address :8080 --public-url http://localhost:8080 `
  --database-driver sqlite --database-dsn gorixpay.db --master-key-file master.key
```

另开一个终端建管理员。这条命令自己会跑迁移，不需要先启动服务：

```powershell
$env:GORIXPAY_ADMIN_PASSWORD = 'admin12345'
go run ./cmd/gorixpay admin create --username admin --display-name Admin --password-env GORIXPAY_ADMIN_PASSWORD
```

密码用 `--password-env` 从环境变量读，避免落在 shell 历史里；`--password` 是明文参数，只在一次性环境用。两个都不给时进入交互输入，要求输入两次且至少 8 位。`admin create` 还接受启动参数（`--database-dsn`、`--master-key-file` 等），所以能给另一个库建管理员。

跑起来之后：

| 地址 | 内容 |
| --- | --- |
| `http://localhost:8080/health/ready` | 就绪探针，返回 `{"status":"ok"}` |
| `http://localhost:8080/admin` | 运营后台 |
| `http://localhost:8080/merchant` | 商户中心 |
| `http://localhost:8080/doc` | 开发文档页 |
| `http://localhost:8080/cashier.php?trade_no=...` | 收银台 SPA |

配置有三种来源，优先级从低到高是 `--config` 文件、`GORIXPAY_*` 环境变量、命令行参数（`internal/platform/config/config.go`）。字段示例见 `deploy/config.example.yaml`。`--environment production` 会强制要求主密钥文件，并关掉沙箱渠道。

前端开发：

```powershell
Push-Location frontend; npm ci; Pop-Location        # 首次
Push-Location frontend; npm run dev; Pop-Location   # Vite 跑在 3000，代理 /api 到 127.0.0.1:8080
```

## 测试分层

全量是 `go test ./... -count=1`。下面几层各自防不同的东西，改哪块跑哪层。

| 层 | 位置 | 怎么跑 | 防什么 | 防不住什么 |
| --- | --- | --- | --- | --- |
| 单元测试 | 各包的 `*_test.go`，`internal/platform/app` 除外 | `go test ./internal/... -count=1` | 金额换算、订单状态机、签名算法、账本借贷平衡、幂等键、渠道选择 | 不经过 HTTP，路由与中间件没覆盖 |
| 契约测试 | `tests/contracts/epay`、`tests/contracts/plugins` | `go test ./tests/contracts/... -count=1` | 上游字段、错误码、签名向量被悄悄改动；插件注册表与 `tests/contracts/plugins/manifest.yaml` 不一致 | 只声明应当是什么，不证明服务真的这样回答 |
| 协议一致性 | `tests/conformance/epay` | `go test ./tests/conformance/epay -count=1 -v` | 起真实 Gin 服务（内存 SQLite + sandbox 渠道 + 真商户凭据），用 HTTP 打 `submit.php`、`mapi.php`、`api.php`、`/api/pay/*`、`/pay/*`、`/cashier.php` | 自己打自己，签名算法可能和上游一起跑偏 |
| 上游 oracle | `tests/conformance/epay/php` | 同上，需要 `php` 在 `PATH` 或设置 `EPAY_PHP_BIN` | 真 PHP 7.4 执行逐字复制的上游函数；`require` 上游 `EpayCore.class.php` 跑全流程 | 机器上没有 PHP 时这几条 `t.Skip`，其余照跑 |
| 端到端 | `internal/platform/app` | `go test ./internal/platform/app -count=1` | 建商户、下单、收银台发起、回调入账、通知投递、退款、代付、生态域的整条链路 | 只跑 sandbox 渠道，真实网关要单独联调 |
| 前端 | `frontend` | `npm --prefix frontend run typecheck`、`npm --prefix frontend run build` | 类型错误、坏路由、打包失败 | 不覆盖交互行为 |

两条约束要记住：

- 协议一致性层里，客户端 SDK 那几条用例需要一份本地上游 PHP 源码（`EPAY_UPSTREAM_DIR` 指过去）。没有它时这些用例 skip 而不是失败，其余 100 多条照跑；oracle 那几条用的是逐字复制的函数体，不依赖上游源码目录。
- `go test ./...` 里 `tests/conformance/epay` 也在内。只想跑协议面时用 `-run` 过滤，别为了快把整包跳过。

浏览器里的手工验收脚本（服务已启动、管理员已建）。两个脚本的用法行都写 `pwsh`（PowerShell 7+），其中 `e2e.ps1` 用到 .NET 5+ 的 `ImportFromPem`，Windows PowerShell 5.1 跑不了它。在 5.1 里执行 `e2e.ps1` 会先打印一行提示再以退出码 2 结束，不会半路崩在密钥解析上：

```powershell
pwsh -File scripts/e2e.ps1 -BaseUrl http://127.0.0.1:8080
pwsh -File scripts/plugin-sandbox-check.ps1 -BaseUrl http://127.0.0.1:8080
```

真实凭据校验在 Windows PowerShell 5.1 就能跑，退出码 1 表示还有渠道缺字段或仍是占位符：

```powershell
Copy-Item deploy/plugin-credentials.example.ps1 deploy/plugin-credentials.local.ps1
# 填值后
powershell -File scripts/plugin-credentials-check.ps1 -File deploy/plugin-credentials.local.ps1
powershell -File scripts/plugin-credentials-check.ps1 -File deploy/plugin-credentials.local.ps1 -Apply
```

`*.local.ps1` 已被 `.gitignore` 忽略，用它装真实凭据。

## 提交前自检

```powershell
gofmt -l .                        # 必须无输出
go build ./...
go vet ./...
go test ./... -count=1
npm --prefix frontend run build   # 改了前端就跑
```

CI（`.github/workflows/ci.yml`）跑同一串：`gofmt -l .`、`go build ./...`、`go vet ./...`、`go test ./... -count=1`，前端 job 用 `npm ci` 加 `npm run build`。本地全绿再推，别让 CI 替你发现编译错误。

改兼容层时加一条更严的：

```powershell
go test ./tests/conformance/epay -count=1 -v
```

## 新增支付插件

一个插件最少要实现 `Plugin`，需要配置表单就实现 `Configurable`，需要收上游回调就实现 `CallbackVerifier`（接口定义在 `internal/plugin/payment/contract.go`）。

1. 建包 `internal/plugin/payment/<code>/plugin.go`，构造函数 `New(client *http.Client) *Plugin`，`nil` 时用 `pluginpayment.OrDefaultClient(nil)`（15 秒超时）。
2. `Metadata()`：`Code` 必须匹配 `^[a-z][a-z0-9_-]{1,63}$`，`Name` 非空，`Capabilities` 至少一项且不能重复。能力值只有 `payment`、`query`、`close`、`refund`、`transfer`。
3. `CreatePayment()`：`CreateRequest.Amount` 是分，和上游报文互转用 `pluginpayment.FormatAmount` 与 `ParseAmount`。读上游响应体用 `pluginpayment.ReadUpstreamBody`，它按 `MaxUpstreamResponseBytes`（64 KiB）截断。返回值必须是 `Action`，并且通过 `Action.Validate()`：`jump` 只接受 http(s) 绝对地址，`qrcode` 接受任意带 scheme 的绝对 URI（微信 `weixin://wxpay/bizpayurl?pr=...` 走这条），`html`、`jsapi`、`app`、`scan` 把内容放进 `Data`。
4. `ConfigFields()`：`Type` 取 `text`、`password`、`url`、`textarea`、`select`，`Label` 不能为空，`Secret: true` 的字段管理端不回显。整份渠道配置由主密钥加密后存 `payment_channels.config_encrypted`，明文不落库。
5. `VerifyCallback()`：验签失败返回 `ErrCallbackRejected`（处理器回 HTTP 401），配置缺项返回 `ErrInvalidConfiguration`（HTTP 500）。`CallbackResult.Amount` 必须换算成分，处理器拿它和订单金额逐分比对。上游只认自定义回执时填 `CallbackResult.Ack`，比如汇付要 `RECV_ORD_ID_<trade_no>`；留空表示回 `success`。
6. 注册：在 `internal/platform/app/app.go` 的插件列表里加 `plugins.Register(<pkg>.New(nil))`。注册表按 code 去重，重复注册或 metadata 非法会让进程启动失败。
7. 契约测试：在 `tests/contracts/plugins/manifest.yaml` 补一段 `code`、`name`、`capabilities`、`callback_verifier`、`upstream`、`action_types`、`config_fields`、`amount_unit`。`tests/contracts/plugins/plugins_test.go` 会断言注册表与 manifest 的插件集合完全一致、`name` 相同、能力相同、`ConfigFields` 的 `type`/`required`/`secret` 与 manifest 相同、`amount_unit` 非空、`action_types` 都在已知集合里。
8. 文档：`docs/plugins.md` 第 1 节清单加一行，第 3 节补该渠道的配置字段，第 4 节补下单与回调报文（签名位置、已支付判定、金额字段与单位），第 5.5 节回调速查表加一行。
9. 单测：照 `internal/plugin/payment/*/plugin_test.go` 的写法，用 `httptest` mock 上游，覆盖下单成功、上游报错、回调验签失败、回调金额不符。

插件列表在 `GET /api/v1/admin/plugins` 上可以直接看到，管理端用它渲染配置表单，所以 `ConfigFields` 写错会立刻体现在表单里。

## 代码风格

- 只提交 `gofmt` 过的代码，`gofmt -l .` 必须空输出。
- 金额内部一律 `int64` 分，对外字符串两位小数。跨层传递金额时不要在中间换成 float64。
- 时间用 UTC 存库，格式化放在展示层或兼容层。
- 业务错误用语义化的 sentinel（`paymentservice.ErrIdempotencyConflict` 这类），映射到协议码只在兼容层做（`internal/compat/epay/` 下的包），别让协议码漏进业务包。
- 注释写为什么，不写做了什么。引用上游行为时写 `文件:行号`，现有测试里有大量这种引用可以照抄格式。
- 不随意加依赖。新增 Go 依赖要能解释 `go.sum` 的每一行变化，前端同理；需要 cgo 的方案直接放弃，SQLite 用的是纯 Go 实现。

## 迁移文件规则

- 文件名 `NNNNNN_<snake_case>.sql`，六位序号，接着当前最大号往后排（现在最大是 `000016`）。`migrations/embed.go` 用 `//go:embed *.sql` 打包，文件名就是版本号。
- 已经执行过的迁移不能改。runner 把 `version` 和内容的 `checksum` 记在 `schema_migrations`，内容一变，启动时报 `migration <file> checksum drift` 并拒绝启动（`internal/platform/migrate/migrate.go`）。
- 只加不删。撤销一个变更就再写一条反向迁移。
- SQL 要能同时跑在 SQLite、MySQL 5.7、PostgreSQL 上。唯一的方言改写是 MySQL 的 `CREATE INDEX IF NOT EXISTS`，`prepareMigration` 会把 `IF NOT EXISTS` 去掉；其它方言差异得自己避开。

## PR 期望

- 一个 PR 做一件事。顺手重构和顺手修 bug 分开提，reviewer 才看得动。
- 描述里贴验证命令与输出原文，`.github/PULL_REQUEST_TEMPLATE.md` 有现成清单。写「已测试」不算验证。
- 改到对外协议面（字段、错误码、签名、状态码）时，同步 `docs/epay-compat-matrix.md` 与 `tests/contracts/epay/manifest.yaml`，否则下一个人会把偏差当成 bug。
- 不提交密钥、`master.key`、`*.local.ps1`、`*.db`、`frontend/dist`。`.gitignore` 已经覆盖这些，别用 `git add -f` 绕过。
- 改动跨多个模块（比如同时动账本和协议层）就先开 issue 说明边界，免得两个人同时改同一张表。
