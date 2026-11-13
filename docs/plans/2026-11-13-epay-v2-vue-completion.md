# GorixPay：易支付完整复刻 + V2 安全修复 + Vue3/Naive UI/Redis 实施计划

> 本文件是并行开发的唯一契约来源。所有成员以本文件为准，禁止各自发明接口。

## 1. 目标与验收

在现有 Go 后端（Gin + GORM，已通过 `go test ./...`）基础上：

1. 补齐易支付（彩虹易支付）V1 全部对外接口：`submit.php`、`mapi.php`、`api.php?act=query|order|orders|refund|settle`。
2. 新增易支付 V2 协议：`/api/:class/:method`（同时保留 `api.php?s=pay/query` 形式），RSA-SHA256 双向签名 + `timestamp` 校验，覆盖 `pay/*`、`merchant/*`、`transfer/*`。
3. 修复“零元购”：订单金额不可被后续请求/回调篡改，V2 通知与请求签名分离，回调必须与订单金额一致，V2 请求防重放，退款/代付必须绑定订单与金额。
4. 前端由 React 整体替换为 **Vue 3 + Naive UI**（Vite + TypeScript + vue-router + pinia），覆盖官网、收银台、商户中心、运营后台、开发文档。
5. 新增 **Redis**（可选依赖，未配置时功能完整降级）：V2 防重放、下单限流、Outbox 分布式锁、健康检查。
6. 补齐商户门户与运营后台缺失接口：资料、仪表盘、公告、代付（转账）、凭据（MD5 + V2 RSA 密钥）。
7. 增加本地沙箱支付渠道（`sandbox`），让 `submit.php → 收银台 → 模拟支付 → 回调入账 → 商户通知` 闭环可本地验收；生产环境默认禁用。

**最终验收命令（Windows PowerShell）：**

```powershell
go build ./...
go vet ./...
go test ./...
npm --prefix frontend run build
```

并完成一次真实 HTTP 闭环：注册商户 → 生成密钥 → 建渠道（sandbox）→ V1 下单 → 收银台发起 → 沙箱支付 → 订单 paid + 账本平衡 + Outbox 通知 → V2 查询验签 → V1 退款。

## 2. 现状基线（已存在，不要重写）

- `internal/platform/*`：配置、数据库（SQLite/MySQL/PostgreSQL）、迁移、密钥加密（AES-GCM + master key）、HTTP 服务、健康检查。
- `internal/identity/*`：管理员/商户登录、会话 Cookie/Bearer、注册商户、RBAC 基础。
- `internal/payment/*`：下单、查询、关闭、渠道编排（orchestrator）、渠道回调入账（`callback.Service.Complete`，幂等 + Outbox 同一事务）。
- `internal/ledger/*`：复式账本、冻结、人工操作；`internal/finance/*`：退款、结算申请/审核。
- `internal/outbox/*` + `internal/notification/epay/*`：商户异步通知 worker。
- `internal/compat/epay/*`：V1 `submit.php`/`mapi.php`、MD5 签名、密钥中间件；`/api/pay/query|close`（非官方路径，保留兼容）。
- `internal/plugin/payment/*`：插件契约与注册表；`epay` 上游插件。
- `migrations/000001..000009`：现有表结构。
- `frontend/`：**旧 React 应用，将被 Vue 应用整体替换**（替换后删除旧 `src`、`@arco-design` 依赖与产物 `dist`）。

## 3. 易支付协议事实（来自 `Epay/` 参考源码与官方文档）

### 3.1 签名

待签名串 `getSignContent`：

1. 取全部参数，剔除 `sign`、`sign_type`、数组值与空值（`''`/`null`）。
2. 按参数名 ASCII 升序排序。
3. 拼 `k1=v1&k2=v2...`（末尾无 `&`）。
4. MD5：`md5(signStr + key)` 小写；RSA：`SHA256withRSA(signStr)` 后 Base64。

V1：MD5（`sign_type` 缺省 MD5），`submit.php/mapi.php` 也支持 `sign_type=RSA`（商户公钥）。
V2：请求必须 `sign_type=RSA`，V2 响应/通知由平台私钥签名，平台公钥公开；请求带 10 位秒级 `timestamp`，服务端校验 `|now - timestamp| <= 300`。

### 3.2 V1 接口语义

