# Epay 兼容性验证矩阵

本文回答一个问题：**这个 Go 实现到底是不是"能直接替换易支付"的复刻？** 答案由可执行的测试给出，不由文档自称。

三套测试各管一段，缺一不可：

| 层次 | 位置 | 管什么 | 为什么不够 |
| --- | --- | --- | --- |
| 契约夹具 | `tests/contracts/epay` | 把上游行为固化成 manifest 和 YAML 样例，保证字段、错误码、签名向量不被悄悄改动 | 只描述"应该是什么"，不证明服务真的这样回答 |
| 服务级一致性 | `tests/conformance/epay` | 启动真实 Gin 服务（内存 SQLite + sandbox 渠道 + 真商户凭据），用 HTTP 打每个入口，断言上游字段与错误码 | 自己打自己，仍可能与上游算法一起跑偏 |
| 上游 oracle | `tests/conformance/epay/php` | 真 PHP 7.4 执行**逐字复制**的上游函数；再用上游自带的 `EpayCore` 客户端 SDK 打通全流程 | 需要机器上有 php，没有时自动 skip |

## 怎么跑

```powershell
$env:GOCACHE='E:\go\build-cache'; $env:GOTMPDIR='E:\go\tmp'
go test ./tests/contracts/epay ./tests/conformance/epay -count=1 -v
```

- PHP 在 `PATH` 里（或设置 `EPAY_PHP_BIN`）时，oracle 与 SDK 用例会真跑；否则这几条 `t.Skip`，其余全跑。
- 上游源码不进仓库（GBK 编码，版权归原作者）。要跑客户端 SDK 用例，把它放到仓库外任意目录，用 `EPAY_UPSTREAM_DIR` 指过去；不指时那几条 `t.Skip` 并在输出里说明缺什么，其余照跑。oracle 脚本里的函数体是逐字复制的，不读上游目录。
- 当前规模：63 个测试函数、109 条用例（含子用例），一次全跑约 15 秒；本机 PHP 7.4 在 PATH 上、上游源码也指给了 `EPAY_UPSTREAM_DIR`，所以 0 条 skip。
- PHP 8.3 同样跑通全部 oracle 与 SDK 用例（`EPAY_PHP_BIN` 指向 8.3 实测，openssl 扩展需要打开），CI 用的是 8.3。

## 入口矩阵

上游行号来自上游源码的本地参照副本；"证据"列是 `tests/conformance/epay` 里真正跑过的用例。

### V1：页面与接口

