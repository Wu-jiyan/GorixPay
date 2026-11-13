# 变更记录

本文件按模块记录仓库相对上一版的变更。写法参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，分类换成了模块，因为这个仓库的改动基本是一批跨层的交付，按 Added/Changed 切会把同一次交付拆散。

`v2.0.0` 于 2026-11-13 打上第一个 tag，下面是这个版本的全部内容。之前的两个 commit 是内部开发阶段，没有对外发过。

## [2.0.0] - 2026-11-13

### 协议

- 易支付 V1 入口全部对齐：`POST|GET /submit.php`、`POST /mapi.php`、`GET /api.php?act=query|order|orders|settle`、`POST /api.php?act=refund|refundapi`、`GET /submit2.php`、`GET /pay/{submit,checkpay,return,ok}/{trade_no}/`。入口清单与逐条上游依据在 `docs/epay-compat-matrix.md`。
- `mapi.php` 成功响应改成上游的扁平字段 `payurl`、`qrcode`、`urlscheme`、`html`（`Payment.php:246-296`），`pay_type`、`pay_info`、`msg` 保留为附加别名，老客户端不受影响。实现 `internal/compat/epay/payment/handler.go`，用例 `TestV1MapiActionFieldsPerPluginAction`。
- 鉴权失败改成 HTTP 200：JSON 入口回 `{"code":-3,...}`，`submit.php` 回 sysmsg HTML 页。上游从不返回 4xx，.NET、Java 这类客户端见到 4xx 会在读 body 之前抛异常。实现 `internal/compat/epay/auth/middleware.go`，用例 `TestV1MapiRejectsInvalidSignature`、`TestV1SubmitRejectsInvalidSignature`。
- 鉴权失败按原因分四个分支：`mapi.php` 缺 `pid` 回 `-4`，`pid` 为空回 `-1`，商户不存在回 `-3`，签名错回 `-3`（`mapi.php:2-5`、`Pay.php:280-281`、`ApiHelper.php:52-54`），用例 `TestV1AuthFailureCodes`。
- `submit.php` 按上游只读一组参数：query 里有 `pid` 就整组读 query，否则整组读 POST，另一组在验签之前丢弃（`Pay.php:12-18`）。`type` 为空时输出上游同款内联跳转脚本，带 urlencoded base64 的 `sitename`（`Pay.php:161`）。用例 `TestV1SubmitReadsOneParameterGroup`、`TestV1SubmitTypeEmptyRedirectsToCashier`。
- `api.php?act=order` 支持 `sign=md5(SYS_KEY.trade_no.SYS_KEY)` 的内部查询模式，`act=refundapi` 用 `key=md5(trade_no+SYS_KEY+trade_no)` 做平台内部退款（`api.php:56-59`、`api.php:147-160`）。系统密钥存在 `system_settings` 的 `epay.v1.system_key`，首次使用时生成，不出任何接口。
- 易支付 V2 覆盖 `pay/*`、`merchant/*`、`transfer/*` 共 12 个方法，`/api/{class}/{method}` 与 `api.php?s=class/method` 等价。RSA-SHA256 双向签名，`timestamp` 允许 ±300 秒，退款、关单、代付按签名哈希一次性消费防重放。参数只读 POST 体（`ApiHelper.php:47-51`），用例 `TestV2ApiPhpSuffixRejectsQueryOnlyParameters`。
- V2 响应签名按上游边界处理：异常响应不签名，业务失败但返回数组型数据时仍签名，`pay/create` 只签成功响应（`ApiHelper.php:30-42`），用例 `TestV2PayCreateFailureUnsigned`、`TestV2ResponseSignatureVerifiesInPHP`。
- 异步通知从 POST body 改成 GET 加 query string（`functions.php:519-522`），格式按订单 `version` 选 MD5 或 RSA，不再只看商户有没有 RSA 凭证（迁移 `000014`）。用例 `TestNotificationSignatureVerifiesInPHP`、`TestSenderKeepsLegacyMd5ForV1OrdersWhenMerchantHasRsa`。
- 浏览器付款完成后跳回商户 `return_url`，参数与通知同格式，上游 `verifyReturn()` 能验签（`functions.php:519-531`）。用例 `TestBrowserReturnCarriesVerifiableSignature`。
- 补齐 `method`、`device`、`sub_openid`、`sub_appid`、`auth_code`、`is_applet` 的解析与语义：`device=jump` 覆盖 method，`qq`/`wechat`/`alipay`/`app` 归一为 mobile，`method=scan` 按付款码前两位推钱包，jsapi 必填 `sub_openid`，scan 必填 `auth_code`（`Pay.php:296-321`、`functions.php:553-575`），用例 `TestV1MapiScanAndDeviceHints`。
- `act=query` 的 `type`、`account` 有真实来源：商户结算资料 `merchants.settle_type`、`merchants.settle_account`（迁移 `000016`），商户中心可填；`merchant/info` 同步反映。
- `merchant/info` 补齐 `order_num`、`order_num_today`、`order_num_lastday`、`order_money_today`、`order_money_lastday`、`pay_status`，`merchant/orders` 补 `refundmoney`，`transfer/query|proof` 补 `biz_no`、`out_biz_no`、`amount`、`cost_money`、`paydate`、`remark` 并把 `status` 改成上游的 0/1/2（`Merchant.php:20-47`、`Transfer.php:60-92`）。

