# 部署

三种方式，选一种就行。下面的命令都在仓库根执行。

| 方式 | 适合 | 升级怎么做 | 需要什么 |
| --- | --- | --- | --- |
| Docker Compose | 单机跑生产，想要现成的 PostgreSQL 与 Redis | `docker compose up -d --build` | Docker 24+ |
| Docker 单容器 | 已有外部数据库，或者要塞进 k8s | 换镜像重启 | Docker 24+ |
| 源码编译 | 要改代码、要调试、要交叉编译到 ARM | 重新编译替换二进制 | Go 1.21+、Node 20+ |

三种方式跑的是同一个二进制、同一套迁移，数据库结构完全一致，可以先单机试再搬生产。

## 1. 先准备三样东西

**主密钥**：32 字节随机数，或这 32 字节的 base64。丢了它，数据库里所有加密字段（渠道密钥、商户私钥、身份证号）都解不开，没有找回路径。

```bash
head -c 32 /dev/urandom | base64 > master_key.txt
```

```powershell
$bytes = New-Object byte[] 32
[Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($bytes)
[Convert]::ToBase64String($bytes) | Set-Content -NoNewline master_key.txt
```

**对外地址**：带 scheme 的绝对 URL，收银台链接、渠道回调地址、商户通知地址都拿它拼。反向代理后面也要填公网地址，不能填 `127.0.0.1`。

**数据库**：SQLite 开箱即用，生产建议 PostgreSQL 16 或 MySQL 8。迁移文件三种方言共用，切换只要改 DSN。

## 2. Docker Compose 部署（推荐）

### 2.1 一键脚本

```bash
bash scripts/deploy.sh
```

Windows（PowerShell 7）：

```powershell
pwsh -File scripts/deploy.ps1
```

脚本做的事：检查 docker 与 compose 在不在 → `deploy/.env` 不存在就从 `deploy/.env.example` 复制并生成随机数据库口令 → `deploy/secrets/master_key.txt` 不存在就生成密钥 → `docker compose up -d --build` → 轮询 `/health/ready` 最多 180 秒 → 打印访问地址与创建管理员命令。

第一次跑会停在「改对外地址」这一步：打开 `deploy/.env`，把 `GORIXPAY_PUBLIC_URL` 改成真实域名，再跑一次。脚本幂等，重复执行不会覆盖已有密钥与口令。想先看它要做什么，加 `--dry-run`（Windows 是 `-DryRun`）。

### 2.2 手动步骤

```bash
cd deploy
cp .env.example .env
# 编辑 .env：POSTGRES_PASSWORD 换成随机口令，GORIXPAY_PUBLIC_URL 换成真实地址
mkdir -p secrets
head -c 32 /dev/urandom | base64 > secrets/master_key.txt
chmod 600 secrets/master_key.txt
cd ..
docker compose -f deploy/compose.yaml up -d --build
```

三个服务：`postgres:16-alpine`、`redis:7-alpine`、`gorixpay`。镜像里的构建阶段会跑 `npm ci && npm run build` 打前端，运行镜像只有二进制和 `dist`，不需要在宿主机装 Node。

数据库口令只写在 `deploy/.env` 一处，postgres 与 GorixPay 的 DSN 都引用同一个变量，改口令不会两边不一致。

### 2.3 验证

```bash
curl -s http://127.0.0.1:8080/health/ready      # {"status":"ok"}
curl -s http://127.0.0.1:8080/api/v1/public/site
curl -s -X POST http://127.0.0.1:8080/mapi.php  # {"code":-4,"msg":"未传入任何参数"}
curl -s "http://127.0.0.1:8080/api.php?act=nope" # {"code":-5,"msg":"No Act!"}
```

最后两条是易支付的协议语义：HTTP 永远 200，错误放在 `code` 里。这两条能对上，说明兼容层起来了。

### 2.4 建管理员

```bash
docker compose -f deploy/compose.yaml exec -T \
  -e GORIXPAY_ADMIN_PASSWORD='换成强密码' \
  gorixpay gorixpay admin create \
  --username admin --display-name 管理员 --password-env GORIXPAY_ADMIN_PASSWORD
```

密码走环境变量，不要用 `--password`（会留在 shell 历史里）。容器里主密钥已经通过 `/run/secrets/master_key` 挂进来了，命令不用再传 `--master-key-file`。建完访问 `/admin/login`。

### 2.5 数据、备份、恢复

数据在命名卷里：`deploy_postgres_data`、`deploy_redis_data`。Redis 只放防重放与限流，丢了不影响账目，但要重新接受一次「重放窗口内」的请求。

