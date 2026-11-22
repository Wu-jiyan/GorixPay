# 系统设置参考

后台「系统设置」里的每一个键，都在这张表里。写它就是写 `system_settings` 表的一行，读它走 `internal/settings.Reader`，改完**当次请求**生效，不需要重启。

三件事保证了这张表不会变成「假设置」：

1. 白名单（`internal/admin/httpapi/settings.go` 的 `allowedSettings`）：不在表里的一律 `400 invalid_request`，所以删一个消费点就必须同步删白名单。
2. 范围校验（同文件的 `settingRules`）：枚举键只能是固定值，数值键有上下限且要求整数，越界回 `400 invalid_setting_value`。
3. 审计：每次成功写入记一条 `setting.updated`，含 key、值、IP、操作人；凭据类键不记明文。

## 站点

| 键 | 类型 | 默认 | 效果 | 读取方 |
| --- | --- | --- | --- | --- |
| `site.name` | text | GorixPay | 浏览器标题、后台侧栏、支付页与页脚的站点名 | `announcement`、前端 `stores/site.ts` |
| `site.logo_url` | text | 空 | 留空用站点名首字方块 | `BrandMark.vue`、支付页品牌行 |
| `site.footer_text` | text | 空 | 官网页脚一行说明 | `PublicFooter.vue` |
| `site.skin` | enum `default` / `dark` / `compact` | default | 写进 `<html data-skin>`，只换配色与密度 | `App.vue` |
| `site.homepage_mode` | enum `landing` / `login` / `url` | landing | `/` 的行为：正常首页 / 直接跳商户登录 / 跳指定网址（对应上游 `homepage`） | `Landing.vue` |
| `site.homepage_url` | text | 空 | 仅 `homepage_mode=url` 时使用 | `Landing.vue` |
| `site.contact_qq` | text | 空 | 客服 QQ（上游 `kfqq`） | 站点载荷 |
| `site.contact_email` | text | 空 | 联系邮箱（上游 `email`） | 站点载荷 |
| `site.icp_number` | text | 空 | 备案号（上游 `footer`） | 站点载荷 |

## 注册与入驻

| 键 | 类型 | 默认 | 效果 | 读取方 |
| --- | --- | --- | --- | --- |
| `register.mode` | enum `open` / `closed` / `invite` | open | 注册门禁（上游 `reg_open`）。closed 回 `register_closed`；invite 无邀请码回 `invite_required` | `identity/httpapi` |
| `register.review_required` | bool | false | 新商户落 `pending`，登录回 403 `merchant_pending`，运营在商户列表点「启用」才通过（上游 `user_review`） | `identity/auth` |
| `register.verify_channel` | enum `off` / `email` / `phone` | off | 注册要先收验证码（上游 `verifytype`）。校验 identifier 与渠道匹配，失败回 `verify_code_required` / `verify_channel_mismatch` / `verify_code_invalid` | `identity/httpapi` + `notify` |

## 支付

| 键 | 类型 | 默认 | 效果 | 读取方 |
| --- | --- | --- | --- | --- |
| `payment.order_expiry_minutes` | int 0–1440 | 30 | 订单可支付窗口。**0 表示不过期**，收银台对零值跳过过期判断 | `payment/service` |
| `payment.min_amount_cents` | int ≥1 | 1 | 单笔最小金额（分），低于则下单被拒 | `payment/service` |
| `payment.max_amount_cents` | int ≥1 | 10000000 | 单笔最大金额（分） | `payment/service` |
| `payment.domain_whitelist_required` | bool | false | 开则 `notify_url` 域名必须已报备（上游 `pay_domain_forbid`） | `domainauth` |
| `payment.domain_self_service` | bool | true | 商户能否自助报备域名；关掉后 `/merchant/domains` 回 `domain_self_service_disabled`（上游 `pay_domain_open`） | `domainauth` |
| `payment.merchant_refund_enabled` | bool | true | 商户能否发起退款；关掉后 `/merchant/refunds` 回 `refund_disabled`，运营仍可退 | `merchant/httpapi` |

## 支付测试（官网 `/test`）

