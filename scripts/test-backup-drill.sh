#!/usr/bin/env bash
set -euo pipefail
# Only invoked by the existing disposable Compose CI stack.
[[ "${GITHUB_ACTIONS:-}" == true ]] || { echo 'Run the documented backup/drill commands explicitly outside CI.' >&2; exit 1; }
umask 077
printf 'APP_ENCRYPTION_KEY=%s\nELS_ADMIN_PASSWORD=%s\nELS_ADMIN_USERNAME=%s\n' "$APP_ENCRYPTION_KEY" "$ELS_ADMIN_PASSWORD" "$ELS_ADMIN_USERNAME" > .env
mkdir -p data/pdf
cp services/api/cmd/api/testdata/questions.pdf data/pdf/restore-fixture.pdf
WORK=$(mktemp -d)
bash scripts/backup-learning.sh "$WORK/backup"
bash scripts/restore-learning-drill.sh "$WORK/backup"
rm -rf -- "$WORK"
