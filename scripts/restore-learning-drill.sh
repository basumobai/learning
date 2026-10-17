#!/usr/bin/env bash
set -euo pipefail
umask 077
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"
BACKUP=${1:?Usage: bash scripts/restore-learning-drill.sh /private/backup-directory}
BACKUP=$(cd -- "$BACKUP" && pwd)
test "$(cat "$BACKUP/COMPLETE")" = learning-backup-v1
(cd "$BACKUP" && sha256sum --check --status SHA256SUMS)
DRILL_DB="learning_restore_$(python3 -c 'import secrets;print(secrets.token_hex(10))')"
[[ "$DRILL_DB" =~ ^learning_restore_[a-f0-9]{20}$ ]] || exit 1
WORK=$(mktemp -d)
SUCCESS=false
CREATED=false
cleanup() {
 if [[ "$SUCCESS" == true ]]; then
  [[ "$CREATED" != true ]] || docker compose exec -T postgres dropdb -U els "$DRILL_DB"
  rm -rf -- "$WORK"
 else
  echo "Restore drill failed; disposable copy retained: $DRILL_DB ; files: $WORK" >&2
 fi
}
trap cleanup EXIT
# The original database is never a restore target; createdb must succeed first.
docker compose exec -T postgres createdb -U els "$DRILL_DB"
CREATED=true
docker compose exec -T postgres pg_restore -U els -d "$DRILL_DB" --no-owner --exit-on-error --single-transaction < "$BACKUP/database.dump"
docker compose exec -T postgres psql -XqAt -v ON_ERROR_STOP=1 -U els -d "$DRILL_DB" < scripts/backup-snapshot.sql > "$WORK/restored.tsv"
cmp "$BACKUP/snapshot.tsv" "$WORK/restored.tsv"
python3 - "$BACKUP" "$WORK" <<'PY'
import hashlib, pathlib, sys, tarfile
backup,work=map(pathlib.Path,sys.argv[1:])
with tarfile.open(backup/'pdf.tar.gz') as archive:
 for member in archive.getmembers():
  if member.issym() or member.islnk():raise SystemExit('Refusing linked backup entries')
 archive.extractall(work/'pdf',filter='data')
# Parse private configuration only to verify presence, never print its contents.
settings={line.split('=',1)[0]:line.split('=',1)[1] for line in (backup/'environment.env').read_text().splitlines() if '=' in line and not line.lstrip().startswith('#')}
if not settings.get('APP_ENCRYPTION_KEY','').strip():raise SystemExit('Backup is missing encryption key')
# Force reads of every restored PDF file; archive integrity was checked above.
for path in (work/'pdf').rglob('*'):
 if path.is_file():hashlib.file_digest(path.open('rb'),'sha256')
PY
SUCCESS=true
echo 'Restore drill passed: immutable revisions, accounts, submissions, FSRS, evidence, PDF archive and encryption key restored to a disposable copy.'
