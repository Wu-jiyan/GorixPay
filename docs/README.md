# 文档索引

每份文档回答一类问题。先看下表挑一份，再按文末的三条阅读路径走。

| 文档 | 回答什么问题 | 读者 |
| --- | --- | --- |
| [../README.md](../README.md) | 项目是什么，易支付兼容到什么程度，五分钟怎么跑起来，已知限制有哪些 | 第一次点进仓库的人 |
| [../README.en.md](../README.en.md) | 上面这份的英文版 | 英文读者 |
| [epay-v2.md](epay-v2.md) | 易支付 V1 与 V2 的接入方法：签名规则、接口清单、错误码、时间戳与防重放要求 | 接入商户、渠道对接方 |
| [epay-compat-matrix.md](epay-compat-matrix.md) | 兼容性的证据：入口清单、17 处修掉的上游偏差、13 处有意保留的偏差，每条对应的测试函数名 | 评估能否替换现有系统的人、reviewer |
| [epay-contract-baseline.md](epay-contract-baseline.md) | 必须兼容的上游行为清单，机器可读事实源放在哪，冲突时以谁为准 | 改兼容层的人 |
| [plugins.md](plugins.md) | 插件契约（`Plugin`、`Configurable`、`CallbackVerifier`）、10 个渠道的配置字段与报文细节、通用回调与状态码 | 运维、插件开发者 |
| [architecture.md](architecture.md) | 模块分层与依赖方向、一次支付请求的 9 步链路、订单状态机、复式账本过账路径、插件契约、迁移机制、12 条已知妥协 | 改核心逻辑的人 |
| [plans/2026-11-13-epay-v2-vue-completion.md](plans/2026-11-13-epay-v2-vue-completion.md) | 第一轮并行开发的范围与验收口径：V2 协议、Vue 前端、Redis、商户门户 | 想知道现有形态是怎么定下来的 |
| [plans/2026-11-13-epay-ecosystem.md](plans/2026-11-13-epay-ecosystem.md) | 第二轮并行开发的范围：实名认证、域名白名单、邀请返佣、风控、充值与保证金、首批插件、迁移工具 | 接手生态域的人 |
| [superpowers/specs/2026-07-12-gorixpay-architecture-design.md](superpowers/specs/2026-07-12-gorixpay-architecture-design.md) | 初版架构设计与选型理由，含当时的取舍 | 想理解为什么这样分层 |
| [superpowers/plans/2026-07-12-gorixpay-implementation-roadmap.md](superpowers/plans/2026-07-12-gorixpay-implementation-roadmap.md) | 分阶段交付路线图 | 想了解开发顺序 |
| [superpowers/plans/2026-07-12-phase-0-epay-contract-baseline.md](superpowers/plans/2026-07-12-phase-0-epay-contract-baseline.md) | 契约基线怎么建立、为什么先做它 | 改契约夹具的人 |
| [superpowers/plans/2026-07-12-phase-1-platform-foundation.md](superpowers/plans/2026-07-12-phase-1-platform-foundation.md) | 平台骨架（配置、数据库、迁移、HTTP、生命周期）的搭建过程 | 改启动与配置的人 |
| [notes/2026-11-login-redirect-loop.md](notes/2026-11-login-redirect-loop.md) | 一次登录后无限刷新的排查记录与三行修复 | 排查前端 401 行为的人 |
| [../SECURITY.md](../SECURITY.md) | 信任边界、资金校验点、为兼容保留的风险面、漏洞上报方式 | 安全评审、线上运维 |
| [../CONTRIBUTING.md](../CONTRIBUTING.md) | 环境准备、测试分层各自防什么、新增插件步骤、迁移文件规则、PR 要求 | 提交代码的人 |
| [../CHANGELOG.md](../CHANGELOG.md) | 2.0.0 交付了什么，按模块分组 | 升级前想知道改了哪些地方的人 |

## 三条阅读路径

要接支付：先看 [epay-v2.md](epay-v2.md) 的 V1 或 V2 章节，按签名规则拼请求；接入某个渠道时看 [plugins.md](plugins.md) 的配置字段与回调报文；上线前对照 [../README.md](../README.md) 的已知限制。

要评估能不能替换现有易支付：先看 [epay-compat-matrix.md](epay-compat-matrix.md) 的入口清单与偏差清单，再看 [epay-contract-baseline.md](epay-contract-baseline.md) 确认哪些上游怪癖是刻意保留的。两份文档里的每条结论都挂着测试函数名，可以直接跑。

要改代码：先读 [../CONTRIBUTING.md](../CONTRIBUTING.md) 的测试分层与迁移规则，再读 [epay-contract-baseline.md](epay-contract-baseline.md) 了解不能动的协议面，改完跑 `go test ./tests/conformance/epay -count=1 -v`。

## 维护规则

文档跟着代码走，改到哪写到哪，不要留下和实现相反的描述。

协议面有变化（字段、错误码、签名、状态码）时，同步 [epay-compat-matrix.md](epay-compat-matrix.md) 与 `tests/contracts/epay/manifest.yaml`，并在本索引对应行的说明里改口径。

新增支付插件时改四处：[plugins.md](plugins.md) 第 1 节清单、第 3 节配置字段、第 4 节报文细节、第 5.5 节回调速查表。

新增或改名文档时在表里加行，说明它回答什么问题、给谁看。历史计划文档（`plans/`、`superpowers/`）按日期归档，不再改写内容，只在索引里更新说明。
