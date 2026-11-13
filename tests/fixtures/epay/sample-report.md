# Epay → GorixPay 迁移报告

- 工具版本：1.0
- 生成时间：2026-11-13T10:23:37Z
- 模式：apply
- 耗时：230 ms
- 退出码：0
- 源库：sqlite `epay-source.db`
- 目标库：sqlite `gorixpay.db`（已打开：true，可写：true，已应用 migration：13）

## 预检

- 结论：通过
- 必需表：7/7 存在
- pre_config 关键项：`sitename=Epay Fixture 支付平台`，`version=2055`

## 迁移计数

| 源表 | 目标表 | 源记录 | 新增 | 跳过 | 失败 | 源金额(分) | 目标金额(分) | 差额(分) |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `pre_user` | `merchants` | 2 | 2 | 0 | 0 | 0 | 0 | 0 |
| `pre_order` | `payment_orders` | 3 | 3 | 0 | 0 | 3249 | 3249 | 0 |
| `pre_refundorder` | `refund_orders` | 1 | 1 | 0 | 0 | 100 | 100 | 0 |
| `pre_settle` | `settlement_orders` | 1 | 1 | 0 | 0 | 1000 | 1000 | 0 |
| `pre_transfer` | `transfer_orders` | 1 | 1 | 0 | 0 | 500 | 500 | 0 |
| `pre_domain` | `merchant_domains` | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| `pre_blacklist` | `blacklist_entries` | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
| `pre_config` | `system_settings` | 7 | 1 | 6 | 0 | 0 | 0 | 0 |

## 商户余额期初

- 商户账户：2 个，写入 2 笔，跳过 0 笔，失败 0 笔
- 金额合计：源 15629 分 / 目标 15629 分（差额 0 分）

## 跳过原因

- `system_settings`：not_whitelisted=6

## 阻断项

- 无

## 警告

- 无

## 已知限制

- pre_plugin / pre_channel / pre_wx* 的插件与渠道配置不迁移（含密钥），需在运营后台重新配置。
- 商户 API 密钥（pre_user.key）与 RSA 公钥不迁移，上线后由商户重新签发。
- pre_user 的邀请关系、消息模板、打印配置，以及 pre_order 的分润、子单、退款流水不做映射。
- pre_user.deposit（保证金）不迁移，只迁移 money 作为可用余额期初。
- 源库时间无时区，按 UTC 写入；通知重试状态（pre_order.notify）不迁移。
- pre_config 只迁移安全白名单键（sitename → site.name），其余键在报告中列为跳过。
