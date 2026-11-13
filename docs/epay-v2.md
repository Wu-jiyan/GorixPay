# GorixPay 易支付兼容协议（V1 / V2）

本文面向接入商户，说明 GorixPay 对彩虹易支付协议的完整复刻范围、签名规则、接口清单与安全要求。
实现事实来源：上游参考源码（本地参照副本，不进仓库）（`includes/lib/api/*.php`、`includes/lib/ApiHelper.php`、`includes/lib/Payment.php`、`api.php`）与易支付官方 V2 文档。

## 1. 协议总览

| 维度 | V1 | V2 |
| --- | --- | --- |
| 签名算法 | MD5（`sign_type=MD5`，缺省） | RSA-SHA256（`sign_type=RSA`） |
| 待签名串 | 排序拼接后追加 `key` | 排序拼接，**不追加密钥** |
| 方向 | 单向（请求签名） | 双向（商户私钥签请求，平台私钥签响应/通知） |
| 时间戳 | 可选 | 必填 `timestamp`（10 位秒），服务端允许 ±300 秒 |
| 网关 | `/submit.php`、`/mapi.php`、`/api.php` | `/api/{class}/{method}`（等价 `/api.php?s=class/method`） |
| 退款/代付 | 仅 `api.php?act=refund` | `pay/refund`、`pay/refundquery`、`transfer/*` |

金额单位：对外一律“元”（字符串，最多两位小数，最小 0.01，最大 100000.00）；内部按分存储。

## 2. 签名规则

### 2.1 待签名串（canonical）

1. 取全部业务参数。
2. 剔除 `sign`、`sign_type`，剔除空值（`""`/`null`）与数组/文件类型。
3. 按参数名 ASCII 升序排序。
4. 以 `k1=v1&k2=v2&...` 拼接，末尾不带 `&`。

### 2.2 MD5（V1）

```text
sign = md5(canonical + merchant_key)   // 32 位小写
```

### 2.3 RSA（V2）

```text
sign = base64( SHA256withRSA(canonical, private_key) )
```

- 商户请求：商户私钥签名，平台用商户公钥验签。
- 平台响应 / 异步通知：平台私钥签名，商户用平台公钥验签。
- 平台公钥在「商户中心 → 密钥管理」展示，也可通过 `GET /api/v1/merchant/credentials/v2` 获取。

### 2.4 时间戳与防重放

- V2 请求必须携带 `timestamp`（10 位秒级字符串），`|now - timestamp| > 300` 返回 `code=-3`。
- 平台对退款、关单、代付类请求按 `(pid, sha256(sign))` 做一次性校验（10 分钟窗口），重复提交返回 `code=-3, msg=重复请求`。
- 平台响应与通知同样带 `timestamp`，商户应校验时间窗。

## 3. V1 接口

### 3.1 页面跳转 `POST|GET /submit.php`

表单参数与 `mapi.php` 相同（`pid`、`type`、`out_trade_no`、`notify_url`、`return_url`、`name`、`money`、`param`、`clientip`、`sign`、`sign_type`），成功时 302 到收银台，参数错误返回 HTML 提示。

### 3.2 API 下单 `POST /mapi.php`

成功：

```json
{"code":1,"msg":"订单创建成功","trade_no":"2026111312000012345","pay_type":"qrcode","pay_info":"https://..."}
```

失败：`{"code":-1,"msg":"..."}`。

### 3.3 查询与账户 `GET|POST /api.php`

| act | 参数 | 说明 |
| --- | --- | --- |
| `query` | `pid`、`key` | 商户账户：`code=1, pid, money, orders, orders_today, orders_lastday, active` |
| `order` | `pid`、`key`、`trade_no` 或 `out_trade_no` | 单笔订单 |
| `orders` | `pid`、`key`、`limit`、`offset`、`status?` | 订单列表 `code=1,count,data[]` |
| `refund` | `pid`、`key`、`trade_no`/`out_trade_no`、`money` | 申请退款，成功 `code=0` |
| `settle` | `pid`、`key`、`limit`、`offset` | 结算记录 |