```bash
# 备份
docker compose -f deploy/compose.yaml exec -T postgres \
  pg_dump -U gorixpay -d gorixpay | gzip > gorixpay-$(date +%F).sql.gz

# 恢复（先停应用，避免写入）
docker compose -f deploy/compose.yaml stop gorixpay
gunzip -c gorixpay-2026-11-14.sql.gz | \
  docker compose -f deploy/compose.yaml exec -T postgres psql -U gorixpay -d gorixpay
docker compose -f deploy/compose.yaml start gorixpay
```

主密钥要和备份分开放。备份里有数据，密钥在别处，两者合起来才是完整的一套。

## 3. 从源码部署

### 3.1 构建

```bash
npm --prefix frontend ci
npm --prefix frontend run build          # 产物在 frontend/dist，Go 直接托管

CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o gorixpay ./cmd/gorixpay
```

交叉编译到 Linux ARM（很多云主机是 ARM）：

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o gorixpay ./cmd/gorixpay
```

二进制需要同目录下有 `frontend/dist`（或者用 `--public-url` 反代到别处的静态服务）。迁移 SQL 已经 `//go:embed` 进二进制，不用额外拷 `migrations/`。

### 3.2 配置

两种写法，二选一。命令行参数优先级最高，其次环境变量，其次 YAML，最后默认值。

```yaml
# config.yaml
environment: production
http:
  address: "127.0.0.1:8080"
  public_url: "https://pay.example.com"
database:
  driver: postgres
  dsn: "host=127.0.0.1 user=gorixpay password=*** dbname=gorixpay port=5432 sslmode=disable"
security:
  master_key_file: "/etc/gorixpay/master_key.txt"
redis:
  addr: "127.0.0.1:6379"
  prefix: "gorixpay:"
sandbox:
  enabled: false
```

```bash
export GORIXPAY_ENVIRONMENT=production
export GORIXPAY_HTTP_ADDRESS=127.0.0.1:8080
export GORIXPAY_PUBLIC_URL=https://pay.example.com
export GORIXPAY_DATABASE_DRIVER=postgres
export GORIXPAY_DATABASE_DSN="host=127.0.0.1 user=gorixpay password=*** dbname=gorixpay port=5432 sslmode=disable"
export GORIXPAY_MASTER_KEY_FILE=/etc/gorixpay/master_key.txt
./gorixpay --config config.yaml
```

`production` 下有两个强制行为：没有主密钥文件直接启动失败；沙箱渠道不注册，传 `--sandbox-enabled=true` 也会被按回 false。

### 3.3 systemd

```ini
# /etc/systemd/system/gorixpay.service
[Unit]
Description=GorixPay
After=network-online.target postgresql.service
Wants=network-online.target

[Service]
Type=simple
User=gorixpay
Group=gorixpay
WorkingDirectory=/opt/gorixpay
EnvironmentFile=/etc/gorixpay/gorixpay.env
ExecStart=/opt/gorixpay/gorixpay --config /etc/gorixpay/config.yaml
Restart=on-failure
RestartSec=3
LimitNOFILE=65535

# 账本与密钥不入日志
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

```bash
useradd --system --home /opt/gorixpay --shell /usr/sbin/nologin gorixpay
install -d -o gorixpay -g gorixpay /opt/gorixpay /etc/gorixpay
install -m 600 -o gorixpay -g gorixpay master_key.txt /etc/gorixpay/master_key.txt
systemctl daemon-reload && systemctl enable --now gorixpay
journalctl -u gorixpay -f
```

进程收到 `SIGTERM` 后会停止收新请求、等在途请求结束再关库，`systemctl restart` 是安全的。

### 3.4 建管理员

```bash
sudo -u gorixpay GORIXPAY_ADMIN_PASSWORD='换成强密码' \
  /opt/gorixpay/gorixpay admin create \
  --username admin --display-name 管理员 --password-env GORIXPAY_ADMIN_PASSWORD \
  --config /etc/gorixpay/config.yaml
```

## 4. Docker 单容器部署

```bash
docker run -d --name gorixpay \
  --restart unless-stopped \
  -p 8080:8080 \
  -e GORIXPAY_ENVIRONMENT=production \
  -e GORIXPAY_PUBLIC_URL=https://pay.example.com \
  -e GORIXPAY_DATABASE_DRIVER=postgres \
  -e GORIXPAY_DATABASE_DSN="host=10.0.0.10 user=gorixpay password=*** dbname=gorixpay port=5432 sslmode=disable" \
  -e GORIXPAY_REDIS_ADDR=10.0.0.11:6379 \
  -e GORIXPAY_MASTER_KEY_FILE=/run/secrets/master_key \
  -v /etc/gorixpay/master_key.txt:/run/secrets/master_key:ro \
  wu-jiyan/gorixpay:latest
