# 易支付覆盖度对照

审计对象是 `E:\epay-upstream\Epay` 这份源码快照（下称上游）。判定依据只有两样：真的打开读过的上游文件（头部注释、`$title`、主要函数、`switch` 分支），本仓库里对应的路由或包。判定只有两种：`已实现`，或者 `不适用`（附一句理由）。已交付的部分算已实现，个别上游细节按设计不做的，在「我们的实现」格里用一句话标出不适用及原因。

上游规模：根目录 13 个 PHP 入口，`admin/` 55 个，`user/` 36 个，`plugins/` 63 个渠道目录，`paypage/` 14 个支付页，`includes/pages/` 28 个通道页面，`includes/lib/` 23 个公共库（外加 `api`、`mail`、`ocr`、`ProfitSharing`、`sms`、`wechat` 六个子目录），`template/` 11 套皮肤。

2026-11-14 补了三批运营面功能，又做了一轮收尾。第一批是用户组与费率、自动结算与自动打款、内部接收方分账、红包批次、CSV 导出与统计（迁移 `000017` 到 `000019`）。第二批是投诉处理、后台安全（图形验证码与 TOTP）、支付方式与按日轮询、通知通道与找回密码（迁移 `000020` 到 `000023`）。第三批是语音设备与云打印、数据清理与测试支付、第三方登录与扫码登录、用户组套餐购买、红包过期退回、官网三档主题（迁移 `000024` 到 `000026`）。收尾轮补了插件定时钩子、投诉自动规则、`/pay/{func}` 八种页面名、`getshop` 轮询与验证码、后台手动与批量付款、注册邀请码、商户通知偏好、商户两份 CSV、聚合收款码与自助测试支付（迁移 `000027`、`000028`）。

结论先给：协议面（V1 六个入口、V2 十二个方法、异步通知、浏览器回跳）是 100% 覆盖。运营面覆盖到订单、退款、结算、分账、投诉（含自动规则）、用户组与套餐、通道与轮询、公众号配置、商户与导出、安全、设备、第三方登录、清理与主题。代码层面只剩公众号模板消息推送没写，另有几处按设计不适用与三个待渠道开通的口子，逐条写在对应表格和第 8、9 节。

## 1. 对外 HTTP 入口

