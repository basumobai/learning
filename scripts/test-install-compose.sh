#!/usr/bin/env bash
# Real, disposable installer acceptance suite. Never runs against a user's deployment.
set -Eeuo pipefail
trap 'printf "Real installer acceptance failed at line %s (status %s).\n" "$LINENO" "$?" >&2' ERR
[[ "${GITHUB_ACTIONS:-}" == true ]] || { echo 'This disposable suite runs only in GitHub Actions.' >&2; exit 2; }
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d "${RUNNER_TEMP:-/tmp}/learning-install-XXXXXXXX")"
fixture="$work/project"
export COMPOSE_PROJECT_NAME="learning_ci_${RANDOM}_${RANDOM}"
[[ "$COMPOSE_PROJECT_NAME" =~ ^learning_ci_[0-9]+_[0-9]+$ ]] || exit 2
mkdir -p "$fixture"
git -C "$ROOT" archive HEAD | tar -x -C "$fixture"
cd "$fixture"
cleanup() {
  # Only the randomly named project created by this test can be removed.
  if [[ -f .env && "$COMPOSE_PROJECT_NAME" =~ ^learning_ci_[0-9]+_[0-9]+$ ]]; then
    docker compose down -v --remove-orphans >/dev/null 2>&1 || true
  fi
  rm -rf -- "$work"
}
trap cleanup EXIT
read -r WEB_PORT API_PORT < <(python3 - <<'PY'
import socket
with socket.socket() as a, socket.socket() as b:
    a.bind(('127.0.0.1',0)); b.bind(('127.0.0.1',0))
    print(a.getsockname()[1],b.getsockname()[1])
PY
)
export WEB_PORT API_PORT
export APP_ORIGIN="http://localhost:$WEB_PORT"
run_install() {
  if ! bash scripts/install.sh "$@" >"$work/install.log" 2>&1; then
    tail -60 "$work/install.log" >&2
    docker compose ps >&2
    return 1
  fi
}
echo 'Real installer: fresh deployment'
run_install
cp .env "$work/original.env"
[[ "$(stat -c %a .env)" == 600 ]]
curl -fsS "http://127.0.0.1:$WEB_PORT/api/v1/auth/register" \
  -H "Origin: $APP_ORIGIN" -H 'Content-Type: application/json' \
  --data '{"username":"retention-student","display_name":"Retention","password":"ci-disposable-student-password"}' >/dev/null
docker compose exec -T postgres psql -U els -d els -v ON_ERROR_STOP=1 -c "INSERT INTO ai_providers(id,name,base_url,api_key) VALUES('retention','Retention','https://provider.test/v1',decode('01020304','hex'))" >/dev/null
snapshot() {
  docker compose exec -T postgres psql -U els -d els -tAc "SELECT (SELECT password_hash FROM users WHERE username='retention-student')||':'||(SELECT encode(api_key,'hex') FROM ai_providers WHERE id='retention')"
}
echo 'Real installer: seeded account and AI ciphertext'
before="$(snapshot)"
[[ -n "$before" ]]
echo 'Real installer: repeated deployment and upgrade'
run_install
run_install --update
cmp .env "$work/original.env"
[[ "$(snapshot)" == "$before" ]]

# The configuration changes; the stored database password must not change automatically.
echo 'Real installer: password conflict must be detected'
sed -i 's/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=deliberately_wrong_ci_password/' .env
if bash scripts/install.sh --recover >"$work/conflict.log" 2>&1; then echo 'Password conflict was not detected' >&2; exit 1; fi
grep -q '数据库认证检查失败' "$work/conflict.log"
[[ "$(snapshot)" == "$before" ]]
echo 'Real installer: restored configuration and recovery'
cp "$work/original.env" .env
run_install --recover
[[ "$(snapshot)" == "$before" ]]

echo 'Real installer: existing volume without configuration'
mv .env "$work/missing.env"
if bash scripts/install.sh --config-only >"$work/missing.log" 2>&1; then echo 'Generated new credentials over existing data' >&2; exit 1; fi
[[ ! -e .env ]]
grep -q '缺失 .env' "$work/missing.log"
mv "$work/missing.env" .env
cmp .env "$work/original.env"
[[ "$(snapshot)" == "$before" ]]

echo 'Real installer: verifying four healthy services'
healthy=0
for (( attempt=1; attempt<=30; attempt++ )); do
  if docker compose ps --format json | python3 -c 'import json,sys; rows=[json.loads(line) for line in sys.stdin if line.strip()]; raise SystemExit(0 if len(rows)==4 and all(r["Health"]=="healthy" for r in rows) else 1)'; then healthy=1; break; fi
  sleep 2
done
(( healthy )) || { echo 'Core services did not become healthy' >&2; docker compose ps >&2; exit 1; }
echo 'Real installer acceptance passed: fresh install, repeat, update, credential conflict, recovery, missing configuration, retained accounts/AI ciphertext, four healthy services.'
