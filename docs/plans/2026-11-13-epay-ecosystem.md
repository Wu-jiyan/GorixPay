# GorixPay 易支付生态补全实施计划（第二轮）

> 本文件是第二轮并行开发的唯一契约。已完成的 V1/V2、收银台、账本、代付、公告、Redis、Vue 基线不再改动语义，只在其上扩展。

## 1. 目标

1. 商户实名认证（个人/企业，提交 → 运营审核 → 通过/驳回，可配置认证费与强制认证）。
2. 支付域名白名单（商户提交、运营审核、下单时校验 notify_url / return_url 域名）。
3. 邀请返佣（邀请码注册绑定、订单支付后按比例返佣入账、返佣记录与统计）。
4. 风控与黑名单（IP/域名/关键词/商户日限额，命中记录 risk_events，后台可查可管）。
5. 商户余额充值 / 保证金（充值订单走收银台，支付成功后入账；保证金独立账户）。
6. 官方支付插件首批：支付宝当面付、微信支付 V3 Native/JSAPI/H5、Jeepay、通用 HTTP、USDT（Epusdt 风格），全部带 httptest mock 契约测试。
7. Epay 历史数据迁移 CLI（MySQL/SQLite 源，dry-run，预检 + 报告 + 幂等重跑）。
8. Vue3 商户端 / 运营端补齐上述页面；全量 go build/vet/test + 前端构建 + 端到端测试。

## 2. 已由 Lead 落地的扩展点（不要重复实现）

`internal/payment/service/create.go`：

```go
type Rejection struct{ Reason string }
func (r Rejection) Error() string

type Guard interface {
    Check(ctx context.Context, command CreateCommand) error
}
func (service *Service) WithGuards(guards ...Guard) *Service
```

- `Rejection` 会被 V1 `mapi.php`/`submit.php` 与 V2 `pay/create|submit` 映射为 `code=-1, msg=Reason`。
- 所有 Guard 在下单落库前执行；返回 error 即拒绝。

`internal/payment/callback/service.go`：

```go
type PostPaymentHook interface {
    OnPayment(ctx context.Context, tx *gorm.DB, order paymentmodel.Order) error
}
func (service *Service) WithHooks(hooks ...PostPaymentHook) *Service
```

- Hook 在“订单置 paid + 商户入账 + Outbox”同一事务内执行。
- `payment_orders.Kind`：`sale`（默认）/`recharge`/`deposit`；只有 `sale` 生成商户通知与返佣；`recharge` 走商户余额入账；`deposit` 由 Hook 记入保证金账户。
- `Order.IsSale()` 已实现。

## 3. 数据模型

### 3.1 迁移 `000012_ecosystem.sql`（backend-v2 负责）

