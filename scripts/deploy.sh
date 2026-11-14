#!/usr/bin/env bash
# GorixPay 一键部署脚本（Linux / macOS，bash 4+）
# 用法：bash scripts/deploy.sh [--dry-run]
#
# 做的事：
#   1. 检查 docker 与 docker compose v2，缺了就打印安装方式并退出 2；
#   2. 准备 deploy/.env（首次运行写入随机 PostgreSQL 口令，然后提示改对外地址）；
#   3. 准备 deploy/secrets/master_key.txt（32 字节随机数的 base64，权限 600）；
#   4. docker compose up -d --build；
#   5. 轮询 http://127.0.0.1:8080/health/ready，最多 180 秒。
#
# 脚本可以重复执行：已存在的 deploy/.env、deploy/secrets/master_key.txt 一律不覆盖。

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEPLOY_DIR="${ROOT_DIR}/deploy"
ENV_EXAMPLE="${DEPLOY_DIR}/.env.example"
ENV_FILE="${DEPLOY_DIR}/.env"
SECRETS_DIR="${DEPLOY_DIR}/secrets"
MASTER_KEY_FILE="${SECRETS_DIR}/master_key.txt"
COMPOSE_REL="deploy/compose.yaml"
HEALTH_URL="http://127.0.0.1:8080/health/ready"
HEALTH_TIMEOUT_SECONDS=180
PLACEHOLDER_PUBLIC_URL="https://pay.example.com"

DRY_RUN=0

usage() {
  cat <<'EOF'
用法：bash scripts/deploy.sh [--dry-run]

  --dry-run   只打印将要执行的步骤，不写文件、不启动容器
  -h, --help  显示本帮助

重复执行是安全的：deploy/.env 与 deploy/secrets/master_key.txt 存在时不会被覆盖。
EOF
}

step() { printf '\n== %s ==\n' "$1"; }
info() { printf '  %s\n' "$1"; }

plan() { printf '  [dry-run] %s\n' "$1"; }

random_password() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 16
    return 0
  fi
  head -c 16 /dev/urandom | base64 | tr -d '\n=+/'
}

random_base64_32() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 32
    return 0
  fi
  head -c 32 /dev/urandom | base64
}

read_env_value() {
  local file="$1" key="$2" line
  line="$(grep -m1 -E "^${key}=" "${file}" 2>/dev/null || true)"
  if [ -z "${line}" ]; then
    printf ''
    return 0
  fi
  printf '%s' "${line#*=}" | tr -d '\r'
}

set_env_value() {
  local file="$1" key="$2" value="$3" tmp="${1}.tmp"
  awk -v key="${key}" -v value="${value}" '
    BEGIN { replaced = 0 }
    index($0, key "=") == 1 { print key "=" value; replaced = 1; next }
    { print }
    END { if (replaced == 0) print key "=" value }
  ' "${file}" > "${tmp}"
  mv "${tmp}" "${file}"
}

health_probe() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsS --max-time 3 "${HEALTH_URL}" >/dev/null 2>&1
    return $?
  fi
  wget -q -T 3 -O /dev/null "${HEALTH_URL}" >/dev/null 2>&1
}

while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY_RUN=1 ;;
    -h|--help) usage; exit 0 ;;
    *)
      printf '未知参数：%s\n\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

cd "${ROOT_DIR}"

step "检查 Docker"
if ! command -v docker >/dev/null 2>&1; then
  cat >&2 <<'EOF'
未找到 docker。安装方式：
  Ubuntu / Debian: sudo apt-get update && sudo apt-get install -y docker.io docker-compose-plugin
  Fedora / RHEL:   sudo dnf install -y docker docker-compose-plugin
  其他发行版:      https://docs.docker.com/engine/install/
装完后确认 docker 服务已启动：sudo systemctl enable --now docker
EOF
  exit 2
fi
if ! docker compose version >/dev/null 2>&1; then
  cat >&2 <<'EOF'
未找到 docker compose 插件（v2）。安装方式：
  Ubuntu / Debian: sudo apt-get install -y docker-compose-plugin
  Fedora / RHEL:   sudo dnf install -y docker-compose-plugin
  其他发行版:      https://docs.docker.com/compose/install/
Compose 老版本（docker-compose v1）不支持本脚本用的命令，请装上 v2 插件。
EOF
  exit 2
fi
info "docker 与 docker compose 就绪"

step "准备 deploy/.env"
if [ -f "${ENV_FILE}" ]; then
  info "deploy/.env 已存在，保持不变"
