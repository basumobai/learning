#!/usr/bin/env bash
# Safe first-run setup for the local-only Docker Compose deployment.
set -Eeuo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="$ROOT/.env"
CONFIG_ONLY=0

usage() {
  cat <<'HELP'
Learning installer (run from any directory)

  bash scripts/install.sh                 Generate .env if absent and start Docker Compose
  bash scripts/install.sh --config-only   Generate/validate .env without starting Docker
  bash scripts/install.sh --help          Show this help

Requires Docker Engine, Docker Compose v2, OpenSSL and curl for full install.
Existing .env, database volumes and accounts are never reset.
The app binds only to 127.0.0.1 by default; use an SSH tunnel for remote access.
HELP
}
die() { printf '错误：%s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "缺少 $1；请先安装后重新运行。"; }
read_env() {
  local key="$1"
  sed -n "s/^${key}=//p" "$ENV_FILE" | tail -n 1
}
normalize_quoted() {
  local v="$1"
  if [[ "$v" == \"*\" && "${#v}" -ge 2 ]]; then
    v="${v:1:${#v}-2}"
  elif [[ "$v" == \'*\' && "${#v}" -ge 2 ]]; then
    v="${v:1:${#v}-2}"
  fi
  printf '%s' "$v"
}

if (( $# > 1 )); then usage >&2; exit 2; fi
case "${1:-}" in
  "") ;;
  --config-only) CONFIG_ONLY=1 ;;
  -h|--help) usage; exit 0 ;;
  *) usage >&2; exit 2 ;;
esac

need openssl
umask 077
new_install=0
new_admin_password=""

if [[ ! -e "$ENV_FILE" ]]; then
  [[ -f "$ROOT/.env.example" ]] || die "找不到 .env.example；请在完整仓库内运行。"
  database_password="$(openssl rand -hex 24)"
  new_admin_password="$(openssl rand -hex 20)"
  encryption_key="$(openssl rand -base64 32 | tr -d '\n')"
  temp_file="$(mktemp "$ROOT/.env.tmp.XXXXXXXX")"
  trap 'rm -f -- "$temp_file"' EXIT
  cat > "$temp_file" <<EOF
# Generated locally. Do not commit this file or share it.
# The encryption key must remain unchanged while encrypted data exists.
POSTGRES_PASSWORD=$database_password
APP_ORIGIN=http://localhost:3000
ELS_ADMIN_USERNAME=admin
ELS_ADMIN_PASSWORD=$new_admin_password
APP_ENCRYPTION_KEY=$encryption_key
EOF
  # Exclusive creation: never overwrite a concurrently created or existing .env.
  if (set -C; cat "$temp_file" > "$ENV_FILE") 2>/dev/null; then
    chmod 600 "$ENV_FILE"
    new_install=1
    printf '已创建 .env（仅当前用户可读）。\n'
  else
    die ".env 已被创建；为保护原有配置，未覆盖。请检查后重试。"
  fi
else
  [[ -f "$ENV_FILE" && ! -L "$ENV_FILE" ]] || die ".env 必须是普通文件，拒绝覆盖符号链接。"
  chmod 600 "$ENV_FILE"
  printf '检测到现有 .env：保留全部配置与密钥。\n'
fi

for key in POSTGRES_PASSWORD APP_ORIGIN ELS_ADMIN_USERNAME ELS_ADMIN_PASSWORD APP_ENCRYPTION_KEY; do
  value="$(normalize_quoted "$(read_env "$key")")"
  [[ -n "$value" ]] || die ".env 的 $key 缺失或为空；请先填写有效配置。"
done

db_password="$(normalize_quoted "$(read_env POSTGRES_PASSWORD)")"
admin_password="$(normalize_quoted "$(read_env ELS_ADMIN_PASSWORD)")"
encryption_key="$(normalize_quoted "$(read_env APP_ENCRYPTION_KEY)")"
[[ "$db_password" =~ ^[a-zA-Z0-9_-]{16,}$ ]] ||
  die "POSTGRES_PASSWORD 应至少 16 位，且仅使用英文、数字、_、-（用于数据库 URL）。"
case "$db_password" in
  replace-with-a-long-alphanumeric-password|change-this-database-password|els_local_only)
    die "POSTGRES_PASSWORD 仍是示例或开发默认值，请改为独立的随机密码。"
    ;;
esac
(( ${#admin_password} >= 15 && ${#admin_password} <= 128 )) ||
  die "ELS_ADMIN_PASSWORD 必须为 15–128 个字符。"
if ! key_bytes="$(printf '%s' "$encryption_key" | openssl base64 -d -A 2>/dev/null | wc -c | tr -d '[:space:]')"; then
  die "APP_ENCRYPTION_KEY 不是有效 Base64；请使用 openssl rand -base64 32。"
fi
[[ "$key_bytes" == "32" ]] ||
  die "APP_ENCRYPTION_KEY 必须是 Base64 编码的 32 字节密钥；不要随意更换旧密钥。"
unset db_password admin_password encryption_key key_bytes

if (( new_install )); then
  printf '\n管理员：admin\n初始密码（仅本次显示，请立即安全保存）：%s\n' "$new_admin_password"
  printf '密码也保存在权限为 600 的 .env 中；首次登录后建议修改密码。\n\n'
fi
unset new_admin_password

if (( CONFIG_ONLY )); then
  printf '配置已验证。启动时运行：docker compose up --build -d\n'
  exit 0
fi

need docker
need curl
docker info >/dev/null 2>&1 || die "Docker 守护进程不可用，或当前用户无访问权限。"
docker compose version >/dev/null 2>&1 || die "需要 Docker Compose v2 插件（使用 docker compose，而非 docker-compose）。"
cd "$ROOT"
docker compose config --quiet || die "Compose 配置校验失败；请检查 .env。"
printf '正在启动 Learning 服务…\n'
docker compose up --build -d || die "启动失败。使用 docker compose ps 和 docker compose logs --tail=100 查看原因。"

healthy=0
for (( attempt=1; attempt<=45; attempt++ )); do
  if curl -fsS --max-time 2 http://127.0.0.1:8080/api/v1/health >/dev/null 2>&1 &&
     curl -fsS --max-time 2 http://127.0.0.1:3000/ >/dev/null 2>&1; then
    healthy=1
    break
  fi
  sleep 2
done
if (( ! healthy )); then
  docker compose ps
  printf '启动后健康检查未通过，请运行 docker compose logs --tail=100。数据库卷与密钥未被重置。\n' >&2
  exit 1
fi

printf '\n安装完成！本机访问：http://localhost:3000\n'
printf '远程 VPS 请通过 SSH 隧道访问；对外开放前务必配置 HTTPS 反向代理与正确的 APP_ORIGIN。\n'
printf '常用命令：docker compose ps | docker compose logs -f --tail=100 | docker compose down\n'