| 上游入口 | 上游做什么 | 我们的实现 | 判定 |
| --- | --- | --- | --- |
| `/submit.php` | 页面跳转下单，GET 有 `pid` 时整组读 GET（`includes/lib/api/Pay.php:8-261`） | `internal/compat/epay/payment/handler.go` 的 `submit` 与 `selectSubmitParameters` | 已实现 |
| `/mapi.php` | API 下单，成功回扁平 `payurl`/`qrcode`/`urlscheme`/`html`（`Payment.php:246-296`） | 同文件 `mapi` | 已实现 |
| `/api.php?act=query\|order\|orders\|refund\|refundapi\|settle` | 账户、单笔、列表、退款、内部退款、结算 | `internal/compat/epay/payment/api.go` | 已实现 |
| `/api/{class}/{method}`（`api.php?s=` 重写） | V2 分发，`pay`/`merchant`/`transfer` 三组 | `internal/compat/epay/v2/handler.go`，12 个方法 | 已实现 |
| `/submit2.php` | 失败重试换通道页，已完成订单渲染 sysmsg | `internal/compat/epay/page/page.go` 的 `submit2` | 已实现 |
| `/pay/{func}/{trade_no}/` | `Plugin::loadForPay` 按 `func` 渲染通道私有支付页 | `internal/compat/epay/page/page.go` 注册了上游八种页面名（`scan`、`qrcode`、`jsapi`、`mini`、`h5`、`red`、`bank`、`wap`），统一落到收银台 SPA；通道私有页面模板本身不适用，理由是我们用收银台统一渲染 | 已实现 |
| `/cashier.php` | 收银台选支付方式 | `frontend/src/pages/cashier/`，前端路由 `/cashier.php` | 已实现 |
| `/gateway.php` | 语音播报设备与扫码枪的 JSON 网关 | `internal/device`：`/api/v1/device/voice/{heartbeat,ack}` 与 `/printer/{poll,ack}`，`device_no` 加 secret、常量时间比对、不走会话。扫码枪独立网关不适用，扫码枪下单走 V1 的 `method=scan` | 已实现 |
| `/getshop.php` | 收银台轮询付款结果，`act=captcha_verify` 换安全验证密钥 | `internal/compat/epay/page/page.go` 的 `getshop`：`act=poll` 回订单状态，`act=captcha_verify` 校验 SVG 验证码并换一次性 key（未接线时回 `code=-1`，不发 key） | 已实现 |
| `/gold.php` | 微信点金计划支付结果 iframe | 不适用：需要微信支付服务商资质，代码不实现，也没有可以本地模拟的接口 | 不适用 |
| `/wework.php` | 企业微信客服回调，驱动 `weixin://biz/ww/kefu/` 跳转支付 | 不适用：需要企业微信资质与客服账号，本地无法模拟 | 不适用 |
| `/index.php` | 官网首页、`?mod=` 模板页、`?doc=` 文档页、`?invite=` 邀请绑定 | SPA 官网与 `/doc` 文档页；邀请码走注册参数；主题由 `site.skin` 控制。上游 11 套官网模板按设计不逐套复刻，只做 `default`、`dark`、`compact` 三档 | 已实现 |
| `/{mod}.html`、`/doc/{name}.html` | nginx 重写到 `index.php` 的伪静态页 | 不适用：SPA 路由没有伪静态，等价能力在 `/doc` 与站内路由 | 不适用 |
| `/config.php` | 数据库连接配置 | YAML、`GORIXPAY_*` 环境变量、启动参数（`internal/platform/config/config.go`） | 已实现 |

## 2. `cron.php` 定时任务

上游 `cron.php` 靠 `?do=` 分派，入口校验 `cronkey`。我们的后台循环：Outbox worker 收发通知，结算规则每分钟巡检，投诉自动规则与插件定时钩子每分钟跑，红包过期与登录态清理每 30 分钟一次。

| `do=` | 上游做什么 | 我们的实现 | 判定 |
| --- | --- | --- | --- |
| `notify` | 失败通知按 1、3、20 分钟等间隔重发，每轮 20 条（`cron.php:192-245`） | `internal/outbox` 的 worker，租约加 CAS，按幂次退避 | 已实现 |
| `notify2` | 手工重发 `notify=-1` 的失败通知 | `POST /api/v1/admin/outbox/requeue-dead` | 已实现 |
| `settle` | 按余额门槛、结算费率、实名强制条件自动生成结算列表（`cron.php:19-66`） | `internal/settlement`：每日按规则生成结算单（同日幂等），后台每分钟巡检 | 已实现 |
| `order` | 订单统计与清理、余额不足提醒、邀请返利结算（`cron.php:67-191`） | 返利在支付回调同事务入账，统计走实时报表，清理走 `POST /admin/maintenance/cleanup`。余额不足提醒不适用：我们用结算最低金额与保证金在下单与结算环节拦截，没有余额预警阈值这个配置项 | 已实现 |
| `profitsharing` | 分账任务与支付宝直清结算任务（`\lib\ProfitSharing\CommUtil::task()`、`Payment::settle_task`） | `internal/profitsharing` 在支付回调里同步给内部接收方分账。渠道侧分账与直清待渠道开通，见第 9 节 | 已实现 |
| `check` | 异步风控检测（`\lib\RiskCheck::execute()`） | 风控在下单链路同步执行（`internal/risk`）。异步巡检不适用：同步拦已经覆盖下单前校验，不需要队列 | 已实现 |
| `complain` | 拉取支付宝、微信投诉（`cron.php:270-319`） | `internal/complaint`：运营端登记、回复、关闭，状态 `pending` 到 `replied` 到 `closed`，超期读取时派生 `overdue`，商户端只读。自动拉取待渠道开通，见第 9 节 | 已实现 |
| `complain_complete` | 投诉自动回复处理（`cron.php:320-339`） | 迁移 `000028` 的 `complaint_auto_rules`：按渠道配置自动回复文本与几小时后关闭，`RunAutoRules` 每分钟跑，`*` 兜底、`disabled` 阻断兜底、重复跑幂等，超时关闭追加系统消息 | 已实现 |
| `plugin` | 调用通道插件的 `_cron` 钩子（`cron.php:340-349`） | `internal/plugin/payment` 的 `CronHook` 接口、`Registry.CronHooks()` 与 `RunCronHooks()`：按注册顺序跑，跑完收集所有错误，panic 转 error；app 层每分钟 ticker 调用 | 已实现 |
| `transfer` | 自动结算转账（支付宝转账接口，含收款方失败码分类，`cron.php:350-401`） | 批量审核通过后自动建代付单（`internal/settlement` 加 `internal/transfer`），缺结算资料或建单失败记 `failed` 加原因；没有直连支付宝转账接口，出款走代付通道 | 已实现 |