```sql
ALTER TABLE merchants ADD COLUMN invited_by VARCHAR(36);
ALTER TABLE merchants ADD COLUMN invite_code VARCHAR(32);
CREATE UNIQUE INDEX idx_merchants_invite_code ON merchants(invite_code);

CREATE TABLE merchant_certifications (
  id VARCHAR(36) PRIMARY KEY,
  merchant_id VARCHAR(36) NOT NULL UNIQUE,
  cert_type VARCHAR(10) NOT NULL,          -- person | corp
  real_name_encrypted TEXT NOT NULL,
  id_no_encrypted TEXT NOT NULL,
  corp_name VARCHAR(128),
  corp_no VARCHAR(64),
  status VARCHAR(20) NOT NULL,             -- pending | approved | rejected
  fee_cents BIGINT NOT NULL DEFAULT 0,
  rejection_reason VARCHAR(255),
  reviewer_id VARCHAR(36),
  submitted_at TIMESTAMP NOT NULL,
  reviewed_at TIMESTAMP,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);

CREATE TABLE merchant_domains (
  id VARCHAR(36) PRIMARY KEY,
  merchant_id VARCHAR(36) NOT NULL,
  domain VARCHAR(191) NOT NULL,
  status VARCHAR(20) NOT NULL,             -- pending | approved | rejected
  rejection_reason VARCHAR(255),
  reviewer_id VARCHAR(36),
  created_at TIMESTAMP NOT NULL,
  reviewed_at TIMESTAMP,
  UNIQUE (merchant_id, domain)
);

CREATE TABLE invite_rebates (
  id VARCHAR(36) PRIMARY KEY,
  inviter_id VARCHAR(36) NOT NULL,
  invitee_id VARCHAR(36) NOT NULL,
  order_id VARCHAR(36) NOT NULL UNIQUE,
  trade_no VARCHAR(32) NOT NULL,
  order_amount BIGINT NOT NULL,
  rebate_amount BIGINT NOT NULL,
  rate_bp INT NOT NULL,
  created_at TIMESTAMP NOT NULL
);

CREATE TABLE blacklist_entries (
  id VARCHAR(36) PRIMARY KEY,
  entry_type VARCHAR(20) NOT NULL,         -- ip | domain | keyword | email
  value VARCHAR(191) NOT NULL,
  remark VARCHAR(191),
  created_at TIMESTAMP NOT NULL,
  UNIQUE (entry_type, value)
);

CREATE TABLE risk_events (
  id VARCHAR(36) PRIMARY KEY,
  merchant_id VARCHAR(36),
  rule_code VARCHAR(50) NOT NULL,
  detail VARCHAR(500) NOT NULL,
  client_ip VARCHAR(45),
  created_at TIMESTAMP NOT NULL
);
```

### 3.2 迁移 `000013_funding.sql`（backend-ops 负责）

```sql
ALTER TABLE payment_orders ADD COLUMN kind VARCHAR(20) NOT NULL DEFAULT 'sale';
CREATE INDEX idx_payment_orders_kind ON payment_orders(kind);
```

保证金用账本账户表达：`owner_type='merchant', purpose='deposit', category='liability'`，无需新表。

## 4. 系统设置键（默认值）

| key | 默认 | 说明 |
| --- | --- | --- |
| `certification.required` | `false` | 未认证禁止下单 |
| `certification.fee_cents` | `0` | 认证通过扣除的费用（分） |
| `payment.domain_whitelist_required` | `false` | 下单校验 notify_url 域名 |
| `invite.enabled` | `false` | 邀请返佣开关 |
| `invite.rate_bp` | `0` | 返佣比例（万分比，100 = 1%） |
| `risk.daily_amount_limit_cents` | `0` | 商户单日成功金额上限（0 不限） |
| `risk.daily_order_limit` | `0` | 商户单日订单数上限（0 不限） |
| `risk.ip_daily_limit` | `0` | 单 IP 单日下单数上限（0 不限） |
| `merchant.deposit_required_cents` | `0` | 保证金下限（展示/提示用） |

设置统一存 `system_settings`，值为 JSON（布尔用 `true`/`false`，数字用 JSON number 字符串）。

## 5. 后端接口

### 5.1 实名认证（商户）

- `GET /api/v1/merchant/certification`
  → `{status, cert_type, real_name_masked, id_no_masked, corp_name, corp_no, fee_cents, required, submitted_at, reviewed_at, rejection_reason}`
- `POST /api/v1/merchant/certification` `{cert_type:"person"|"corp", real_name, id_no, corp_name?, corp_no?}`
  → 201 `{status:"pending"}`；已通过返回 409；身份证 18 位校验（最后位 X）；企业需 corp_name + corp_no。
- 运营：
  - `GET /api/v1/admin/certifications?status=&keyword=&page=&page_size=`
  - `POST /api/v1/admin/certifications/:merchant_id/review` `{action:"approve"|"reject", reason?}`
  - 通过时若 `certification.fee_cents>0`，从商户可用余额扣费（账本分录 `cert-fee:{merchantID}`，不足则 400）。
- 强制认证：Guard 读 `certification.required`，未 `approved` 返回 `Rejection{Reason:"请先完成实名认证"}`。

### 5.2 域名白名单