- `POST /submit.php`：表单/GET 提交，成功 302/脚本跳转收银台；参数错误输出 HTML 提示。
- `POST /mapi.php`：返回 JSON `{"code":1,"trade_no","pay_type","pay_info"}`；失败 `{"code":-1,"msg"}`。
- `GET|POST /api.php?act=query`：商户账户信息；参数 `pid`、`key`（明文）；返回 `code=1, pid, money, orders, orders_today, orders_lastday, active`。
- `GET|POST /api.php?act=order`：订单查询；参数 `pid/key`（或 `trade_no/sign` 内部签名）+ `trade_no` 或 `out_trade_no`；返回 `code=1, trade_no, out_trade_no, type, name, money, status, addtime, endtime, param, buyer`。
- `act=orders`：分页列表 `code=1, count, data[]`。
- `act=refund`：`pid/key` + `trade_no|out_trade_no` + `money`；成功 `code=0`。
- `act=settle`：结算记录 `code=1, data[]`。
- 错误码沿用：`-1` 业务失败、`-2` 未开启、`-3` 验签/密钥失败、`-4` 参数缺失、`-5` 无此接口。

### 3.3 V2 接口（`/api/:class/:method`）

方法清单（与参考源码 `includes/lib/api/*.php` 一致）：

| 方法 | 必填 | 说明 |
| --- | --- | --- |
| `pay/submit` | pid, out_trade_no, notify_url, name, money, type(可空跳收银台), timestamp, sign | 页面跳转，302 到 `/cashier.php?trade_no=` |
| `pay/create` | 同上 + `method`(web/jump/jsapi/scan/app)、`clientip` | JSON 下单，返回 `pay_type/pay_info` |
| `pay/query` | pid + `trade_no` 或 `out_trade_no` | 订单查询 |
| `pay/close` | pid + 订单号 | 关闭未支付订单 |
| `pay/refund` | pid + 订单号 + `money`（可选 `out_refund_no`） | 创建退款（走审核/账本） |
| `pay/refundquery` | pid + `trade_no`/`out_trade_no`/`refund_no` | 退款查询 |
| `merchant/info` | pid | 账户信息（余额、订单数） |
| `merchant/orders` | pid + 分页 | 订单列表 |
| `transfer/submit` | pid + `out_transfer_no` + `money` + 收款账户信息 | 代付申请 |
| `transfer/query` | pid + `transfer_no`/`out_transfer_no` | 代付查询 |
| `transfer/proof` | pid + 代付单号 | 代付凭证（占位返回状态） |
| `transfer/balance` | pid | 可提现余额 |

统一响应：成功 `code=0`，失败 `code<0` 且 `msg` 可读；**所有 V2 响应**都追加 `timestamp`（秒字符串）、`sign_type:"RSA"`、`sign`（平台私钥对除 `sign/sign_type` 外的响应字段签名）。

请求验签：查询商户 → 使用商户 RSA 公钥（`sign_type=RSA`）或 MD5 密钥；`timestamp` 超窗返回 `code=-3, msg=时间戳字段不正确`；签名失败 `code=-3`。

`api.php?s=pay/query` 与 `/api/pay/query` 等价（同一 handler）。

## 4. 零元购（0 元支付/伪造入账）修复设计

攻击面与对应措施：

1. **金额为 0 / 负数 / 超过两位小数 / 科学计数法**：`ParseAmount` 统一拒绝；最小 0.01 元，最大 `100000.00` 元（可配置 `payment.max_amount`）；下单、V2、回调三处复用同一校验。
2. **金额篡改**：订单金额在创建后不可变；幂等冲突（同 `out_trade_no` 不同金额）返回 `-1`；收银台/沙箱支付接口**不接受**客户端金额，只按订单金额扣款入账。
3. **回调金额伪造**：渠道回调必须 `money` 与订单 `amount` 完全一致，否则 409 且不入账（已有，补测试）。
4. **签名重放**：V1 下单签名与通知签名同算法（官方历史问题）。V2 起：
   - 请求只由商户私钥签名、平台用商户公钥验签；
   - 平台响应/通知只由平台私钥签名、商户用平台公钥验签；
   - V2 请求 `timestamp` ±300s，并按 `(merchant_id, sign)` 做一次性防重放（Redis，无 Redis 时落库 `epay_request_nonces`）；
   - 商户通知（`notification/epay`）在 V2 商户上使用平台私钥签名 + `sign_type=RSA`，V1 商户保持 MD5。
5. **未支付即入账**：只有 `callback.Service.Complete` 能把订单置为 `paid`；沙箱渠道仅在 `sandbox.enabled=true` 且非 production 时注册；无“手动标记已支付”的管理端接口。
6. **退款/代付金额**：退款累计不得超过订单实付；代付必须扣减可用余额或冻结，重复 `out_refund_no`/`out_transfer_no` 幂等。
7. **测试**（必须新增）：
   - `submit.php` 携带 `money=0`、`-1`、`0.001`、`1e3` 全部拒绝；
   - 伪造回调（错误 MD5 / 正确签名但金额不符 / 重放）不入账；
   - V2 请求：无 timestamp、过期 timestamp、错误签名、篡改 money、重复 sign 全部拒绝；
   - 沙箱关闭时 `/api/v1/sandbox/*` 返回 404/403，订单保持 pending。