| 键 | 类型 | 默认 | 效果 |
| --- | --- | --- | --- |
| `site.test_payment_enabled` | bool | true | 是否对外开放测试下单 |
| `site.test_payment_pid` | int | 0 | 测试订单落在哪个商户（易支付老 UID）；0 取最早注册的启用商户 |
| `site.test_payment_max_cents` | int ≥1 | 100 | 单笔上限，默认 ￥1 |
| `site.test_payment_hourly_limit` | int 1–1000 | 10 | 每 IP 每小时笔数 |

## 风控与保证金

| 键 | 类型 | 默认 | 效果 | 读取方 |
| --- | --- | --- | --- | --- |
| `risk.daily_amount_limit_cents` | int | 0 | 单商户单日交易额上限，0 不限制 | `risk` |
| `risk.daily_order_limit` | int | 0 | 单商户单日订单数上限 | `risk` |
| `risk.ip_daily_limit` | int | 0 | 单 IP 单日下单上限 | `risk` |
| `risk.ip_whitelist` | text | 空 | 逗号或换行分隔，支持 CIDR。**命中的来源不计数、不受上面三条限额约束** | `risk` |
| `merchant.deposit_required_cents` | int | 0 | 保证金要求，未达标不能发销售单 | `funding` |
| `certification.required` | bool | false | 强制实名 | `certification` |
| `certification.fee_cents` | int | 0 | 实名认证费，从商户余额扣 | `certification` |

## 邀请返佣

| 键 | 类型 | 默认 | 效果 |
| --- | --- | --- | --- |
| `invite.enabled` | bool | false | 是否计算返佣 |
| `invite.rate_bp` | int 0–10000 | 0 | 返佣比例（万分之一为单位，10000 = 100%） |

## 通知与验证码

| 键 | 类型 | 默认 | 效果 |
| --- | --- | --- | --- |
| `notify.smtp_host` | text | 空 | SMTP 主机。**出站过 netguard**：回环、私网、云元数据地址一律拒 |
| `notify.smtp_port` | int | 587 | 常用 465 / 587 / 25 |
| `notify.smtp_username` | text | 空 | — |
| `notify.smtp_password` | secret | 空 | 主密钥加密存储，接口只回「已配置」 |
| `notify.smtp_from` | text | 空 | 发件人地址；与 host 都填了才算配置完成 |
| `notify.sms_endpoint` | text | 空 | 短信网关 URL，POST JSON `{phone, message}`。出站过 netguard |
| `notify.sms_token` | secret | 空 | 以 `Authorization: Bearer` 发送 |
| `wechat.mp.template_id` | text | 空 | 公众号模板消息 ID，留空不发 |
| `notification.max_attempts` | int 1–50 | 5 | 回调投递最大尝试次数（启动时读进 outbox worker） |
| `notification.poll_interval_seconds` | int 1–3600 | 1 | outbox 轮询间隔 |

## 登录与安全

| 键 | 类型 | 默认 | 效果 |
| --- | --- | --- | --- |
| `security.captcha_required` | bool | false | 登录要求图形验证码；同时出现在 `/public/site` 让登录页决定是否显示 |
| `oauth.wechat.app_id` / `.app_secret` | text / secret | 空 | 微信开放平台凭据 |
| `oauth.qq.app_id` / `.app_secret` | text / secret | 空 | QQ 互联凭据 |
| `oauth.douyin.app_id` / `.app_secret` | text / secret | 空 | 抖音开放平台凭据 |

## 加一个新键的顺序

少一步就会出现「存了没人读」或「读了存不进去」，这两类问题在本仓都真实发生过：

1. `allowedSettings` 加键，否则后台保存就 400。
2. 有范围或枚举的，`settingRules` 加规则。
3. 写消费点，用 `settings.Reader`（`TextOr` / `Bool` / `Int`），不要自己再写一份 JSON 解析。
4. 前端 `frontend/src/pages/admin/Settings.vue` 加字段（`type` 取 `text` / `textarea` / `number` / `bool` / `select` / `secret` / `cents`）。
5. 本表加一行，写明默认值与读取方 —— 这一行是下次审计时唯一能对上号的地方。