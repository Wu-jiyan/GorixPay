# 易支付覆盖度对照

审计对象是 `E:\epay-upstream\Epay` 这份源码快照（下称上游）。判定依据只有两样：真的打开读过的上游文件（头部注释、`$title`、主要函数、`switch` 分支），本仓库里对应的路由或包。标记是 `已实现`、`部分实现`、`未实现`，另有一个 `不适用` 用在「产品形态不同、没有缺口」的行上（第 6 节有唯一一例，附理由）。只读了菜单文字与 `$title`、没有逐页读表单字段的行，列在第 9 节。

上游规模：根目录 13 个 PHP 入口，`admin/` 55 个，`user/` 36 个，`plugins/` 63 个渠道目录，`paypage/` 14 个支付页，`includes/pages/` 28 个通道页面，`includes/lib/` 23 个公共库（外加 `api`、`mail`、`ocr`、`ProfitSharing`、`sms`、`wechat` 六个子目录），`template/` 11 套皮肤。

2026-11-14 补了两批运营面功能。第一批是用户组与费率、自动结算与自动打款、内部接收方分账、红包批次、CSV 导出与统计（迁移 `000017` 到 `000019`）。第二批是投诉处理、后台安全（图形验证码与 TOTP）、支付方式与按日轮询、通知通道与找回密码（迁移 `000020` 到 `000023`）。两批都有服务包、HTTP 接口与测试。投诉的自动拉取、渠道侧分账、红包领取仍是 pending，第 2、3、4、6 节按现状标注。

结论先给：协议面（V1 六个入口、V2 十二个方法、异步通知、浏览器回跳）是 100% 覆盖。运营面在两批之后补齐了结算、分账、用户组、导出、投诉、安全、支付方式、通知与找回密码。剩下的是第三方登录、语音设备与打印机、官网皮肤、`gateway.php`/`gold.php`/`wework.php`、数据清理与测试支付页、用户组付费购买、红包领取、渠道侧分账、投诉自动拉取。第 7 节给分场景结论。

## 1. 对外 HTTP 入口

| 上游入口 | 上游做什么 | 我们的实现 | 判定 |
| --- | --- | --- | --- |
| `/submit.php` | 页面跳转下单，GET 有 `pid` 时整组读 GET（`includes/lib/api/Pay.php:8-261`） | `internal/compat/epay/payment/handler.go` 的 `submit` 与 `selectSubmitParameters` | 已实现 |
| `/mapi.php` | API 下单，成功回扁平 `payurl`/`qrcode`/`urlscheme`/`html`（`Payment.php:246-296`） | 同文件 `mapi` | 已实现 |
| `/api.php?act=query\|order\|orders\|refund\|refundapi\|settle` | 账户、单笔、列表、退款、内部退款、结算 | `internal/compat/epay/payment/api.go` | 已实现 |
| `/api/{class}/{method}`（`api.php?s=` 重写） | V2 分发，`pay`/`merchant`/`transfer` 三组 | `internal/compat/epay/v2/handler.go`，12 个方法 | 已实现 |
| `/submit2.php` | 失败重试换通道页，已完成订单渲染 sysmsg | `internal/compat/epay/page/page.go` 的 `submit2` | 已实现 |
| `/pay/{func}/{trade_no}/` | `Plugin::loadForPay` 按 `func` 渲染通道私有支付页（扫码页、H5、JSAPI、小程序、红包） | 只实现 `submit`、`checkpay`、`return`、`ok` 四个 func，支付动作交给收银台 SPA | 部分实现 |
| `/cashier.php` | 收银台选支付方式 | `frontend/src/pages/cashier/`，前端路由 `/cashier.php` | 已实现 |
| `/gateway.php` | 语音播报设备与扫码枪的 JSON 网关（`heartbeat`、`qrcode` 付款码下单） | 无 | 未实现 |
| `/getshop.php` | 收银台轮询付款结果，`act=captcha_verify` 换安全验证密钥 | `/api/v1/cashier/orders/:trade_no/status` 覆盖轮询；无 `captcha_verify` | 部分实现 |
| `/gold.php` | 微信点金计划支付结果 iframe | 无 | 未实现 |
| `/wework.php` | 企业微信客服回调（`kf_msg_or_event`），驱动 `weixin://biz/ww/kefu/` 跳转支付 | 无 | 未实现 |
| `/index.php` | 官网首页、`?mod=` 模板页、`?doc=` 文档页、`?invite=` 邀请绑定 | SPA 官网与 `/doc` 文档页；邀请码走注册参数 | 部分实现 |
| `/{mod}.html`、`/doc/{name}.html` | nginx 重写到 `index.php` 的模板页与文档页 | 没有 `.html` 路由，只有 SPA 路由 | 部分实现 |
| `/config.php` | 数据库连接配置 | YAML、`GORIXPAY_*` 环境变量、启动参数（`internal/platform/config/config.go`） | 已实现（形态不同） |

