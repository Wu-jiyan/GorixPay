# GorixPay 安全模型

GorixPay 是易支付（彩虹易支付）协议的 Go 实现。商户、渠道上游、平台三方之间的信任关系落在入口函数和数据库约束里，这里记录边界位置、资金校验点、以及为兼容性保留的风险面。

## 1. 信任边界

### 商户侧

商户持有两样东西之一：MD5 密钥，或 V2 RSA 私钥。入口用 `pid` 定位商户，再用 `internal/merchant/credential/` 解出密钥验签。

| 能力 | 入口 | 约束 |
| --- | --- | --- |
| 下单 | `submit.php`、`mapi.php`、`/api/pay/create` | 只能用自己的 `pid`，金额受 `paymentmodel.ValidateAmount` 与解析器限制 |
| 查单、关单 | `api.php?act=order`、`act=orders`、`/api/pay/query`、`/api/pay/close` | 查询条件强制带 `merchant_id` 或 `legacy_uid` |
| 退款 | `api.php?act=refund`、`/api/pay/refund` | 累计退款不得超过订单实付（`internal/finance/service/service.go:115`） |
| 代付 | `/api/pay/transfer/submit`、`/api/v1/merchant/transfers` | 申请即冻结余额，余额不足直接失败 |
| 读密钥 | `GET /api/v1/merchant/credentials/v2` | 只回公钥和平台公钥，私钥只在轮换响应里出现一次 |

做不到的事按代码路径列：读别的商户订单没有入口，`internal/compat/epay/payment/api.go:262` 和 `:504` 把商户条件写死在查询里；改订单金额没有入口，`payment_orders.amount` 只在 `Service.Create` 写入（`internal/payment/service/create.go:86`）；改订单状态没有入口，唯一把订单置 `paid` 的函数是 `callback.Service.Complete`（`internal/payment/callback/service.go:80`）。

### 渠道上游

渠道回调来源按不可信处理。`internal/payment/callback/http.go:35` 的 `callback` 先按 `channel_id` 找 active 渠道，再解密渠道配置验签：epay 插件校验 `pid`、`trade_status=TRADE_SUCCESS` 与 `md5(canonical + key)`（`callback/http.go:57-61`），然后解析 `money`、拼 `sha256(channelID + "\n" + canonical)` 作为幂等摘要（`callback/http.go:77-82`）。

上游能做的只有一件事：把 pending 或 processing 的订单推到 paid，且金额必须与订单完全相等。上游不能创建订单、不能读商户列表、不能发起退款，回调里出现的 `trade_no` 只用于定位订单，不参与金额决定。

### 公网

未登录可访问的只有收银台展示接口和支付动作。`GET /api/v1/cashier/orders/:trade_no` 返回 `trade_no`、`name`、`amount`、`currency`、`payment_type`、`status`、`kind`、`return_url`、时间字段（`internal/compat/epay/payment/handler.go:325-343`），不含 `notify_url` 和 `attach`。发起支付（`cashier.go:94` 的 `start`）不接受客户端金额，只读订单行的 `Amount`。

沙箱完成接口只在启用沙箱时注册（`internal/payment/cashier/cashier.go:89-90`），关闭时路径不存在，请求落到 `NoRoute` 返回 404。

## 2. 密钥

### 主密钥

配置项 `Security.MasterKeyFile`，生产环境未配置直接启动失败（`internal/platform/config/config.go:104`）。`secret.LoadKeyFile` 接受 32 字节原始内容或它的 base64（`internal/platform/secret/keyfile.go:10-22`）。`internal/platform/app/app.go:108-113` 加载后调用 `secret.NewCipher(key, "v1")`。

`Cipher.Encrypt`（`internal/platform/secret/cipher.go:36-44`）用 AES-256-GCM，随机 nonce，输出 `v1.<base64url(nonce||sealed)>`。AAD 由调用方传入，因此同一主密钥下不同用途的密文不能互换。

### 入库加密

