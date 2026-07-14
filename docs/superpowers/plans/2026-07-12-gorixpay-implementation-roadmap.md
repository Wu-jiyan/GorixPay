# GroixPay Implementation Roadmap

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 分阶段交付一个完整兼容彩虹易支付 Epay、具备强一致资金账本、三数据库支持和 Go + React 管理界面的 GroixPay。

**Architecture:** 后端采用 Gin 驱动的事务型模块化单体，核心资金变化与 Transactional Outbox 在同一数据库事务提交，Redis 仅作为可选增强。前端采用 React 单应用和独立轻量收银台入口，历史协议由兼容层转换，支付渠道由编译期注册的 Go 插件实现。

**Tech Stack:** Go、Gin、GORM、SQLite、MySQL 5.7+、PostgreSQL、可选 Redis、React、TypeScript、Vite、Arco Design、Lodash、TanStack Query、Zustand、Vitest、Playwright、Docker Compose

---

## 1. 计划拆分原则

本项目包含基础设施、身份权限、资金账本、支付核心、兼容协议、支付插件、清结算、前端和数据迁移等可独立验收的子系统。实施时为每个阶段建立独立详细计划；每份阶段计划必须包含准确文件路径、测试先行步骤、执行命令、预期结果和阶段验收门槛。

阶段之间只允许依赖已验收的公开接口。不得为了推进后续阶段绕过前置阶段的质量门禁。

## 2. 目标工程结构

```text
GorixPay/
├── cmd/
│   ├── gorixpay/
│   └── migrate-epay/
├── internal/
│   ├── identity/
│   ├── merchant/
│   ├── payment/
│   ├── channel/
│   ├── plugin/
│   ├── refund/
│   ├── ledger/
│   ├── settlement/
│   ├── transfer/
│   ├── profitshare/
│   ├── risk/
│   ├── notification/
│   ├── outbox/
│   ├── audit/
│   ├── compat/
│   └── platform/
├── plugins/
├── web/
├── migrations/
├── deploy/
├── docs/
├── tests/
├── go.mod
└── Makefile
```

## 3. 阶段总览

| 阶段 | 交付物 | 独立验收结果 |
| --- | --- | --- |
| 0（已通过） | Epay 行为基线与契约样本 | 可重复验证旧系统签名、字段、状态码和通知语义 |
| 1 | 工程骨架与平台基础 | 单二进制启动，三数据库初始化，健康检查通过 |
| 2 | 身份、商户、RBAC 与密钥 | 管理员和商户可安全登录，权限与敏感配置可管理 |
| 3 | 账本、幂等与 Outbox | 并发资金操作不重复，账本平衡，任务可可靠重试 |
| 4 | 渠道、路由、风控与插件契约 | 可注册模拟插件并完成确定性渠道选择 |
| 5 | 支付、退款与 Epay API | 使用模拟渠道完成兼容下单、回调、查询和退款闭环 |
| 6 | 结算、代付与分账 | 出款类业务与资金账本闭环通过 |
| 7 | 首批生产支付插件 | 支付宝、微信 V3、易支付上游和 Jeepay 契约通过 |
| 8 | React 全量界面 | 后台、商户端、官网、文档和收银台可用 |
| 9 | Epay 全量数据迁移 | 全量迁移并输出记录、状态和金额校验报告 |
| 10 | 部署、安全与发布验收 | 单容器和 Compose 均通过故障、性能与安全验收 |

## 4. 阶段 0：Epay 行为基线

**详细计划文件：** `docs/superpowers/plans/2026-07-12-phase-0-epay-contract-baseline.md`

**主要输入：**

- `Epay/submit.php`
- `Epay/mapi.php`
- `Epay/api.php`
- `Epay/pay.php`
- `Epay/includes/lib/ApiHelper.php`
- `Epay/includes/lib/Payment.php`
- `Epay/includes/lib/api/Pay.php`
- `Epay/includes/lib/api/Merchant.php`
- `Epay/includes/lib/api/Transfer.php`
- `Epay/template/default/doc/`

**必须交付：**

- [x] 建立旧版与新版 API 路由清单。
- [x] 建立每个接口的请求字段、默认值、签名和响应矩阵。
- [x] 固化 MD5、RSA 请求签名与平台响应签名测试向量。
- [x] 固化成功、参数错误、签名错误、重复订单和渠道错误样本。
- [x] 固化同步返回、异步通知字段及成功回执文本。
- [x] 建立支付动作 `jump/html/qrcode/urlscheme/jsapi/app/scan/wxplugin/wxapp` 样本。
- [x] 输出 `tests/contracts/epay/manifest.yaml` 作为兼容验收事实源。