## 3. 运营后台（`admin/`，55 个 PHP）

上游后台是 Bootstrap 多页应用，功能按 `admin/head.php` 的菜单分组。我们的后台是 Vue SPA（`frontend/src/pages/admin/`）加 `/api/v1/admin/*` 接口。

| 分组 | 上游文件 | 上游能力 | 我们的实现 | 判定 |
| --- | --- | --- | --- | --- |
| 登录与会话 | `login.php`、`code.php`、`log.php`、`sso.php`、`index.php`、`head.php` | 账号密码登录、图形验证码、登录日志、一键登录商户、后台首页 | 邮箱登录加会话、SVG 验证码（`/api/v1/captcha`，只存哈希、一次性）、TOTP（`/admin/security/totp/*`，密钥 AES-GCM 加密）、登录尝试审计；缺码回 400 `captcha_required` 或 401 `totp_required`。一键登录商户（`sso.php`）不适用：运营不持商户会话，排障走商户详情、订单与审计日志 | 已实现 |
| 订单 | `order.php`、`export.php`、`buyerstat.php`、`blacklist.php`、`ajax_order.php` | 订单列表、导出、支付用户统计、支付黑名单 | `/admin/orders` 与关单、`GET /admin/export/orders.csv`、`GET /admin/stats/buyers`、`/admin/blacklist`（`internal/reporting`、`internal/risk`） | 已实现 |
| 分账 | `ps_receiver.php`、`ps_order.php`、`ajax_profitsharing.php` | 分账规则与分账记录 | `internal/profitsharing`：接收方增删改查、分账记录、重试；按 `rate_bp` 分账、手续费先扣、向下取整到分，内部接收方同事务走账本（幂等键 `split:{orderID}:{receiverID}`），费率合计超 100% 拒绝。渠道侧直清分账待渠道开通，见第 9 节 | 已实现 |
| 结算 | `slist.php`、`settle.php`、`settle_batch.php`、`ajax_settle.php` | 结算列表、批量结算、批量转账 | `internal/settlement`：结算规则增删改查与手动触发、`/admin/settlements/batch-process`、`/admin/settlements/:id/auto-transfer`，每分钟巡检，同日幂等，审核通过自动建代付单，缺结算资料记 `failed` 并写原因 | 已实现 |
| 付款与红包 | `transfer.php`、`transfer_add.php`、`transfer_red.php`、`transfer_batch.php`、`transfer_export.php`、`transfer_stat.php`、`ajax_transfer.php` | 付款记录、新增付款、创建红包、批量付款、导出、付款统计 | `internal/transfer`：后台手动付款 `POST /admin/transfers`（`kind=manual`，幂等键按 sha256 派生）、批量付款 `POST /admin/transfers/batch`（逐条创建，失败整批回滚）、红包批次与撤销、`GET /admin/export/transfers.csv`。红包领取待渠道开通，见第 9 节 | 已实现 |
| 商户 | `ulist.php`、`uset.php`、`ustat.php`、`record.php`、`record_export.php`、`ajax_user.php` | 商户列表与详情、商户支付统计、资金明细与导出 | `/admin/merchants`、`/merchants/:id`、改状态、`/admin/stats/merchants`、`/admin/ledger/*`、`GET /admin/export/merchants.csv` 与 `/admin/export/ledger.csv` | 已实现 |
| 用户组 | `glist.php`、`gedit.php`、`group.php` | 用户组设置、编辑、购买套餐 | `internal/merchantgroup`：组增删改查与启停、商户归组，组内 `rate_bp`、可用支付方式、`settle_mode`（normal/direct）；`internal/groupshop`：定价、套餐购买（余额扣款归组，退款反向入账并解组，幂等键带 attempts 后缀） | 已实现 |
| 邀请码 | `invitecode.php` | 注册邀请码管理 | `internal/invitecode`（迁移 `000027`）：注册邀请码增删改查；注册带 `invite_code` 原子消费次数，注册失败回退，20 并发抢 5 个名额的用例通过 | 已实现 |
| 授权域名 | `domain.php` | 商户支付域名审核 | `/admin/domains` 加审核（`internal/domainauth`） | 已实现 |
| 支付接口 | `pay_channel.php`、`pay_type.php`、`pay_plugin.php`、`pay_roll.php`、`pay_weixin.php`、`pay_wework.php`、`plugin_page.php`、`ajax_pay.php` | 通道、支付方式、插件、轮询、公众号与小程序配置、微信客服 | `internal/channelops`：通道与插件、`pay_types` 与公开接口、`pay_rolls` 按日权重接进渠道选择（当天有记录就替换权重，权重 0 当天不参与）、`wechat_mp_configs` 密钥加密；`site.payment_types` 也读这张表。企微客服支付配置（`pay_wework.php`）不适用，理由同 `/wework.php`，需要企业微信资质 | 已实现 |
| 系统设置 | `set.php`（19 个 `mod`）、`set_totp.php`、`set_wxkf.php`、`gonggao.php` | 站点、支付、风控、结算、转账、认证、登录、通知、邮件短信、模板、计划任务、代理、上传等配置 | `GET/PUT /api/v1/admin/settings/:key`（白名单 23 个键，含 `site.skin`、`security.captcha_required`、`oauth.{wechat,qq,douyin}.{app_id,app_secret}`）、公告、通知通道与测试发送、TOTP 与验证码开关。中转代理与 PHP 式 cron 配置不适用：没有 PHP 进程，定时任务硬接在 app 层（结算、客诉、插件钩子、红包过期、OAuth 清理） | 已实现 |
| 报表与导出 | `download.php`、`export.php`、`record_export.php`、`transfer_export.php`、`ustat.php`、`buyerstat.php` | CSV 导出与统计报表 | `internal/reporting`：后台 7 个 CSV（orders、refunds、settlements、transfers、merchants、ledger、buyers）、商户 4 个 CSV（orders、records、complaints、profit-sharing）、`/admin/stats/{buyers,merchants}`、`/merchant/stats/daily`；都带 BOM 与截断提示 | 已实现 |
| 安全与运维 | `risk.php`、`clean.php`、`testsubmit.php`、`gettoken.php`、`loader_helper.php` | 风控记录、数据清理、测试支付、取用户标识、插件加载辅助 | 风控事件、Outbox、审计、登录尝试审计、`POST /admin/maintenance/cleanup`（四张表白名单，只删 30 天前，默认 dry-run，真删要 `confirm:"DELETE"`）、`POST /admin/channels/:id/test-order`。`gettoken.php` 不适用：会话 token 由登录接口签发，没有单独的取 token 端点 | 已实现 |