| 数据 | AAD | 位置 |
| --- | --- | --- |
| 渠道配置（含 pid、key、扩展字段） | `payment-channel:<channelID>` | `internal/channel/config/service.go:54-64` |
| 商户 MD5 密钥 | `<merchantID>:md5` | `internal/merchant/credential/service.go:90` |
| 商户 RSA 私钥（PKCS#8 PEM） | `<merchantID>:rsa` | `internal/merchant/credential/rsa.go:70` |
| 平台 V2 私钥 | `system:epay.v2.platform_private_key` | `internal/compat/epay/v2/signer.go:24`、`:96` |

公钥按公开数据处理：商户 RSA 公钥明文存 `public_key`，平台公钥明文存 `system_settings` 的 `epay.v2.platform_public_key`。平台密钥对在首次 `LoadSigner` 时生成 2048 位（`signer.go:51-96`）。

渠道配置解密只发生在使用点：回调验签（`callback/http.go:48`）和插件下单前。没有把解密结果写回数据库或缓存的路径。

### 不回显与日志

管理端渠道列表返回 `configured` 布尔值，不返回配置内容（`internal/admin/httpapi/handler.go:255`）。商户凭据列表只返回 `key_id`、类型、状态、时间（`internal/merchant/httpapi/credential.go:46`）。明文只在轮换响应里出现一次：`POST /api/v1/merchant/credentials/md5/rotate` 回 `secret`（`credential.go:65-66`），`POST /api/v1/merchant/credentials/v2/rotate` 回 `private_key`（`internal/compat/epay/v2/merchantroutes.go:60`）。

访问日志字段是 `request_id`、`method`、`path`、`status`、`duration_ms`（`internal/platform/httpserver/server.go:48-54`），不记 query、body、header，所以密钥不会从平台日志泄漏。第三方网关是否记录完整 URI 取决于对方配置，见第 6 节。

### 轮换

`RotateMD5` 在事务里把旧 active 置 `revoked` 并填 `revoked_at`，再插入新凭据（`internal/merchant/credential/service.go:42`）。`RotateRSA` 同构（`rsa.go:23`）。轮换立即生效，旧密钥下一次请求就失败，商户需要先更新 SDK 再轮换。

## 3. 资金校验

金额解析有两套，差异是兼容性带来的。

V1 入口的 `ParseAmount`（`internal/compat/epay/payment/handler.go:430`）用 `big.Rat` 转分并四舍五入到分，所以 `1.001` 得到 100，这是上游 `money` 允许三位小数的怪癖。V2 的 `ParseAmount`（`internal/compat/epay/v2/amount.go:23`）只接受最多两位小数、大于 0、不超过 100000.00 元（`MaxAmountCents`，`amount.go:13`）。`0`、`-1`、`0.001`、`1e3`、`100000.01` 在 V2 全部拒绝。

创建后不可变靠两处：

1. `Service.Create`（`create.go:86`）是唯一写 `amount` 的函数，状态机只更新 `status`、`channel_trade_no`、`paid_at`。
2. 同一个 `(merchant_id, out_trade_no)` 再次提交时，`compareCommand`（`create.go:177`）比较金额、名称、`notify_url`、`client_ip`、`attach`，任一不同返回 `ErrIdempotencyConflict`，调用方拿到 `-1`。

回调金额严格相等在 `Complete` 的事务里判断：`result.Amount != completion.Amount` 返回 `ErrAmountMismatch`（`internal/payment/callback/service.go:105`），订单状态不变，回调记不进 `payment_callbacks`。epay 回调的 `money` 先经过 `parseCallbackAmount`（`callback/http.go:90-107`）校验格式。

零元购的拦截点：

- `paymentmodel.ValidateAmount`（`internal/payment/model/order.go:95`）拒绝小于等于 0。
- 两套 `ParseAmount` 拒绝 0、负数、科学计数法、三位以上小数（V2）。
- 收银台和沙箱完成不读客户端金额，用订单行里的 `order.Amount`（`cashier.go:192`）。
- 退款累计上限 `reserved + request.Amount > order.Amount`（`finance/service/service.go:115`，`reserved` 是该订单非 rejected 退款之和）。