### 支付插件

`internal/plugin/payment/` 下有 10 个插件。注册表（`internal/plugin/payment/registry.go`）按 code 去重、校验 metadata，`GET /api/v1/admin/plugins` 直接读注册表。

| 插件 | 上游 | 返回的动作类型 | 回调 |
| --- | --- | --- | --- |
| `epay` | 任意易支付站点 `/mapi.php` | jump、qrcode、urlscheme、html、jsapi、app、scan、wxplugin、wxapp | 专用路由 `/api/v1/payment/callback/epay/:channel_id` |
| `sandbox` | 本地沙箱，无外部网关 | jump | 无，由沙箱完成接口驱动 |
| `alipay` | `alipay.trade.precreate` | qrcode | RSA2 + 支付宝公钥 |
| `wechatpayv3` | `/v3/pay/transactions/{native,jsapi,h5}` | qrcode、jsapi、jump | 平台证书 + AES-256-GCM 解密 `resource` |
| `jeepay` | `/api/pay/unifiedOrder` | qrcode、jump、html | JSON + MD5 |
| `generic` | 可配置 `api_url` 加参数模板 | 9 种动作都支持 | 表单或扁平 JSON + 可选 MD5 |
| `usdt` | Epusdt `/api/v1/pay/create` | jump、qrcode | JSON 或表单 + MD5 |
| `qqpay` | 统一下单 XML | qrcode | XML + MD5 |
| `unionpay` | 全渠道 5.1.0 前台交易 | html、jump | 表单 + SHA256withRSA |
| `huifu` | `/v3/trade/payment/jspay` | qrcode、jsapi | `resp_data` + 汇付公钥，回执要求 `RECV_ORD_ID_<trade_no>` |

- 插件用 `ConfigFields()` 发布配置表单，`Secret` 字段管理端不回显；整份配置 AES-GCM 加密存 `payment_channels.config_encrypted`，无明文字段。
- 通用回调 `POST /api/v1/payment/callback/{plugin}/{channel_id}` 统一处理渠道选择、验签、金额比对、幂等（`Digest = sha256(plugin + channel_id + 原始报文)`）与入账后通知（`internal/payment/callback/plugin.go`）。用例 `TestPluginCallbackIsIdempotent`、`TestPluginCallbackRejectsAmountMismatch`。
- `CallbackResult.Ack` 让只认自定义回执的上游能停止重推，汇付是 `RECV_ORD_ID_<trade_no>`，未设置时回纯文本 `success`。
- 管理端 `POST /api/v1/admin/channels/:id/test` 只调插件下发一次订单，不落单、不改状态，运维用它验凭据（`internal/admin/httpapi/plugin.go`），用例 `TestAdminChannelTestInvokesPluginWithoutPersisting`。
- `internal/plugin/payment/paymenttest` 是插件契约测试的测试替身，不注册进生产注册表。

### 资金与账本

- 复式账本 `internal/ledger`：整数分记账，一笔交易写多条分录并校验借贷平衡，余额分可用与冻结，`ledger_transactions` 与 `ledger_entries` 一起提交。
- 退款走账本与幂等键：累计退款不超过订单实付，`out_refund_no` 重复提交返回原单（`internal/finance/service`），上游 `api.php?act=refund` 回 `code=0` 并带 `uid`、`reducemoney`（`Order.php:134`）。
- 结算单与代付：代付申请即冻结余额，审核通过扣减冻结，驳回释放（`internal/transfer`），用例 `TestCreateFreezesFundsAndIsIdempotent`、`TestApproveConsumesFreezeAndDebitsPlatform`。
- 余额充值与保证金走收银台下单，回调后入账一次，充值订单与销售订单用 `payment_orders.kind` 区分（迁移 `000013`），用例 `TestRechargePaymentCreditsMerchantBalanceOnce`、`TestDepositPaymentCreditsDepositAccountOnce`。
- 认证费与邀请返佣在支付完成的同一事务里入账，返佣按万分比向下取整，零额不落账（`internal/invite`），用例 `TestRebateHookCreditsInviterOnce`、`TestRebateRoundsDownAndSkipsZero`。

### 可靠性

- Transactional Outbox：资金变更与待发通知在同一事务提交，worker 用租约认领加 CAS 更新，失败按指数退避重试，超过上限进死信（`internal/outbox`），用例 `TestWorkerReclaimsExpiredLease`、`TestWorkerRetriesThenMovesMessageToDeadState`。
- 商户通知成功判定要求响应正文包含 `success`、`SUCCESS`、`Success` 三者之一，HTTP 200 但正文是 `ok` 或空一律算失败重试（`internal/notification/epay`），用例 `TestSenderRejectsOKAndEmptyHTTP200Bodies`。重试间隔在 `tests/contracts/epay/fixtures/notifications.yaml` 里固化。
- Redis 是可选项：配置了就用它做 V2 防重放与收银台下单限流；没配置时防重放回落到 `epay_request_nonces` 表（`internal/compat/epay/v2/replay.go`），限流不生效，其余功能不受影响。
- 进程收到 `SIGTERM` 后在 15 秒内停止接收新请求并关库，`/health/ready` 反映数据库可用性（`cmd/gorixpay/main.go`、`internal/platform/health`）。

