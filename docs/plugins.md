# GorixPay 支付插件接入与运维

本文面向两类读者：

- **运维/运营**：在后台创建渠道、填配置、做连通性测试、排查回调问题。
- **接入开发者**：给 GorixPay 增加新插件，或对接某个上游的报文细节。

事实来源全部是工作区代码：`internal/plugin/payment/**`、`internal/channel/config/**`、`internal/payment/orchestrator/service.go`、`internal/payment/callback/plugin.go`、`internal/admin/httpapi/plugin.go`、`tests/contracts/plugins/**`。文中所有密钥一律用 `<...>` 占位，仓库里不出现真实凭据。

> 金额单位：对外协议（易支付 V1/V2、上游请求）一律“元/字符串两位小数”，平台内部一律“分/int64”。插件负责两者转换，回调金额会被换算成分再与订单比对。

---

## 1. 插件清单

| code | 名称 | 能力 | 上游 | 回调验签 | 典型场景 |
| --- | --- | --- | --- | --- | --- |
| `epay` | Epay | payment | 任意易支付上游 `/mapi.php` | 专用路由 `/callback/epay/:channel_id`（不走通用回调） | 接其它易支付站点做上游 |
| `sandbox` | 沙箱支付 | payment | 无（本地） | 无（由沙箱完成接口驱动） | 联调、演示、端到端测试 |
| `alipay` | 支付宝当面付 | payment | `alipay.trade.precreate` | ✅ 支付宝公钥 RSA2 | 扫码收款（PC/收银台二维码） |
| `wechatpayv3` | 微信支付 V3 | payment | Native / JSAPI / H5 | ✅ 平台证书 + AES-256-GCM | 微信扫码、公众号内支付、H5 |
| `jeepay` | Jeepay 聚合支付 | payment | `/api/pay/unifiedOrder` | ✅ MD5 | 聚合通道（支付宝/微信/银联等） |
| `generic` | 通用 HTTP 上游 | payment | 可配置 URL + 参数模板 | ✅ 可选 MD5（也可关闭） | 长尾自建上游、私有协议 |
| `usdt` | USDT (Epusdt) | payment | `/api/v1/pay/create` | ✅ MD5 | 加密货币收款 |
| `qqpay` | QQ 钱包 | payment | 统一下单（XML）+ `code_url` | ✅ XML + MD5 | QQ 扫码收款 |
| `unionpay` | 银联全渠道 | payment | 全渠道 5.1.0 前台交易（自动提交表单） | ✅ 表单 + SHA256withRSA | 银联卡（借记卡/信用卡）收款 |
| `huifu` | 汇付斗拱 | payment | `/v3/trade/payment/jspay` | ✅ `resp_data` + 汇付公钥 SHA256withRSA | 支付宝/微信扫码与 JSAPI（斗拱直连或渠道商） |

插件 code 由注册表强校验：小写、满足 `^[a-z][a-z0-9_-]{1,63}$`、能力列表非空、不可重复注册。`epay`/`sandbox` 是历史插件，不实现 `Configurable`（无配置表单）也不实现 `CallbackVerifier`（`epay` 用专用回调处理器）。

`GET /api/v1/admin/plugins` 返回的就是注册表里的实际内容，以它为准。

---

## 2. 插件契约（新增插件必读）

```go
// 旧插件接口，未改动
type Plugin interface {
    Metadata() Metadata
    CreatePayment(context.Context, CreateRequest) (Action, error)
}

// 可选能力：发布渠道配置表单
type ConfigField struct { Key, Label, Type string; Required, Secret bool }
type Configurable interface { ConfigFields() []ConfigField }

// 可选能力：验证并解析异步通知
type CallbackResult struct {
    TradeNo        string            // 平台订单号（out_trade_no / mchOrderNo / order_id）
    ChannelTradeNo string            // 上游流水号
    Amount         int64             // 分
    Paid           bool
    Raw            map[string]string // 归一化后的原始字段，便于审计
}
type CallbackVerifier interface {
    VerifyCallback(ctx context.Context, r *http.Request, config map[string]string) (CallbackResult, error)
}
```

`ConfigField.Type` 取值：`text` / `password` / `url` / `textarea` / `select`。`Secret: true` 表示该字段属于凭据，管理端不应回显其值（当前实现里配置**整体**加密存储，列表接口只返回 `configured` 布尔值，因此不存在任何明文回显）。

`CreateRequest` 的关键字段：

| 字段 | 说明 |
| --- | --- |
| `TradeNo` | 平台订单号，作为上游 `out_trade_no` |
| `Amount` | 订单金额（分） |
| `PaymentType` | 渠道支付方式字符串（如 `alipay`、`wechatpayv3_jsapi`） |
| `NotifyURL` | 平台自己的通用回调地址：`{PublicURL}/api/v1/payment/callback/{plugin}/{channel_id}` |
| `ReturnURL` / `ClientIP` | 商户回跳地址 / 客户端 IP |
| `Configuration` | 渠道解密后的扁平配置（见第 3 节） |
| `Extra` | 编排器附加信息，目前含 `site_url`；JSAPI 的 `openid` 也从这里读 |

`Action.Type` 支持 `jump` / `qrcode` / `html` / `urlscheme` / `jsapi` / `app` / `scan` / `wxplugin` / `wxapp`。
其中 `jump` 只接受 http(s) 绝对 URL；`qrcode` 允许任意带 scheme 的绝对 URI（微信 `weixin://wxpay/bizpayurl?pr=...` 就是这种）；`html`/`jsapi`/`app` 等把内容放在 `Data` 里。

新增插件流程：

1. 在 `internal/plugin/payment/<code>/` 建包，提供 `New(client *http.Client) *Plugin`（`nil` 走 15 秒超时的默认客户端）。
2. 实现 `Metadata()`、`CreatePayment()`；需要表单就加 `ConfigFields()`；需要回调就加 `VerifyCallback()`。
3. 在 `tests/contracts/plugins/manifest.yaml` 登记 code/能力/配置字段/金额单位，契约测试会对齐注册表。
4. 由 Lead 在 `internal/platform/app/app.go` 用 `plugins.Register(...)` 装配。

统一辅助函数（`internal/plugin/payment/support.go`）：`FormatAmount(分) → "12.34"`、`ParseAmount("12.34") → 分`、`ParseCentsAmount`、`RequireConfig(config, keys...)`、`ConfigValue`、`ReadUpstreamBody`（64 KiB 上限）。

---

## 3. 渠道配置模型

### 3.1 存储结构

```go
type Config struct {
    APIURL string            `json:"api_url"` // 老字段
    PID    string            `json:"pid"`     // 老字段
    Key    string            `json:"key"`     // 老字段
    Extra  map[string]string `json:"extra,omitempty"` // 插件私有配置
}
```