## 4. 商户中心（`user/`，36 个 PHP）

| 分组 | 上游文件 | 上游能力 | 我们的实现 | 判定 |
| --- | --- | --- | --- | --- |
| 登录与注册 | `login.php`、`reg.php`、`findpwd.php`、`oauth.php`、`connect.php`、`wxlogin.php`、`qrlogin.php`、`douyinoauth.php`、`openid.php`、`head.php`、`foot.php` | 账号密码、找回密码、QQ 与微信登录、抖音授权、扫码登录、取 openid | `internal/oauth`：微信、QQ、抖音 OAuth（一次性 state、已绑定直接登录、未绑定 409 带 `bind_ticket`、密码绑定）、自绘 SVG 扫码登录（ticket 5 分钟，`pending` 到 `scanned` 到 `confirmed`，token 只发一次）、绑定列表与解绑；找回密码与验证码走 `internal/notify`。取 openid 页（`openid.php`）不适用：JSAPI 的 openid 由商户随单传 `sub_openid` | 已实现 |
| 账户资料 | `index.php`、`completeinfo.php`、`editinfo.php`、`userinfo.php`、`ajax.php`、`ajax2.php` | 首页概览、完善资料、账户与接口信息、密钥类型、重置密钥、RSA 生成、结算资料、通知配置、绑定 | `/merchant/dashboard`、`/profile`、`/credentials`、密钥轮换、结算资料；通知偏好 `internal/notifyprefs`（`GET/PUT /merchant/notification-prefs`，`Notify(ctx, merchantID, scene, subject, body)` 按偏好与渠道发信，通道未配置只记警告不阻塞） | 已实现 |
| 实名认证 | `certificate.php`、`certificate_mobile.php`、`alipaycert.php`、`alipaycertok.php` | 提交实名资料，接支付宝、阿里云、腾讯云 OCR 与人脸核验 | `/merchant/certification` 提交加运营审核（`internal/certification`）。自动 OCR 与人脸核验不适用：需要阿里云、腾讯云凭据，仓库不内置，保留人工审核 | 已实现 |
| 订单 | `order.php`、`download.php`、`ajax2.php` 的 `order`/`subOrders`/`notify`/`printOrder` | 订单列表与详情、补发通知、小票打印、三份 CSV | `/merchant/orders` 与详情、`GET /merchant/export/{orders,complaints,profit-sharing}.csv`（BOM、截断头、按会话隔离）、补发通知走 `requeue-dead`、小票走 `internal/device` 打印任务。子订单不适用：我们用分账接收方记录替代，没有子订单概念 | 已实现 |
| 资金 | `record.php`、`settle.php`、`apply.php`、`recharge.php`、`deposit.php`、`groupbuy.php` | 资金明细、结算记录、申请提现、余额充值、保证金、用户组购买 | `/merchant/records` 与 `GET /merchant/export/records.csv`、结算记录与申请、`/recharge`、`/deposit`、`/merchant/group-plans` 与 `POST /merchant/group-purchases` | 已实现 |
| 代付与红包 | `transfer.php`、`transfer_add.php`、`transfer_red.php` | 代付管理、新增代付、创建红包 | `/merchant/transfers` 列表与申请、红包批次（金额随机且合计守恒、批次号幂等、超 24 小时撤销解冻）。红包领取待渠道开通，见第 9 节 | 已实现 |
| 收款工具 | `onecode.php`、`test.php` | 聚合收款码、测试支付 | `internal/merchanttools`：`GET /merchant/pay-code` 返回真 QR（`rsc.io/qr` 编码）加支付页 `/pay/code/{type}`；`POST /merchant/test-order` 上限 1 元，未接线回 503 | 已实现 |
| 域名、邀请与公告 | `domain.php`、`invite.php` | 授权支付域名、邀请返现 | `/merchant/domains`、`/merchant/invite` | 已实现 |
| 风控与投诉 | 菜单引用的 `mchrisk.php`、`complain.php` | 商户侧风险提示、投诉记录 | `/merchant/risk-events`；投诉只读 `/merchant/complaints`（页面 `pages/merchant/Complaints.vue`），回复由运营在后台做。这两份上游文件本身不在这份源码里；自动拉取待渠道开通，见第 9 节 | 已实现 |