```

用 SQLite 就把 `DATABASE_*` 换成挂卷：

```bash
  -e GORIXPAY_DATABASE_DRIVER=sqlite \
  -e GORIXPAY_DATABASE_DSN=/app/data/gorixpay.db \
  -v /var/lib/gorixpay:/app/data \
```

镜像里 `WORKDIR` 是 `/app`，容器以非 root 用户 `gorixpay` 运行（镜像里固定为 UID/GID `10001`），挂进去的目录要让它可写：`chown -R 10001:10001 /var/lib/gorixpay`。

想自己构建镜像：

```bash
docker build -f deploy/Dockerfile -t gorixpay:local .
# 境外网络把依赖源换回官方
docker build -f deploy/Dockerfile \
  --build-arg NPM_REGISTRY=https://registry.npmjs.org \
  --build-arg GOPROXY=https://proxy.golang.org,direct \
  -t gorixpay:local .
```

## 5. 反向代理与 HTTPS

nginx：

```nginx
server {
    listen 443 ssl http2;
    server_name pay.example.com;

    ssl_certificate     /etc/letsencrypt/live/pay.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/pay.example.com/privkey.pem;

    # 迁移 CLI 的导入包、渠道回调大报文
    client_max_body_size 32m;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 60s;
    }
}

server {
    listen 80;
    server_name pay.example.com;
    return 301 https://$host$request_uri;
}
```

Caddy：

```caddyfile
pay.example.com {
    encode zstd gzip
    reverse_proxy 127.0.0.1:8080
}
```

代理后面要注意三件事：`GORIXPAY_PUBLIC_URL` 必须是公网地址（渠道回调、收银台链接都拿它拼）；`X-Forwarded-For` 要透传，风控的 IP 黑名单与限额按它取客户端 IP；网关层不要改写 `?act=`、`?s=` 这类查询串，易支付协议对参数位置敏感。

## 6. 升级、回滚、数据迁移

升级=换二进制或重建镜像，迁移在启动时自动执行，已执行的版本记在 `schema_migrations` 里，重复启动不会重跑。

```bash
# Compose
git pull && docker compose -f deploy/compose.yaml up -d --build
# 源码
systemctl stop gorixpay && install -m 755 gorixpay /opt/gorixpay/gorixpay && systemctl start gorixpay
```

迁移只加不删，所以回滚老版本通常也能启动，但新版本写过的数据老版本不一定认识。**升级前先备份**，回滚顺序永远是「先回滚二进制，再按需从备份恢复」。

从易支付迁数据单独有工具，见 README 的「从易支付迁移数据」。

## 7. 排查

| 现象 | 原因 | 处理 |
| --- | --- | --- |
| `required variable POSTGRES_PASSWORD is missing a value` | 当前 shell 没有该变量，也没 `deploy/.env` | 建 `deploy/.env`，或临时 `POSTGRES_PASSWORD=xxx docker compose ...` |
| `failed to mount ... /run/secrets/...: no such file` | 主密钥文件没生成 | 生成 `deploy/secrets/master_key.txt` |
| 登录后立刻掉线、回调 404 | `GORIXPAY_PUBLIC_URL` 填了 `127.0.0.1` 或 http | 改成公网 https 地址后重启 |
| 容器里 `npm ci` 报 network | 容器访问不了官方源 | 构建时传 `--build-arg NPM_REGISTRY=https://registry.npmmirror.com` |
| 启动报 `migration ... checksum drift` | 改过历史迁移文件 | 恢复原文件；要改就新增一条迁移 |
| 启动报 `master key must contain 32 raw bytes or their base64 encoding` | 密钥文件内容不对（多打了换行以外的字符） | 重新生成，注意是 32 字节或它的 base64 |
| `/health/ready` 一直不 ok | 数据库连不上 | 看 `docker compose logs gorixpay`，先验 DSN |
| admin create 提示需要交互终端 | 没给 `--password-env` 也没 `--password` | 用 `-e GORIXPAY_ADMIN_PASSWORD=...` 加 `--password-env` |

## 8. 生产检查清单

- 主密钥已生成并离线备份，文件权限 600，只有服务账号可读
- `GORIXPAY_ENVIRONMENT=production`，沙箱渠道确认未注册（`GET /api/v1/admin/plugins` 里没有 `sandbox`）
- `GORIXPAY_PUBLIC_URL` 是公网 https 地址，`/health/ready` 从外网可达
- 数据库与主密钥分开备份，恢复演练过一次
- 管理员强密码，建完立刻登录确认
- 反向代理透传 `X-Forwarded-For`，限流与黑名单才有意义
- Redis 配了密码与内网绑定，没暴露公网
- 日志进了 journald 或文件轮转，磁盘有水位告警
- 时区统一 UTC，展示层再转本地时间
