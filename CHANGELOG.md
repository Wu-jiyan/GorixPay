# 变更记录

本文件按模块记录仓库相对上一版的变更。写法参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，分类换成了模块，因为这个仓库的改动基本是一批跨层的交付，按 Added/Changed 切会把同一次交付拆散。

`v2.0.0` 于 2026-11-13 打上第一个 tag，下面是这个版本的全部内容。之前的两个 commit 是内部开发阶段，没有对外发过。

## [2.3.0] - 2026-11-22

一轮「设置项完整性 + 买家支付页 + 后台暴露面」的收口。两轮只读审计（系统设置完整性、后台入口暴露）把上游 Epay 的 116 个配置键与本仓的 35 个逐条比对，缺什么补什么，存了没人读的要么接线要么删掉。

### 系统设置
- **注册门禁三态** `register.mode`：开启 / 关闭 / 仅邀请（上游 `reg_open`）。关闭时注册接口回 `register_closed`，仅邀请时无邀请码回 `invite_required`，注册页据此隐藏表单而不是让人填完才被拒。
- **首页显示模式** `site.homepage_mode` + `site.homepage_url`：正常首页 / 直接跳登录页 / 跳指定网址（上游 `homepage`）。站点信息异步拉取，跳转放在挂载后，不会闪一下首页再走。
- **注册审核流** `register.review_required`：新商户落 `pending`，登录回 403 `merchant_pending`（与「密码错误」区分），运营在商户列表点「启用」即通过。实测全链路：注册 → pending → 登录被拦 → PATCH active → 登录成功。
- **注册验证方式** `register.verify_channel`：off / email / phone（上游 `verifytype`）。复用现有验证码服务与限流（每目标 1 分钟 1 次、每天 10 次），新增 `register` 场景；注册页出现发送按钮与 60 秒冷却。
- **授权站点域名拆成两个开关**：`payment.domain_whitelist_required`（不做就拦）与新的 `payment.domain_self_service`（商户能否自助报备）。上游本来就是 `pay_domain_forbid` / `pay_domain_open` 两个键，之前压成了一个布尔。
- **单笔金额上下限** `payment.min_amount_cents` / `max_amount_cents`：真正在下单链路校验，不再硬编码 1 分 ~ 10 万元。
- **商户自助退款开关** `payment.merchant_refund_enabled`：关掉后 `/merchant/refunds` 回 `refund_disabled`，运营仍可退。
- **风控 IP 白名单** `risk.ip_whitelist`：逗号或换行分隔，支持 CIDR；命中的来源不计数、不受日限额约束。
- **联系方式与备案号** `site.contact_qq` / `contact_email` / `icp_number`（上游 `kfqq` / `email` / `footer`）。
- **设置写入的范围校验**：新增 `settingRules`，枚举键只能取固定值、数值键有上下限且要求整数，越界回 400 `invalid_setting_value`。此前 `invite.rate_bp` 能写 50000（500% 返佣）、`site.skin` 能写任意串、风控限额能写负数并静默变成「不限」。
- **设置改动落审计**：每次写设置记一条 `setting.updated`（key、值、IP、操作人），凭据类键仍不记明文。
- **订单有效期真正读配置**：`payment.order_expiry_minutes` 之前没被读，所有订单硬编码 5 分钟到期，买家选通道、跳 App、输密码稍慢就撞「订单已过期」。现在按设置走（0 表示不过期），收银台对零值跳过过期判断。
- **通知重试参数接线**：`notification.max_attempts` / `poll_interval_seconds` 之前传的是空 `Options`，永远是编译期默认值（5 次 / 1 秒）。
- 新增 `internal/settings.Reader`：注册门禁、首页模式、注册验证、金额上下限、退款开关、通知重试都走同一个只读入口，改完当次请求生效。