- `GET /api/v1/merchant/domains` → `{items:[{id,domain,status,rejection_reason,created_at,reviewed_at}]}`
- `POST /api/v1/merchant/domains` `{domain}` → 201；允许 `example.com` 或 `*.example.com`；去重；格式校验。
- `DELETE /api/v1/merchant/domains/:id`（仅 pending/approved 可删）
- 运营：
  - `GET /api/v1/admin/domains?status=`
  - `POST /api/v1/admin/domains/:id/review` `{action:"approve"|"reject", reason?}`
- 下单校验：`payment.domain_whitelist_required=true` 时，取 `notify_url` 的 host；命中该商户 `approved` 的精确域名或 `*.父域` 才放行，否则 `Rejection{Reason:"当前支付域名未授权"}`。`return_url` 若存在也做同样校验。

### 5.3 邀请返佣

- 注册接口扩展：`POST /api/v1/auth/register` 增加可选 `invite`（邀请码）；新商户生成 8 位 `invite_code`，写入 `invited_by`。
- `GET /api/v1/merchant/invite` → `{enabled, rate_bp, invite_code, invite_url, invited_count, today_income, total_income}`
- `GET /api/v1/merchant/invite/records?page=&page_size=` → 分页 `invite_rebates`（展示 invitee display_name、trade_no、order_amount、rebate_amount、created_at）
- 返佣 Hook：订单 paid 且 Kind=sale 时，若 inviter 存在且 `invite.enabled=true`：
  - `rebate = order.Amount * rate_bp / 10000`（向下取整，0 则跳过）；
  - 账本：inviter 可用余额 credit / 平台清算 debit，幂等键 `rebate:{orderID}`；
  - 写 `invite_rebates`，唯一键 order_id 保证重放不重复。
- `GET /api/v1/admin/invites?page=` → 返佣记录（可按 inviter_id 过滤）。

### 5.4 风控 / 黑名单

- `GET /api/v1/admin/blacklist?entry_type=`、`POST /api/v1/admin/blacklist` `{entry_type,value,remark}`、`DELETE /api/v1/admin/blacklist/:id`
- `GET /api/v1/admin/risk-events?page=&merchant_id=&rule_code=`
- 商户：`GET /api/v1/merchant/risk-events?page=`（只看自己的）
- Guard 顺序：认证 → 域名 → 黑名单 → 限额。命中写 `risk_events` 并拒绝（通用文案，避免给攻击者信息；黑名单命中用 `Rejection{Reason:"订单提交失败，请稍后重试"}`）。
- 限额统计：当日 `paid` 订单数/金额（按 `merchant_id`），以及同 `client_ip` 当日订单数。

### 5.5 充值 / 保证金（backend-ops）

- `POST /api/v1/merchant/recharge` `{amount(分)}` → 创建 `kind=recharge` 订单（`PaymentType="cashier"`，`NotifyURL={PublicURL}/api/v1/funding/notify`），返回 `{trade_no, amount, cashier_url}`
- `GET /api/v1/merchant/recharge` → 充值订单分页（Kind=recharge）
- `GET /api/v1/merchant/deposit` → `{deposit_balance, required_cents, enabled}`
- `POST /api/v1/merchant/deposit` `{amount}` → `kind=deposit` 订单，返回同上
- `GET /api/v1/merchant/deposit/orders` → 保证金订单分页
- 充值订单支付成功：商户可用余额 +amount（`postIncome` 已覆盖 recharge）。
- 保证金订单支付成功：Hook 给 `purpose=deposit` 账户 credit，平台 debit，幂等 `deposit:{orderID}`。
- `POST /api/v1/internal/funding/notify` 不需要：充值订单用平台自身 URL 只是占位，实际入账由渠道回调驱动。
- 非 sale 订单不允许退款（退款接口校验 Kind）。

### 5.6 插件管理

- `GET /api/v1/admin/plugins` → 注册表元数据 `[{code,name,capabilities,config_fields:[{key,label,type,required,secret}]}]`
- 渠道创建/更新接受 `config` 对象（map[string]string），后端加密存储；旧字段 `api_url/pid/key` 继续兼容并映射进 config。
- `POST /api/v1/admin/channels/:id/test` `{payment_type, amount}` → 调用插件创建一笔测试支付，返回 `{ok,pay_type,pay_info,error}`（不落订单）。