整份 `Config` 序列化后由主密钥加密（AES-GCM，附加认证数据为 `payment-channel:{channel_id}`），存 `payment_channels.config_encrypted`。**不存在**只存部分字段的分表，也没有明文字段。

### 3.2 `Values()`：插件最终拿到什么

`Config.Values()` 把老字段与 `Extra` 拉平成 `map[string]string`：

1. `api_url`、`pid`、`key` 非空则写入；
2. 遍历 `Extra`，空键/空值跳过；
3. **`Extra` 覆盖同名键**，即 `Extra["api_url"]` 会盖掉 `APIURL` 字段。

因此“老三元组”和“新 config map”在插件眼里是同一块扁平的键值空间，插件内部统一按 key 读取（例如 `config["app_id"]`），不需要关心运维是通过哪条路径填的。

### 3.3 创建/更新渠道：老字段与 config map 的关系

请求体（`POST /api/v1/admin/channels`、`PUT /api/v1/admin/channels/:id`）：

```json
{
  "name": "支付宝-主通道",
  "plugin_code": "alipay",
  "payment_type": "alipay",
  "priority": 1,
  "weight": 100,
  "status": "active",
  "api_url": "",
  "pid": "",
  "key": "",
  "config": {
    "app_id": "<APP_ID>",
    "private_key": "-----BEGIN PRIVATE KEY-----\n<REDACTED>\n-----END PRIVATE KEY-----",
    "alipay_public_key": "-----BEGIN PUBLIC KEY-----\n<REDACTED>\n-----END PUBLIC KEY-----"
  }
}
```

合并规则：

- `config` 里的非空键全部进入 `Extra`；同名的 `api_url/pid/key` 以 `config` 为准（显式的 config 值优先）。
- 老的 `api_url/pid/key` 仍照旧写入 `Config.APIURL/PID/Key`，同时也写进 `Extra`，方便非 epay 插件用同一套 key 读取。
- 值统一 `TrimSpace`；空键、空值丢弃。
- 校验（handler 层）：`sandbox` 直接放行；否则“老三元组齐全”或“`config` 至少有一个非空键”二者之一即可。
- 校验（config 层）：老三元组必须**全有或全无**（半填视为错误）；`api_url` 若存在必须是合法的 http(s) 绝对地址且不带 userinfo；若既无老字段也无 `Extra`，拒绝保存。
- `sandbox` 继续保持历史默认值（`http://sandbox.local` / `sandbox` / `sandbox`），除非显式覆盖。

列表接口 `GET /api/v1/admin/channels` 返回 `id / name / plugin_code / payment_type / priority / weight / status / configured / created_at / updated_at`，其中 `configured = config_encrypted != ""`，**不含任何密钥**，也不返回 `config` 内容。

epay 上游示例（老字段写法，前端当前形态）：

```json
{
  "name": "epay-上游A", "plugin_code": "epay", "payment_type": "alipay",
  "priority": 1, "weight": 100, "status": "active",
  "api_url": "https://upstream.example.com", "pid": "1001", "key": "<MERCHANT_KEY>"
}
```

---

## 4. 各插件配置字段

字段表里 `type` 对应 `ConfigField.Type`；`secret` 为 `true` 的键是凭据，落库加密、接口不回显。示例值全部是占位符。

### 4.1 `epay`（老插件，无配置表单）

| key | 说明 | 必填 |
| --- | --- | --- |
| `api_url` | 上游易支付站点地址，插件请求 `{api_url}/mapi.php` | ✅ |
| `pid` | 上游商户号 | ✅ |
| `key` | 上游商户密钥（MD5） | ✅（secret） |

```json
{"api_url":"https://upstream.example.com","pid":"1001","key":"<MERCHANT_KEY>"}
```

回调走专用路由 `POST /api/v1/payment/callback/epay/:channel_id`（校验 `pid`、`trade_status=TRADE_SUCCESS`、MD5 签名、金额），与通用回调互不影响，两个路由可以同时注册（注册顺序无关）。

### 4.2 `sandbox`（无配置）

无 `config_fields`。仅在 `sandbox.enabled=true` 且环境非 `production` 时注册。下单返回 `jump` 动作，跳转到 `{site_url}/pay/sandbox/{trade_no}` 本地确认页。

### 4.3 `alipay`：支付宝当面付

| key | label | type | required | secret |
| --- | --- | --- | --- | --- |
| `app_id` | 应用 APPID | text | ✅ | false |
| `private_key` | 应用私钥 (PKCS8 PEM) | textarea | ✅ | true |
| `alipay_public_key` | 支付宝公钥 (PEM) | textarea | ✅ | true |
| `gateway_url` | 网关地址 | url | false | false |

- 请求：`POST {gateway_url}`（默认 `https://openapi.alipay.com/gateway.do`），参数 `method=alipay.trade.precreate`、`sign_type=RSA2`、`biz_content={out_trade_no,total_amount,subject,timeout_express:15m}`，签名 `SHA256withRSA`（PKCS#1 v1.5）后 base64，待签名串为排序后的 `k=v&...`。
- 响应：取 `alipay_trade_precreate_response.code == "10000"` 且 `qr_code` 非空 → 返回 `ActionQRCode`。
- 回调：`form` 参数剔除 `sign`/`sign_type`、剔空值后字典序拼接，用 `alipay_public_key` 验 RSA2；`app_id` 与配置不一致则拒绝。`trade_status` ∈ {`TRADE_SUCCESS`,`TRADE_FINISHED`} 视为已支付；`total_amount` 换算成分。

```json
{
  "app_id": "<APP_ID>",
  "private_key": "-----BEGIN PRIVATE KEY-----\n<REDACTED>\n-----END PRIVATE KEY-----",
  "alipay_public_key": "-----BEGIN PUBLIC KEY-----\n<REDACTED>\n-----END PUBLIC KEY-----",
  "gateway_url": "https://openapi.alipay.com/gateway.do"
}
```

### 4.4 `wechatpayv3`：微信支付 V3

| key | label | type | required | secret |
| --- | --- | --- | --- | --- |
| `mch_id` | 商户号 | text | ✅ | false |
| `app_id` | 应用 APPID | text | ✅ | false |
| `serial_no` | 商户证书序列号 | text | ✅ | false |
| `private_key` | 商户私钥 (PKCS8 PEM) | textarea | ✅ | true |
| `api_v3_key` | APIv3 密钥 (32 字节) | password | ✅ | true |
| `platform_public_key` | 平台证书公钥 (PEM) | textarea | false | true |
| `platform_serial` | 平台证书序列号 | text | false | false |
| `api_base_url` | API 地址 | url | false | false |