### 安全
- **设置项造成的 SSRF 面**：运营能在控制台填任意 SMTP 主机与短信网关地址，这两条出站此前完全没护栏 —— `smtp.SendMail` 直接拨 host:port（填 `127.0.0.1` 就能打本机 25/465/587），短信网关直接 POST 到运营填的 URL。现在都过 netguard：新增 `ValidateHostPort`（裸 host:port 规则）与导出的 `Control()` 拨号钩子（挡住「域名解析到 169.254.169.254」），SMTP 换成自带 `sendMailWithDialer` 保留 STARTTLS/AUTH。
- 后台入口不再对访客暴露：官网页脚去掉「运营入口」按钮、移动端抽屉去掉 `/admin/login` 明文、商户登录页去掉「运营登录」链接、官网 FAQ 的「运营后台」改为「平台侧」。后台地址只留给知道 URL 的人。

### 买家支付页
- **手机支付宝能真正唤起 App**：href 由通道 H5 地址改为 `alipays://platformapi/startApp?appId=20000067&url=…`，之前点了只是跳到汇付网页。
- **电脑端支付宝不再报「当前订单支付方式不支持」**：PC 上不再 pin `A_NATIVE`/`U_NATIVE` —— 这两个是汇付托管的产品级开关，商户没开就会回这个错；微信一直正常正是因为它从没 pin 过 `T_NATIVE`。现在只有「微信内 + 微信支付」pin `T_JSAPI`、「支付宝内 + 支付宝」pin `A_JSAPI`。
- **去掉「返回商户」按钮**：未支付时它指向 `checkpay`，而 `checkpay` 又会把人送回支付页，点一下等于原地闪跳。
- 页面加厚：品牌色状态条、支付方式徽标 + 待支付 chip、操作区浅底框、勾选式信任说明、订单状态行；支付链接截断展示 + 复制按钮取 hidden 全量值。
- **版面改成「摘要 + 操作」双栏**（参考正规收银台的结构，不借它的配色）：桌面左栏说清这是什么钱（支付方式、收款方、金额、信任说明、客服），右栏说清怎么付（扫码 / 唤起 App、支付链接、明细两列、主按钮）；手机仍单列，右栏标题自动隐藏避免与按钮文字撞车。明细从纵向列表改为两列并排，手机回落单列。
- **新增「我已完成支付」按钮**：先查一次状态，已付才跳转，没付只提示「暂未收到付款」。不直接跳 `checkpay`，否则又变成那个点了会闪回原页的按钮。
- **新增客服块**：只在运营填了 `site.contact_qq` / `site.contact_email` 时出现，不编造「客服在线 08:00-22:00」这类没有数据支撑的文案。
- 手机微信保留「复制链接，去微信付款」主操作与「打开微信」次按钮（`weixin://`）。
- 测试支付页提交后直接跳到服务端支付页，与易支付 `test.php` 一致，不再在本页二次解析二维码。

### 界面一致性
- 容器内边距修正 16 处：首页 FAQ（`4px 8px` → `12px 24px 20px`）、官网页脚（`20px 0 24px` → `28px 0 32px`）、后台与商户中心窄屏内容区（12px → `var(--gp-gutter)`）、五个登录注册页、文档页、审计页展开行、安全页二维码区、收银台二维码静区（6px → 10px，影响扫码识别率）、商户首页公告列表左基线。
- 60 张页面级容器卡去掉 `size="small"`，与同页 medium 卡左基线对齐；42 张统计小卡/纯文字卡保留 small。
- 22 处区块间距从内联 `margin-top: 16px` 换成 `.gp-section-gap`；11 处元素内部间距刻意跳过，避免与弹窗自身节奏打架。

