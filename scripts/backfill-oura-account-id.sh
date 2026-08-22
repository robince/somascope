#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/backfill-oura-account-id.sh --db /absolute/path/to/somascope.db [--apply]

Without --apply, prints a preview without displaying the account ID. With
--apply, creates a timestamped backup and fills a blank Oura external_account_id
only when archived personal_info contains exactly one stable non-empty ID.
Health data, OAuth tokens, cursors, and sync history are unchanged.

Stop somascope before --apply.
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
active_runs="$(query_scalar "SELECT COUNT(*) FROM sync_runs WHERE status='running';")"
oura_connection="$(query_scalar "SELECT COUNT(*) FROM connections WHERE provider='oura';")"
oura_has_account_id="$(query_scalar "SELECT COUNT(*) FROM connections WHERE provider='oura' AND NULLIF(TRIM(external_account_id),'') IS NOT NULL;")"
oura_candidate_ids="$(query_scalar "SELECT COUNT(DISTINCT NULLIF(TRIM(json_extract(payload_json,'$.id')),'')) FROM raw_documents WHERE provider='oura' AND document_kind='personal_info';")"

echo "Database: $db_path"
if [[ "$oura_connection" == "0" ]]; then
  echo "error: no Oura connection present" >&2; exit 1
elif [[ "$oura_has_account_id" == "1" ]]; then
  echo "Oura account ID: already populated; no change needed"; exit 0
else
  echo "Oura account ID: blank; archived stable identity candidates=$oura_candidate_ids"
fi
if [[ "$oura_candidate_ids" != "1" ]]; then
  echo "error: expected exactly one archived Oura identity, found $oura_candidate_ids" >&2; exit 1
fi
if [[ "$apply_changes" != true ]]; then
  echo "Preview only. Re-run with --apply after stopping somascope."; exit 0
fi
if [[ "$active_runs" != "0" ]]; then
  echo "error: $active_runs sync run(s) are marked running; stop somascope before applying" >&2; exit 1
fi

backup_path="${db_path}.backup-oura-id-$(date -u +%Y%m%dT%H%M%SZ)"
sqlite3 "$db_path" ".backup '$backup_path'"
sqlite3 -bail "$db_path" <<'SQL'
BEGIN IMMEDIATE;
UPDATE connections
SET external_account_id = (
      SELECT NULLIF(TRIM(json_extract(payload_json, '$.id')), '')
      FROM raw_documents
      WHERE provider='oura' AND document_kind='personal_info'
        AND NULLIF(TRIM(json_extract(payload_json, '$.id')), '') IS NOT NULL
      ORDER BY fetched_at DESC, id DESC LIMIT 1
    ),
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE provider='oura' AND NULLIF(TRIM(external_account_id), '') IS NULL;
COMMIT;
SQL

oura_backfilled="$(query_scalar "SELECT COUNT(*) FROM connections WHERE provider='oura' AND NULLIF(TRIM(external_account_id),'') IS NOT NULL;")"
if [[ "$oura_backfilled" != "1" ]]; then
  echo "error: post-backfill verification failed; restore from $backup_path" >&2; exit 1
fi
echo "Applied successfully."
echo "Backup: $backup_path"
echo "Oura account ID populated: yes"
