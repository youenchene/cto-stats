#!/usr/bin/env bash
set -euo pipefail

# Write env for cron jobs
ENV_FILE=/etc/cron.env
{
  echo "export CONFIG_PATH=\"${CONFIG_PATH:-/config/config.yml}\""
  echo "export DATA_DIR=\"${DATA_DIR:-/data}\""
  if [ -n "${GITHUB_TOKEN:-}" ]; then
    echo "export GITHUB_TOKEN=\"${GITHUB_TOKEN}\""
  fi
} > "$ENV_FILE"
chmod 600 "$ENV_FILE"

# Install crontab: daily at 05:00 -> run import then calculate
CRON_FILE=/etc/crontabs/root
mkdir -p /etc/crontabs
: > "$CRON_FILE"
echo "0 1 * * * /usr/local/bin/run-jobs.sh >> /proc/1/fd/1 2>&1" >> "$CRON_FILE"
chmod 600 "$CRON_FILE"

# Start cron in background (busybox crond)
crond -l 8 -L /proc/1/fd/1

# Run an initial import + calculate on container start (non-fatal) without blocking the web
if [ "${IMPORT_ON_START:-true}" != "false" ]; then
  echo "[INFO] Starting initial import+calculate in background... (logs: stdout)"
  nohup /usr/local/bin/run-jobs.sh > /proc/1/fd/1 2>&1 &
  echo "[INFO] init jobs PID: $!"
fi

# Launch the web server (defaults provided via CMD)
exec "$@"