## 2. `cron.php` 定时任务

上游 `cron.php` 靠 `?do=` 分派，入口校验 `cronkey`。我们的后台循环有两个：Outbox worker 负责通知收发，结算巡检每分钟跑一次规则。

| `do=` | 上游做什么 | 我们的实现 | 判定 |
| --- | --- | --- | --- |
| `notify` | 失败通知按 1、3、20 分钟等间隔重发，每轮 20 条（`cron.php:192-245`） | `internal/outbox` 的 worker，租约加 CAS，按幂次退避 | 已实现 |
| `notify2` | 手工重发 `notify=-1` 的失败通知 | `POST /api/v1/admin/outbox/requeue-dead` | 已实现（入口不同） |
| `settle` | 按余额门槛、结算费率、实名强制条件自动生成结算列表（`cron.php:19-66`） | `internal/settlement`：每日按规则生成结算单（同日幂等，缺结算资料跳过），后台每分钟巡检；规则里带最低金额、费率、周期 | 已实现 |
| `order` | 订单统计与清理、余额不足提醒、邀请返利结算（`cron.php:67-191`） | 返利在支付回调同事务入账；无余额提醒与清理任务 | 部分实现 |
| `profitsharing` | 分账任务与支付宝直清结算任务（`\lib\ProfitSharing\CommUtil::task()`、`Payment::settle_task`） | `internal/profitsharing` 在支付回调里同步给内部接收方分账；渠道侧分账与支付宝直清结算任务未接 | 已实现 |
| `check` | 异步风控检测（`\lib\RiskCheck::execute()`） | 风控在下单链路同步执行（`internal/risk`），无巡检任务 | 部分实现 |
| `complain` | 拉取支付宝、微信投诉（`cron.php:270-319`） | `internal/complaint`：`/admin/complaints` 登记、回复、关闭，状态 `pending` 到 `replied` 到 `closed`，超期在读取时派生 `overdue`，商户端 `/merchant/complaints` 只读自己的。自动拉取未接，`Puller` 接口留着，插件侧没有实现 | 部分实现 |
| `complain_complete` | 投诉自动回复处理（`cron.php:320-339`） | 手工回复与关闭已通；超期自动处理与模板回复没有 | 部分实现 |
| `plugin` | 调用通道插件的 `_cron` 钩子（`cron.php:340-349`） | 无插件定时钩子 | 未实现 |
| `transfer` | 自动结算转账（支付宝转账接口，含收款方失败码分类，`cron.php:350-401`） | `POST /admin/settlements/:id/auto-transfer` 与批量审核在通过后自动建代付单（`internal/transfer`），缺结算资料或建单失败记 `failed` 加原因；没有直连支付宝转账接口 | 已实现 |

## 3. 运营后台（`admin/`，55 个 PHP）

上游后台是 Bootstrap 多页应用，功能按 `admin/head.php` 的菜单分组。我们的后台是 Vue SPA（`frontend/src/pages/admin/`，27 个页面，第一批新增 `Groups`、`SettleRules`、`ProfitSharing`、`Reports`，第二批新增 `Complaints`、`PayTypes`、`PayRolls`、`WechatMp`、`NotificationChannels`、`Security`）加 `/api/v1/admin/*` 接口。

