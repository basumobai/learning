#!/usr/bin/env bash
# No Docker/network needed: test setup against isolated temporary fixtures.
set -Eeuo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf -- "$WORK"' EXIT
mkdir -p "$WORK/scripts" "$WORK/fakebin"
cp "$ROOT/scripts/install.sh" "$WORK/scripts/install.sh"
cp "$ROOT/.env.example" "$WORK/.env.example"
bash -n "$WORK/scripts/install.sh"
bash "$WORK/scripts/install.sh" --help >/dev/null
first="$(bash "$WORK/scripts/install.sh" --config-only)"
[[ -f "$WORK/.env" ]] || { echo 'missing generated .env'; exit 1; }
[[ "$first" == *"初始密码"* ]] || { echo 'initial login info missing'; exit 1; }
[[ "$(stat -c %a "$WORK/.env")" == 600 ]] || { echo 'incorrect permissions'; exit 1; }
for key in POSTGRES_PASSWORD ELS_ADMIN_PASSWORD APP_ENCRYPTION_KEY APP_ORIGIN; do
  grep -q "^${key}=." "$WORK/.env" || { echo "empty $key"; exit 1; }
done
cp "$WORK/.env" "$WORK/snapshot"
second="$(bash "$WORK/scripts/install.sh" --config-only)"
cmp "$WORK/.env" "$WORK/snapshot"
[[ "$second" != *"初始密码"* ]] || { echo 'leaked existing password'; exit 1; }

# Reject missing/invalid keys rather than silently regenerating them.
sed -i 's/^APP_ENCRYPTION_KEY=.*/APP_ENCRYPTION_KEY=invalid/' "$WORK/.env"
if bash "$WORK/scripts/install.sh" --config-only >"$WORK/fail.log" 2>&1; then
  echo 'accepted invalid encryption key'
  exit 1
fi
grep -q 'APP_ENCRYPTION_KEY' "$WORK/fail.log"
cp "$WORK/snapshot" "$WORK/.env"

# Simulate a full startup without touching host Docker or network.
cat > "$WORK/fakebin/docker" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$MOCK_DOCKER_LOG"
if [[ "$1" == info ]]; then exit 0; fi
if [[ "$1" == compose ]]; then exit 0; fi
exit 1
MOCK
cat > "$WORK/fakebin/curl" <<'MOCK'
#!/usr/bin/env bash
exit 0
MOCK
chmod +x "$WORK/fakebin/docker" "$WORK/fakebin/curl"
export MOCK_DOCKER_LOG="$WORK/docker.log"
PATH="$WORK/fakebin:$PATH" bash "$WORK/scripts/install.sh" >"$WORK/install.log"
grep -q '^compose config --quiet$' "$MOCK_DOCKER_LOG"
grep -q '^compose up --build -d$' "$MOCK_DOCKER_LOG"
grep -q '安装完成' "$WORK/install.log"
cmp "$WORK/.env" "$WORK/snapshot"
echo 'Installer checks passed: generated secrets, permissions, idempotence, invalid key rejection, Compose flow.'
