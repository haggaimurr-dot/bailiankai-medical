#!/bin/bash
# P2 行124: 可用性探活。定时查各入口存活，失败记日志；同一目标连续 3 次失败记提醒。
# 用法: probe.sh [--quiet]   (加 --quiet 则全绿时不输出)
# 建议 cron: */5 * * * * /opt/bailiankai/deploy/probe.sh --quiet
set -u
LOGDIR=/var/log/bailiankai
LOG=$LOGDIR/probe.log
ALERTS=$LOGDIR/probe-alerts.log
mkdir -p "$LOGDIR" 2>/dev/null || { echo "写日志目录失败(需 sudo 或预建目录)"; exit 1; }
QUIET="${1:-}"

TARGETS="prod-admin|http://127.0.0.1:18083/|200
test-patient|http://127.0.0.1:18084/|200
test-workbench|http://127.0.0.1:18088/|200
test-api|http://127.0.0.1:18085/healthz|200
prod-api|http://127.0.0.1:18081/healthz|200"

ts=$(date '+%F %T')
failed=""
while IFS='|' read -r name url expect; do
  [ -z "$name" ] && continue
  code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 10 "$url" 2>/dev/null || echo "000")
  if [ "$code" = "$expect" ]; then
    echo "$ts $name OK $code" >> "$LOG"
  else
    echo "$ts $name FAIL $code (expect $expect)" >> "$LOG"
    # 连续 3 次失败才记提醒（防抖动）
    recent=$(grep " $name " "$LOG" | tail -3 | grep -c FAIL || true)
    if [ "$recent" -ge 3 ]; then
      echo "$ts ALERT $name 连续${recent}次探活失败 url=$url" >> "$ALERTS"
      failed="$failed $name"
    fi
  fi
done <<< "$TARGETS"

if [ -n "$failed" ]; then
  echo "探活异常:$failed（详见 $ALERTS）"
  exit 1
fi
[ "$QUIET" = "--quiet" ] || echo "探活全绿 ($(date '+%F %T'))"