### 工程
- CI 增加 `go mod tidy` 漂移检查、`go test -race ./internal/...` 独立 job、前端类型检查独立步骤。`-race` 第一次跑就抓出一条错误断言（整改见下）与内存库复用问题。
- `internal/transfer` 的两处测试修正：`approval_race_test.go` 的 approve 分支原本断言 available 3000，等于要求「冻结既退回又消耗」（商户凭空少 1000），实际正确值是 4000；这条断言一直被 reject 先抢到状态掩盖着。`transferFixture` 的内存 SQLite 改成每轮唯一名，`go test -count=N` 不再撞主键。
- 新增 `.github/workflows/release.yml`：打 tag 构建 linux/amd64 与 arm64 二进制 + 前端产物，用 `gh release create` 发布并附 sha256（校验文件写纯文件名，`sha256sum -c` 开箱可用）；release notes 直接取本文件的版本段落；支持 `workflow_dispatch` 重跑已存在的 tag（覆盖资产与说明，不重复创建）。
- `/health/live` 与 `/health/ready` 返回 `version` / `commit` / `built_at`，由 link time 注入，可在生产直接确认跑的是哪个 tag。
## [2.2.2] - 2026-11-14

把 2.2.1 结尾列的七条已知缺口一次收干净。

### 凭据类设置

- **运营后台填的第三方密钥不再明文入库**：`oauth.*.app_secret`、`notify.smtp_password`、`notify.sms_token` 这类键写入前用主密钥加密（AEAD 附加数据绑定到具体键，密文挪到别的键解不开），列表接口只返回「已配置/未配置」，不回显任何值。没有主密钥的部署写凭据直接 `409 cipher_missing`，不再静默存明文。
- **读取端补齐**：`internal/settings` 统一了「JSON 包裹 + 可选加密」的读取规则，OAuth 的 `credentials`、SMTP/短信发送器都改用它。此前 OAuth 拿到的是带引号的原始字符串，所以「后台填了密钥」和「OAuth 能用」之间是断的；现在填完就能拿到 302 跳转。历史明文值继续可用（读的时候按明文处理），旧的 `oauth:<provider>` 附加数据密文也保留回退。
- 空值写入表示「不修改」，因为页面永远拿不到明文，输入框留空不能当成清空。

### 商户端

- 商户中心补 `POST /api/v1/merchant/transfers/red/{batch_no}/cancel`：只能撤自己的批次（别人的批次与不存在的批次都回 404，不泄漏存在性），已审核过的批次仍按冲突拒绝。
- 商户端 3 个 CSV 导出（订单、投诉、分账）接入页面，带当前筛选、服务端文件名、in-flight 锁。
- 红包页改成「我的红包批次」列表 + 撤销，错误码翻成人话（有已审核红包、无权限、批次不存在、平台未开放）。
- `merchant/PayCode.vue` 的 `v-html` 改成 data URL `<img>`；全前端已无 `v-html`/`innerHTML` 实际使用。

### 运营端

- 商户归组弹窗支持回显当前所属组、改组与「移出用户组」；`GET /admin/merchants` 补上 `GroupID` 字段。
- 系统设置页适配凭据新形状（密码框 + 状态标签 + 留空不修改 + `cipher_missing` 中文提示）。

### 性能与运维

- **首屏 chunk 1586 kB → 31 kB**（gzip 458 → 9.6）：`vite.config.ts` 按依赖拆 `manualChunks`，`main.ts` 改成按需注册 44 个 naive-ui 组件；页面路由本来就是全动态导入。构建体积告警阈值调回 800 kB。
- **outbox 历史行按保留期清理**：`completed` 超过 7 天分批删（默认 500/批、5 分钟一次、单轮最多 10 批），`dead` 与 `pending` 不动；参数进 `Options`，零值取默认。
- **CSV 导出改流式**：先计数再写头，按 500 行分页边查边写，BOM/表头/列序/截断提示/公式中和全部保持；中途出错补一行「导出中断」而不是半行数据或 panic。

## [2.2.1] - 2026-11-14

一轮完整的自审（安全面、资金面、协议面、数据面、前端各一遍）之后的修复。每条都是先有可复现的失败或读码证据，再改。

### 资金正确性