else
  if [ "${DRY_RUN}" -eq 1 ]; then
    plan "复制 deploy/.env.example 到 deploy/.env"
    plan "写入随机 POSTGRES_PASSWORD（16 字节十六进制，不打印）"
    plan "提示修改 GORIXPAY_PUBLIC_URL 并退出 0"
  else
    generated_password="$(random_password)"
    cp "${ENV_EXAMPLE}" "${ENV_FILE}"
    set_env_value "${ENV_FILE}" "POSTGRES_PASSWORD" "${generated_password}"
    chmod 600 "${ENV_FILE}"
    info "已生成 deploy/.env 并写入随机 PostgreSQL 口令（不显示数值）"
    info "下一步：编辑 deploy/.env，把 GORIXPAY_PUBLIC_URL 改成真实地址，然后重新执行本脚本。"
    exit 0
  fi
fi

step "检查对外地址"
public_url=""
if [ -f "${ENV_FILE}" ]; then
  public_url="$(read_env_value "${ENV_FILE}" "GORIXPAY_PUBLIC_URL")"
fi
if [ -z "${public_url}" ] || [ "${public_url}" = "${PLACEHOLDER_PUBLIC_URL}" ]; then
  if [ "${DRY_RUN}" -eq 1 ]; then
    plan "GORIXPAY_PUBLIC_URL 还是占位值，真实运行到这里会停下并提示修改后重跑"
    public_url="${PLACEHOLDER_PUBLIC_URL}"
  else
    info "deploy/.env 里的 GORIXPAY_PUBLIC_URL 还是占位值（${PLACEHOLDER_PUBLIC_URL}）"
    info "改成真实地址后重新执行本脚本。"
    exit 0
  fi
else
  info "对外地址：${public_url}"
fi

postgres_password=""
if [ -f "${ENV_FILE}" ]; then
  postgres_password="$(read_env_value "${ENV_FILE}" "POSTGRES_PASSWORD")"
fi
if [ "${DRY_RUN}" -eq 0 ] && [ -z "${postgres_password}" ]; then
  info "deploy/.env 里的 POSTGRES_PASSWORD 为空，请填一个随机口令后重跑。"
  exit 2
fi

step "准备 deploy/secrets"
if [ "${DRY_RUN}" -eq 1 ]; then
  plan "创建目录 deploy/secrets（已存在则跳过）"
  plan "不存在时生成 deploy/secrets/master_key.txt：32 字节随机数的 base64，权限 600"
  plan "已存在时保持原样，不覆盖"
else
  mkdir -p "${SECRETS_DIR}"
  if [ -f "${MASTER_KEY_FILE}" ]; then
    info "deploy/secrets/master_key.txt 已存在，保持不变"
  else
    random_base64_32 > "${MASTER_KEY_FILE}"
    chmod 600 "${MASTER_KEY_FILE}"
    info "已生成 deploy/secrets/master_key.txt（内容不显示，丢失后已加密字段无法解密）"
  fi
  chmod 600 "${ENV_FILE}" 2>/dev/null || true
fi

step "启动容器"
if [ "${DRY_RUN}" -eq 1 ]; then
  plan "docker compose -f ${COMPOSE_REL} up -d --build"
else
  info "+ docker compose -f ${COMPOSE_REL} up -d --build"
  docker compose -f "${COMPOSE_REL}" up -d --build
fi

step "等待健康检查"
if [ "${DRY_RUN}" -eq 1 ]; then
  plan "轮询 ${HEALTH_URL}，最多 ${HEALTH_TIMEOUT_SECONDS} 秒，每 3 秒一次"
else
  if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
    info "健康检查需要 curl 或 wget，两者都没找到，装一个后重跑。"
    exit 2
  fi
  deadline=$(( $(date +%s) + HEALTH_TIMEOUT_SECONDS ))
  healthy=0
  while :; do
    if health_probe; then
      healthy=1
      break
    fi
    if [ "$(date +%s)" -ge "${deadline}" ]; then
      break
    fi
    printf '.'
    sleep 3
  done
  printf '\n'
  if [ "${healthy}" -ne 1 ]; then
    info "健康检查 ${HEALTH_TIMEOUT_SECONDS} 秒内没有通过，下面是 gorixpay 最后 50 行日志："
    docker compose -f "${COMPOSE_REL}" logs --tail 50 gorixpay || true
    exit 1
  fi
  info "健康检查通过"
fi

step "部署完成"
info "访问地址：${public_url}"
info "运营后台：${public_url}/admin/login"
info "数据卷：deploy_postgres_data、deploy_redis_data"
info "查看卷路径：docker volume inspect deploy_postgres_data --format '{{.Mountpoint}}'"
info "删除卷会连数据一起删掉：docker compose -f ${COMPOSE_REL} down -v"
cat <<'EOF'

创建管理员（在仓库根目录执行，会交互式提示输入口令，至少 8 位）：
  docker compose -f deploy/compose.yaml exec -it gorixpay gorixpay admin create \
    --username admin --display-name 管理员

主密钥从容器内的 /run/secrets/master_key 读取，不需要再传 --master-key-file。
EOF