| 入口 | 上游 | 我们 | 证据 |
| --- | --- | --- | --- |
| `POST/GET /submit.php` | `includes/lib/api/Pay.php:8` | 同路径，GET 有 `pid` 时整组读 GET，否则读 POST | `TestV1SubmitReadsOneParameterGroup`、`TestV1SubmitParameterErrorsRenderSysMessage`、`TestV1SubmitRejectsInvalidSignature` |
| `POST /mapi.php` | `Pay.php:263` | 同路径，成功返回 `code/trade_no` + 扁平 `payurl`\|`qrcode`\|`urlscheme`\|`html` | `TestV1MapiCreatesOrderWithFlatPayurl`、`TestV1MapiActionFieldsPerPluginAction`、`TestV1MapiValidatesParameters`、`TestV1MapiRejectsInvalidSignature` |
| `GET /api.php?act=query` | `api.php:13` | 明文 `pid+key`，RSA 商户被拒，返回账户全部字段 | `TestV1APIQueryReturnsUpstreamAccountFields`、`TestV1APIQueryRejectsBadCredentials` |
| `GET /api.php?act=order` | `api.php:54` | `trade_no` 优先；`bill_trade_no`/`payurl` 齐全；支持 `sign=md5(SYS_KEY.trade_no.SYS_KEY)` 内部模式 | `TestV1APIOrderReturnsUpstreamOrderFields`、`TestV1APIOrderSystemSignMode`、`TestV1APIOrderErrorCodes` |
| `GET /api.php?act=orders` | `api.php:86` | 分页 + `limit` 上限 50 | `TestV1APIOrdersPaginates`、`TestV1APIOrdersLimitCappedAtFifty` |
| `GET /api.php?act=settle` | `api.php:31` | 结算记录，`limit` 默认 10 上限 50 | `TestV1APISettleListsRecords` |
| `POST /api.php?act=refund` | `api.php:116` | 成功 `code=0`，含 `uid`/`reducemoney` | `TestV1APIRefundReturnsUpstreamFields`、`TestV1APIRefundErrorCodes` |
| `POST /api.php?act=refundapi` | `api.php:147` | 内部密钥 `md5(trade_no+SYS_KEY+trade_no)` | `TestV1APIRefundAPIInternalKey` |
| 未知 `act` | `api.php:171` | `{"code":-5,"msg":"No Act!"}` | `TestV1APIUnknownActReturnsNoAct` |
| `GET /pay/submit/{trade_no}/` | `pay.php:19` | 302 到收银台 / 已支付则转 `/pay/return/` | `TestBrowserReturnCarriesVerifiableSignature` |
| `GET /pay/return/{trade_no}/` | `functions.php:519` | 302 到商户 `return_url`，带与通知同格式的签名参数 | `TestBrowserReturnCarriesVerifiableSignature` |
| `GET /pay/ok/{trade_no}/` | `pages/ok.php` | 极简成功页 | 同上用例的收尾断言 |
| `GET /submit2.php?trade_no=&typeid=&retry=` | `submit2.php` | 未支付订单回收银台，未知/已完成订单渲染 sysmsg 页 | `TestSubmit2RetryPage` |
| `GET /cashier.php` | `cashier.php` | SPA 收银台（`frontend/src/pages/cashier/Cashier.vue`） | 前端构建 + 人工流程 |

### V2：`/api/{class}/{method}` 与 `api.php?s=`

| 方法 | 上游 | 关键契约 | 证据 |
| --- | --- | --- | --- |
| `pay/submit` | `Pay.php:8` | GET/POST 均可，渲染 HTML | `TestV2PaySubmitRedirects`、`TestV2PaySubmitRejectsBadAmount` |
| `pay/create` | `Pay.php:263` | 成功 `code=0` + `pay_type/pay_info` + 签名；失败不签名 | `TestV2PayCreateSuccessSigned`、`TestV2PayCreateFailureUnsigned` |
| `pay/query` | `Pay.php:535` | 条件字段过滤，响应签名可验 | `TestV2PayQuerySignedEnvelope`、`TestV2PayQueryMissingSelectorRejected` |
| `pay/close` | `Pay.php:622` | `code=0` | `TestV2PayCloseSuccessAndFailure` |
| `pay/refund` | `Pay.php:559` | 退款成功/超额/未支付 | `TestV2PayRefundSuccessAndFailures` |
| `pay/refundquery` | `Pay.php:597` | 退款单查询 | `TestV2PayRefundQuerySuccessAndFailures` |
| `merchant/info` | `Merchant.php:8` | 账户信息，字段名与上游一致（`order_num*`/`order_money*`/`pay_status`） | `TestV2MerchantInfoSuccessAndFailure` |
| `merchant/orders` | `Merchant.php:25` | `limit≤50`，复刻"POST 判存在、GET 取值"怪癖，含 `refundmoney` | `TestV2MerchantOrdersSuccessAndFailure`、`TestV2MerchantOrdersStatusQuirk` |
| `transfer/submit` | `Transfer.php:17` | 代付申请 | `TestV2TransferSubmitSuccessAndFailures` |
| `transfer/query` | `Transfer.php:42` | 代付查询 | `TestV2TransferQuerySuccessAndFailure` |
| `transfer/proof` | `Transfer.php:94` | 复刻 `out_biz_no` 查 `biz_no` 列的上游缺陷 | `TestV2TransferProofQuirk` |
| `transfer/balance` | `Transfer.php:119` | 可提现余额 | `TestV2TransferBalanceSuccessAndFailure` |

