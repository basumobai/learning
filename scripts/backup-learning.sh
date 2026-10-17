#!/usr/bin/env bash
set -euo pipefail
umask 077
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"
OUT=${1:?Usage: bash scripts/backup-learning.sh /private/new-backup-directory}
mkdir -m 700 -- "$OUT"
OUT=$(cd -- "$OUT" && pwd)
test -f .env || { echo 'A local .env is required so the encryption key is included.' >&2; exit 1; }
PDF_DIR=$(docker compose config --format json | python3 -c 'import json,sys; c=json.load(sys.stdin); print(next(v["source"] for v in c["services"]["api"]["volumes"] if v["target"]=="/data/pdf"))')
python3 - "$PDF_DIR" <<'PY'
from pathlib import Path
import sys
root=Path(sys.argv[1])
if any(p.is_symlink() for p in root.rglob('*')):
 raise SystemExit('PDF backup requires a directory without symbolic links.')
PY
# Hold one exported PostgreSQL snapshot for both dump and logical checks.
coproc HOLDER { docker compose exec -T postgres psql -XqAt -v ON_ERROR_STOP=1 -U els -d els; }
HOLDER_PROCESS=$HOLDER_PID
exec {SNAP_IN}>&"${HOLDER[1]}"
exec {SNAP_OUT}<&"${HOLDER[0]}"
cleanup() { printf '\\q\n' >&"$SNAP_IN" 2>/dev/null || true; wait "$HOLDER_PROCESS" 2>/dev/null || true; }
trap cleanup EXIT
printf 'BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;\nSELECT pg_export_snapshot();\n' >&"$SNAP_IN"
read -r SNAPSHOT <&"$SNAP_OUT"
[[ "$SNAPSHOT" =~ ^[A-Fa-f0-9-]+$ ]] || { echo 'Invalid snapshot response' >&2; exit 1; }
docker compose exec -T postgres pg_dump -U els -d els --format=custom --no-owner --snapshot="$SNAPSHOT" > "$OUT/database.dump"
docker compose exec -T postgres psql -XqAt -v ON_ERROR_STOP=1 -U els -d els -c "BEGIN ISOLATION LEVEL REPEATABLE READ; SET TRANSACTION SNAPSHOT '$SNAPSHOT';" -f /dev/stdin -c ROLLBACK < scripts/backup-snapshot.sql > "$OUT/snapshot.tsv"
cp -- .env "$OUT/environment.env"
chmod 600 "$OUT/environment.env"
tar -czf "$OUT/pdf.tar.gz" -C "$PDF_DIR" .
(cd "$OUT" && sha256sum database.dump environment.env pdf.tar.gz snapshot.tsv > SHA256SUMS)
printf 'learning-backup-v1\n' > "$OUT/COMPLETE"
echo "Backup completed: $OUT"