| 分组 | 上游文件 | 上游能力 | 我们的实现 | 判定 |
| --- | --- | --- | --- | --- |
| 登录与会话 | `login.php`、`code.php`、`log.php`、`sso.php`、`index.php`、`head.php` | 账号密码登录、图形验证码、登录日志、一键登录商户、后台首页 | 邮箱登录加会话（`/api/v1/auth/administrator/login`）、SVG 图形验证码（`GET /api/v1/captcha`，只存哈希、一次性）、TOTP 两步验证（`/admin/security/totp/*`，密钥 AES-GCM 加密）、登录尝试审计（`/admin/security/login-attempts`）；无一键登录商户（`sso.php`） | 部分实现 |
| 订单 | `order.php`、`export.php`、`buyerstat.php`、`blacklist.php`、`ajax_order.php` | 订单列表、导出、支付用户统计、支付黑名单 | `/admin/orders`、`/admin/orders/:id`、关单；`GET /admin/export/orders.csv`；`GET /admin/stats/buyers`；`/admin/blacklist`（`internal/reporting`、`internal/risk`） | 已实现 |
| 分账 | `ps_receiver.php`、`ps_order.php`、`ajax_profitsharing.php` | 分账规则与分账记录 | `internal/profitsharing`：`/admin/profit-sharing/receivers` 增删改查、`/admin/profit-sharing/orders`、`/orders/:id/retry`；按 `rate_bp` 分账、手续费先扣、向下取整到分，内部接收方同事务走账本，幂等键 `split:{orderID}:{receiverID}`，费率合计超 100% 拒绝。渠道侧分账未接，`receiver_type=channel` 的行记 `pending` 加原因 | 已实现 |
| 结算 | `slist.php`、`settle.php`、`settle_batch.php`、`ajax_settle.php` | 结算列表、批量结算、批量转账（支付宝、微信两条通道） | `internal/settlement`：`/admin/settle-rules` 增删改查与手动触发、`/admin/settlements/batch-process`、`/admin/settlements/:id/auto-transfer`，后台每分钟巡检；按规则生成结算单同日幂等，审核通过后自动建代付单，缺结算资料记 `failed` 并写原因 | 已实现 |
| 付款与红包 | `transfer.php`、`transfer_add.php`、`transfer_red.php`、`transfer_batch.php`、`transfer_export.php`、`transfer_stat.php`、`ajax_transfer.php` | 付款记录、新增付款、创建红包、批量付款、导出、付款统计 | `/admin/transfers` 列表与审核、`/admin/transfers/red` 红包批次、`GET /admin/export/transfers.csv`；后台手动新增付款与批量付款没有入口，红包领取未接渠道（`claimed_count` 恒 0） | 部分实现 |
| 商户 | `ulist.php`、`uset.php`、`ustat.php`、`record.php`、`record_export.php`、`ajax_user.php` | 商户列表与详情、商户支付统计、资金明细与导出 | `/admin/merchants`、`/merchants/:id`、改状态、`/admin/stats/merchants`、`/admin/ledger/*`、`GET /admin/export/merchants.csv` 与 `/admin/export/ledger.csv` | 已实现 |
| 用户组 | `glist.php`、`gedit.php`、`group.php` | 用户组设置、编辑、购买套餐 | `internal/merchantgroup`：`/admin/groups` 增删改查与启停、`PATCH /admin/merchants/:id/group` 归组；组内 `rate_bp`、可用支付方式、`settle_mode`（normal/direct）。上游 `group.php` 的付费套餐购买未做，见第 8 节 | 已实现 |
| 邀请码 | `invitecode.php` | 注册邀请码管理 | 邀请返佣与邀请记录（`/admin/invites`）；无注册邀请码 | 部分实现 |
| 授权域名 | `domain.php` | 商户支付域名审核 | `/admin/domains` 加审核（`internal/domainauth`） | 已实现 |
| 支付接口 | `pay_channel.php`、`pay_type.php`、`pay_plugin.php`、`pay_roll.php`、`pay_weixin.php`、`pay_wework.php`、`plugin_page.php`、`ajax_pay.php` | 通道、支付方式、插件、轮询、公众号与小程序配置、微信客服 | `internal/channelops`：`/admin/channels`（含权重、优先级）、`/admin/plugins`、`/channels/:id/test`、`/admin/pay-types` 与公开的 `/api/v1/public/pay-types`、`/admin/pay-rolls`（按日权重已接进渠道选择，当天有记录就替换权重，权重 0 当天不参与）、`/admin/wechat-mp-configs`（密钥加密）；`site.payment_types` 也读这张表。企业微信客服支付配置（`pay_wework.php`）未做 | 部分实现 |
| 系统设置 | `set.php`（19 个 `mod`）、`set_totp.php`、`set_wxkf.php`、`gonggao.php` | 站点、支付、风控、结算、转账、认证、登录、通知、邮件短信、模板、计划任务、代理、上传等配置；后台两步验证；H5 客服支付 | `GET/PUT /api/v1/admin/settings/:key`（白名单键）、公告 CRUD、通知通道 CRUD 与测试发送（`internal/notify`：`/admin/notification-channels`）、TOTP 与验证码设置；无模板、快捷登录（oauth）、中转代理、计划任务配置 | 部分实现 |
| 报表与导出 | `download.php`、`export.php`、`record_export.php`、`transfer_export.php`、`ustat.php`、`buyerstat.php` | CSV 导出与统计报表 | `internal/reporting`：9 个 CSV 接口（后台的 `orders`、`refunds`、`settlements`、`transfers`、`merchants`、`ledger`、`buyers`，商户的 `orders`、`records`，带 BOM 与截断提示）、`/admin/stats/buyers`、`/admin/stats/merchants`、`/merchant/stats/daily` | 已实现 |
| 安全与运维 | `risk.php`、`clean.php`、`testsubmit.php`、`gettoken.php`、`loader_helper.php` | 风控记录、数据清理、测试支付、取用户标识、插件加载辅助 | `/admin/risk-events`、`/admin/outbox/*`、`/admin/audit-events`、登录尝试审计 `/admin/security/login-attempts`；无数据清理、无测试支付页、无 `gettoken` | 部分实现 |

