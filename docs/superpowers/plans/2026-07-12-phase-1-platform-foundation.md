# Platform Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 建立可启动、可测试、支持 SQLite/MySQL/PostgreSQL 的 GroixPay Go 平台骨架。

**Architecture:** `cmd/gorixpay` 只负责解析启动参数和进程退出，`internal/platform/app` 组合配置、数据库、迁移、HTTP 和生命周期。数据库差异收敛在 dialect 与迁移层；Redis 不参与本阶段启动条件。

**Tech Stack:** Go 1.21、Gin、GORM、glebarez/sqlite、gorm MySQL/PostgreSQL drivers、slog、YAML、Docker

---

### Task 1: 配置加载

**Files:**
- Create: `internal/platform/config/config.go`
- Create: `internal/platform/config/config_test.go`

- [ ] **Step 1: 编写失败测试，覆盖默认值、YAML 文件、环境变量和命令行覆盖顺序**
- [ ] **Step 2: 运行 `go test ./internal/platform/config -v`，确认缺少加载器而失败**
- [ ] **Step 3: 实现 `Load(args []string, getenv func(string) string) (Config, error)`**
- [ ] **Step 4: 验证数据库驱动只接受 `sqlite/mysql/postgres`，生产模式必须提供主密钥来源**
- [ ] **Step 5: 运行配置测试并确认通过**

### Task 2: 数据库与方言

**Files:**
- Create: `internal/platform/database/database.go`
- Create: `internal/platform/database/database_test.go`
- Create: `internal/platform/database/dialect.go`

- [ ] **Step 1: 编写失败测试，要求 SQLite 内存库可连接并识别方言**
- [ ] **Step 2: 运行 `go test ./internal/platform/database -v`，确认失败**
- [ ] **Step 3: 实现三种 GORM Dialector 创建、连接池配置和 SQLite pragma**
- [ ] **Step 4: 实现方言能力对象，暴露占位符、锁和时间表达式差异**
- [ ] **Step 5: 运行数据库测试并确认 SQLite 通过**

### Task 3: 版本迁移器

**Files:**
- Create: `internal/platform/migrate/migrate.go`
- Create: `internal/platform/migrate/migrate_test.go`
- Create: `migrations/000001_platform.sql`

- [ ] **Step 1: 编写失败测试，覆盖空库升级、重复执行和校验和漂移拒绝**
- [ ] **Step 2: 运行 `go test ./internal/platform/migrate -v`，确认失败**
- [ ] **Step 3: 实现嵌入迁移、`schema_migrations`、事务执行和 SHA-256 校验和**
- [ ] **Step 4: 创建平台初始表 `system_settings` 和 `audit_events`**
- [ ] **Step 5: 运行迁移测试并确认幂等**

### Task 4: 日志与 HTTP 基础

**Files:**
- Create: `internal/platform/logging/logging.go`
- Create: `internal/platform/httpserver/server.go`
- Create: `internal/platform/httpserver/server_test.go`

- [ ] **Step 1: 编写失败测试，覆盖请求 ID、恢复中间件、安全响应头和 JSON 404**
- [ ] **Step 2: 运行 `go test ./internal/platform/httpserver -v`，确认失败**
- [ ] **Step 3: 实现 JSON `slog`、Gin release/debug 模式和中间件链**
- [ ] **Step 4: 确保日志不记录请求体、Authorization 和 Cookie**
- [ ] **Step 5: 运行 HTTP 测试并确认通过**

### Task 5: 健康检查

**Files:**
- Create: `internal/platform/health/handler.go`
- Create: `internal/platform/health/handler_test.go`

- [ ] **Step 1: 编写失败测试，要求 `/health/live` 始终反映进程存活，`/health/ready` 检查数据库**
- [ ] **Step 2: 运行 `go test ./internal/platform/health -v`，确认失败**
- [ ] **Step 3: 实现可注入 Checker 与稳定 JSON 响应**
- [ ] **Step 4: 数据库不可用时 readiness 返回 HTTP 503 且不泄露 DSN**
- [ ] **Step 5: 运行健康检查测试并确认通过**

### Task 6: 应用组合与生命周期

**Files:**
- Create: `internal/platform/app/app.go`
- Create: `internal/platform/app/app_test.go`
- Create: `cmd/gorixpay/main.go`

- [ ] **Step 1: 编写失败测试，使用 SQLite 内存库启动应用并请求健康端点**
- [ ] **Step 2: 运行 `go test ./internal/platform/app -v`，确认失败**
- [ ] **Step 3: 组合配置、日志、数据库、迁移、路由和 HTTP Server**
- [ ] **Step 4: 实现 SIGINT/SIGTERM 优雅停机和明确退出码**
- [ ] **Step 5: 运行应用测试和 `go run ./cmd/gorixpay --help`**

### Task 7: 容器部署

**Files:**
- Create: `deploy/Dockerfile`
- Create: `deploy/compose.yaml`
- Create: `deploy/config.example.yaml`
- Create: `.dockerignore`

- [ ] **Step 1: 编写 Compose 配置检查命令并确认文件缺失**
- [ ] **Step 2: 创建非 root、多阶段 Go 生产镜像**
- [ ] **Step 3: 创建单容器 SQLite 示例和 PostgreSQL + Redis Compose 栈**
- [ ] **Step 4: 配置健康检查、持久化卷和 Secret 挂载，不写入真实密钥**
- [ ] **Step 5: 运行 `docker compose -f deploy/compose.yaml config`**

### Task 8: 阶段验收

**Files:**
- Modify: `docs/superpowers/plans/2026-07-12-gorixpay-implementation-roadmap.md`

- [ ] **Step 1: 运行 `go test ./...`**
- [ ] **Step 2: 运行 `go vet ./...`**
- [ ] **Step 3: 使用 SQLite 临时文件启动二进制，验证 live 与 ready**
- [ ] **Step 4: 验证未配置 Redis 时应用正常启动**
- [ ] **Step 5: 在可用环境执行 MySQL/PostgreSQL 集成测试与 Compose 配置校验**

## 完成标准

- 单个 Go 命令可使用 SQLite 启动服务。
- 配置覆盖顺序确定且敏感配置校验明确。
- 三数据库驱动和统一迁移接口完成。
- 健康检查、结构化日志、请求 ID 和优雅停机可验证。
- Redis 缺失不阻断启动。
- 全量 Go 测试与静态检查通过。
