# Epay 兼容契约基线

## 目的

本文件记录 GroixPay 必须兼容的 Epay 源码行为。机器可读事实源位于 `tests/contracts/epay/manifest.yaml`。当公开文档与源码冲突时，以当前 `Epay/` 源码实际行为为准；历史缺陷先在兼容入口保留，GroixPay 自有 `/api/v1/` 不继承这些缺陷。

## 入口矩阵

| 入口 | 方法 | 鉴权 | 成功码 | 响应 |
| --- | --- | --- | --- | --- |
| `/submit.php` | GET/POST | MD5 或 RSA 参数签名 | 无 | HTML、脚本跳转或支付页 |
| `/mapi.php` | POST | MD5 或 RSA 参数签名 | `1` | JSON，存在历史 HTML 边界路径 |
| `/api.php?act=*` | GET 或混合 GET/POST | 多数为明文 `pid + key` | 查询为 `1`，退款为 `0` | JSON |
| `/api/{class}/{method}` | POST | 时间戳 + MD5/RSA 参数签名 | `0` | 签名 JSON，`pay/submit` 除外 |
| `/pay/{action}/{trade_no}/` | 渠道决定 | 插件验签 | 无 | 插件特定回执或页面 |

## 签名规范

参数按 PHP `ksort()` 升序排列，排除数组、`null`、空字符串、纯空白字符串、`sign` 和 `sign_type`。保留值原始空白，不做 URL 编码，使用 `&` 拼接 `key=value`。

MD5 计算为：

```text
lowercase_hex(md5(sign_content + merchant_key))
```

RSA 使用 SHA256WithRSA PKCS#1 v1.5 和标准 Base64。请求由商户私钥签名，平台用商户公钥验证；平台响应和新版通知由平台私钥签名。

重要边界：响应中的数组字段完全不参与签名；普通新版方法返回的数组即使 `code=-1` 也会被响应层签名，但异常响应不签名，`pay/create` 只签成功响应。

## 支付与订单

- `submit.php` 在 GET 存在 `pid` 时整组使用 GET，否则整组使用 POST，不混合读取。
- `submit.php` 的 `return_url` 必填但不验证 URL 格式；`mapi.php` 的 `return_url` 可选。
- 商户订单号允许字母、数字、点、下划线、连字符和竖线。
- 金额只验证正数、数字及字符集合，没有强制最多两位小数。
- 十日内相同商户订单号复用旧订单；已支付或关键字段变化时拒绝。
- 订单状态为 `0` 未支付、`1` 已支付、`2` 已退款、`3` 已冻结、`4` 预授权。
- `/pay/{action}/{trade_no}/` 的动作不是固定白名单，而是动态映射到订单插件同名方法。

## 回调与通知

渠道插件负责验签、订单号、金额和渠道状态校验，并返回插件特定回执。上游回执可能是 `success/fail`、`OK/FAIL`、XML 或 JSON，不能全局硬编码。

同步返回和异步通知都可能先将订单置为成功。首次处理负责账务和商户通知；后续回调不应重复入账。旧系统并发更新不是严格原子，GroixPay 必须保持外部结果兼容，但内部使用条件更新、唯一幂等键和账本事务消除重复入账风险。

商户通知使用 GET。响应正文只要包含 `success`、`SUCCESS` 或 `Success` 即成功，HTTP 200 本身不代表成功。只有异步路径首次完成订单时，通知失败才创建自动重试；旧系统大致在首次失败后第 1、3、19、55、115 分钟重投，最终设置 `notify=-1`。

## 明确保留的历史语义

- 旧 `/api.php?act=*` 使用明文商户密钥并拒绝 RSA 商户。
- 旧接口与新版接口使用不同成功码。
- 数组响应字段不参与平台 RSA 签名。
- `merchant/orders` 的状态参数存在 POST 检测、GET 读取的缺陷。
- `transfer/proof` 的 `out_biz_no` 查询错误使用 `biz_no` 列。
- 同步路径首次完成且商户通知失败时不自动安排重试。
- 插件对渠道回调的确认文本由插件决定。

这些行为必须拥有契约测试。涉及安全的旧接口默认保留兼容，但应支持配置关闭，并在文档与审计中标记为遗留接口。

## 兼容处置

- 严格兼容：旧入口的字段校验、签名数组排除、通知重试和插件回执语义保持不变。
- 可配置遗留：使用明文商户密钥的旧 `/api.php?act=*` 默认兼容，但支持全局关闭并记录安全审计。
- 新 API 禁止继承：并发回调可能重复执行账务的内部缺陷不得进入 `/api/v1/` 或领域核心。

## 来源

主要来源包括：

- `Epay/submit.php`
- `Epay/mapi.php`
- `Epay/api.php`
- `Epay/pay.php`
- `Epay/includes/lib/ApiHelper.php`
- `Epay/includes/lib/Payment.php`
- `Epay/includes/lib/Plugin.php`
- `Epay/includes/lib/api/Pay.php`
- `Epay/includes/lib/api/Merchant.php`
- `Epay/includes/lib/api/Transfer.php`
- `Epay/includes/functions.php`
- `Epay/cron.php`

每条机器契约均在 `manifest.yaml` 中记录对应源码文件和起始行。

## 阶段验收

- 验收日期：2026-07-12
- 契约范围：2 个支付入口、6 个旧版操作、12 个新版操作。
- 请求样本：40 条，所有公开操作均包含成功和失败样本。
- 响应样本：40 条，覆盖 HTML、JSON、签名与非签名响应。
- 通知样本：5 条，覆盖 V0/V1、三种成功正文和 HTTP 200 失败边界。
- 支付动作及渠道回执样本：13 条。
- 签名向量：2 组清单向量和 1 组固定 RSA 密钥向量。
- 动态环境：Go 1.21.4、PHP 7.4.3、OpenSSL 扩展可用。
- 验收命令：`go test ./tests/contracts/epay -v` 与 `go vet ./tests/contracts/epay`。
- 验收结果：16 个 Go 测试全部通过，PHP 与 Go 的 MD5、RSA 结果逐字节一致。