### 生态

- 实名认证：个人与企业两种主体，提交后人工审核，通过时按设置收认证费，列表脱敏展示证件号（`internal/certification`），用例 `TestApprovalChargesCertificationFee`、`TestMaskedValuesHideSensitiveParts`。
- 支付域名白名单：商户提交域名，运营审核，支持 `*.example.com` 通配，下单时校验 `notify_url` 域名（`internal/domainauth`），用例 `TestDomainReviewFlowAndGuard`。
- 邀请返佣：邀请码注册绑定、支付成功后按万分比返佣、返佣记录与统计（`internal/invite`、`merchant/invite` 页面）。
- 风控：IP、域名、关键词、邮箱四类黑名单与商户日限额，命中写 `risk_events`，下单直接拒绝（`internal/risk`），用例 `TestBlacklistHitsAreRejectedAndRecorded`、`TestDailyAmountLimit`。
- 公告与站点设置：草稿与发布两种状态，公开接口只回已发布内容（`internal/announcement`）。

### 前端

- 前端从 React 换成 Vue 3.5 + TypeScript 5.6 + Vite 5 + Naive UI，36 个页面组件，覆盖官网、注册登录、收银台、商户中心、运营后台与开发文档页。
- 收银台是独立 SPA 路由 `/cashier.php`，带 sandbox 完成页 `/pay/sandbox/:tradeNo`，运营后台在 `/admin`，商户中心在 `/merchant`。
- 登录拦截器修复：401 跳转不再对登录接口自身生效，也不会在登录页重复 `location.replace`（记录见 `docs/notes/2026-11-login-redirect-loop.md`）。
- 渠道配置表单按 `GET /api/v1/admin/plugins` 返回的 `config_fields` 动态渲染，新增插件不需要改前端。

### 迁移工具

- `cmd/migrate-epay` 把 MySQL 或 SQLite 的易支付库迁到 GorixPay：预检（缺表、负数金额、精度、重复单号）、`--dry-run`、幂等重跑、`--batch-size` 分批、JSON 与 Markdown 双份报告，退出码 0 成功、1 运行错误、2 预检拦截（`internal/migration/epay`），用例 `TestDryRunDoesNotWriteTarget`、`TestApplyIsIdempotent`、`TestNegativeAmountBlocksPreflight`。
- 报告不打印迁移前的密文配置，只给字段名与长度（`TestReportDoesNotLeakPreConfigSecrets`）。

### 测试与验证

- 协议一致性：`tests/conformance/epay` 有 63 个测试函数、109 条用例，起真实 HTTP 服务打每个入口，约 14 秒跑完，本机有 PHP 7.4 时 0 条 skip。
- 上游 oracle：真 PHP 执行逐字复制的 `getSignContent`、`makeSign`、`verifySign`，签名逐字节比对（`TestPHPOracleSigningParity`）；再用上游自带 `EpayCore` 客户端 SDK 跑下单、查单、退款与通知验签（`TestRealClientSDKCreatesOrder`、`TestRealClientSDKRefundsOrder`）。
- 契约夹具：`tests/contracts/epay` 把路由、字段、错误码、签名向量、通知格式固化在 `manifest.yaml` 与 YAML 样本里，`tests/contracts/plugins` 校验注册表与 `plugins/manifest.yaml` 一致。
- 端到端：`internal/platform/app` 跑 sandbox 下单到通知的完整链路，以及认证、域名、邀请、风控、充值、保证金一整条生态链路（`TestEndToEndSandboxPaymentAndEpayProtocols`、`TestEcosystemCertificationDomainsInviteRiskAndFunding`）。
- 手工脚本：`scripts/e2e.ps1` 走浏览器视角的验收，`scripts/plugin-sandbox-check.ps1` 逐渠道打 `POST /admin/channels/:id/test`，`scripts/plugin-credentials-check.ps1` 校验凭据齐全并按 `config_fields` 建渠道。
- CI：`.github/workflows/ci.yml` 装 PHP 8.3 后跑 `git ls-files` 版的 gofmt 检查、`go build ./...`、`go vet ./...`、`go test ./... -count=1`（oracle 真跑，`EpayCore` SDK 那几条因为仓库里没有上游源码而 skip），前端 job 用 Node 20 跑 `npm ci` 与 `npm run build`。
- 全量规模：`go test ./...` 66 个包里 53 个有测试，全部通过；仓库 163 个 Go 文件、292 个 `Test` 函数、迁移 `000001` 到 `000016` 十六个文件，三套方言共用。