- 产品选择由 `payment_type` 决定：含 `jsapi` 或 `mp` → JSAPI；含 `h5` / `wap` / `mweb` → H5；其余 → Native。
- 请求头：`Authorization: WECHATPAY2-SHA256-RSA2048 mchid="...",nonce_str="...",signature="...",timestamp="...",serial_no="..."`，签名串 `METHOD\nPATH\ntimestamp\nnonce\nbody\n`。
- 返回：Native `code_url` → `ActionQRCode`；H5 `h5_url` → `ActionJump`；JSAPI `prepay_id` → `ActionJSAPI`，`Data` 为 `{appId,timeStamp,nonceStr,package:"prepay_id=...",signType:"RSA",paySign}`（`paySign` 对 `appId\ntimeStamp\nnonceStr\npackage\n` 签名）。
- JSAPI 需要 `openid`：优先 `CreateRequest.Extra["openid"]`，其次渠道配置里的 `openid`；缺失时下单失败并提示 `openid`。
- 回调：校验 `Wechatpay-Timestamp/Nonce/Signature/Serial` 四个头（`platform_serial` 配了就比对序列号），签名串 `timestamp\nnonce\nbody\n`，平台公钥验签；时间戳超出 ±300 秒拒绝；再用 `api_v3_key` 做 AES-256-GCM（`AEAD_AES_256_GCM`，`associated_data` 作为 AAD）解密 `resource`；校验 `mchid`/`appid` 与配置一致；`event_type=TRANSACTION.SUCCESS` 且 `trade_state=SUCCESS` 才算已支付，金额取 `amount.total`（分）。

```json
{
  "mch_id": "<MCH_ID>",
  "app_id": "<APP_ID>",
  "serial_no": "<MERCHANT_CERT_SERIAL>",
  "private_key": "-----BEGIN PRIVATE KEY-----\n<REDACTED>\n-----END PRIVATE KEY-----",
  "api_v3_key": "<32_BYTE_API_V3_KEY>",
  "platform_public_key": "-----BEGIN PUBLIC KEY-----\n<REDACTED>\n-----END PUBLIC KEY-----",
  "platform_serial": "<PLATFORM_CERT_SERIAL>"
}
```

### 4.5 `jeepay`：Jeepay 聚合支付

| key | label | type | required | secret |
| --- | --- | --- | --- | --- |
| `api_url` | 接口地址 | url | ✅ | false |
| `mch_no` | 商户号 | text | ✅ | false |
| `app_id` | 应用 ID | text | ✅ | false |
| `key` | 接口密钥 | password | ✅ | true |
| `way_code` | 默认支付方式 | text | false | false |

- 请求：`POST {api_url}/api/pay/unifiedOrder`，`version=1.0`、`reqTime=yyyyMMddHHmmss`、`reqId`、`mchNo`、`appId`、`wayCode`、`amount`（分）、`currency=cny`、`mchOrderNo`、`goodsName`、`notifyUrl`、`returnUrl`、`clientIp`、`signType=MD5`。
- 签名：非空参数剔除 `sign` 后按名排序拼 `k=v&...`，末尾追加 `&key=<KEY>`，取 32 位大写 MD5。
- `wayCode` 映射：`alipay`/`ali_qr`/`alipay_qr`/空 → `ALI_QR`；`alipay_wap` → `ALI_WAP`；`wechat`/`wechatpay`/`wx_native` → `WX_NATIVE`；`jsapi`/`wechat_jsapi` → `WX_JSAPI`；`h5`/`wap`/`mweb` → `WX_H5`；其它值原样大写（`-`→`_`）。配了 `way_code` 则直接用它。
- 响应：`code=0` 且 `data.payData` 非空；按 `payDataType` 转动作：`codeUrl`/`qrCodeUrl`/`qrCode`/`code_url` → `qrcode`，`form`/`html`/`formHtml` → `html`，`payUrl`/`jumpUrl`/`url` → `jump`；未知类型按 `payData` 是否 `http(s)` 前缀兜底。
- 回调：JSON 报文，顶层标量字段（数字保留字面形式）剔除 `sign` 后同规则验签；`mchNo` 在配置与报文都有值时比对，不一致拒绝；`state == "2"` 为已支付，`amount` 为分。

```json
{
  "api_url": "https://jeepay.example.com",
  "mch_no": "<MCH_NO>",
  "app_id": "<APP_ID>",
  "key": "<API_KEY>",
  "way_code": "ALI_QR"
}
```

### 4.6 `generic`：通用 HTTP 上游

| key | label | type | required | secret |
| --- | --- | --- | --- | --- |
| `api_url` | 下单接口地址 | url | ✅ | false |
| `method` | 请求方法 (POST/GET) | text | false | false |
| `param_template` | 参数模板 | textarea | false | false |
| `sign_type` | 签名方式 (md5/none) | select | false | false |
| `key` | 签名密钥 | password | false | true |

- 默认模板（epay 兼容）：

  ```text
  pid={pid}&type={pay_type}&out_trade_no={trade_no}&money={amount}&name={name}&notify_url={notify_url}&return_url={return_url}&clientip={client_ip}&sign_type=MD5&sign={sign}
  ```

- 可用占位符：`{trade_no}`/`{out_trade_no}`、`{amount}`/`{money}`（元，两位小数）、`{amount_cents}`（分）、`{name}`、`{notify_url}`、`{return_url}`、`{pay_type}`/`{payment_type}`、`{client_ip}`、`{pid}`、`{site_url}`。模板先按 query 语法解析，再逐值替换（不做额外 URL 转义，交给表单/查询编码）。
- 签名：模板里的 `{sign}` 只是占位，实际值由插件写入或删除。`sign_type=md5`（或配了 `key` 而未指定 sign_type）时，对剔除 `sign`/`sign_type`/空值后的参数用易支付 MD5 规则计算并写入 `sign`；`sign_type=none` 则不签名（`{sign}` 键被移除）。
- `method=GET` 时参数拼到查询串，否则以 `application/x-www-form-urlencoded` POST。
- 响应：`{code, msg|message, trade_no, pay_type, pay_info}`，`code` 接受 `1`/`"1"`/`true`/`"true"`/`"success"`；`pay_type` 决定动作类型，未知类型按 `pay_info` 是否 http(s) 前缀兜底为 `jump`。
- 回调：同时支持 `urlencoded` 与扁平 JSON；`sign_type=md5` 时按易支付规则验签；已支付判定为 `trade_status` ∈ {`TRADE_SUCCESS`,`TRADE_FINISHED`,`SUCCESS`}，`trade_status` 缺失时看 `code`/`status`；金额取 `money`，没有则 `amount`。