公共规则：`pid+timestamp+sign`，`timestamp` ±300 秒，参数只读 POST 体（`ApiHelper.php:47`），签名错误统一 `-3`，未知路由 `-5`，重放拒绝，`api.php?s=` 与路径等价。证据：`TestV2RejectsBadSignature`、`TestV2RejectsMissingTimestamp`、`TestV2RejectsStaleTimestamp`、`TestV2RejectsReplay`、`TestV2RejectsUnknownRoute`、`TestV2RejectsGETParameters`、`TestV2ApiPhpSuffixMatchesPathDispatch`、`TestV2ApiPhpSuffixRejectsQueryOnlyParameters`。

### 签名

| 项 | 上游 | 结果 |
| --- | --- | --- |
| 排序 | `ksort()` 字节序 | 一致 |
| 拼接 | `k=v&...`，不 URL 编码，不 trim | 一致（含前后空格用例） |
| 排除 | 数组、`null`、空串、纯空白、`sign`、`sign_type` | 一致 |
| MD5 | `md5(content + key)` 小写 | 逐字节一致 |
| RSA | SHA256 + PKCS#1 v1.5 + Base64 | 逐字节一致（确定性填充） |

证据：`TestPHPOracleSigningParity`（真 PHP 跑上游函数体，6 组参数矩阵逐字节比对）、`TestRSAVectorUsesSHA256PKCS1v15`、`TestMD5EmptyContentVector`。

### 通知

| 项 | 上游 | 结果 |
| --- | --- | --- |
| 传输 | GET + query string（`functions.php:519-524`） | 已改为 GET；`TestNotificationSignatureVerifiesInPHP` 用商户 SDK 验签通过 |
| 格式选择 | 订单 `version`：0=MD5、1=RSA（`functions.php:503`） | 已按订单版本选择，不按凭证有无；`TestSenderKeepsLegacyMd5ForV1OrdersWhenMerchantHasRsa` |
| 成功正文 | 含 `success/SUCCESS/Success` 才算成功 | `TestSenderRejectsOKAndEmptyHTTP200Bodies` |
| 重试 | 首次失败后约 1/3/19/55/115 分钟 | 契约夹具固化，见 `tests/contracts/epay/fixtures/notifications.yaml` |

### 真实客户端

`Epay/plugins/epay/inc/EpayCore.class.php` 是易支付官方发给商户的客户端类，直接 `require` 原文件、不复制不改写：

| 客户端方法 | 证据 |
| --- | --- |
| `apiPay()`（curl 打 `mapi.php`） | `TestRealClientSDKCreatesOrder` |
| `getPayLink()` → `submit.php` | `TestRealClientSDKPayLink` |
| `queryOrder()`（`api.php?act=order`） | `TestRealClientSDKQueriesOrder` |
| `refund()`（`api.php?act=refund`） | `TestRealClientSDKRefundsOrder` |
| `verifyNotify()` / `verifyReturn()` | `TestNotificationSignatureVerifiesInPHP`、`TestBrowserReturnCarriesVerifiableSignature` |

V2 响应签名另由 PHP `openssl_verify` 独立验证：`TestV2ResponseSignatureVerifiesInPHP`。

### 真实进程联调（不是 httptest）

测试跑的是进程内 HTTP 服务，另外用真实二进制验过一遍（`go run ./cmd/gorixpay` + 临时库 + 临时密钥）：

| 检查 | 结果 |
| --- | --- |
| `GET /health/ready` | 200 |
| `GET /cashier.php?trade_no=...` | 200，返回 SPA（`text/html`，含 `id="app"`） |
| `POST /mapi.php` 空请求 | `200 {"code":-4,"msg":"未传入任何参数"}` |
| `GET /api.php?act=nope` | `200 {"code":-5,"msg":"No Act!"}` |
| `GET /submit2.php?trade_no=<未支付>` | `302 → /cashier.php?trade_no=...` |
| `GET /pay/return/<未支付>` | `302 → /cashier.php?trade_no=...` |
| **上游 `EpayCore` SDK 打真实进程** | `{"code":1,"trade_no":...,"payurl":...}`，真 PHP 客户端、真监听端口、真签名 |

