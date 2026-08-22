#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/reset-google-health-data.sh --db /absolute/path/to/somascope.db [--apply]

Without --apply, prints a read-only preview. With --apply, creates a timestamped
SQLite backup and deletes only Google Health canonical data, raw documents,
cursors, and sync-run history. OAuth tokens, credentials, and settings remain.

Stop somascope before --apply. Then restart and use Google Health Update for the
normal 30-day bootstrap, or Backfill from date for a longer range.
EOF
}

db_path=""
apply_changes=false
while (($# > 0)); do
  case "$1" in
    --db) (($# >= 2)) || { echo "error: --db requires a path" >&2; exit 2; }; db_path="$2"; shift 2 ;;
    --apply) apply_changes=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "error: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ -z "$db_path" || "$db_path" != /* ]]; then
  echo "error: --db must name an absolute database path" >&2; exit 2
fi
if [[ "$db_path" == *$'\n'* || "$db_path" == *"'"* || "$db_path" == *'"'* ]]; then
  echo "error: database paths containing quotes or newlines are not supported" >&2; exit 2
fi
[[ -f "$db_path" ]] || { echo "error: database does not exist: $db_path" >&2; exit 2; }
command -v sqlite3 >/dev/null 2>&1 || { echo "error: sqlite3 is required" >&2; exit 2; }

query_scalar() { sqlite3 -readonly "$db_path" "$1"; }
active_runs="$(query_scalar "SELECT COUNT(*) FROM sync_runs WHERE status = 'running';")"
foreign_key_violations_before="$(query_scalar "SELECT COUNT(*) FROM pragma_foreign_key_check;")"
google_daily="$(query_scalar "SELECT COUNT(*) FROM daily_records WHERE provider = 'google_health';")"
google_sleep="$(query_scalar "SELECT COUNT(*) FROM sleep_sessions WHERE provider = 'google_health';")"
google_raw="$(query_scalar "SELECT COUNT(*) FROM raw_documents WHERE provider = 'google_health';")"
google_cursors="$(query_scalar "SELECT COUNT(*) FROM sync_state WHERE provider = 'google_health';")"
google_runs="$(query_scalar "SELECT COUNT(*) FROM sync_runs WHERE provider = 'google_health';")"

echo "Database: $db_path"
echo "Google Health rows to clear: daily=$google_daily sleep=$google_sleep raw=$google_raw cursors=$google_cursors runs=$google_runs"
if [[ "$apply_changes" != true ]]; then
  echo "Preview only. Re-run with --apply after stopping somascope."; exit 0
fi
if [[ "$active_runs" != "0" ]]; then
  echo "error: $active_runs sync run(s) are marked running; stop somascope before applying" >&2; exit 1
fi

backup_path="${db_path}.backup-google-reset-$(date -u +%Y%m%dT%H%M%SZ)"
sqlite3 "$db_path" ".backup '$backup_path'"
sqlite3 -bail "$db_path" <<'SQL'
PRAGMA foreign_keys = ON;
BEGIN IMMEDIATE;
DELETE FROM daily_records WHERE provider = 'google_health';
DELETE FROM sleep_sessions WHERE provider = 'google_health';
DELETE FROM sync_state WHERE provider = 'google_health';
DELETE FROM sync_run_entities
WHERE run_id IN (SELECT id FROM sync_runs WHERE provider = 'google_health');
DELETE FROM sync_runs WHERE provider = 'google_health';
DELETE FROM raw_documents WHERE provider = 'google_health';
COMMIT;
SQL

remaining_google="$(query_scalar "SELECT (SELECT COUNT(*) FROM daily_records WHERE provider='google_health') + (SELECT COUNT(*) FROM sleep_sessions WHERE provider='google_health') + (SELECT COUNT(*) FROM raw_documents WHERE provider='google_health') + (SELECT COUNT(*) FROM sync_state WHERE provider='google_health') + (SELECT COUNT(*) FROM sync_runs WHERE provider='google_health');")"
oauth_preserved="$(query_scalar "SELECT COUNT(*) FROM connections WHERE provider='google_health' AND NULLIF(TRIM(access_token),'') IS NOT NULL;")"
foreign_key_violations_after="$(query_scalar "SELECT COUNT(*) FROM pragma_foreign_key_check;")"
if [[ "$remaining_google" != "0" || "$oauth_preserved" != "1" || "$foreign_key_violations_after" != "$foreign_key_violations_before" ]]; then
  echo "error: post-reset verification failed; restore from $backup_path" >&2; exit 1
fi
echo "Applied successfully."
echo "Backup: $backup_path"
echo "Google Health OAuth connection preserved: yes"