```json
{
  "api_url": "https://longtail.example.com/pay/create",
  "method": "POST",
  "sign_type": "md5",
  "key": "<SIGN_KEY>",
  "param_template": "mch_id={pid}&order_no={trade_no}&total_fee={amount_cents}&callback={notify_url}&sign={sign}"
}
```

### 4.7 `usdt`：USDT (Epusdt 风格)

| key | label | type | required | secret |
| --- | --- | --- | --- | --- |
| `api_url` | 网关地址 | url | ✅ | false |
| `key` | 商户密钥 | password | ✅ | true |
| `action` | 返回方式 (jump/qrcode) | select | false | false |
| `notify_url` | 异步通知地址覆盖 | url | false | false |

- 请求：`POST {api_url}/api/v1/pay/create`，JSON `{order_id,amount,notify_url,redirect_url,sign}`，`amount` 是两位小数字符串，`sign = md5(order_id + amount + key)`（小写）。`notify_url` 可被渠道配置里的同名键覆盖。
- 响应：`status_code == 200` 且 `data.payment_url` 非空；`action=jump`（默认）返回 `ActionJump`，`action=qrcode` 返回 `ActionQRCode`。
- 回调：JSON 或表单；`sign` 按 `md5(trade_id + order_id + amount + key)` 校验，同时兼容创建时那套 `md5(order_id + amount + key)`；`status == "2"` 为已支付，金额取 `amount`。

```json
{
  "api_url": "https://epusdt.example.com",
  "key": "<USDT_KEY>",
  "action": "jump",
  "notify_url": "https://pay.example.com/api/v1/payment/callback/usdt/<CHANNEL_ID>"
}
```

### 4.8 `qqpay`：QQ 钱包

| key | label | type | required | secret |
| --- | --- | --- | --- | --- |
| `mch_id` | 商户号 | text | ✅ | false |
| `app_id` | 应用 APPID | text | ✅ | false |
| `key` | 商户密钥 | password | ✅ | true |
| `api_url` | 统一下单地址 | url | false | false |

- 请求：`POST` `text/xml` 到 `api_url`（默认 `https://qpay.qq.com/cgi-bin/pay/qpay_unified_order.cgi`），`<xml>` 扁平字段 `mch_id`/`appid`/`nonce_str`/`body`/`out_trade_no`/`total_fee`(分)/`spbill_create_ip`/`notify_url`/`trade_type=NATIVE`/`time_expire`(yyyyMMddHHmmss，默认 now+15 分钟)/`sign`。`body` 内的 `&`、`<` 等字符会做 XML 实体转义，可安全往返。
- 签名：非空字段剔除 `sign` 后按名排序拼 `k=v&k2=v2`，末尾追加 `&key=<KEY>`，取 32 位**大写** MD5。
- 响应：`return_code=SUCCESS` 且 `result_code=SUCCESS` 且 `code_url` 非空 → `ActionQRCode`；否则返回可读错误（`return_msg`，或 `err_code` + `err_code_des`）。
- 回调：XML 文档，整份非空字段（除 `sign`）参与验签；`mch_id` 双方都有值时比对；`return_code` 与 `result_code` 同时为 `SUCCESS` 才算已支付，金额取 `total_fee`（分），上游流水号取 `transaction_id`（回落 `out_transaction_id`），已支付但缺流水号会被拒绝。非支付事件返回 `Paid=false`。

```json
{
  "mch_id": "<MCH_ID>",
  "app_id": "<APP_ID>",
  "key": "<QQPAY_KEY>",
  "api_url": "https://qpay.qq.com/cgi-bin/pay/qpay_unified_order.cgi"
}
```

### 4.9 `unionpay`：银联全渠道 5.1.0

| key | label | type | required | secret |
| --- | --- | --- | --- | --- |
| `mer_id` | 商户号 | text | ✅ | false |
| `cert_id` | 证书 ID | text | ✅ | false |
| `private_key` | 商户私钥 (PKCS8 PEM) | textarea | ✅ | true |
| `unionpay_public_key` | 银联公钥 (PEM) | textarea | ✅ | true |
| `api_url` | 前台交易地址 | url | false | false |
| `front_url` | 前台回跳地址 | url | false | false |
| `submit_mode` | 提交方式 (html/jump) | select | false | false |

- 报文：`version=5.1.0`、`encoding=UTF-8`、`txnType=01`、`txnSubType=01`、`bizType=000201`、`channelType=07`、`accessType=0`、`currencyCode=156`、`merId`、`certId`、`orderId`(= 平台 `trade_no`)、`txnAmt`(分)、`txnTime`(yyyyMMddHHmmss)、`frontUrl`、`backUrl`(= 编排器下发的通用回调地址)、`signMethod=01`。
- 签名：所有非空字段剔除 `sign` 后按名排序，逐项拼 `k=v&`（**保留末尾 `&`**，与银联 5.1.0 SDK 一致），`SHA256withRSA`（PKCS#1 v1.5）后 base64。
- 提交方式：`submit_mode=html`（默认）返回 `ActionHTML`，一段含全部隐藏域的自动 `submit()` 表单，由浏览器 POST 到 `api_url`；`submit_mode=jump` 返回 `ActionJump`，同样的参数放在查询串里。**两种模式都不在插件内发起出站请求**，所以 `/channels/:id/test` 对银联只会拿到表单或 URL，不会真的打到银联网关。
- `frontUrl` 取渠道配置 `front_url`，缺失时回落到订单 `return_url`；`backUrl` 必须有值，否则下单直接报错。
- 回调：`application/x-www-form-urlencoded`，剔除 `sign` 后同规则排序拼串，用 `unionpay_public_key` 验 SHA256 签名；`signMethod` 若存在必须为 `01`；`merId` 双方都有值时比对；`respCode == "00"` 为已支付，金额取 `txnAmt`（分），上游流水号取 `queryId`（回落 `traceNo`），已支付但缺流水号会被拒绝。

```json
{
  "mer_id": "<MER_ID>",
  "cert_id": "<CERT_ID>",
  "private_key": "-----BEGIN PRIVATE KEY-----\n<REDACTED>\n-----END PRIVATE KEY-----",
  "unionpay_public_key": "-----BEGIN PUBLIC KEY-----\n<REDACTED>\n-----END PUBLIC KEY-----",
  "front_url": "https://merchant.example/return",
  "submit_mode": "html"
}
```

### 4.10 `huifu`：汇付斗拱

| key | label | type | required | secret |
| --- | --- | --- | --- | --- |
| `sys_id` | 汇付系统号 / 渠道商 ID | text | ✅ | false |
| `product_id` | 汇付产品号 | text | ✅ | false |
| `private_key` | 商户私钥 | textarea | ✅ | true |
| `huifu_public_key` | 汇付公钥 | textarea | ✅ | true |
| `huifu_id` | 子商户号（直连商户留空） | text | false | false |