**质量门禁：** 所有样本必须能追溯到 Epay 源码路径或可重复运行结果，不允许凭文档猜测行为。

## 5. 阶段 1：工程与平台基础

**详细计划文件：** `docs/superpowers/plans/2026-07-12-phase-1-platform-foundation.md`

**计划创建：**

- `go.mod`
- `cmd/gorixpay/main.go`
- `internal/platform/app/app.go`
- `internal/platform/config/config.go`
- `internal/platform/database/database.go`
- `internal/platform/database/dialect.go`
- `internal/platform/httpserver/server.go`
- `internal/platform/logging/logging.go`
- `internal/platform/health/handler.go`
- `migrations/`
- `deploy/Dockerfile`
- `deploy/compose.yaml`

**必须交付：**

- [ ] 初始化 Go 模块并锁定实际使用的依赖版本。
- [ ] 用测试定义配置优先级和必填敏感项。
- [ ] 实现 SQLite、MySQL、PostgreSQL 连接与方言识别。
- [ ] 实现可重复执行、带版本记录的数据库迁移器。
- [ ] 实现 Gin 服务、统一请求标识、错误恢复和结构化日志。
- [ ] 实现 `/health/live`、`/health/ready` 和优雅停机。
- [ ] 提供单容器 SQLite 与 Compose PostgreSQL + Redis 配置。
- [ ] 在三数据库上执行同一组迁移集成测试。

**质量门禁：** 不配置 Redis 时服务完整启动；三种数据库均能从空库升级到最新版本并重复运行迁移而不改变结果。

## 6. 阶段 2：身份、商户与安全配置

**详细计划文件：** `docs/superpowers/plans/2026-07-12-phase-2-identity-merchant-security.md`

**计划目录：**

- `internal/identity/`
- `internal/merchant/`
- `internal/audit/`
- `internal/platform/crypto/`

**必须交付：**

- [ ] 定义管理员、商户、角色、权限、会话和 API 凭据模型。
- [ ] 实现 Argon2id 密码散列及 Epay 旧密码登录后升级。
- [ ] 实现安全 Cookie Session 与可撤销 API Token。
- [ ] 实现 RBAC 权限点、角色绑定和数据范围。
- [ ] 实现 TOTP MFA、恢复码、会话查看与撤销。
- [ ] 实现 AES-256-GCM 信封加密、主密钥版本和轮换。
- [ ] 实现登录、安全、权限和密钥操作审计。
- [ ] 增加暴力登录、会话固定、越权和敏感字段泄露测试。

**质量门禁：** 未授权、越权和过期会话全部被后端拒绝；任何日志和响应均不出现明文密码、Token 或渠道密钥。

## 7. 阶段 3：账本、幂等与 Outbox

**详细计划文件：** `docs/superpowers/plans/2026-07-12-phase-3-ledger-idempotency-outbox.md`

**计划目录：**

- `internal/ledger/`
- `internal/outbox/`
- `internal/platform/idempotency/`
- `internal/platform/transaction/`

**必须交付：**

- [ ] 定义资金账户、账本交易、账本分录、余额快照和冻结记录。
- [ ] 使用整数最小货币单位并实现严格字符串金额转换。
- [ ] 实现每笔账本交易借贷平衡检查。
- [ ] 实现业务幂等键唯一约束和重复结果重放。
- [ ] 实现事务重试与数据库方言锁策略。
- [ ] 实现 Outbox 写入、租约抢占、续租、完成、重试和死信。
- [ ] 实现 SQLite 单 Worker 保护及 PostgreSQL/MySQL 多 Worker 抢占。
- [ ] 验证 Redis 中断不影响任务事实和最终执行。
- [ ] 增加并发扣款、重复入账、回滚、崩溃恢复和账本不平衡测试。

**质量门禁：** 所有随机和并发资金测试均保持借贷平衡；Worker 在事务提交后崩溃并重启时不丢任务且不重复产生业务效果。

## 8. 阶段 4：渠道、路由、风控与插件契约

**详细计划文件：** `docs/superpowers/plans/2026-07-12-phase-4-channel-routing-plugin-contract.md`

**计划目录：**

- `internal/channel/`
- `internal/plugin/`
- `internal/risk/`
- `plugins/mock/`

**必须交付：**

- [ ] 定义支付类型、渠道账户、能力、限额、时间窗口和费率模型。
- [ ] 实现指定渠道、权重轮询、可用性过滤和确定性路由。
- [ ] 实现商户组费率、渠道费率、入账金额和平台利润计算。
- [ ] 定义按能力拆分的 Go 插件接口和统一支付动作。
- [ ] 实现插件注册表、配置 Schema、健康状态和错误映射。
- [ ] 实现域名、IP、地区、商品、金额、频率和商户状态风控规则。
- [ ] 建立模拟插件，覆盖全部支付动作和渠道错误。
- [ ] 增加路由公平性、边界金额、时间窗口和风控组合测试。

