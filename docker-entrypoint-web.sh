#!/usr/bin/env bash
set -euo pipefail

# Minimal web-only entrypoint for Cloud Run (no cron, no background jobs)
# Expects CONFIG_PATH, DATA_DIR, and UI_DIR to be provided via environment

exec "$@"