- 与生产环境 `pay.25y.cn` 上 Epay 的 `huifu` 插件同协议：`POST https://api.huifu.com/v3/trade/payment/jspay`，报文 `{sys_id, product_id, data, sign}`。
- 签名：`data` 按 key 排序后用 **PHP `json_encode(JSON_UNESCAPED_SLASHES|JSON_UNESCAPED_UNICODE)`** 序列化，再用商户私钥 `SHA256withRSA`（PKCS#1 v1.5）签名并 base64。Go 侧必须关闭 HTML 转义（`SetEscapeHTML(false)`），否则 `<`/`&` 会让验签失败。
- 响应：`{data, sign}`，用汇付公钥按同样规则验签；`resp_code == "00000100"` 为成功，其它码取 `resp_desc`。
- 支付方式映射：`alipay` → `A_NATIVE`（扫码，返回 `qr_code`）/ `A_JSAPI`（`buyer_id`）；`wxpay` → `T_NATIVE` / `T_JSAPI`（`sub_appid`+`sub_openid`）；`bank` → `U_NATIVE` / `U_JSAPI`。
- 回调：`application/x-www-form-urlencoded`，字段 `resp_data`（JSON 串）+ `sign`，用**汇付公钥对 `resp_data` 原始字节**验签；`trans_stat == "S"` 为支付成功，`req_seq_id` 必须等于平台 `trade_no`，`hf_seq_id` 作通道流水，`out_trans_id` 作银行流水。应答体必须是 `RECV_ORD_ID_<trade_no>`（斗拱只认这个，否则持续重推）。
- 私钥/公钥既接受纯 base64（Epay 的存法，无 PEM 头），也接受整段 PEM；纯 base64 会自动按 64 字符换行补头。

```json
{
  "sys_id": "<SYS_ID>",
  "product_id": "PAYUN",
  "private_key": "MIIEvwIBADANBgkqhkiG9w0BAQEFAASCBKkwggSlAgEAAoIBAQ...",
  "huifu_public_key": "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA...",
  "huifu_id": ""
}
```

> 真实网关已验证：用生产渠道的 `sys_id=6666000197954883`、`product_id=PAYUN` 对 `api.huifu.com` 下单 0.01 元拿到 `qr_code`、关单受理、查询不存在订单返回 `23000001 交易不存在`，请求与响应双向验签均通过。

---

## 5. 通用渠道回调

### 5.1 路由与注册

```text
POST /api/v1/payment/callback/:plugin/:channel_id
```

由 `paymentcallback.RegisterPlugins(router, db, configs, callbackService, registry)` 注册；`:plugin` 必须与渠道的 `plugin_code` 一致。编排器下发订单时会把 `NotifyURL` 拼成这个地址（`PublicURL` + 路径），因此插件不需要自己知道渠道 ID。

### 5.2 处理流程

1. **定位渠道**：`id = :channel_id AND plugin_code = :plugin AND status = active`，找不到 → `404`。
2. **定位插件**：注册表 `Lookup(plugin_code)`，不存在 → `404`；插件未实现 `CallbackVerifier` → `404`（例如 `sandbox`）。
3. **解密配置**：取渠道 `config_encrypted` 解密为 `Config.Values()`，失败 → `500`。
4. **读原始报文**：最多 1 MiB，超限 → `400`；读完把 `body` 回绕，插件仍能读到同样的字节（`ParseForm` 或 JSON 解码都可用）。
5. **验签 + 解析**：调用 `VerifyCallback(ctx, r, config)`；返回 `ErrInvalidConfiguration` → `500`，其它错误 → `401`。
6. **非支付通知**：验签通过但 `Paid=false`（关单、退款、等待付款等）→ 直接 `200 success`，不写任何状态（避免上游无限重试）。
7. **字段完整性**：`TradeNo`/`ChannelTradeNo`/`Amount > 0` 任一缺失 → `400`。
8. **查订单**：按 `trade_no` 查 `payment_orders`，不存在 → `404`。
9. **金额校验**：`result.Amount` 必须等于 `order.Amount`（分），不等 → `400`，**不入账**。
10. **入账**：`callbackService.Complete(...)`，`Digest = sha256(plugin + "\n" + channel_id + "\n" + 原始 body)`，成功 → `200 success`。

### 5.3 幂等语义

- `Digest` 是 `payment_callbacks.digest` 的唯一键：同一条报文（同插件、同渠道、同字节）重复投递时 `Complete` 检测到已存在记录 → 直接返回成功，**不重复入账、不重复发 Outbox 通知**。
- 同一订单换一条报文（例如上游换了 `channel_trade_no`）→ `409`（`channel_trade_no` 唯一约束冲突或“订单不可支付”）。
- 商户入账本身还有账本幂等键 `payment:{orderID}` 兜底。

### 5.4 状态码对照

| 状态码 | 触发条件 | 上游应如何 |
| --- | --- | --- |
| `200 success` | 入账成功；或重复投递（幂等）；或验签通过的非支付通知 | 停止重试 |
| `400 fail` | 渠道参数缺失、报文超限、`TradeNo`/流水号/金额缺失、金额与订单不符 | 停止重试并人工介入 |
| `401 fail` | 验签失败、报文畸形、时间戳过期、appid/mchid/mchNo 不匹配 | 视为伪造，停止重试 |
| `404 fail` | 渠道不存在/已禁用、plugin_code 不匹配、插件未注册或不支持验签、订单不存在 | 停止重试 |
| `409 fail` | 流水号冲突、订单已支付且流水号不同、订单状态不可支付 | 停止重试 |
| `500 fail` | 配置解密失败、配置缺必填项（`ErrInvalidConfiguration`） | 运维修配置后手动补单 |

响应体统一是纯文本 `success` / `fail`，方便上游按字符串判断。

### 5.5 各插件回调报文速查

| 插件 | 传输 | 签名位置 | 已支付判定 | 金额字段 |
| --- | --- | --- | --- | --- |
| alipay | `application/x-www-form-urlencoded` | `sign`（RSA2） | `trade_status` ∈ {`TRADE_SUCCESS`,`TRADE_FINISHED`} | `total_amount`（元） |
| wechatpayv3 | JSON + 4 个 `Wechatpay-*` 头 | `Wechatpay-Signature`（平台证书） | `event_type=TRANSACTION.SUCCESS` 且 `trade_state=SUCCESS` | `resource.amount.total`（分，GCM 解密后） |
| jeepay | JSON | `sign`（MD5 大写） | `state == "2"` | `amount`（分） |
| generic | 表单或扁平 JSON | `sign`（可选 MD5） | `trade_status` ∈ {`TRADE_SUCCESS`,`TRADE_FINISHED`,`SUCCESS`}，缺失时看 `code`/`status` | `money` 或 `amount`（元） |
| usdt | JSON 或表单 | `sign`（MD5 小写） | `status == "2"` | `amount`（元） |
| qqpay | XML | 文档内 `sign`（MD5 大写） | `return_code` 与 `result_code` 均为 `SUCCESS` | `total_fee`（分） |
| unionpay | 表单 | `sign`（SHA256withRSA，末尾带 `&` 的拼串） | `respCode == "00"` | `txnAmt`（分） |