## 4. 商户中心（`user/`，36 个 PHP）

| 分组 | 上游文件 | 上游能力 | 我们的实现 | 判定 |
| --- | --- | --- | --- | --- |
| 登录与注册 | `login.php`、`reg.php`、`findpwd.php`、`oauth.php`、`connect.php`、`wxlogin.php`、`qrlogin.php`、`douyinoauth.php`、`openid.php`、`head.php`、`foot.php` | 账号密码、找回密码、QQ 与微信登录、抖音授权、扫码登录、取 openid | `/api/v1/auth/register`、`/login`；找回密码 `POST /api/v1/merchant/password/forgot` 与 `/reset`（验证码 60 秒一次、每天 10 次，一次性 token，重置后作废该商户全部会话，改密走 Argon2id）；验证码 `POST /api/v1/code`；无第三方登录（QQ、微信、抖音、扫码）、无 openid 页 | 部分实现 |
| 账户资料 | `index.php`、`completeinfo.php`、`editinfo.php`、`userinfo.php`、`ajax.php`、`ajax2.php` | 首页概览、完善资料、账户与接口信息、密钥类型、重置密钥、RSA 生成、结算资料、通知配置、绑定 | `/merchant/dashboard`、`/profile`、`/credentials`、`md5/rotate`、`v2/rotate`；结算资料在 profile；短信验证码走 `/api/v1/code`；无通知偏好配置 | 部分实现 |
| 实名认证 | `certificate.php`、`certificate_mobile.php`、`alipaycert.php`、`alipaycertok.php` | 提交实名资料，接支付宝、阿里云、腾讯云 OCR 与人脸核验 | `/merchant/certification` 提交加运营审核（`internal/certification`）；无自动 OCR 与人脸核验 | 部分实现 |
| 订单 | `order.php`、`download.php`、`ajax2.php` 的 `order`/`subOrders`/`notify`/`printOrder` | 订单列表与详情、补发通知、小票打印；`download.php` 导出三份 CSV（`order_*.csv`、`complain_*.csv`、`psorder_*.csv`） | `/merchant/orders`、`/orders/:id`、`GET /merchant/export/orders.csv`；补发通知走管理端 `requeue-dead`；投诉与分账的两份 CSV 没有（对应功能未接）、无子订单、无打印 | 部分实现 |
| 资金 | `record.php`、`settle.php`、`apply.php`、`recharge.php`、`deposit.php`、`groupbuy.php` | 资金明细、结算记录、申请提现、余额充值、保证金、用户组购买 | `/merchant/records`（`GET /merchant/records`、`GET /merchant/export/records.csv`）、`/merchant/settlements`（含申请）、`/recharge`、`/deposit`；用户组付费购买未做 | 部分实现 |
| 代付与红包 | `transfer.php`、`transfer_add.php`、`transfer_red.php` | 代付管理、新增代付、创建红包 | `/merchant/transfers` 列表与申请、`/merchant/transfers/red` 红包批次（`transfer.CreateRed`：按批次建 N 个 `kind=red` 代付单，金额随机且合计守恒，批次号幂等）；领取未接渠道，`claimed_count` 恒 0 | 已实现 |
| 收款工具 | `onecode.php`、`test.php` | 聚合收款码、测试支付 | 无聚合收款码；测试支付用本地 sandbox 渠道替代 | 部分实现 |
| 域名、邀请与公告 | `domain.php`、`invite.php` | 授权支付域名、邀请返现 | `/merchant/domains`、`/merchant/invite` | 已实现 |
| 风控与投诉 | 菜单引用的 `mchrisk.php`、`complain.php` | 商户侧风险提示、投诉记录 | `/merchant/risk-events`；投诉只读 `/merchant/complaints` 与 `/merchant/complaints/:id`（`internal/complaint`），回复由运营在后台做；这两份上游文件本身不在这份源码里 | 部分实现 |