**质量门禁：** 相同输入和渠道状态得到可解释的路由结果；插件不能取得订单仓储或资金仓储依赖。

## 9. 阶段 5：支付核心与 Epay 兼容

**详细计划文件：** `docs/superpowers/plans/2026-07-12-phase-5-payment-epay-compat.md`

**计划目录：**

- `internal/payment/`
- `internal/refund/`
- `internal/notification/`
- `internal/compat/`
- `tests/contracts/epay/`

**必须交付：**

- [ ] 定义支付订单、支付尝试、回调和退款状态机。
- [ ] 实现 `(merchant_id, out_trade_no)` 唯一约束下的并发创建。
- [ ] 实现下单、查询、关闭、回调、退款和退款查询应用服务。
- [ ] 将支付成功、商户入账、账本和通知 Outbox 放入同一事务。
- [ ] 实现 MD5、RSA 签名与平台 RSA 响应签名。
- [ ] 实现 `/submit.php`、`/mapi.php`、`/api.php` 和 `/api/{class}/{method}`。
- [ ] 实现 `/pay/{method}/{trade_no}/` 及所有兼容支付动作。
- [ ] 实现商户同步返回、异步通知、回执识别和重试。
- [ ] 使用阶段 0 样本执行字节级契约测试。
- [ ] 增加重复、乱序、并发回调与渠道超时测试。

**质量门禁：** 模拟渠道支付闭环通过；重复和乱序回调不会重复入账或回退终态；阶段 0 兼容样本全部通过。

## 10. 阶段 6：结算、代付与分账

**详细计划文件：** `docs/superpowers/plans/2026-07-12-phase-6-settlement-transfer-profitshare.md`

**计划目录：**

- `internal/settlement/`
- `internal/transfer/`
- `internal/profitshare/`

**必须交付：**

- [ ] 实现结算规则、申请、审核、批次、出款和失败解冻。
- [ ] 实现代付申请、余额校验、渠道提交、查询和凭证。
- [ ] 实现分账接收方、分账提交、查询、回退和关闭。
- [ ] 所有资金动作通过账本和幂等键执行。
- [ ] 实现 Epay 代付与商户信息兼容接口。
- [ ] 增加审核竞争、重复提交、部分失败、渠道超时和人工恢复测试。

**质量门禁：** 任意失败路径不会造成余额、冻结金额和业务状态不一致；批次任务可以安全重跑。

## 11. 阶段 7：首批支付插件

**详细计划文件：** `docs/superpowers/plans/2026-07-12-phase-7-payment-plugins.md`

**计划目录：**

- `plugins/alipay/`
- `plugins/wechatpayv3/`
- `plugins/epay/`
- `plugins/jeepay/`

**必须交付：**

- [ ] 实现支付宝官方支付及已确认的服务商能力。
- [ ] 实现微信支付 V3 Native、JSAPI、H5、App、小程序和服务商模式。
- [ ] 实现彩虹易支付上游的下单、查询、退款和回调。
- [ ] 实现 Jeepay 上游的下单、查询、退款和回调。
- [ ] 为每个插件提供配置 Schema、能力元数据和健康检查。
- [ ] 使用本地 HTTP Mock 固化请求签名、响应解析和回调验签。
- [ ] 验证超时、网络失败、重复回调、证书轮换和平台错误映射。

**质量门禁：** 测试不访问真实支付平台；每个声明能力均有成功与失败契约测试；未声明能力返回统一不支持错误。

## 12. 阶段 8：React 界面

**详细计划文件：** `docs/superpowers/plans/2026-07-12-phase-8-react-app.md`

**计划目录：**

- `web/src/admin/`
- `web/src/merchant/`
- `web/src/public/`
- `web/src/docs/`
- `web/src/cashier/`
- `web/src/shared/`

**必须交付：**

- [ ] 初始化 React、TypeScript、Vite、Arco Design 和测试环境。
- [ ] 从 OpenAPI 生成 API 类型和客户端。
- [ ] 实现登录、Session 恢复、Token 刷新、MFA 和权限路由。
- [ ] 实现运营后台全部业务模块页面。
- [ ] 实现商户中心全部业务模块页面。
- [ ] 实现官网、在线开发文档和公共账户页面。
- [ ] 实现独立轻量收银台、支付等待和结果入口。
- [ ] 实现服务端分页、筛选、导出、错误边界和加载状态。
- [ ] 使用 Vitest 覆盖权限与核心交互，使用 Playwright 覆盖支付闭环。