- **结算审核与自动代付双重出账**：审核结算时已经把商户可用余额扣掉进平台现金，随后的自动打款又冻结并再扣一次，一笔 4000 的结算实际出账 8000。新增 `transfer.CreateSettled` 只落代付单、不再过账，自动打款改走它；加两条回归测试（余额守恒、结算额等于全额余额时不再失败）。
- **代付审核非原子**：解冻、出账、改状态原本是三个独立事务，最终更新失败会留下「已解冻未出账」；并发通过 + 驳回可以把已出账的单子改成驳回。改成单事务内先条件更新抢占状态、再动账；账本侧补 `ReleaseInTransaction`。
- **费率乘法 int64 溢出**：`amount*rate/10000` 在金额超过 9.2e14 分时回绕，100% 费率算成 0。抽出 `internal/money.ApplyBasisPoints`（先除后乘），结算费、分账、邀请返佣、报表手续费四处统一。
- 订单金额上限收敛到 `paymentmodel.MaxAmount`（100000.00 元），充值/保证金不再能提交任意大金额。

### 安全

- **商户通知 SSRF**：`notify_url` 由商户填写、服务端直接请求。新增 `internal/platform/netguard`：出站 client 带超时、拒绝跟随到内网的重定向、在拨号层拦住回环/内网/链路本地/云元数据地址（DNS rebinding 也挡），下单时另有 `URLGuard` 提前拒绝。同机部署用 `GORIXPAY_ALLOW_PRIVATE_NOTIFY_HOSTS=true` 放行内网，元数据地址始终拒绝。
- **通知请求无超时**：原先 `http.DefaultClient` 无超时，一个只连不答的商户就能把单线程 outbox 永久钉住。默认 15 秒，可配。
- **回调不校验渠道归属**：渠道回调只按 `trade_no` 查单，任何渠道（或持有一个渠道凭据的人）都能确认别人的订单。现在要求该订单确实走过这个渠道（按 attempt 判断，兼容多次尝试）。
- **generic 渠道无密钥时回调完全不验签**：无 key 一律拒绝回调，下单也要求 key；`key` 在渠道配置里标记为必填。
- **USDT 通知的 `status` 不在签名内**：签名串补入 `status`，并删掉「下单签名也能当通知签名」的兜底；比较改恒时。（真实 Epusdt 的旧签名通知会被拒，这是有意的破坏性变更。）
- **TOTP 可重放**：同一 6 位码在 ±1 步窗口内可反复使用。新增 `admin_totp.last_used_step`，验码成功后原子认领步长，重放返回 `totp_reused`。
- **登录无限速**：新增 `LoginGuard`，同一标识符 15 分钟内失败 10 次即锁定（429 `too_many_attempts`），检查在密码哈希之前，避免暴力破解把 Argon2id 当内存放大器。商户登录此前既不校验验证码也不写审计，现在两条都补上，且验证码/TOTP 字段真的传进登录流程。
- **生产日志打印重置令牌**：无短信/邮件通道时回落到 logSender，把令牌整条打到 stdout。生产环境（`GORIXPAY_ENVIRONMENT=production`）改用只记「消息被抑制」的发送器，正文不进日志。
- **反代下 cookie 缺 Secure**：`Secure` 原先只看本进程这一跳的 TLS 状态，反代终止 TLS 时永远是 false。改为公共 URL 是 https 就带 Secure。
- **`X-Forwarded-For` 可伪造**：Gin 默认信任所有代理，IP 黑名单、登录锁定、收银台限流都按客户端可编造的地址计数。默认不信任任何代理，用 `GORIXPAY_TRUSTED_PROXIES` 显式放开。
- **停用商户不撤销会话**：封停后旧 cookie 还能用满 7 天。停用即撤销该商户全部会话。
- CSV 公式注入：导出单元格以 `=` `+` `-` `@` 开头时前置单引号，中文与列顺序不变。

### 数据层

