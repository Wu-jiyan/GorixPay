# Epay Contract Baseline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 建立可机器执行、可追溯到 Epay 源码的兼容契约基线，作为 GroixPay 后续实现的验收事实源。

**Architecture:** 使用 YAML 清单描述路由、字段、签名、响应、通知及历史怪异语义，并使用 Go 测试加载和校验清单。静态源码样本先固化确定性规则，动态行为通过隔离的 PHP 夹具与固定 RSA 测试密钥补齐，不连接真实支付渠道。

**Tech Stack:** YAML、Go testing、PHP CLI、OpenSSL、PowerShell

---

### Task 1: 契约清单 Schema 与加载器

**Files:**
- Create: `tests/contracts/epay/schema.go`
- Create: `tests/contracts/epay/schema_test.go`
- Modify: `tests/contracts/epay/manifest.yaml`

- [ ] **Step 1: 编写失败测试，要求清单可加载且关键入口齐全**
- [ ] **Step 2: 运行 `go test ./tests/contracts/epay -run TestManifest -v`，确认因加载器不存在而失败**
- [ ] **Step 3: 定义最小强类型 Schema，解析版本、签名、路由、通知和兼容怪异语义**
- [ ] **Step 4: 校验所有源码引用均为仓库相对路径并包含正整数行号**
- [ ] **Step 5: 再次运行测试，确认清单解析和必填约束通过**

### Task 2: 签名规范化与 MD5 向量

**Files:**
- Create: `tests/contracts/epay/signing_reference.go`
- Create: `tests/contracts/epay/signing_reference_test.go`

- [ ] **Step 1: 编写失败测试覆盖排序、数组排除、空值排除、零值保留和值空白保留**
- [ ] **Step 2: 运行 `go test ./tests/contracts/epay -run 'TestCanonical|TestMD5' -v`，确认失败**
- [ ] **Step 3: 实现仅供契约验证的 Epay 规范化和 MD5 参考算法**
- [ ] **Step 4: 验证空待签名串与密钥 `123` 的结果为 `202cb962ac59075b964b07152d234b70`**
- [ ] **Step 5: 运行相关测试并确认全部通过**

### Task 3: 固定 RSA 契约向量

**Files:**
- Create: `tests/contracts/epay/fixtures/rsa_private.pem`
- Create: `tests/contracts/epay/fixtures/rsa_public.pem`
- Create: `tests/contracts/epay/rsa_vectors.yaml`
- Create: `tests/contracts/epay/rsa_reference_test.go`

- [ ] **Step 1: 生成仅用于测试的 2048 位 RSA 密钥对，文件中明确使用不可用于生产的测试名称**
- [ ] **Step 2: 固化请求参数、待签名 UTF-8 字符串、Base64 签名和公钥验签结果**
- [ ] **Step 3: 编写失败测试验证 SHA256WithRSA PKCS#1 v1.5，而非 RSA-PSS**
- [ ] **Step 4: 运行 `go test ./tests/contracts/epay -run TestRSA -v` 并确认通过**
- [ ] **Step 5: 增加篡改参数、错误公钥和小写 `rsa` 回落 MD5 的边界测试**

### Task 4: PHP 源行为探针

**Files:**
- Create: `tests/contracts/epay/probe/signing.php`
- Create: `tests/contracts/epay/probe/README.md`
- Create: `tests/contracts/epay/probe_test.go`

- [ ] **Step 1: 编写隔离探针，仅加载 Epay 签名函数所需代码，不连接数据库**
- [ ] **Step 2: 探针输出规范化字符串、MD5 和 RSA 结果的 JSON**
- [ ] **Step 3: Go 测试检测 `php` 命令；存在时执行探针，不存在时明确跳过动态交叉验证**
- [ ] **Step 4: 比较 PHP 输出与 Go 参考实现，任何字节差异都使测试失败**
- [ ] **Step 5: 运行 `go test ./tests/contracts/epay -run TestPHPProbe -v` 并记录通过或明确跳过原因**

### Task 5: 路由与字段契约校验

**Files:**
- Create: `tests/contracts/epay/routes_test.go`
- Create: `tests/contracts/epay/fixtures/requests.yaml`
- Create: `tests/contracts/epay/fixtures/responses.yaml`

- [ ] **Step 1: 为 `/submit.php`、`/mapi.php`、旧 `act` 接口和 12 个新版方法建立请求样本**
- [ ] **Step 2: 为成功码、无参数、签名错误、业务错误、插件错误和未知路由建立响应样本**
- [ ] **Step 3: 编写测试确保每个清单操作至少有一个成功与一个失败样本**
- [ ] **Step 4: 验证 GET/POST 位置、条件必填、二选一字段和最大分页限制**
- [ ] **Step 5: 运行 `go test ./tests/contracts/epay -run 'TestRoute|TestFixtureCoverage' -v` 并确认通过**

### Task 6: 通知与支付动作契约

**Files:**
- Create: `tests/contracts/epay/notifications_test.go`
- Create: `tests/contracts/epay/fixtures/notifications.yaml`
- Create: `tests/contracts/epay/fixtures/payment_actions.yaml`

- [ ] **Step 1: 固化 V0 MD5 与 V1 RSA 商户通知字段、签名和查询串样本**
- [ ] **Step 2: 覆盖 `success`、`SUCCESS`、`Success` 成功及 `ok`、HTTP 200 空正文失败**
- [ ] **Step 3: 固化 1、3、19、55、115 分钟附近的旧通知重试时间语义**
- [ ] **Step 4: 覆盖所有兼容支付动作和插件特定渠道回执类别**
- [ ] **Step 5: 运行 `go test ./tests/contracts/epay -run 'TestNotification|TestPaymentAction' -v` 并确认通过**

### Task 7: 来源追溯与差异报告

**Files:**
- Create: `tests/contracts/epay/source_trace_test.go`
- Modify: `docs/epay-contract-baseline.md`

- [ ] **Step 1: 编写测试验证清单中的源码文件存在且起始行有效**
- [ ] **Step 2: 检查公开文档与源码差异，记录 `page/offset`、金额小数位和额外 API 字段等差异**
- [ ] **Step 3: 将历史缺陷分类为严格兼容、配置可关闭、GroixPay 新 API 禁止继承**
- [ ] **Step 4: 运行 `go test ./tests/contracts/epay -run TestSourceTrace -v` 并确认通过**

### Task 8: 阶段验收

**Files:**
- Modify: `docs/epay-contract-baseline.md`
- Modify: `docs/superpowers/plans/2026-07-12-gorixpay-implementation-roadmap.md`

- [x] **Step 1: 运行 `go test ./tests/contracts/epay -v`，确认全部静态契约测试通过**
- [x] **Step 2: 在具备 PHP CLI 时运行动态探针，确认 Go 与 PHP 结果一致**
- [x] **Step 3: 扫描清单中的空来源、重复 ID、无样本操作和未定义状态**
- [x] **Step 4: 在基线文档记录最终样本数量、已知怪异语义和动态探针环境**
- [x] **Step 5: 将路线图阶段 0 标记为通过后，才编写阶段 1 详细计划**

## 完成标准

- 所有公开支付、查询、退款、商户和代付接口均在清单中。
- 每个接口至少有成功和失败样本。
- MD5 与 RSA 均有固定、跨语言可验证的测试向量。
- 通知字段、回执识别、重试时序和支付动作均有契约测试。
- 每项关键行为可追溯到 Epay 源码文件和行号。
- 文档与实现差异被明确记录，不以公开文档覆盖源码事实。