通道（插件）联调用 [`scripts/plugin-sandbox-check.ps1`](../scripts/plugin-sandbox-check.ps1)：汇总所有已配置渠道，逐个用真实凭据调 `POST /admin/channels/:id/test`，打印 `pay_type/pay_info` 或失败原因，退出码可用于 CI。

## 这次跑测试发现并修掉的偏差

| # | 偏差 | 上游依据 | 修复 |
| --- | --- | --- | --- |
| 1 | `mapi.php` 返回 `pay_type/pay_info`，SDK 读到的是空 | `Payment.php:245-296` | 改为扁平 `payurl/qrcode/urlscheme/html`，`pay_type/pay_info` 保留为附加别名 |
| 2 | 异步通知用 POST body，商户 `$_GET` 验签必失败 | `functions.php:519-522` | 改为 GET + query string |
| 3 | 通知格式只看商户有没有 RSA 凭证，V1 老集成被换成 RSA | `functions.php:503`、`Pay.php:171` | 新增 `payment_orders.version`（迁移 `000014`），按订单版本选格式 |
| 4 | 浏览器付款完成后不跳回 `return_url`，`verifyReturn()` 无从验签 | `functions.php:519-531` | 新增 `/pay/submit`、`/pay/checkpay`、`/pay/return`、`/pay/ok` 与收银台回跳 |
| 5 | V2 从 GET/query 取参数 | `ApiHelper.php:47-51` | 只读 POST 体；仅 `pay/submit` 支持 query |
| 6 | V2 异常响应带签名 | `ApiHelper.php:30-42` | 异常不签名，数组型业务失败仍签名 |
| 7 | `api.php?act=query` 缺 `key/type/account/username` | `api.php:24` | 全部补齐 |
| 8 | `act=refund` 缺 `uid/reducemoney` | `Order.php:139` | 补齐 |
| 9 | `merchant/info` 用 `orders/orders_today/orders_lastday`，SDK 找不到 `order_num*` | `Merchant.php:20` | 补 `order_num/order_num_today/order_num_lastday/order_money_today/order_money_lastday/pay_status`，旧名保留为别名 |
| 10 | `merchant/orders` 缺 `refundmoney` | `Merchant.php:47` | 按订单聚合已通过退款金额补齐 |
| 11 | `pay/refundquery` 缺 `uid/reducemoney` | `Pay.php:618` | 补齐 |
| 12 | `transfer/query|proof` 用 `transfer_no/out_transfer_no/money`，`status` 是字符串 | `Transfer.php:60-92` | 补 `biz_no/out_biz_no/amount/cost_money/paydate/remark`，`status` 改为上游 0/1/2，旧名保留为别名 |
| 13 | 签名失败返回 HTTP 401，上游从不返回 4xx | `functions.php:1430`（JSON 200）、`functions.php:382`（sysmsg HTML 200） | JSON 入口回 200 + 失败码，submit.php 回 200 + sysmsg 页；.NET/Java 这类"4xx 就抛异常"的客户端才不会直接崩 |
| 14 | 鉴权失败统一 `code:-3`，上游按原因区分 | `mapi.php:2-5`（缺 pid -4）、`Pay.php:280-281`（空 pid -1）、`ApiHelper.php:52-54`（商户不存在 -3） | 补齐四个分支并逐条固化：`TestV1AuthFailureCodes` |
| 15 | `method`/`device`/`sub_openid`/`auth_code`/`is_applet` 完全没解析 | `Pay.php:296-321`、`functions.php:553-575`（`getScanPayType`） | 解析并复刻语义：`device=jump` 覆盖 method、qq/wechat/alipay/app 归一为 mobile、`method=scan` 按付款码前两位推钱包（25-30 支付宝、10-15 微信、91 QQ、62 银行、01 数币）、jsapi 必填 `sub_openid`、scan 必填 `auth_code`。证据：`TestV1MapiScanAndDeviceHints`、`TestV2PayCreateMethodHints` |
| 16 | 付款人身份（`sub_openid`）不落库，JSAPI 从收银台发起时拿不到 openid | `Pay.php:425` 写 `pre_order.buyer` | 新增 `payment_orders.buyer`（迁移 `000015`），插件请求的 `Extra` 带 `sub_openid/sub_appid/auth_code/is_applet/method/device` |
| 17 | `act=query` 的 `type`/`account` 恒为 `0`/空串 | `api.php:28` 读 `pre_user.settle_id/account` | 新增商户结算资料（迁移 `000016`）：`merchants.settle_type/settle_account`，商户中心可填，`act=query` 与 `merchant/info` 实时反映 |