## 4. V2 防重放

V2 请求先验签名，再消费一次性 nonce，顺序在 `authenticate`（`internal/compat/epay/v2/handler.go:230`）里固定。

`timestamp` 校验在 `handler.go:250-257`：字段必须存在、必须是 10 位十进制秒、`|now - ts| <= 300s`（`TimestampTolerance` 常量在 `handler.go:29`）。`sign_type` 必须是 `RSA`（`:258`），公钥取自商户 active RSA 凭据（`:265-274`）。

是否消费 nonce 由 `requiresOnce`（`:276`）判断，覆盖范围如下：

| 方法 | 消费 nonce | 说明 |
| --- | --- | --- |
| `pay/refund` | 是 | 重复退款会二次扣款 |
| `pay/close` | 是 | 状态迁移只允许一次 |
| `transfer/submit` | 是 | 重复申请会重复冻结 |
| `transfer/query`、`transfer/proof`、`transfer/balance` | 是（`transfer/` 前缀） | 与代付同组，统一消费 |
| `pay/query` | 否 | 只读 |
| `pay/create`、`pay/submit` | 否 | 幂等由 `(merchant_id, out_trade_no)` 唯一索引保证 |

键的构成：`ReplayKey(merchant.ID, method, signature)` 对 `sign` 求 sha256 后拼 `merchantID:method:hash`（`internal/compat/epay/v2/replay.go:40-44`）。TTL 常量 `NonceTTL = 10 * time.Minute`（`replay.go:23`）。

存储有两条路径，语义一致：

- 配置了 Redis 时 `Client.Once` 用 `SET NX` 加 TTL（`internal/platform/redis/client.go:82-90`），只有一个调用者拿到 true。
- 未配置 Redis 时落 `epay_request_nonces` 表，`sign_hash` 有唯一约束，插入用 `INSERT ... ON CONFLICT DO NOTHING`（`replay.go:79`），`RowsAffected == 1` 才算首次，其余返回 `-3 重复请求`（`handler.go:282-284`）。

并发语义：两条路径都把「首次」判定交给存储层的原子操作，Go 侧不读后写，因此同一签名并发到达时最多一个成功。插入前会删除 `expires_at < now` 的旧行（`replay.go:68`），清理不参与判定。

退款、关单、代付按签名消费的理由：这三个动作有资金或状态副作用，抓包重放必须在 5 分钟 timestamp 窗口内也失败。查询天然幂等，下单有业务唯一键，把它们的签名也塞进 nonce 表只会放大 Redis 与数据库写入。

## 5. 商户通知

通知用 GET，参数拼在 query 上（`internal/notification/epay/sender.go:73`、`:114-124`），这是上游 `curl_get` 的行为，商户 SDK 读 `$_GET`。后果是完整 URL 会出现在商户访问日志、反向代理日志、CDN 缓存键和浏览器历史里，`sign` 与金额都在 URL 上。平台自己的 accessLog 不记 query（`httpserver/server.go:51`），链路上其他环节记不记由对方决定。

正因如此，通知必须验签，并且商户要用自己订单表里的金额和状态做二次核对，不能只看 `trade_status`。

MD5 通知：`notificationValues`（`sender.go:144-161`）把 `pid`、`trade_no`、`out_trade_no`、`type`、`name`、`money`、`trade_status=TRADE_SUCCESS` 与 `sign_type=MD5` 一起算 `md5(canonical + key)`。

RSA 通知：订单 `Version >= 1` 且商户有 active RSA 凭据时走 `notificationValuesRSA`（`sender.go:165-190`），字段多出 `api_trade_no`、`param`、`buyer`、`timestamp`，`sign_type=RSA`，用平台私钥签。商户用平台公钥验，公钥从 `GET /api/v1/merchant/credentials/v2` 的 `platform_public_key` 取（`v2/merchantroutes.go:18-38`）。

canonical 规则与请求签名一致：剔除 `sign`、`sign_type`、空值，按 key ASCII 升序拼 `k=v&...`，不做 URL 编码（`internal/compat/epay/signing/signing.go:18-32`）。验签时 `sign` 本身不参与签名。