## 5. 渠道插件（`plugins/`，63 个目录）

| 家族 | 上游目录（按目录名归类，未逐个读实现） | 我们的对应 |
| --- | --- | --- |
| 支付宝系（7） | `alipay`、`alipaycode`、`alipayd`、`alipayg`、`alipayhk`、`alipayrp`、`alipaysl` | `alipay`（当面付扫码） |
| 微信系（5） | `wxpay`、`wxpayn`、`wxpayng`、`wxpaynp`、`wxpaysl` | `wechatpayv3`（Native、JSAPI、H5） |
| QQ 与抖音（2） | `qqpay`、`douyinpay` | `qqpay` |
| 银联与持牌收单（25） | `unionpay`、`allinpay`、`chinaums`、`dinpay`、`fuiou`、`fuiou2`、`fuiou3`、`heepay`、`helipay`、`hlpay`、`hnapay`、`huolian`、`jdpay`、`kuaiqian`、`lakala`、`lakalamoss`、`leshua`、`ltzf`、`sandpay`、`shengpay`、`suixingpay`、`swiftpass`、`swiftpass2`、`umfpay`、`yeepay`、`ysepay` | `unionpay`（全渠道前台交易）、`huifu`（斗拱） |
| 聚合与四方（20） | `adapay`、`baofu`、`duolabao`、`easypay`、`epay`、`epayn`、`fubei`、`haipay`、`huifu`、`jeepay`、`jlpay`、`passpay`、`vmq`、`xorpay`、`xsy`、`xunhupay`、`yinyingtong`、`yseqt`、`zhangyishou`、`zyu` | `epay`（易支付上游）、`jeepay`、`generic`（配置化长尾）、`sandbox`（本地） |
| 海外与加密（3） | `bepusdt`、`paypal`、`stripe` | `usdt`（Epusdt 风格） |

上游 63 个目录里能对上我们的有 9 类报文族（`epay` 对 `epay`/`epayn`，`alipay` 对 `alipay`，`wechatpayv3` 对 `wxpay` 系，`qqpay`、`unionpay`、`huifu`、`jeepay`、`usdt` 对 `bepusdt`，`generic` 是没有专属目录的长尾）。其余 54 个目录没有对应实现。`sandbox` 是本地的沙箱插件，不对应上游目录。长尾靠 `generic` 的 URL 加参数模板接入，前提是上游报文能用表单或扁平 JSON 表达，并且签名是 MD5 或无签名。

## 6. 支付页、公共库与皮肤

