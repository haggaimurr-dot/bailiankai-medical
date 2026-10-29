#!/bin/bash
# P2 行122: 发布后自动冒烟检查（服务存活即可，不做逐页 200 检查）。
# 查各服务 systemctl is-active + healthz，结果汇总输出；失败标红并提示回滚。
# 用法: smoke-check.sh [test|prod|all]   (默认 test)
# 测试"停服"路径: SMOKE_EXTRA_DOWN="svc:127.0.0.1:19999/healthz" smoke-check.sh test
set -u
MODE="${1:-test}"
FAILED=0

check_unit() { # $1=unit $2=healthz-url $3=label
  local unit="$1" url="$2" label="$3" active http
  active=$(systemctl is-active "$unit" 2>/dev/null || echo "unknown")
  http=$(curl -s -o /dev/null -w "%{http_code}" --max-time 8 "$url" 2>/dev/null || echo "000")
  if [ "$active" = "active" ] && [ "$http" = "200" ]; then
    echo "  [OK] $label ($unit active, healthz 200)"
  else
    echo "  [FAIL] $label ($unit=$active, healthz=$http)"
    FAILED=1
  fi
}

echo "== 冒烟检查 ($MODE) =="
case "$MODE" in
  test|all)
    check_unit "bailiankai-webtest-api.service" "http://127.0.0.1:18085/healthz" "测试 API"
    # 前端由 nginx 提供：只确认端口有响应（不逐页验 200，遵用户规则）
    for p in 18084 18088; do
      code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 8 "http://127.0.0.1:$p/" 2>/dev/null || echo "000")
      if [ "$code" = "200" ]; then echo "  [OK] nginx 端口 $p (HTTP $code)"; else echo "  [FAIL] nginx 端口 $p (HTTP $code)"; FAILED=1; fi
    done
    ;;
esac
case "$MODE" in
  prod|all)
    check_unit "bailiankai-api.service" "http://127.0.0.1:18081/healthz" "生产 API"
    ;;
esac
# 额外注入的"停服"目标（用于验证失败路径，不真停服务）
if [ -n "${SMOKE_EXTRA_DOWN:-}" ]; then
  IFS=':' read -r label url <<< "$SMOKE_EXTRA_DOWN"
  code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 5 "$url" 2>/dev/null || echo "000")
  if [ "$code" = "200" ]; then echo "  [OK] $label"; else echo "  [FAIL] $label (healthz=$code)"; FAILED=1; fi
fi

echo
if [ "$FAILED" = 0 ]; then
  echo "冒烟检查全绿"
else
  echo "冒烟检查发现异常：请检查上方 [FAIL] 项，必要时回滚到上个版本（服务器保留 .bak 备份）"
  exit 1
fi