## 5. 渠道插件（`plugins/`，63 个目录）

| 家族 | 上游目录（按目录名归类，未逐个读实现） | 我们的对应 |
| --- | --- | --- |
| 支付宝系（7） | `alipay`、`alipaycode`、`alipayd`、`alipayg`、`alipayhk`、`alipayrp`、`alipaysl` | `alipay`（当面付扫码） |
| 微信系（5） | `wxpay`、`wxpayn`、`wxpayng`、`wxpaynp`、`wxpaysl` | `wechatpayv3`（Native、JSAPI、H5） |
| QQ 与抖音（2） | `qqpay`、`douyinpay` | `qqpay` |
| 银联与持牌收单（25） | `unionpay`、`allinpay`、`chinaums`、`dinpay`、`fuiou`、`fuiou2`、`fuiou3`、`heepay`、`helipay`、`hlpay`、`hnapay`、`huolian`、`jdpay`、`kuaiqian`、`lakala`、`lakalamoss`、`leshua`、`ltzf`、`sandpay`、`shengpay`、`suixingpay`、`swiftpass`、`swiftpass2`、`umfpay`、`yeepay`、`ysepay` | `unionpay`（全渠道前台交易）、`huifu`（斗拱） |
| 聚合与四方（20） | `adapay`、`baofu`、`duolabao`、`easypay`、`epay`、`epayn`、`fubei`、`haipay`、`huifu`、`jeepay`、`jlpay`、`passpay`、`vmq`、`xorpay`、`xsy`、`xunhupay`、`yinyingtong`、`yseqt`、`zhangyishou`、`zyu` | `epay`（易支付上游）、`jeepay`、`generic`（配置化长尾）、`sandbox`（本地） |
| 海外与加密（3） | `bepusdt`、`paypal`、`stripe` | `usdt`（Epusdt 风格） |