- **迁移执行器**：改为逐条执行语句（MySQL 默认禁用多语句，第一个迁移在 MySQL 上开箱即失败）；PostgreSQL/MySQL 先取咨询锁再迁移，两个实例同时启动不再有一个因主键冲突退出；MySQL 对「已存在」类错误（1050/1060/1061/1091/1826）容忍，半途失败后重跑可收敛。
- **SQLite 外键失效**：`PRAGMA foreign_keys` 只对当时那条连接生效，连接回收后外键静默关闭。改用驱动 DSN pragma，每条新连接都带；启动时校验，不是 1 直接报错。
- 迁移 `000029`：订单表补 `(merchant_id, created_at)`、`(status, created_at)`、`expires_at`、`paid_at`、`(kind, created_at)`，outbox 补队列与租约索引（此前 outbox 完全没有索引，而 worker 每秒轮询一次）。
- 迁移 `000030`：投诉表三个索引 + TOTP 步长列。
- 投诉自动规则原先每分钟无上限拉全部超时投诉，没有规则的渠道还会被反复全量扫。改成按渠道过滤下推 SQL、单批上限 100、无规则直接返回。

### 稳定性

- 后台四个 worker 收进 `backgroundGroup`：每个都带 panic recover（此前一个 panic 会带走整个支付进程），`Close` 会等它们退出再关连接池（此前只等 outbox）。

### 工程

- 修复文档与实现不一致：README 的页面数（36 → 59）、发版版本号、覆盖度文档里的验证码路径（`/api/v1/captcha` → `/api/v1/auth/captcha`）、部署文档里的挂载目录属主（镜像固定 UID/GID 10001）。
- `.dockerignore` 排除 `deploy/secrets`、`*.pem`、`deploy/.env`；镜像固定运行用户 UID/GID；compose 三个服务补 `restart`、宽限停止时间与 `no-new-privileges`。

## [2.2.0] - 2026-11-14

### 结算自动化

- 新增 `settle_rules`（迁移 `000018`）：最低结算金额、费率、周期、自动打款开关、按商户或全局。
- `RunAutoSettle` 按规则生成结算单，同日同商户只出一张（幂等键落 `out_settlement_no`），可提现金额扣掉冻结与待审核结算，避免重复出单。
- 批量审核 `POST /api/v1/admin/settlements/batch-process`；审核通过且规则开了自动打款时调 `internal/transfer` 建代付单，商户没有结算资料记 `failed` 并写原因。
- 后台每分钟巡检一次（`settlement.Service.Start`），多副本靠 `last_run_at` 条件更新抢占。

### 分账与红包

- 新增 `profit_sharing_receivers`、`profit_sharing_orders`（迁移 `000019`）、`transfer_orders.kind/batch_no/remark`。
- 手续费先扣再按接收方 `rate_bp` 分账，金额向下取整到分；内部接收方在同一支付事务里过账本，幂等键 `split:{orderID}:{receiverID}`；渠道接收方记 `pending`，不假装调通渠道。
- 接收方合计费率超过 100% 直接拒绝；有流水的接收方不允许硬删。
- 红包：一次创建 N 个 `kind=red` 代付单，金额随机且合计等于总额，批次号幂等。

### 用户组与费率

- 新增 `merchant_groups` 与 `merchants.group_id/settle_mode`（迁移 `000017`）：组内费率（万分比）、可用支付方式、结算模式。
- 结算与分账都通过注入的 `RateBp` 套用商户费率，未分组按 0 处理。

### 报表与导出

- 买家统计、商户统计（手续费按组费率算）、每日统计、商户资金明细。
- 8 种 CSV 导出：admin 的订单、退款、结算、代付、商户、账本、买家，merchant 的订单、资金明细。UTF-8 BOM、附件头、10 万行截断提示。

### 工程

- 前端新增 7 个页面：运营端的用户组、结算规则、分账、报表；商户端的资金明细、分账管理、红包。
- 修复 `internal/settlement` 测试里用墙钟纳秒做幂等键导致的偶发失败。

### 投诉处理

- 新增 `complaints` 与 `complaint_messages`（迁移 `000020`）：运营端手工登记、回复、关闭，商户端只读自己的。
- 状态机 `pending → replied → closed`，超期派生 `overdue`；`(channel, complaint_no)` 唯一，重复登记 409，关闭后回复 409，跨商户详情 404。
- 渠道自动拉取留了 `Puller` 接口，插件侧还没有拉取接口，所以目前是人工闭环。