**质量门禁：** 前端不以隐藏控件代替后端鉴权；收银台构建不包含后台主包；关键流程满足键盘操作和基础无障碍要求。

## 13. 阶段 9：Epay 全量数据迁移

**详细计划文件：** `docs/superpowers/plans/2026-07-12-phase-9-epay-data-migration.md`

**计划目录：**

- `cmd/migrate-epay/`
- `internal/migration/epay/`
- `tests/fixtures/epay/`

**必须交付：**

- [ ] 读取 Epay 数据库版本、前缀、表结构和配置。
- [ ] 实现预检、预演、分批、检查点和幂等重跑。
- [ ] 迁移配置、商户、分组、渠道和插件配置。
- [ ] 迁移订单、退款、结算、代付、分账和资金记录。
- [ ] 严格转换金额并记录不可转换数据。
- [ ] 重建账户余额和可审计的账本期初分录。
- [ ] 校验记录数、状态分布、金额汇总和关联完整性。
- [ ] 输出 Markdown 与 JSON 报告及阻断清单。
- [ ] 使用脱敏 Epay 数据快照执行端到端迁移测试。

**质量门禁：** 任何金额差异、孤立资金记录、重复业务键或无法解密的关键渠道配置均阻断切换，不允许静默丢弃。

## 14. 阶段 10：部署与发布验收

**详细计划文件：** `docs/superpowers/plans/2026-07-12-phase-10-release-hardening.md`

**计划目录：**

- `deploy/`
- `.github/workflows/`
- `tests/e2e/`
- `docs/operations/`

**必须交付：**

- [ ] 构建嵌入前端资源的多阶段生产镜像。
- [ ] 验证单容器 SQLite 持久化、备份、恢复和升级。
- [ ] 验证 Compose PostgreSQL + Redis 的 Web/Worker 分离部署。
- [ ] 建立格式、静态分析、单元、三数据库集成、前端和镜像 CI。
- [ ] 执行 Redis 中断、Worker 崩溃、数据库重连和通知失败演练。
- [ ] 执行下单、回调和通知链路基准测试并记录基线。
- [ ] 执行依赖、镜像、配置、密钥和权限安全检查。
- [ ] 编写安装、升级、备份、恢复、密钥轮换、死信处理和回滚手册。
- [ ] 执行 Epay 停写、迁移、校验、切流和回滚演练。

**质量门禁：** 两种部署模式均完成支付闭环；Redis 故障不影响正确性；恢复演练可还原数据库、证书和加密配置；所有阻断级检查通过。

## 15. 全局测试命令约定

阶段 1 建立实际工具后，所有阶段统一提供以下入口：

```powershell
go test ./...
go vet ./...
go test -race ./...
npm --prefix web run lint
npm --prefix web run test
npm --prefix web run build
npm --prefix web run test:e2e
docker compose -f deploy/compose.yaml config
```

数据库集成测试通过环境变量选择目标：

```powershell
$env:GORIXPAY_TEST_DATABASE = "sqlite"
go test ./internal/... -tags=integration

$env:GORIXPAY_TEST_DATABASE = "mysql"
go test ./internal/... -tags=integration

$env:GORIXPAY_TEST_DATABASE = "postgres"
go test ./internal/... -tags=integration
```

每份详细计划必须把命令收敛到项目脚本或 Makefile，并明确成功输出。Windows 是当前开发环境，因此不得只提供 Bash 专属脚本。

## 16. 全局完成定义

- [ ] Epay 兼容契约样本全部通过。
- [ ] 三种数据库的迁移与核心集成测试全部通过。
- [ ] 所有资金交易借贷平衡且可追溯。
- [ ] 重复、乱序和并发请求不产生重复资金效果。
- [ ] Redis 下线时核心支付和可靠任务继续工作。
- [ ] 单容器与 Compose 部署均通过支付闭环。
- [ ] Epay 全量迁移报告无阻断项。
- [ ] 后台、商户端、官网、文档和收银台达到功能验收范围。
- [ ] 日志、响应、指标、镜像和仓库不存在明文敏感信息。
- [ ] 安装、升级、备份、恢复、轮换和回滚手册完成演练。

## 17. 执行规则

1. 每个阶段开始前先编写并自审对应详细计划。
2. 实现遵循测试先行：失败测试、最小实现、通过测试、局部重构。
3. 优先运行最小相关测试，再运行模块测试和全量测试。
4. 不修复与当前阶段无关的问题，只记录独立缺陷。
5. 不提交密钥、证书、真实商户数据或生产数据库快照。
6. 未经用户明确要求，不创建 Git 提交或分支。
7. 每个阶段通过质量门禁后才进入下一阶段。
