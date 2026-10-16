#!/usr/bin/env bash
# Safe first-run setup for the local-only Docker Compose deployment.
set -Eeuo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="$ROOT/.env"
CONFIG_ONLY=0
MODE=auto
EXPECTED_ORIGIN="${APP_ORIGIN:-}"

usage() {
  cat <<'HELP'
Learning installer (run from any directory)

  bash scripts/install.sh                 Generate .env if absent and start Docker Compose
  bash scripts/install.sh --config-only   Generate/validate .env without starting Docker
  bash scripts/install.sh --origin https://www.sumobai.com
  bash scripts/install.sh --update        Upgrade an existing configured installation
  bash scripts/install.sh --recover       Check credentials before restarting services
  bash scripts/install.sh --help          Show this help

Requires Docker Engine, Docker Compose v2, Python 3, OpenSSL and curl for full install.
Even --config-only checks Docker volumes before generating new secrets.
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

while (( $# )); do
  case "$1" in
    --config-only) CONFIG_ONLY=1; shift ;;
    --update) MODE=update; shift ;;
    --recover) MODE=recover; shift ;;
    --origin) (( $# >= 2 )) || die "--origin 缺少域名"; EXPECTED_ORIGIN="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) usage >&2; exit 2 ;;
  esac
done

need openssl
need python3
need docker
docker info >/dev/null 2>&1 || die "Docker 不可用，无法安全判断旧数据库是否存在；请启动 Docker 并确认当前用户权限后重试。未生成新密钥。"
docker compose version >/dev/null 2>&1 || die "需要 Docker Compose v2 插件。"
cd "$ROOT"
# Do not render Compose before .env is recovered: required secrets may be absent.
# Include orphaned/renamed projects so moving a checkout cannot silently strand data.
volume_names="$(docker volume ls --format '{{.Name}}' 2>/dev/null)" || die "无法检查数据库卷；未生成新密钥。"
labelled_volumes="$(docker volume ls --filter label=com.docker.compose.volume=postgres_data --format '{{.Name}}' 2>/dev/null)" || die "无法检查数据库卷标签。"
has_database=0
if [[ -n "$labelled_volumes" ]] || [[ "$volume_names" =~ (^|$'\n')([a-zA-Z0-9_.-]+_)?postgres_data($|$'\n') ]]; then
  has_database=1
fi
if [[ ! -e "$ENV_FILE" ]]; then
  (( ! has_database )) || die "检测到已有 PostgreSQL 数据卷，但缺失 .env。已停止，未生成密码/加密密钥。请从原部署或备份恢复完整 .env（尤其 POSTGRES_PASSWORD、APP_ENCRYPTION_KEY），确认原 Compose 项目名后运行 --recover。不要删除卷。详见 docs/DEPLOY.md。"
  [[ "$MODE" == auto ]] || die "--$MODE 需要原有 .env；请先恢复配置，不能当作全新安装。"
fi
port_valid() {
  [[ "$1" =~ ^[1-9][0-9]{3,4}$ ]] && (( $1 >= 1024 && $1 <= 65535 ))
}
port_available() {
  local port="$1" listeners
  if command -v ss >/dev/null 2>&1; then
    listeners="$(ss -H -ltn "( sport = :$port )" 2>&1)" || die "无法检查监听端口；请检查 ss。"
    [[ -z "$listeners" ]]
  else
    need python3
    PORT_TO_TEST="$port" python3 - <<'PY'
import os, socket, sys
with socket.socket() as sock:
    try:
        sock.bind(("127.0.0.1", int(os.environ["PORT_TO_TEST"])))
    except OSError:
        sys.exit(1)
PY
  fi
}
choose_port() {
  local label="$1" current="$2" excluded="${3:-}" selection
  while true; do
    if [[ -t 0 ]]; then
      read -r -p "$label [$current]: " selection || die "端口选择已取消；尚未生成配置。"
      selection="${selection:-$current}"
    else
      selection="$current"
    fi
    port_valid "$selection" || { [[ -t 0 ]] || die "$label 端口无效"; echo "端口需在 1024–65535 之间，不能含前导零。" >&2; continue; }
    if [[ "$selection" == "$excluded" ]]; then
      [[ -t 0 ]] || die "Web 与 API 端口不可相同"
      echo "Web 与 API 端口不可相同，请换一个。" >&2
      continue
    fi
    if (( ! CONFIG_ONLY )) && ! port_available "$selection"; then
      [[ -t 0 ]] || die "$label 端口 $selection 已被占用"
      echo "端口 $selection 已占用，请换一个。" >&2
      continue
    fi
    printf '%s' "$selection"
    return
  done
}
umask 077
new_install=0
new_admin_password=""

if [[ ! -e "$ENV_FILE" ]]; then
  [[ -f "$ROOT/.env.example" ]] || die "找不到 .env.example；请在完整仓库内运行。"
  # Choose before creating .env so a cancelled/conflicting install can be retried.
  web_port="$(choose_port 'Web 访问端口' "${WEB_PORT:-3000}")"
  api_port="$(choose_port 'API 端口' "${API_PORT:-8080}" "$web_port")"
  database_password="$(openssl rand -hex 24)"
  new_admin_password="$(openssl rand -hex 20)"
  encryption_key="$(openssl rand -base64 32 | tr -d '\n')"
  temp_file="$(mktemp "$ROOT/.env.tmp.XXXXXXXX")"
  trap 'rm -f -- "$temp_file"' EXIT
  cat > "$temp_file" <<EOF
# Generated locally. Do not commit this file or share it.
# The encryption key must remain unchanged while encrypted data exists.
POSTGRES_PASSWORD=$database_password
APP_ORIGIN=${EXPECTED_ORIGIN:-http://localhost:$web_port}
WEB_PORT=$web_port
API_PORT=$api_port
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

if (( ! new_install )); then
  # Match Compose defaults for deployments created before configurable ports.
  web_port="$(normalize_quoted "$(read_env WEB_PORT)")"
  api_port="$(normalize_quoted "$(read_env API_PORT)")"
  web_port="${web_port:-3000}"
  api_port="${api_port:-8080}"
  port_valid "$web_port" && port_valid "$api_port" || die "端口需在 1024–65535 之间，不能含前导零"
fi
[[ "$web_port" != "$api_port" ]] || die "Web 与 API 端口不可相同"
# Compose and health probes must use the same values even with exported shell vars.
export WEB_PORT="$web_port" API_PORT="$api_port"

for key in POSTGRES_PASSWORD APP_ORIGIN ELS_ADMIN_USERNAME ELS_ADMIN_PASSWORD APP_ENCRYPTION_KEY; do
  value="$(normalize_quoted "$(read_env "$key")")"
  [[ -n "$value" ]] || die ".env 的 $key 缺失或为空；请先填写有效配置。"
done

app_origin="$(normalize_quoted "$(read_env APP_ORIGIN)")"
ORIGIN_TO_CHECK="$app_origin" python3 - <<'PY' || die "APP_ORIGIN 必须是单一 HTTPS 域名（本机允许 HTTP），不能带路径、查询或用户信息。"
import os, urllib.parse
u = urllib.parse.urlsplit(os.environ['ORIGIN_TO_CHECK'])
try:
    valid = (u.scheme in ('http', 'https') and bool(u.hostname) and not u.username
             and not u.password and u.path in ('', '/') and not u.query and not u.fragment
             and u.port != 0 and not any(c.isspace() for c in u.netloc)
             and (u.scheme == 'https' or u.hostname in ('localhost', '127.0.0.1')))
except ValueError:
    valid = False
raise SystemExit(0 if valid else 1)
PY
[[ -z "$EXPECTED_ORIGIN" || "${EXPECTED_ORIGIN%/}" == "${app_origin%/}" ]] || die "指定域名与 .env 的 APP_ORIGIN 不一致。请核对正式域名后仅修改 APP_ORIGIN，再重新运行；原有密钥未覆盖。"
if [[ "$app_origin" == http://localhost:* && "$app_origin" != "http://localhost:$web_port" ]]; then
  die "APP_ORIGIN 的本机端口与 WEB_PORT 不一致，请核对 .env。"
fi
printf '访问来源：%s（必须与浏览器地址一致）\n' "$app_origin"

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
# .env is authoritative; ignore unrelated exported credentials from this shell.
export POSTGRES_PASSWORD="$db_password" ELS_ADMIN_PASSWORD="$admin_password"
export APP_ENCRYPTION_KEY="$encryption_key" APP_ORIGIN="$app_origin"
export ELS_ADMIN_USERNAME="$(normalize_quoted "$(read_env ELS_ADMIN_USERNAME)")"
unset admin_password encryption_key key_bytes

if (( new_install )); then
  printf '\n管理员：admin\n初始密码（仅本次显示，请立即安全保存）：%s\n' "$new_admin_password"
  printf '密码也保存在权限为 600 的 .env 中；首次登录后建议修改密码。\n\n'
fi
unset new_admin_password

if (( CONFIG_ONLY )); then
  printf '配置已验证。启动时运行：docker compose up --build -d\n'
  exit 0
fi

need curl
docker compose config --quiet >/dev/null 2>&1 || die "Compose 配置校验失败；请核对 .env 的字段和 docs/DEPLOY.md（不要公开配置输出）。"
# Refuse an accidental project rename when known data belongs to a different project.
expected_volume="$(docker compose config --format json 2>/dev/null | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["volumes"]["postgres_data"]["name"])' 2>/dev/null)" || die "无法识别当前 Compose 数据卷，请检查项目配置。"
if (( has_database )); then
  if ! printf '%s\n' "$volume_names" | grep -Fxq -- "$expected_volume"; then
    die "检测到旧数据库，但当前项目将使用不同的数据卷。请恢复原 COMPOSE_PROJECT_NAME 或在原部署目录运行；未创建新数据库。"
  fi
fi
check_service_port() {
  local service="$1" container_port="$2" host_port="$3" published
  # A running service belonging to THIS Compose project may reuse its mapping.
  published="$(docker compose port "$service" "$container_port" 2>/dev/null || true)"
  [[ "$published" == "127.0.0.1:$host_port" ]] && return 0
  port_available "$host_port" || die "$service 端口 $host_port 已被其他服务占用；请修改 .env 中的端口，不要停止其他业务。"
}
check_service_port web 3000 "$web_port"
check_service_port api 8080 "$api_port"
printf '正在启动 Learning 服务…\n'
printf '部署模式：%s；数据库与密钥保留。\n' "$MODE"
docker compose up -d postgres redis || die "数据库/缓存启动失败；请检查 Docker 磁盘空间和容器状态。"
database_ready=0
for (( attempt=1; attempt<=30; attempt++ )); do
  if docker compose exec -T postgres pg_isready -U els -d els >/dev/null 2>&1; then database_ready=1; break; fi
  sleep 2
done
(( database_ready )) || die "PostgreSQL 未就绪；请检查 docker compose ps 与数据库日志，不要删除卷。"
# TCP authentication is required: pg_isready and local socket trust do not verify a password.
# Pass the secret through stdin, never as a process argument or diagnostic output.
if ! printf '%s\n' "$db_password" | docker compose exec -T postgres sh -c 'IFS= read -r PGPASSWORD; export PGPASSWORD; exec psql -h 127.0.0.1 -U els -d els -w -tAc "SELECT 1"' >/dev/null 2>&1; then
  die "数据库认证检查失败（密码不一致、数据库缺失或 TCP 认证配置异常）。未重建数据库或轮换密钥。先恢复原 .env；如需人工同步密码，运行 docker compose exec postgres psql -U els -d els，再使用交互式 \\password els，详见 docs/DEPLOY.md。"
fi
unset db_password
docker compose up --build -d || die "启动失败。使用 docker compose ps 和 docker compose logs --tail=100 查看原因。"

healthy=0
for (( attempt=1; attempt<=45; attempt++ )); do
  if curl -fsS --max-time 2 http://127.0.0.1:${api_port}/api/v1/health >/dev/null 2>&1 &&
     curl -fsS --max-time 2 http://127.0.0.1:${web_port}/api/v1/health >/dev/null 2>&1; then
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

printf '\n安装完成！本机访问：http://localhost:%s\n' "$web_port"
printf '远程 VPS 请通过 SSH 隧道访问；对外开放前务必配置 HTTPS 反向代理与正确的 APP_ORIGIN。\n'
printf '常用命令：docker compose ps | docker compose logs -f --tail=100 | docker compose down\n'