上游 63 个目录里能对上我们的有 9 类报文族（`epay` 对 `epay`/`epayn`，`alipay` 对 `alipay`，`wechatpayv3` 对 `wxpay` 系，`qqpay`、`unionpay`、`huifu`、`jeepay`、`usdt` 对 `bepusdt`，`generic` 是没有专属目录的长尾）。其余 54 个目录没有对应实现，长尾靠 `generic` 的 URL 加参数模板接入，前提是上游报文能用表单或扁平 JSON 表达，并且签名是 MD5 或无签名。`sandbox` 是本地的沙箱插件，不对应上游目录。

## 6. 支付页、公共库与皮肤

| 上游 | 内容 | 我们的实现 | 判定 |
| --- | --- | --- | --- |
| `paypage/` 14 个页面 | 支付结果页、错误页、微信小程序与转账页、红包四件套 | 收银台 SPA 加 `/pay/ok`、`/pay/return` 覆盖结果页与错误页。小程序页与红包分享页不适用：依赖微信环境，本地无法复现，红包只在商户中心与后台按批次创建 | 已实现 |
| `includes/pages/` 28 个页面 | 各通道扫码页、H5 页、JSAPI 页模板 | 不适用：支付页统一由收银台 SPA 渲染，通道只返回动作类型与链接，没有按通道分页的模板 | 不适用 |
| `includes/lib/` 公共库 | `Payment`、`Channel`、`Order`、`Plugin`、`ApiHelper`、`RiskCheck`、`TOTP`、`Printer`、`Ip2Region`、`MsgNotice`、`VerifyCode`、`GeetestLib`、`Oauth`、`QC`、`sms`、`mail`、`ocr`、`ProfitSharing`、`wechat` | 协议四个对应 `signing`、`channel/router`、`payment/service`、`payment/orchestrator`；分账、`TOTP` 与 `VerifyCode`、`sms` 与 `mail`、`Printer`、`Oauth` 都有对应包。`Ip2Region`、`GeetestLib`、`ocr` 不适用：分别要离线 IP 库文件、极验第三方 key、云厂商 OCR 凭据，仓库不内置 | 已实现 |
| `template/` 11 套皮肤 | 官网首页 10 套加后台模板 | `site.skin` 三档主题（`default`、`dark`、`compact`）。其余模板不逐套复刻是按设计选择，不做 11 套皮肤 | 已实现 |
| `gateway.php` 的语音设备 | `voice_devid` 绑定、播报金额 | `internal/device`：`voice_devices` 与 `voice_broadcasts`，支付成功钩子同事务入队播报（如「微信到账 12.34 元」），设备按 `device_no` 加 secret 拉取与 ack；管理端增删改、测试、重置密钥 | 已实现 |