| 上游 | 内容 | 我们的实现 | 判定 |
| --- | --- | --- | --- |
| `paypage/` 14 个页面 | 支付结果页、错误页、微信小程序与转账页、红包四件套 | 收银台 SPA 加 `/pay/ok`、`/pay/return`；红包只有商户中心与后台的批次创建，没有支付页那一套 | 部分实现 |
| `includes/pages/` 28 个页面 | 各通道扫码页、H5 页、JSAPI 页模板 | 不适用：支付页统一由收银台 SPA 渲染，通道只返回动作类型与链接，没有按通道分页的模板 | 不适用 |
| `includes/lib/` 公共库 | `Payment`、`Channel`、`Order`、`Plugin`、`ApiHelper`、`RiskCheck`、`TOTP`、`Printer`、`Ip2Region`、`MsgNotice`、`VerifyCode`、`GeetestLib`、`Oauth`、`QC`、`sms`、`mail`、`ocr`、`ProfitSharing`、`wechat` | 协议相关的四个对应 `signing`、`channel/router`、`payment/service`、`payment/orchestrator`；分账对应 `internal/profitsharing`；`TOTP` 与 `VerifyCode` 对应 `internal/security`（自实现 RFC 6238，密钥加密存 `admin_totp`；SVG 验证码只存哈希、一次性）；`sms`、`mail` 对应 `internal/notify`（内置 logSender，生产要自备网关）；`Printer`、`Ip2Region`、`GeetestLib`、`Oauth`、`ocr` 仍无 | 部分实现 |
| `template/` 11 套皮肤 | 官网首页 10 套加后台模板 | 单一 Vue 官网，无皮肤切换 | 未实现 |
| `gateway.php` 的语音设备 | `voice_devid` 绑定、播报金额 | 无 | 未实现 |

## 7. 协议面与运营面的区别

协议面覆盖完整。V1 的 `submit.php`、`mapi.php`、`api.php?act=*`（含内部 `refundapi` 与系统签名查询）、`submit2.php`、`/pay/{submit,checkpay,return,ok}/{trade_no}/` 全部有实现，字段、错误码、签名规则、HTTP 永远 200 这些怪癖都对齐，`tests/conformance/epay` 有 63 个测试函数、109 条用例守着，其中一部分真跑 PHP 7.4 与上游 `EpayCore` 客户端 SDK。V2 的 12 个方法齐全，双向签名、时间戳窗口、防重放都有。异步通知与浏览器回跳也补齐了。

运营面在 2026-11-14 的两批之后补齐了主链路。后台页面从 Bootstrap 多页换成 Vue SPA，能对上的是订单、退款、结算（规则、批量、自动打款）、分账（内部接收方）、代付与红包批次、投诉、通道与插件、支付方式与按日轮询、公众号配置、商户与用户组费率、认证、域名、风控、账本、消息队列、公告、设置（含验证码、TOTP、通知通道）、审计、CSV 导出与统计。还缺的是第三方登录、语音设备与云打印机、官网皮肤、`gateway.php`/`gold.php`/`wework.php`、数据清理、测试支付页、`gettoken`、用户组付费购买。投诉自动拉取、渠道侧分账、红包领取三处是 pending，接口与字段留好了位置，原因写在返回里。

按场景给结论：

| 场景 | 能否直接替换 | 依据 |
| --- | --- | --- |
| 商户只用 `submit.php`、`mapi.php`、`api.php` 加通知与回跳 | 能 | 协议面全覆盖，109 条一致性用例，上游 SDK 实测通过 |
| 商户已接 V2（`/api/{class}/{method}`） | 能 | 12 个方法、签名、时间戳、防重放均实现 |
| 运营靠后台看单、审退款、审结算、管通道 | 能 | 对应页面与接口都在，流程是人工审核 |
| 依赖自动生成结算单与自动打款 | 能 | `internal/settlement`：规则生成结算单同日幂等，批量审核，通过后自动建代付单，缺结算资料记 `failed` 并写原因 |
| 依赖分账（`ProfitSharing`） | 能 | 内部接收方按万分比同事务走账本，幂等键 `split:{orderID}:{receiverID}`；渠道侧直清分账仍是 pending，`receiver_type=channel` 的行记 pending |
| 依赖支付宝、微信投诉处理 | 能 | 运营端登记、回复、关闭，状态机 `pending` 到 `replied` 到 `closed`，超期读取时派生 `overdue`，商户端只读；自动拉取未接，`Puller` 接口留着 |
| 依赖用户组与费率、商户直清模式 | 能 | `internal/merchantgroup`：`rate_bp`、组内可用支付方式、`settle_mode` 的 normal/direct、商户归组；付费购买套餐未做 |
| 依赖语音播报设备、云打印机、公众号模板消息 | 不能 | 这三块都没有实现 |
| 依赖 CSV 对账单与导出 | 能 | 9 个 CSV 接口，带 BOM 与截断提示；统计有买家、商户、每日三类 |

