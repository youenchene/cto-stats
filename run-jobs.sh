#!/usr/bin/env bash
set -euo pipefail

# Load env for cron (CONFIG_PATH, DATA_DIR, GITHUB_TOKEN)
if [ -f /etc/cron.env ]; then
  # shellcheck disable=SC1091
  source /etc/cron.env
fi

log() { echo "[$(date -Iseconds)] $*"; }

log "Starting scheduled job: import -> calculate"

# Run import (org and other filters are taken from config.yml if provided via CONFIG_PATH)
if ! cto-stats import; then
  log "Import failed"
  exit 1
fi

# Run calculate
if ! cto-stats calculate; then
  log "Calculate failed"
  exit 1
fi

log "Scheduled job completed successfully"