## 5. 后端新增接口契约

### 5.1 收银台（公开）

- `GET /api/v1/cashier/orders/:trade_no`（已有）：返回订单展示信息，追加 `pay_type/pay_info/channel` 仅在已发起时存在。
- `POST /api/v1/cashier/orders/:trade_no/start`：发起/继续支付；调用 orchestrator；成功返回：
  ```json
  {"pay_type":"qrcode|jump|urlscheme|html|jsapi|scan|app|wxplugin|wxapp",
   "pay_info":"...","channel":{"id":"...","name":"...","plugin_code":"sandbox"},
   "status":"processing"}
  ```
  无可用渠道 → 503 `{"code":"no_channel","message":"没有可用的支付通道"}`；已支付 → 409；订单过期 → 410。
- `GET /api/v1/cashier/orders/:trade_no/status`：轻量轮询 `{status, paid_at}`。

### 5.2 沙箱渠道（仅开发/演示）

- 插件 `internal/plugin/payment/sandbox`，`Metadata().Code = "sandbox"`，`CreatePayment` 返回 `ActionJump` 到 `/pay/sandbox/{trade_no}`。
- `POST /api/v1/sandbox/orders/:trade_no/complete`：校验 `sandbox.enabled && environment != production`，按订单金额调用 `callback.Service.Complete`（`ChannelTradeNo = "SANDBOX"+trade_no`，`Digest = sha256("sandbox:"+order.ID)`），返回 `{status:"paid"}`。
- 注册条件：`cfg.Sandbox.Enabled`（默认 false；development 默认 true）。

### 5.3 商户端（`/api/v1/merchant`，商户会话）

- `GET /profile` → `{id, display_name, email, phone, status, created_at}`；`PUT /profile` `{display_name}`。
- `GET /dashboard` → `{today_order_count, today_paid_amount, total_paid_amount, available_balance, frozen_balance, pending_refunds, pending_settlements}`。
- `GET /credentials`（已有）、`POST /credentials/md5/rotate`（已有）。
- `GET /credentials/v2` → `{key_id, public_key, created_at, platform_public_key}`。
- `POST /credentials/v2/rotate` → `{key_id, private_key, public_key, platform_public_key}`（私钥仅返回一次）。
- `GET /transfers`、`POST /transfers`（`{out_transfer_no, amount(分), account_name, account_no, bank_name, branch_name?}`）、`GET /transfers/:id`。
- `GET /announcements`（公开同款）。
- 已有：`/orders`、`/orders/statistics`、`/orders/:id`、`/refunds`、`/settlements`。

### 5.4 运营后台（`/api/v1/admin`，管理员会话）

已有：dashboard、settings、audit-events、merchants、orders、channels、refunds、settlements、ledger、outbox。

新增：

- `GET /merchants/:id`：商户详情（含 MD5 密钥 key_id、V2 key_id、余额账户）。
- `GET /transfers`、`GET /transfers/:id`、`POST /transfers/:id/process` `{action:approve|reject, reason}`。
- `GET /announcements`、`POST /announcements` `{title, content, status}`、`PUT /announcements/:id`、`DELETE /announcements/:id`。
- `GET /stats/daily?days=14` → `[{date, order_count, paid_count, paid_amount}]`。
- `GET /stats/summary` → 顶部卡片数据（可复用 dashboard）。

### 5.5 公开

- `GET /api/v1/public/site` → `{name, logo_url, announcement, payment_types:["alipay","wxpay","qqpay"]}`。
- `GET /api/v1/public/announcements` → 公告列表（已发布）。

## 6. 数据模型新增（迁移文件按归属创建）

`000010_epay_v2.sql`（backend-v2）：

- `merchant_credentials.credential_type='rsa'`（复用现有表；`secret_encrypted` 存 PKCS8 私钥密文，`public_key` 存 PEM）。
- `system_settings` 键：`epay.v2.platform_private_key`（密文）、`epay.v2.platform_public_key`（PEM）。
- `epay_request_nonces(id VARCHAR(36) PK, merchant_id, sign_hash VARCHAR(64) UNIQUE, method VARCHAR(32), created_at, expires_at)` + 索引。

`000011_transfers_announcements.sql`（backend-ops）：

- `transfer_orders(id, transfer_no UNIQUE, merchant_id, out_transfer_no, amount CHECK>0, currency, account_name, account_no, bank_name, branch_name, status, ledger_transaction_id, rejection_reason, processed_by, processed_at, created_at, updated_at, UNIQUE(merchant_id,out_transfer_no))`。
- `announcements(id, title, content, status, published_at, created_at, updated_at)`。
- `payment_attempts` 追加 `action_type VARCHAR(20)`、`action_payload TEXT`（收银台复用已发起的支付动作）。