### 后台安全

- 新增 `admin_totp`、`login_attempts`、`captcha_challenges`（迁移 `000022`）。
- 两步验证按 RFC 6238 自己实现（HMAC-SHA1、30 秒、±1 窗口），密钥用 AES-GCM 加密入库。
- 图形验证码用 SVG 现画，库里只存 `sha256(id:code)`，2 分钟过期、一次性消费。
- 登录接口新增 `captcha_id`/`captcha_code`/`totp_code` 三个可选字段；设置项 `security.captcha_required` 打开后强制验证码，失败与成功都写 `login_attempts`。

### 支付方式与按日轮询

- 新增 `pay_types`、`pay_rolls`、`wechat_mp_configs`（迁移 `000023`）。
- 公开接口 `GET /api/v1/public/pay-types`；`/api/v1/public/site` 的 `payment_types` 改为读这张表。
- 按日轮询接进渠道选择：当天有记录就替换权重，`weight=0` 表示当天不参与，没有记录按原权重。
- 公众号配置的 `app_secret`/`aes_key` 加密存储，列表只回 `secret_configured`。

### 通知与找回密码

- 新增 `notification_channels`、`verify_codes`、`password_resets`（迁移 `000021`）。
- 短信与邮件通道可配可测；没注入真实网关时打到 stdout，并在页面提示。
- 验证码同 target 60 秒一次、每天 10 次；token 只存哈希、一次性；重置后作废该商户全部会话；改密复用 Argon2id 升级序列，下限 8 位。

### 工程（第二批）

- 前端新增运营端五页（投诉、安全设置、支付方式、按日轮询、公众号配置）与商户端两处（我的投诉、找回密码 + 账户资料里的改密卡片），登录页接入图形验证码。
- `internal/channel/router` 增加可选的按日轮询钩子（`WithRollResolver`），不注入时行为不变。

### 语音设备与云打印

- 新增 `voice_devices`、`voice_broadcasts`、`printers`、`print_jobs`（迁移 `000024`）。
- 设备侧走 `device_no` + 密钥（常量时间比对，不走会话）：语音心跳取走待播报并标记已下发，打印机轮询取任务，未确认最多重投 3 次。
- 支付成功钩子在同一事务里给本商户与全局设备入队播报、给本商户打印机入队小票，按订单判重，重放不会重复打印。

### 运维小件

- `POST /api/v1/admin/maintenance/cleanup`：四张表白名单，只删 30 天前数据，默认 dry-run，真删要 `confirm:"DELETE"`。
- `POST /api/v1/admin/channels/:id/test-order`：运营端发起一笔测试订单，返回 `trade_no` 与支付动作。

### 第三方登录与扫码登录

- 新增 `oauth_bindings`、`qr_login_sessions`、`oauth_states`（迁移 `000025`）。
- 微信/QQ/抖音 OAuth：一次性 state 落库，已绑定直接登录，未绑定返回 `bind_ticket` 用密码绑定，商户端可查绑定与解绑。
- 扫码登录：SVG 二维码自己画，ticket 5 分钟，`pending → scanned → confirmed`，token 只发一次；后台每 30 分钟清理过期 state 与 ticket。

### 用户组购买与红包退回

- `merchant_groups.price_cents` 与 `group_purchases`（迁移 `000026`）：组买断价、余额扣款归组、退款反向入账并解组；幂等键带 `attempts` 后缀，退款后再买同一组会真的扣款。
- 红包批次：整批撤销解冻与过期自动退回（后台每 30 分钟扫一次，超 24 小时未审核整批撤销）。

### 站点皮肤

- 设置项 `site.skin`（`default` / `dark` / `compact`），`/api/v1/public/site` 返回 `skin`，前端据此切换 Naive 主题与页面留白。

### 修掉的三个问题

