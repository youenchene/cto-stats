#!/usr/bin/env bash
set -euo pipefail

# This script is intended to run once and exit (Cloud Run Jobs).
# It relies directly on environment variables (CONFIG_PATH, DATA_DIR, GITHUB_TOKEN)
# provided by the runtime; no cron environment file is used.

log() { echo "[$(date -Iseconds)] $*"; }

log "Starting job: import -> calculate"

log "Start Issues import & calculate"

# Run import (org and other filters are taken from config.yml if provided via CONFIG_PATH)
if ! cto-stats import --issues; then
  log "Import failed"
  exit 1
fi

# Run calculate
if ! cto-stats calculate --issues; then
  log "Calculate failed"
  exit 1
fi


log "Start Cloud Spending"

# Run import (org and other filters are taken from config.yml if provided via CONFIG_PATH)
if ! cto-stats import --cloudspending; then
  log "Import failed"
  exit 1
fi

# Run calculate
if ! cto-stats calculate --cloudspending; then
  log "Calculate failed"
  exit 1
fi

log "Start PR import & calculate"

if ! cto-stats import --pr; then
  log "Import failed"
  exit 1
fi

# Run calculate
if ! cto-stats calculate --pr; then
  log "Calculate failed"
  exit 1
fi

log "Job completed successfully"