应答判定：响应体包含 `success`、`SUCCESS`、`Success` 之一才算送达（`sender.go:90`），HTTP 状态码不参与判断。验签失败的通知应当直接丢弃，不要回 success。

## 6. 已知风险面

Epay V1 的 MD5。签名是 `md5(canonical + key)`（`signing/signing.go:34-37`），密钥参与运算，单纯 MD5 碰撞不构成伪造。真正的上限在密钥泄漏与时间：V1 入口不校验 `timestamp`（`internal/compat/epay/auth/middleware.go:34-37`），签名请求可以无限期重放。下单靠 `out_trade_no` 幂等挡住重复建单，退款靠 `out_refund_no` 幂等（`api.go:335-345`），关单靠状态条件，三处都不依赖时间窗。

`api.php` 的内部密钥。`epay.v1.system_key` 明文存在 `system_settings`（`internal/compat/epay/payment/api.go:35`、`:419-441`），用于 `act=order` 的 `sign=md5(SYS_KEY.trade_no.SYS_KEY)` 模式（`api.go:134-141`、`:167-181`）和 `act=refundapi`（`api.go:369-396`）。它不出现在任何接口响应里，但能读库的人可以查任意订单，能写库的人可以用内部退款对任意订单退款。这个键的信任等级等于数据库读写权限。

明文 pid/key。`act=query`、`act=order`、`act=orders`、`act=settle` 用 GET 传 `pid` 和 `key`（`api.go:496-519`），`act=query` 还会把收到的 key 原样回显（`api.go:115-131`）。密钥会进反代日志、浏览器历史和商户侧脚本。部署时应在网关层限制 `api.php` 来源并对 query 打码，缩短密钥轮换周期。

公网可读的订单信息。`trade_no` 由 `yyyymmddHHMMSS` 加 5 位随机数生成（`internal/payment/service/create.go` 的 `generateTradeNo`），收银台展示接口不鉴权（`handler.go:325-343`），返回 `name`、`amount`、`return_url`。知道或枚举到 `trade_no` 的人能读到这三项。没有绑定会话的收银台页面默认采用这个模型，商户不应在 `name` 里放敏感信息。

沙箱渠道。启用条件在 `app.go:146`：`cfg.Sandbox.Enabled && cfg.Environment != "production"`，配置层在 production 强制关闭（`config.go:107-109`）。路由只在启用时注册（`cashier.go:89-90`），完成接口再次检查开关（`cashier.go:154`）和渠道插件码（`cashier.go:183`、`:280-285`），非沙箱订单不进入完成分支。

`?act=` 的注入面。所有参数经 `Request.Form.Get` 取值后用 `strconv.Atoi` 或 `ParseInt` 收窄成数字或枚举，SQL 全部是 GORM 占位符，例如 `Where("merchant_id = ?", ...)`（`api.go:262`）和 `Where("legacy_uid = ?", pid)`（`api.go:504`）。`api.go` 里没有字符串拼接 SQL，没有 `Exec`，没有 `Raw`。

其他事实。V1 认证失败返回 HTTP 200，错误只在 body 的 `code` 里（`middleware.go:20-32`），只看状态码的监控会漏报。`api.php` 没有速率限制，限流只挂在收银台的发起与轮询（`cashier.go:27-29`、`:96`、`:141`）。平台不记录请求体，出现资金争议时需要靠账本流水和 `payment_callbacks` 还原。

## 7. 漏洞上报

用 GitHub Security Advisory 的私密报告入口提交，入口在仓库 Security 标签页的 Report a vulnerability。不要开公开 issue，也不要在 PR 里写利用细节。

邮箱占位 `<security@example.com>`，替换成仓库 owner 的实际地址后生效。

支持版本：`main` 分支与 `v2.0.0` 标签。`v2` 系列之前没有对外发布过，报告时附 commit 哈希。

报告内容给三样：可复现的步骤、影响面（能否跨商户、能否动资金）、受影响的 commit。响应时限没有对外承诺。