错误码：`-1` 业务失败、`-2` 未开启、`-3` 验签/密钥错误、`-4` 参数缺失、`-5` 不存在的 act。

> 说明：官方 V1 的 `act=query` 是“账户信息”，不是订单查询；订单查询请用 `act=order`。

## 4. V2 接口

所有 V2 请求：`POST`（`pay/submit` 同时支持 GET），`Content-Type: application/x-www-form-urlencoded`，响应 `application/json`。
所有 V2 响应：`code`（0 成功，<0 失败）、`msg`、`data...`、`timestamp`、`sign_type:"RSA"`、`sign`。

### 4.1 `pay/submit`

参数：`pid`、`type?`、`out_trade_no`、`notify_url`、`return_url?`、`name`、`money`、`param?`、`clientip?`、`device?`、`timestamp`、`sign`、`sign_type=RSA`。
成功 302 到 `{站点}/cashier.php?trade_no=...`。

### 4.2 `pay/create`

在 `pay/submit` 基础上增加 `method`（`web|jump|jsapi|scan|app`）、`clientip`（必填）、`auth_code?`、`sub_openid?`、`sub_appid?`。
成功：

```json
{
  "code": 0,
  "trade_no": "2026111312000012345",
  "pay_type": "qrcode",
  "pay_info": "https://qr.alipay.com/xxx",
  "timestamp": "1763000000",
  "sign_type": "RSA",
  "sign": "Base64..."
}
```

`pay_type` 取值：`jump`、`qrcode`、`urlscheme`、`html`、`jsapi`、`app`、`scan`、`wxplugin`、`wxapp`。`pay_info` 在 `jump/qrcode/urlscheme` 下为 URL，其他类型为对象。

### 4.3 `pay/query`

参数：`pid` + `trade_no` 或 `out_trade_no`。
响应：`code=0, trade_no, out_trade_no, api_trade_no, type, pid, name, money, param, buyer, clientip, status, addtime, endtime, refundmoney`。
`status` 为易支付数字状态：`0` 未支付、`1` 已支付、`2` 已关闭/失败。

### 4.4 `pay/close`

参数：`pid` + 订单号；成功 `code=0, msg=订单关闭成功`。

### 4.5 `pay/refund`

参数：`pid`、`trade_no` 或 `out_trade_no`、`money`、`out_refund_no?`。
同一 `out_refund_no` 幂等；累计退款不得超过订单实付。响应：`code=0, refund_no, out_refund_no, trade_no, money, status, msg`。

### 4.6 `pay/refundquery`

参数：`pid` + `refund_no` 或 `out_refund_no`；响应含 `refund_no/out_refund_no/trade_no/out_trade_no/money/status/addtime/endtime`。

### 4.7 `merchant/info`、`merchant/orders`

- `merchant/info`：`code=0, pid, money, orders, orders_today, orders_lastday, active`。
- `merchant/orders`：`limit`（<=50）、`offset`、`status?`，返回 `code=0, count, data[]`。

### 4.8 `transfer/submit`、`transfer/query`、`transfer/proof`、`transfer/balance`

- `transfer/submit`：`pid`、`out_transfer_no`、`money`、`account_name`、`account_no`、`bank_name`、`branch_name?`；申请后代付单进入待审核。
- `transfer/query`：`transfer_no` 或 `out_transfer_no`。
- `transfer/proof`：返回代付状态与凭证占位字段。
- `transfer/balance`：返回可用余额与冻结余额。

## 5. 异步通知（商户）

订单支付成功后，平台向 `notify_url` 发起 `POST`（`application/x-www-form-urlencoded`），参数：

| 字段 | 说明 |
| --- | --- |
| `pid` | 商户 ID（legacy uid） |
| `trade_no` | 平台订单号 |
| `out_trade_no` | 商户订单号 |
| `api_trade_no` | 渠道交易号 |
| `type` | 支付方式 |
| `name` | 商品名 |
| `money` | 金额（元，两位小数） |
| `trade_status` | 固定 `TRADE_SUCCESS` |
| `param` | 透传参数 |
| `buyer` | 买家标识（可空） |
| `timestamp` | 秒级时间戳 |
| `sign_type` | `MD5`（V1 商户）或 `RSA`（V2 商户） |
| `sign` | 签名 |