## 7. 前端（Vue 3 + Naive UI）

技术栈：Vue 3.5 + TypeScript + Vite 5 + vue-router 4 + pinia + naive-ui 2.4x + axios + dayjs。

路由与文件（`frontend/src/`）：

```
/                          pages/public/Landing.vue
/register                  pages/public/Register.vue
/login                     pages/public/Login.vue            （商户登录）
/admin/login               pages/public/AdminLogin.vue
/doc                       pages/public/Docs.vue             （V1/V2 接入文档）
/cashier.php               pages/cashier/Cashier.vue         （query 参数 trade_no）
/pay/sandbox/:tradeNo      pages/cashier/Sandbox.vue
/merchant/*  layout MerchantLayout.vue
  dashboard  orders  refunds  settlements  transfers  credentials  profile
/admin/*     layout AdminLayout.vue
  dashboard  merchants  orders  channels  refunds  settlements  transfers
  ledger  outbox  announcements  settings  audit
```

约定：

- 统一 `http`（axios 实例）处理 401：商户跳 `/login`，管理员跳 `/admin/login`，**登录接口自身 401 不跳转**（避免死循环）。
- 金额后端单位为“分”，前端统一 `formatMoney`。
- 表格使用 `n-data-table` 服务端分页；表单使用 `n-form`；状态使用 `n-tag`。
- 禁止在前端做权限判断替代后端鉴权；路由守卫只做体验层跳转。
- 所有页面必须 `npm run build` 通过（`vue-tsc` 严格类型）。

文件归属（避免冲突）：

- Lead：`frontend/package.json`、`vite.config.ts`、`tsconfig*`、`index.html`、`src/main.ts`、`src/App.vue`、`src/router/**`、`src/api/**`、`src/stores/**`、`src/layouts/**`、`src/components/**`、`src/styles/**`、`src/utils/**`。
- `fe-admin`：仅 `src/pages/admin/**`。
- `fe-merchant`：仅 `src/pages/public/**`、`src/pages/merchant/**`、`src/pages/cashier/**`。

## 8. 并行分工与写作用域

| 成员 | 写作用域 | 交付 |
| --- | --- | --- |
| lead | `internal/platform/app/app.go`、`internal/platform/config/config.go`（仅 Redis/Sandbox 字段合并）、`frontend` 脚手架、`docs/**`、`README.md`、`deploy/**` | 装配、集成、验收 |
| backend-v2 | `internal/compat/epay/**`、`internal/merchant/credential/**`、`internal/notification/epay/**`、`internal/transfer/**`、`migrations/000010_*`、`tests/contracts/epay/**` | V1 api.php、V2 协议、RSA 密钥、防重放、代付、通知签名 |
| backend-ops | `internal/plugin/payment/sandbox/**`、`internal/payment/cashier/**`、`internal/payment/orchestrator/**`、`internal/merchant/httpapi/**`（新增文件）、`internal/admin/httpapi/**`（新增文件）、`internal/announcement/**`、`internal/platform/redis/**`、`internal/platform/config/**`、`migrations/000011_*` | 沙箱、公告、商户资料/仪表盘、Redis、收银台 start/status（transfer_orders 表由本任务创建） |
| fe-admin | `frontend/src/pages/admin/**` | 11 个运营页面 |
| fe-merchant | `frontend/src/pages/public/**`、`pages/merchant/**`、`pages/cashier/**` | 官网/文档/登录注册/收银台/商户 7 页 |

规则：

- 禁止修改他人作用域文件；需要跨域改动先在任务里说明并由 Lead 执行。
- `internal/platform/app/app.go` 只由 Lead 修改；各包提供 `Register(...)` 或构造函数，签名见任务说明。
- 迁移编号不得重复；SQL 必须兼容 SQLite / MySQL / PostgreSQL。
- 每个成员先 `go build ./<自己的包>`，再跑自己的测试；最终全量由 Lead 执行。
- 提交前跑 `gofmt -w`（前端 `npm run build`）。

## 9. 验收清单

- [ ] `go build ./... && go vet ./... && go test ./...` 全绿。
- [ ] `npm --prefix frontend run build` 产出 `frontend/dist` 且 `index.html` 可被 Go SPA handler 命中。
- [ ] V1：`submit.php` / `mapi.php` / `api.php?act=*` 全通。
- [ ] V2：`/api/pay/create|query|close|refund|refundquery`、`/api/merchant/info|orders`、`/api/transfer/*` 全通且响应签名可验。
- [ ] 零元购用例全部拒绝，正常沙箱支付闭环成功。
- [ ] 未配置 Redis 正常启动；配置 Redis 后防重放/限流/锁生效。
- [ ] 文档：`docs/epay-v2.md`（接入说明）、`README.md`（启动/演示步骤）。