一句话：把一套正在跑的易支付换成本项目，收单链路加上订单、退款、结算、分账、投诉、通知、导出这条运营主链可以切换；第三方登录、设备类功能与三处插件侧能力（投诉自动拉取、渠道侧分账、红包领取）仍需补齐。

## 8. 如果要补齐，按重要性排序

1. 渠道侧分账接入（支付宝、微信直清）：现在 `receiver_type=channel` 的行只记 pending 加原因，钱留在商户余额里。
2. 红包领取接入渠道：现在只有建单、冻结与审核，`claimed_count` 恒 0，领不了。
3. 投诉自动拉取：手工登记与回复已经能跑，插件侧还没有拉取接口，`internal/complaint` 里的 `Puller` 是空的。
4. 用户组付费购买（`admin/group.php`、`user/groupbuy.php`）：现在只有组配置与费率，没有按组收费的购买流程。
5. 第三方登录（`user/oauth.php`、`connect.php`、`wxlogin.php`、`qrlogin.php`、`douyinoauth.php`、`openid.php`）：QQ、微信、抖音、扫码登录都没有，只有邮箱密码。
6. 语音设备网关与云打印机（`gateway.php`、`lib\Printer`）：线下扫码枪与语音播报场景。
7. 微信生态页面（`gold.php` 点金计划、`wework.php` 企业微信客服回调）：都没有实现。
8. 后台运维小件（`clean.php`、`testsubmit.php`、`gettoken.php`）：数据清理、测试支付页、取用户标识。
9. 官网皮肤切换（`template/` 11 套）：现在只有一套 Vue 官网，没有皮肤配置。

## 9. 口径与未核实

- 「工单」与「抽奖」在上游源码里不存在：`工单`、`抽奖`、`lottery` 全文检索无匹配；`ticket` 的命中全部是微信 `jsapi_ticket`，与工单无关。上游确实有红包（`transfer_red.php`、`paypage/red*.php`），已在第 3、4 节列出。
- 上游菜单引用了但这份源码里没有的文件：`complain.php`、`mchrisk.php`（`user/`）、`applyments_channel.php`、`applyments_merchant.php`、`satf_transfer.php`（`admin/`）。它们由 `class_exists('\lib\Applyments\CommUtil')`、`class_exists('\lib\AlipaySATF\AlipaySATF')` 这类判断隐藏，属于未随包发布的扩展。同理 `cron.php` 的 `complain` 分支调用 `\lib\Complain\CommUtil`，该命名空间在此份源码中不存在。
- `plugins/` 的 63 个目录只按目录名归类，没有逐个读实现，单个插件的可用状态与报文细节未核实。
- `includes/lib/ProfitSharing/` 的 16 个文件只读了目录与 `CommUtil` 的调用点。我们自己的分账在 `internal/profitsharing`，标「已实现」指的是内部接收方这一半，渠道侧直清分账没有接。
- `alipayd`、`wxpaynp` 的「直清结算」（`Payment::settle_task`）只读到调用点，未读插件侧的 settle 实现。
- 2026-11-14 第一批的判定依据是读迁移 `000017` 到 `000019`、`internal/merchantgroup`、`internal/settlement`、`internal/profitsharing`、`internal/reporting` 的模型、服务与路由注册，加上新页面组件清单。前端的 22 个后台页与 13 个商户页没有逐屏点过，也没做多副本并发的实测（结算的当日幂等靠 `settle_rules.last_run_at` 领取加进程内重入保护，我只读了代码与注释）。
- 2026-11-14 第二批的判定依据是读迁移 `000020` 到 `000023`、`internal/complaint`、`internal/security`、`internal/channelops`、`internal/notify` 的模型、服务、路由与测试，以及 `internal/channel/router/router.go` 里 `RollResolver` 的接线。没有在浏览器里走完整的「验证码加重试加 TOTP」登录流程，短信与邮件也只有内置的 logSender 可测（真实网关要自备）。
- 第 3、4 节里有的行只读了 `$title` 与菜单文字，没逐页读表单字段（`gedit.php`、`group.php`、`glist.php`、`ustat.php`、`buyerstat.php` 这几个）。
- 我们这一侧的「已实现」指路由与页面存在、且协议面有测试覆盖；后台与商户中心的功能没有逐条端到端用例，判定来自路由清单、服务测试与页面组件清单。