---

## 6. 管理端接口

以下接口都挂在 `/api/v1/admin` 组下，需要管理员会话（`Authenticate` + `RequireAdministrator`）。

### 6.1 `GET /api/v1/admin/plugins`

读取注册表，返回全部插件的元数据与配置表单定义。

```json
{
  "items": [
    {
      "code": "alipay",
      "name": "支付宝当面付",
      "capabilities": ["payment"],
      "config_fields": [
        {"key": "app_id", "label": "应用 APPID", "type": "text", "required": true, "secret": false},
        {"key": "private_key", "label": "应用私钥 (PKCS8 PEM)", "type": "textarea", "required": true, "secret": true}
      ]
    }
  ]
}
```

- `items` 按 `code` 升序；`capabilities` 目前都是 `["payment"]`。
- 不实现 `Configurable` 的插件（`epay`、`sandbox`）返回 `config_fields: []`。
- 只返回字段描述，**不返回任何配置值**。

### 6.2 `POST /api/v1/admin/channels/:id/test`

用渠道已保存的配置调一次插件下单，用于排错。**不落订单、不改订单状态、不写 Outbox、不影响路由权重**；只有配置里指向的外网地址才会真正发一次请求（自测时把 `gateway_url`/`api_url` 指向本地 mock 即可）。

请求：

```json
{"payment_type": "alipay", "amount": 1234, "plugin_code": "alipay"}
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `payment_type` | 否 | 覆盖渠道的 `payment_type`（例如测 `wechatpayv3_jsapi`） |
| `amount` | 否 | 分；缺省或 `0` 用 100（1.00 元）；负数 → `400` |
| `plugin_code` | 否 | 传入时必须与渠道一致，否则 `400` |

成功响应（HTTP 200）：

```json
{"ok": true, "pay_type": "qrcode", "pay_info": "https://qr.alipay.com/xxx", "error": ""}
```

失败响应（HTTP 200，便于前端直接展示）：

```json
{"ok": false, "pay_type": "", "pay_info": "", "error": "alipay rejected the order: code=40004 msg=Business Failed ..."}
```

- `pay_type` 即 `Action.Type`；`pay_info` 对 `jump`/`qrcode`/`urlscheme` 是 URL，对 `html` 是 HTML 片段，其余是 `Data` 的 JSON 字符串。
- 渠道不存在 → `404`；请求体非法（含 `amount < 0`）→ `400`。
- 插件未注册、插件调用失败、渠道配置解密失败一律 `200` + `ok:false`；其中插件调用失败会带空的 `pay_type`/`pay_info`，另外两种只返回 `{ok,error}`，前端按需兜底。
- 测试单的 `TradeNo` 形如 `TEST<纳秒>`，`NotifyURL` 由当前请求的 scheme/host 拼出，不要拿它去和真实订单对应。

### 6.3 `GET /api/v1/admin/channels`（补充说明）

返回 `plugin_code` 与 `configured`，不返回密钥、不返回 `config` 内容；前端展示“已配置/未配置”即可。

---

## 7. 安全须知

1. **凭据加密存储**：整份渠道配置用主密钥（`security.master_key_file`，或 `--master-key-file` / `GORIXPAY_MASTER_KEY_FILE`）AES-GCM 加密，AAD 绑定渠道 ID，换渠道 ID 无法解密。私钥、`api_v3_key`、上游 `key` 都不会以明文出现在数据库、日志或接口响应里。
2. **只回显一次原则**：管理端列表/详情只回 `configured` 布尔值；插件元数据只描述字段。要改密钥就走“重新填一遍再 PUT”，不做“读取已存值”。
3. **回调必须验签**：除 `sandbox` 外所有渠道都验签；`generic` 若把 `sign_type` 配成 `none`，等于放弃验签，只应在可信内网使用。
4. **金额以订单为准**：回调金额只做比对，绝不按回调金额入账；`Complete` 内部还会再校验一次，并向账本写幂等键 `payment:{orderID}`。
5. **幂等键设计**：`digest = sha256(plugin + channel + raw body)`，同一报文重放不会二次入账；换报文会被唯一约束挡住。
6. **时间窗**：`wechatpayv3` 校验 `±300s`；支付宝/Jeepay/USDT 的上游本身不带强制时间窗，建议在网关层限制回调来源 IP（如有条件）。
7. **传输**：`api_url`/`gateway_url` 只接受 `http(s)` 绝对地址且禁止 userinfo，避免把凭据写进 URL；生产必须 HTTPS。
8. **测试接口**：`/channels/:id/test` 会消耗一次上游下单请求（可能产生上游侧无效单），生产环境建议只在新增渠道时使用。

---

## 8. 已知限制

- **alipay**：只校验回调签名，**不校验网关响应的 `sign`**（仅校验 `code=10000` + `qr_code` 非空）。
- **wechatpayv3**：单平台证书模型（`platform_public_key` + 可选 `platform_serial`），未实现平台证书自动下载与滚动；平台换证需要手工更新配置。JSAPI 需要调用方提供 `openid`；H5 缺 `client_ip` 时回落 `127.0.0.1`。
- **jeepay**：`wayCode` 由 `payment_type` 关键字映射（可用 `way_code` 覆盖）；回调只把顶层标量字段纳入签名/比对，嵌套对象不参与。
- **generic**：请求只支持表单与 GET 查询串（模板参数化），**不支持 JSON 请求体模板**；回调支持表单与扁平 JSON。
- **usdt**：回调签名按 `md5(trade_id+order_id+amount+key)`，并兼容创建风格的 `md5(order_id+amount+key)`；若实际 Epusdt 部署用了别的拼接顺序，只需改 `usdt.notifySign` 一处。
- **qqpay**：只实现 `NATIVE`（扫码），请求与回调都是 XML、不做 JSON 兼容；`time_expire` 固定为 15 分钟；响应里只有 `prepay_id` 而没有 `code_url` 时不会降级成别的动作，直接报错。
- **unionpay**：只覆盖前台交易（`frontTransReq`，`txnType=01` 消费）+ 回调验签，**没有交易查询、没有自动对账、没有退款**，失败/超时单需要到银联商户后台人工核对；`frontUrl` 必须可访问，否则银联前台回跳会报错；签名串**末尾带 `&`**，若对接方自研网关不按银联 SDK 规则拼串会验签失败。
- **huifu**：只实现「下单 + 回调」；**没有查询、关单、退款**，未支付单靠斗拱侧自动过期（实测关单接口对 `jspay` 单会返回 `交易不存在`，属上游语义，接关单前需要先确认）。回调应答必须是 `RECV_ORD_ID_<trade_no>`（P/F 状态也要原样回），否则斗拱会持续重推。`sys_id`/`product_id` 对应 Epay 插件里的 `汇付系统号`/`汇付产品号`；渠道商模式记得填 `huifu_id`（子商户号）。
- **通用回调**：验签通过但非支付事件（`Paid=false`）返回 `200 success` 但不入账；退款类通知目前只做确认，不驱动退款状态机。
- **插件能力**：本批插件只声明 `payment` 能力，查询/关单/退款/代付能力尚未落到插件层，相关操作仍走平台自有流程。
- **渠道测试接口**：失败一律 `200 + ok:false`，不区分“配置错误”与“上游拒绝”，接运维告警时需解析 `error` 文本。

---

## 9. 支付宝 mock 端到端流程（对应 `tests/contracts/plugins/e2e_test.go`）

该测试用一个 `httptest.Server` 假扮支付宝网关，把“加密配置 → 下单 → 二维码 → 签名回调 → 入账一次 → 重放不重复 → 伪造/改价被拒”整条链路跑通，可作为新插件 e2e 模板。

### 9.1 步骤

1. **准备两对 RSA 密钥**：商户密钥（渠道 `private_key`，签请求）与平台密钥（渠道 `alipay_public_key`，签回调）。测试用的是内存里现生成的 2048 位密钥，仓库不含私钥文件。
2. **建库并迁移**：`merchants`、`payment_orders`、`payment_attempts`、`payment_channels`、`payment_callbacks`、`outbox_messages`。
3. **写渠道**：`plugin_code=alipay`，`config` 含 `app_id` / `private_key` / `alipay_public_key` / `gateway_url=<httptest URL>`，整体加密后入库。
4. **装配**：注册 `alipay.New(gateway.Client())` → `channelrouter.New` → `paymentorchestrator.NewConfigured(db, router, time.Now, configs, "https://pay.example")`。
5. **下单**：`orchestrator.Start(ctx, order.TradeNo)`。mock 网关校验请求 RSA2 签名与 `biz_content`，返回 `{"alipay_trade_precreate_response":{"code":"10000","qr_code":"https://qr.alipay.com/e2e"}}`；断言动作为 `qrcode` 且 URL 一致，同时断言网关收到的 `notify_url == https://pay.example/api/v1/payment/callback/alipay/channel-alipay`。
6. **签名回调**：用平台私钥对 `app_id/out_trade_no/total_amount/trade_no/trade_status/sign_type` 排序拼接后签名，`POST /api/v1/payment/callback/alipay/channel-alipay` → `200 success`。
7. **断言入账一次**：订单 `status=paid`、`channel_trade_no=ALI-E2E-1`、`payment_callbacks` 1 条、`outbox_messages` 1 条。
8. **重放同一报文**：仍返回 `200 success`，上述三条计数不变（幂等）。
9. **伪造签名**：把 `sign` 改成 `Zm9yZ2Vk` → `401`，订单与计数不变。
10. **改价**：用平台私钥签一份 `total_amount=0.01` 的合法报文 → `400`（金额与订单不符），不入账。