## 7. 协议面与运营面的区别

协议面覆盖完整。V1 的 `submit.php`、`mapi.php`、`api.php?act=*`（含内部 `refundapi` 与系统签名查询）、`submit2.php`、`/pay/{func}/{trade_no}/`（八种页面名）、`getshop.php` 全部有实现，字段、错误码、签名规则、HTTP 永远 200 这些怪癖都对齐，`tests/conformance/epay` 有 63 个测试函数、109 条用例守着，其中一部分真跑 PHP 7.4 与上游 `EpayCore` 客户端 SDK。V2 的 12 个方法齐全，双向签名、时间戳窗口、防重放都有。异步通知与浏览器回跳也补齐了。

运营面覆盖到订单、退款、结算（规则、批量、自动打款）、分账（内部接收方）、代付与红包批次、投诉（含自动规则）、通道与插件、支付方式与按日轮询、公众号配置、商户与用户组费率与套餐购买、认证、域名、风控、账本、消息队列、公告、设置（验证码、TOTP、通知通道、皮肤）、审计、设备、第三方登录、数据清理与导出。代码层面只剩公众号模板消息推送没写；点金计划与企微客服属于不适用，三个待渠道开通的口子单列在第 9 节。

按场景给结论：

| 场景 | 能否直接替换 | 依据 |
| --- | --- | --- |
| 商户只用 `submit.php`、`mapi.php`、`api.php` 加通知与回跳 | 能 | 协议面全覆盖，109 条一致性用例，上游 SDK 实测通过 |
| 商户已接 V2（`/api/{class}/{method}`） | 能 | 12 个方法、签名、时间戳、防重放均实现 |
| 运营靠后台看单、审退款、审结算、管通道 | 能 | 对应页面与接口都在，流程是人工审核 |
| 依赖自动生成结算单与自动打款 | 能 | 规则生成结算单同日幂等，批量审核，通过后自动建代付单 |
| 依赖分账（`ProfitSharing`） | 能，渠道侧见第 9 节 | 内部接收方按万分比同事务走账本；渠道侧直清分账是待渠道项 |
| 依赖支付宝、微信投诉处理 | 能，自动规则已接 | 登记、自动回复、超时关闭、商户只读；拉取是待渠道项 |
| 依赖用户组与费率、商户直清模式、套餐购买 | 能 | `internal/merchantgroup` 加 `internal/groupshop` |
| 依赖语音播报设备、云打印机 | 能 | `internal/device` |
| 依赖第三方登录与扫码登录 | 能，需自备开放平台应用 | `internal/oauth` |
| 依赖 CSV 对账单与导出 | 能 | 后台 7 个加商户 4 个 CSV，带 BOM 与截断提示 |
| 依赖公众号模板消息、点金计划、企微客服 | 不能 | 模板消息推送没写；点金计划与企微客服不适用（要微信服务商与企业微信资质） |