- V1 商户：`md5(canonical + merchant_key)`。
- V2 商户：平台私钥 RSA-SHA256 签名，商户用平台公钥验签。
- 切换规则：商户在「密钥管理」生成/轮换 V2 RSA 密钥后，后续异步通知自动切换为 `sign_type=RSA`；升级验签逻辑前请勿生成 V2 密钥。
- 商户返回纯文本 `success` 视为成功，否则平台按退避策略重试（Outbox 死信可人工重投）。

## 6. 零元购（0 元入账）防护说明

历史易支付 V1 的风险点：下单请求与“支付成功通知”共用同一套 MD5 规则，泄露的请求签名可能被复用/拼接成伪造通知，站点只验签与金额就入账，导致未付款却到账。

GorixPay 的处置：

1. **签名方向分离**：V2 请求只由商户私钥签名，平台响应/通知只由平台私钥签名；请求签名无法当通知使用。
2. **时间戳 + 一次性签名**：V2 请求必须带 `timestamp`（±300s），退款/关单/代付按签名哈希一次性消费，重放直接拒绝。
3. **金额以订单为准**：下单金额落库后不可变；回调金额必须与订单金额完全相等；收银台与沙箱支付接口不接受客户端金额。
4. **状态只能由渠道回调推进**：只有 `payment/callback` 的 `Complete` 能把订单置为已支付，没有“手动标记已支付”接口；沙箱渠道仅在开发环境显式开启。
5. **退款/代付边界**：累计退款不超过实付；代付先冻结后扣减，重复业务单号幂等。

## 7. 接入示例

### 7.1 V2 请求签名（Go）

```go
func canonical(params map[string]string) string {
    keys := make([]string, 0, len(params))
    for k, v := range params {
        if k == "sign" || k == "sign_type" || v == "" { continue }
        keys = append(keys, k)
    }
    sort.Strings(keys)
    parts := make([]string, 0, len(keys))
    for _, k := range keys { parts = append(parts, k+"="+params[k]) }
    return strings.Join(parts, "&")
}

func signRSA(params map[string]string, privateKey *rsa.PrivateKey) (string, error) {
    digest := sha256.Sum256([]byte(canonical(params)))
    signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
    if err != nil { return "", err }
    return base64.StdEncoding.EncodeToString(signature), nil
}
```

### 7.2 发起 V2 下单

```bash
curl -X POST 'https://pay.example.com/api/pay/create' \
  -d 'pid=10001' \
  -d 'type=alipay' \
  -d 'method=web' \
  -d 'out_trade_no=ORDER20261113001' \
  -d 'notify_url=https://shop.example.com/notify' \
  -d 'return_url=https://shop.example.com/return' \
  -d 'name=测试商品' \
  -d 'money=0.01' \
  -d 'clientip=127.0.0.1' \
  -d 'timestamp=1763000000' \
  -d 'sign_type=RSA' \
  -d 'sign=<Base64>'
```

### 7.3 验签平台响应（PHP）

```php
$publicKey = openssl_pkey_get_public($platformPublicKeyPem);
$sign = base64_decode($response['sign']);
unset($response['sign'], $response['sign_type']);
ksort($response);
$content = urldecode(http_build_query($response));
$ok = openssl_verify($content, $sign, $publicKey, OPENSSL_ALGO_SHA256) === 1;
```

> 注意：`http_build_query` 默认会做 URL 编码，必须与平台一致的“未编码原始值 + `&`”拼接；生产接入建议直接使用 SDK 或按第 2.1 节实现 canonical。

## 8. 兼容性验收

- V1 契约样本：`tests/contracts/epay/`（路由、签名、通知、支付动作）。
- V2 契约测试：`internal/compat/epay/v2/*_test.go`（签名向量、时间戳、篡改、重放、0 元拒绝）。
- 端到端脚本：`scripts/e2e.ps1`（注册 → 建渠道 → 下单 → 沙箱支付 → 查询 → 退款）。