### 9.2 对应断言位置

- 下单与 `notify_url`：`TestAlipayChannelRunsFromCreateToCallback` 前半段。
- 入账唯一性与重放：`assertPaidOnce`（统计 `payment_callbacks` 与 `outbox_messages`）。
- 伪造/改价：同一测试末段的 `forged` 与 `cheap` 两次投递。
- 插件单位量级测试（成功/上游报错/垃圾响应/超时/回调验签成败）：`internal/plugin/payment/alipay/plugin_test.go` 等各插件包。

### 9.3 本地复现

```powershell
go test ./tests/contracts/plugins/ -run TestAlipayChannelRunsFromCreateToCallback -v
go test ./internal/plugin/payment/... ./internal/payment/callback/... ./internal/admin/... ./tests/contracts/plugins/...
```

---

## 10. 排障速查

| 现象 | 优先检查 |
| --- | --- |
| 下单报 `invalid channel configuration` | 渠道 `config` 必填项是否齐（对照第 4 节表），值是否被 TrimSpace 成空 |
| 下单报 `no available payment channel` | 渠道 `status=active` 且 `weight>0`，`plugin_code` 已注册、`payment_type` 与订单一致 |
| 回调 `401` | 密钥/公钥是否填反（私钥签、公钥验）；微信看四个 `Wechatpay-*` 头是否被反代丢掉 |
| 回调 `404` | `:plugin` 与渠道 `plugin_code` 是否一致；渠道是否被禁用；订单 `trade_no` 是否存在 |
| 回调 `400` 且 `error` 说金额不符 | 上游币种/单位（元 vs 分）与订单金额是否一致 |
| 回调 `200` 但订单未支付 | 是否属于非支付事件（`Paid=false`）；或把 `sign_type` 配成 `none` 后报文里缺少已支付标记字段 |
| `channels/:id/test` 返回 `ok:false` | 直接看 `error` 文本：多为配置缺项或上游拒绝码 |

---

## 11. 真实网关联调脚本

`scripts/plugin-sandbox-check.ps1` 把所有已配置渠道交给对应插件各发起一次下单，用来在**真凭据**下验证连通性、签名方向和返回结构（不写订单、不改状态）。

```powershell
# 全部渠道（含 sandbox）
powershell -File scripts/plugin-sandbox-check.ps1 -BaseUrl http://127.0.0.1:8080 -AdminUser admin -AdminPassword admin123

# 只测真实网关，跳过本地沙箱
powershell -File scripts/plugin-sandbox-check.ps1 -SkipSandbox

# 只测某个渠道（按 name 或 id）
powershell -File scripts/plugin-sandbox-check.ps1 -Channel sandbox-alipay
```

输出示例：

```text
=== 逐个渠道发起测试支付 ===
  [PASS] alipay-prod  plugin=alipay pay_type=qrcode pay_info=https://qr.alipay.com/bax0...
  [FAIL] wxpay-prod   plugin=wechatpayv3 error=invalid channel configuration: serial_no is required
```

退出码：`0` 全通过，`1` 有渠道失败（可直接接 CI）。注意事项：