- `cipher` 在 `security.New` 之前没加载，两步验证拿到 nil cipher，TOTP 绑定直接 500。
- `decodeBool` 只认 JSON 布尔，后台写入的字符串 `"true"` 被判成 false，图形验证码开关不生效。
- 验证码 SVG 用 `<text>` 输出字符，答案能被脚本直接读出来；改成位图矩形绘制，并补了「SVG 里不得出现 text 节点」的断言。
- 回调钩子改为一次性挂全部：`WithHooks` 是替换语义，第二次调用会把资金、返佣、分账三个钩子丢掉。

### 收口：运营面最后一批

- 后台手动付款与批量付款：`POST /api/v1/admin/transfers`、`/transfers/batch`，批量失败整批补偿回滚，幂等键经 sha256 派生（列宽与字符集限制）。
- 客诉自动规则（迁移 `000028`）：按渠道自动回复、超时自动关闭、`*` 兜底、disabled 阻断兜底、重复跑幂等；`GET/POST/PUT/DELETE /admin/complaint-auto-rules`。
- 注册邀请码（迁移 `000027`）：`invite_codes` 增删改查，注册带码原子消费次数、注册失败回退，并发不超发。
- 商户通知偏好（同迁移）：支付/结算/投诉三个开关加邮件短信渠道，`Notify` 在通道未配置时只记警告，不阻塞主流程。
- 商户投诉与分账两份 CSV 导出，复用 BOM 与截断头。
- 聚合收款码：`GET /merchant/pay-code` 返回**真 QR**（`rsc.io/qr` 编码，模块渲染成 SVG 矩形）与支付页 `/pay/code/{type}`；商户自助测试支付上限 1 元。
- 插件定时钩子：`CronHook` 接口、`Registry.CronHooks()`、`RunCronHooks()`（跑完全部收集错误，panic 转 error），后台每分钟调用。
- 协议补口：`/pay/{func}/{trade_no}/` 覆盖 scan、qrcode、jsapi、mini、h5、red、bank、wap 八种上游页面名，统一落到收银台；`getshop.php` 补 `act=poll` 与 `act=captcha_verify`。
- 公众号模板消息：`stable_token` 按 `app_id` 缓存、`template/send` 发送、`errcode` 统一包装、`40001/40014/42001` 清缓存重试一次；`notifyprefs` 新增 `wechat` 渠道，没绑 openid 或没配模板时回退邮件并记警告。

## [2.1.0] - 2026-11-14

### 站点身份可配置

- `site.name`、`site.logo_url` 从"存在数据库里但只有官网读"变成整站生效：官网、收银台、商户登录、注册、运营登录、运营后台与商户中心侧栏、浏览器标题全部跟随；`logo_url` 有值时渲染图片，加载失败自动退回默认图标。
- 新增 `site.footer_text`（页脚文本，填备案号或公司名），官网页脚单独一行渲染，留空不显示。管理端 `PUT /api/v1/admin/settings/site.footer_text`，公开端随 `/api/v1/public/site` 返回。
- 前端新增 `stores/site.ts`：站点身份只拉一次、失败可重试，标题统一走 `setPageTitle()`。

### 部署

- 新增 `scripts/deploy.sh` 与 `scripts/deploy.ps1`：检查 docker、生成 `deploy/.env` 与主密钥、起容器、轮询健康检查、打印管理员创建命令，幂等且支持 dry-run。
- 新增 `deploy/.env.example`。
- `deploy/Dockerfile` 的依赖源可覆盖：`--build-arg NPM_REGISTRY=...`、`--build-arg GOPROXY=...`，默认走国内镜像，境外网络可换回官方源。
- 新增 `docs/deployment.md`：三种部署方式、反向代理、备份恢复、升级回滚、排查表、生产检查清单。

### 文档

- 新增 `docs/epay-coverage.md`：逐个上游入口、后台页面、cron 分支对照实现状态（11 项已实现、26 项部分实现、14 项未实现），并给出"能不能直接替换"的分场景结论。

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