## 6. 支付插件契约（backend-plugins）

在现有 `pluginpayment.Plugin` 之上扩展可选能力接口（不要破坏旧插件）：

```go
type ConfigField struct { Key, Label, Type string; Required, Secret bool }
type Configurable interface { ConfigFields() []ConfigField }
type CallbackVerifier interface {
    VerifyCallback(ctx context.Context, r *http.Request, config map[string]string) (CallbackResult, error)
}
type CallbackResult struct { TradeNo, ChannelTradeNo string; Amount int64; Paid bool; Raw map[string]string }
```

首批插件（`internal/plugin/payment/` 下）：

1. `alipay`：当面付 `alipay.trade.precreate`，RSA2 签名；回调验签（支付宝公钥）；返回 `ActionQRCode`。
2. `wechatpayv3`：Native/JSAPI/H5 下单，APIv3 商户私钥签名 + 平台证书验签/解密回调；返回 `ActionQRCode`/`ActionJSAPI`。
3. `jeepay`：`/api/pay/unifiedOrder`，MD5 签名；返回二维码/跳转。
4. `generic`：可配置 URL/参数模板/签名方式的通用上游（覆盖其余长尾渠道）。
5. `usdt`：Epusdt 风格 `POST /api/v1/pay/create`，`order_id/amount/notify_url/redirect_url` + `sign`（MD5），回调验签。

测试要求：全部用 `httptest.Server` mock 上游，不发真实网络；覆盖成功、验签失败、金额不一致、超时。

## 7. Epay 数据迁移 CLI（backend-migrate）

命令：

```powershell
go run ./cmd/migrate-epay --source-driver mysql --source-dsn "..." --target-driver sqlite --target-dsn gorixpay.db --dry-run --report migrate-report.md
```

- 预检：源表存在性、`pre_config` 版本、金额精度、重复业务键、无法映射字段。
- 迁移：`pre_user`→merchants（uid→legacy_uid、pwd→password_hash 保留 MD5 待登录升级、money→期初账本）、`pre_order`→payment_orders、`pre_refundorder`→refund_orders、`pre_settle`→settlement_orders、`pre_transfer`→transfer_orders、`pre_config`→system_settings、`pre_domain`→merchant_domains、`pre_blacklist`→blacklist_entries。
- 期初余额：每商户写 `opening-balance:{uid}` 账本交易（merchant credit / platform debit），幂等。
- 幂等：重复运行按 legacy_uid/trade_no 跳过；报告 JSON + Markdown（记录数、金额合计、跳过、阻断项）。
- 金额差异、重复 trade_no、负数金额 → 阻断退出码 2。
- 测试：用 SQLite 源库按 Epay 表结构建 fixture，跑两次验证幂等与报告。

## 8. 前端路由与归属

新增路由（Lead 已加到 router）：

商户中心：
- `/merchant/certification` 实名认证
- `/merchant/domains` 支付域名
- `/merchant/invite` 邀请返佣
- `/merchant/recharge` 余额充值 / 保证金

运营后台：
- `/admin/certifications` 实名审核
- `/admin/domains` 域名审核
- `/admin/risk` 风控与黑名单
- `/admin/plugins` 插件与渠道
- `/admin/invites` 返佣记录

文件归属：
- fe-merchant：`pages/merchant/{Certification,Domains,Invite,Recharge}.vue`
- fe-admin：`pages/admin/{Certifications,Domains,Risk,Plugins,Invites}.vue`
- Lead：router、api/types、layouts 菜单。

## 9. 验收

- `go build ./... && go vet ./... && go test ./...` 全绿。
- 新增端到端测试覆盖：认证提交/审核/强制拦截、域名审核/拦截、邀请返佣入账、黑名单拦截、充值入账、保证金入账、插件 mock 下单。
- `npm --prefix frontend run build` 通过，新增页面可访问。
- 迁移 CLI 在 fixture 上 dry-run 与实跑均通过，二次运行记录数不变。