一句话：把一套正在跑的易支付换成本项目，收单链路加运营主链都能切换；不适用项都需要外部资质或属于产品形态选择，待渠道项要等渠道插件接口。

## 8. 还没做的

1. 公众号模板消息推送：`wechat_mp_configs` 能建配置，发模板消息没写，其余通知走短信、邮件与站内。

## 9. 待渠道开通（当前标不适用）

- 渠道侧分账（支付宝、微信直清）：`receiver_type=channel` 的行只记 pending 加原因，要等渠道插件提供分账接口。
- 红包领取：批次能建、能撤销、超时会退回，领取要渠道真实的打款接口。
- 投诉自动拉取：`internal/complaint` 的 `Puller` 是空的，插件侧没有拉取接口，现在靠运营登记加自动规则回复。

## 10. 口径与未核实

- 「工单」与「抽奖」在上游源码里不存在：`工单`、`抽奖`、`lottery` 全文检索无匹配；`ticket` 的命中全部是微信 `jsapi_ticket`。上游确实有红包（`transfer_red.php`、`paypage/red*.php`），已在第 3、4 节列出。
- 上游菜单引用了但这份源码里没有的文件：`complain.php`、`mchrisk.php`（`user/`）、`applyments_channel.php`、`applyments_merchant.php`、`satf_transfer.php`（`admin/`），由 `class_exists('\lib\Applyments\CommUtil')`、`class_exists('\lib\AlipaySATF\AlipaySATF')` 这类判断隐藏。`cron.php` 的 `complain` 分支调用 `\lib\Complain\CommUtil`，该命名空间也不存在，所以投诉是我们自己设计的工作流。
- `plugins/` 的 63 个目录只按目录名归类，没有逐个读实现，单个插件的可用状态与报文细节未核实。
- `includes/lib/ProfitSharing/` 的 16 个文件只读了目录与 `CommUtil` 的调用点；`alipayd`、`wxpaynp` 的「直清结算」只读到调用点。
- 第一批（迁移 `000017` 到 `000019`）的判定依据是读 `internal/merchantgroup`、`internal/settlement`、`internal/profitsharing`、`internal/reporting` 的模型、服务与路由注册。第二批（`000020` 到 `000023`）读的是 `internal/complaint`、`internal/security`、`internal/channelops`、`internal/notify` 与 `channel/router` 的 `RollResolver` 接线。第三批（`000024` 到 `000026`）读的是 `internal/device`、`internal/oauth`、`internal/groupshop` 与 `app.go` 里的支付钩子、30 分钟巡检与皮肤设置。
- 收尾轮的判定依据是读迁移 `000027`、`000028`、`internal/invitecode`、`internal/notifyprefs`、`internal/merchanttools`、`internal/plugin/payment` 的 `CronHook`、`internal/compat/epay/page` 的八种页面名与 `getshop`、`internal/transfer` 的后台付款与批量付款、`internal/reporting` 的商户两份 CSV。本轮我自己跑过 `go test ./... -count=1`：68 个包全部通过（含 `tests/conformance/epay` 约 41 秒、`tests/contracts/plugins` 约 29 秒），0 失败；前端 typecheck 与 build 由负责那部分的人跑过，我没有复跑。
- 第 3、4 节里有的行只读了 `$title` 与菜单文字，没逐页读表单字段（`gedit.php`、`group.php`、`glist.php`、`ustat.php`、`buyerstat.php` 这几个）。
- 「已实现」指路由与页面存在、且有测试或端到端检查覆盖；后台与商户中心的页面没有逐屏点过，判定来自路由清单、服务测试与页面组件清单。
