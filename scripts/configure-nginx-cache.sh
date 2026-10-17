#!/usr/bin/env bash
# Fix stale Learning admin HTML caused by inherited Nginx proxy caching.
set -Eeuo pipefail

FILE="/www/server/panel/vhost/nginx/proxy/www.sumobai.com/97644b88e388b43cb81e6f8fb317c93e_www.sumobai.com.conf"
NGINX="/www/server/nginx/sbin/nginx"
APPLY=0
while (( $# )); do
  case "$1" in
    --apply) APPLY=1; shift ;;
    --file) [[ $# -ge 2 ]] || { echo "Missing path for --file" >&2; exit 2; }; FILE="$2"; shift 2 ;;
    -h|--help) echo "Usage: bash scripts/configure-nginx-cache.sh [--apply] [--file PATH]"; exit 0 ;;
    *) echo "Unknown argument" >&2; exit 2 ;;
  esac
done
[[ -f "$FILE" && ! -L "$FILE" ]] || { echo "Learning proxy config not found" >&2; exit 1; }
[[ "$FILE" == /www/server/panel/vhost/nginx/proxy/www.sumobai.com/*.conf ]] || {
  echo "Refusing non-Learning proxy config" >&2; exit 1;
}
[[ -x "$NGINX" ]] || { echo "Expected BaoTa Nginx not found" >&2; exit 1; }
command -v python3 >/dev/null || { echo "Python 3 required" >&2; exit 1; }
export LEARNING_PROXY_FILE="$FILE"

if (( ! APPLY )); then
  python3 - <<'PY'
import os
from pathlib import Path
s = Path(os.environ["LEARNING_PROXY_FILE"]).read_text()
if s.count("proxy_pass http://127.0.0.1:9090;") != 1:
    raise SystemExit("Unexpected Learning proxy target; refusing changes")
print("DRY RUN: will add proxy_cache off, proxy_cache_bypass 1, proxy_no_cache 1")
PY
  echo "Run with --apply to modify configuration."
  exit 0
fi

BACKUP="$(mktemp -p "$(dirname "$FILE")" .learning-proxy-backup.XXXXXXXX)"
cp -a "$FILE" "$BACKUP"
rollback() { cp -a "$BACKUP" "$FILE"; echo "Restored config from backup" >&2; }
trap rollback ERR
python3 - <<'PY'
import os
from pathlib import Path
p = Path(os.environ["LEARNING_PROXY_FILE"])
s = p.read_text()
anchor = "    proxy_pass http://127.0.0.1:9090;"
if s.count(anchor) != 1:
    raise SystemExit("Unexpected Learning proxy target; refusing changes")
for directive in ("proxy_cache off;", "proxy_cache_bypass 1;", "proxy_no_cache 1;"):
    if directive not in s:
        s = s.replace(anchor, "    " + directive + "\n" + anchor, 1)
p.write_text(s)
PY
"$NGINX" -t
"$NGINX" -s reload
trap - ERR
echo "Learning proxy caching disabled. Backup: $BACKUP"