## 有意保留的偏差

这些不是漏掉，是明确选择，改动会影响现有调用方或安全性，所以留档：

| # | 偏差 | 理由 |
| --- | --- | --- |
| D1 | `mapi.php` 在扁平字段之外**额外**返回 `pay_type/pay_info/msg` | 纯增量；老客户端忽略未知字段 |
| D2 | `submit.php` 指定 `type` 时统一 302 到内部收银台，而不是直接渲染通道页面 | 收银台统一承担通道选择、二维码渲染、状态轮询；买家多点一次"立即支付"，或由收银台自动发起 |
| D3 | 通知里 `money` 固定两位小数（上游是 PHP float，可能是 `10`） | 两位小数对金额更安全；商户验签用的是我们发出去的原样字符串，签名自洽 |
| D4 | 后台/商户中心是 SPA，不是上游 `admin/*.php`、`user/*.php` | 产品形态不同；HTTP 协议面（`submit/mapi/api/pay/*`）保持一致 |
| D5 | 支付插件只做了 10 个（epay/sandbox/alipay/wechatpayv3/qqpay/unionpay/huifu/jeepay/generic/usdt） | 上游 `plugins/` 下 63 个渠道插件是渠道商务关系，不是协议；长尾用 `generic` 配置化接入 |
| D6 | `merchant/orders` 多回一个 `count`，`limit/offset` 非法时回未签名的 `-4` | 上游完全不校验 `limit`（`intval` 后直接进 SQL），多回计数对 SDK 安全；校验属加固 |
| D7 | `pay/close` 对已关闭订单幂等返回 `code=0` | 上游回 `-1`「该订单状态不支持关闭！」；幂等让客户端重试友好，业务结果一致 |
| D8 | `transfer/submit` 重复 `out_biz_no` 幂等返回原单 | 上游回 `-1`「该交易号已存在」；幂等防止网络重试造成重复代付 |
| D9 | `transfer/proof` 返回代付单 + 占位 `proof_url` | 上游把凭证查询交给通道插件（`Transfer::proof` → `Plugin::call('transfer_proof')`），凭证形状由插件决定 |
| D10 | `pay/query` 固定输出空字段，不做 `array_filter` | 上游会省略空字段；固定键名对强类型客户端更友好 |
| D11 | V2 增加重放保护（`pay/refund`、`pay/close`、`transfer/*`） | 上游没有防重放；这是为了堵住签名重放刷单，不影响正常调用 |
| D12 | `return_url` 自带 query 时，我们同样用 `&` 追加签名参数 | 与上游一致（`functions.php:525-530`）；这会让商户自己的参数进入 `$_GET` 并破坏 `verifyReturn()`，上游也是同样行为，测试里注释说明而不"修好"它 |
| D13 | `act=query` 的 `type`/`account` 来自商户结算资料（`merchants.settle_type/settle_account`），未设置时 `0`/空串 | 对应上游 `pre_user.settle_id/account`，商户在「账户资料」里填；`username` 仍用登录名，上游是开户名 |

## 不在这一层验证的部分

- 各上游通道的**真实网关**联调（需要真商户号/证书）：插件用 `httptest` mock 上游，验签与金额校验有单测。
- 上游后台的运营功能细节（代付审核流、利润分成、进件）：已实现主干，未逐页对齐上游交互。
- `cron.php` 的重试调度语义：以 outbox 重试近似，间隔在夹具里固化，未做逐秒对齐。