- 脚本调用的是运营接口 `POST /api/v1/admin/channels/:id/test`，需要管理员账号；生产环境不要放默认口令。
- 测试下单会按 `-Amount`（默认 100 分）构造金额，微信/支付宝沙箱可能对最低金额有要求，必要时调大。
- 报 `invalid channel configuration` 说明渠道 `config` 缺项；报上游错误码说明凭据或 IP 白名单/回调域名不匹配。

---

## 12. 真网关凭据清单（要准备什么）

把下面的值填进 `deploy/plugin-credentials.local.ps1`（模板见 `deploy/plugin-credentials.example.ps1`），**不要直接贴在聊天/工单里**。填完按顺序跑：

```powershell
powershell -File scripts/plugin-credentials-check.ps1 -File deploy/plugin-credentials.local.ps1           # 只校验
powershell -File scripts/plugin-credentials-check.ps1 -File deploy/plugin-credentials.local.ps1 -Apply    # 写入渠道
powershell -File scripts/plugin-sandbox-check.ps1 -SkipSandbox                                            # 打网关
```

### 12.1 支付宝当面付（`alipay`）

| 字段 | 从哪里拿 | 说明 |
| --- | --- | --- |
| `app_id` | 开放平台 → 应用详情 | 16 位应用 APPID |
| `private_key` | 开放平台 → 开发设置 → 接口加签方式（密钥） | **应用私钥**，PKCS8 PEM；不是支付宝公钥 |
| `alipay_public_key` | 同上页面的「支付宝公钥」 | 用「公钥」模式时平台生成的那把，别填成自己的应用公钥 |
| `gateway_url` | 默认 `https://openapi.alipay.com/gateway.do` | 沙箱用 `https://openapi-sandbox.dl.alipaydev.com/gateway.do` |
| 需要的产品 | 当面付 / 手机网站支付 | 只开「电脑网站支付」时扫码会失败 |

### 12.2 微信支付 V3（`wechatpayv3`）

| 字段 | 从哪里拿 | 说明 |
| --- | --- | --- |
| `mch_id` | 商户平台 → 账户中心 | 商户号 |
| `app_id` | 商户平台 → 产品中心 → AppID 账号管理 | 与商户号已绑定 |
| `serial_no` | 账户中心 → API 安全 → API 证书 | 商户证书序列号（40 位十六进制） |
| `private_key` | 下载的 `apiclient_key.pem` 内容 | 商户 API 私钥 |
| `api_v3_key` | 账户中心 → API 安全 → APIv3 密钥 | 32 字节，自己设置的 |
| `platform_public_key` / `platform_serial` | 平台证书 | 可留空，插件会用 V3 接口下载 |
| `api_base_url` | 默认 `https://api.mch.weixin.qq.com` | — |

回调需要公网可达的 HTTPS 域名，本地联调先用 `scripts/plugin-sandbox-check.ps1` 验下单（不依赖回调）。

### 12.3 QQ 钱包（`qqpay`）

| 字段 | 从哪里拿 |
| --- | --- |
| `mch_id` | QQ 钱包商户平台 → 商户信息 |
| `app_id` | 同上（应用 ID） |
| `key` | 商户平台 → 密钥管理（MD5 密钥） |
| `api_url` | 默认统一下单地址，一般不用改 |

### 12.4 银联全渠道（`unionpay`）

| 字段 | 从哪里拿 | 说明 |
| --- | --- | --- |
| `mer_id` | 银联商户入网材料 | 15 位商户号 |
| `cert_id` | 签名证书 | 证书 ID，不是证书文件名 |
| `private_key` | 商户签名证书对应的私钥 | PKCS8 PEM |
| `unionpay_public_key` | 银联提供的验签公钥 | PEM |
| `submit_mode` | `html`（自动提交表单）或 `jump` | 取决于前端怎么接 |
| `front_url` | 你自己的前台回跳页 | 可留空 |

### 12.5 Jeepay 聚合（`jeepay`）

`api_url`（网关地址）、`mch_no`（商户号）、`app_id`（应用 ID）、`key`（接口密钥）、`way_code`（如 `WX_NATIVE`/`ALI_QR`/`QR_CASHIER`）。

### 12.6 USDT / Epusdt（`usdt`）

`api_url`（`/api/v1/order/create-transaction`）、`key`（商户密钥）、`action`（`qrcode` 或 `jump`）、`notify_url`（可留空用平台回调）。

### 12.7 通用 HTTP 上游（`generic`）

用于接没有现成插件的长尾聚合：`api_url`、`method`（POST/GET）、`param_template`（JSON 模板，占位符 `{trade_no} {amount} {name} {notify_url} {return_url} {clientip}`）、`sign_type`（`md5`/`none`）、`key`。

### 12.8 Epay 上游（`epay`）

用平台自带的 epay 插件对接另一个易支付：`api_url`（对方站点根地址，含结尾斜杠）、`pid`、`key`（对方给的商户密钥）。这三个走渠道的 legacy 字段，不放在 `config` 里。

### 12.9 汇付斗拱（`huifu`）

| 字段 | 从哪里拿 | 说明 |
| --- | --- | --- |
| `sys_id` | 斗拱控制台 → 商户信息 / 渠道商 ID | Epay 里的「汇付系统号」；渠道商模式下填渠道商 ID |
| `product_id` | 斗拱控制台 → 产品中心 | Epay 里的「汇付产品号」，如 `PAYUN` |
| `private_key` | 斗拱控制台 → 密钥管理 → 商户私钥 | 纯 base64（无 PEM 头）或整段 PEM 都行，插件会补齐 |
| `huifu_public_key` | 同上 → 汇付公钥 | 验回调与响应签名 |
| `huifu_id` | 子商户号 | 直连商户留空（默认用 `sys_id`）；渠道商必须填 |

联调要点：`/channels/:id/test` 会真发一笔下单请求（默认 1 元），成功时返回 `pay_type=qrcode` 与 `qr.alipay.com` 链接；报 `resp_desc` 时多半是 `sys_id`/`product_id` 不匹配或私钥没配对。

### 12.10 联调检查表

| 现象 | 多半是 |
| --- | --- |
| `invalid channel configuration: <field> is required` | 该字段没填/全空格 |
| 支付宝 `isv.invalid-signature` | 私钥与 APPID 不匹配，或把应用公钥填进了 `alipay_public_key` |
| 微信 `401 Unauthorized` / `SIGN_ERROR` | `serial_no` 与私钥不配对，或系统时间偏差 > 5 分钟 |
| 微信 `404` + `NOT_ENOUGH` | 商户号没开通对应产品 |
| 银联 `respCode=01` 之类 | 证书 ID 与私钥不匹配、或用了测试证书打生产网关 |
| `no available payment channel` | 渠道 `status` 不是 active、`weight=0`、`payment_type` 与下单 type 不一致 |
