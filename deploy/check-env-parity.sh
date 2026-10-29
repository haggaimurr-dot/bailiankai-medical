#!/bin/bash
# P2 行121: 预发布(测试)与生产环境同构校验
# 比对环境变量键名、systemd 启动命令、goose 迁移版本是否一致。
# 不比对密钥值与业务数据。需 sudo 运行(读 root-only 的 api.env)。
set -u
PROD_ENV=/etc/bailiankai/api.env
TEST_ENV=/etc/bailiankai-webtest/api.env
FAILED=0

section() { echo; echo "== $1 =="; }

section "环境变量键名"
prod_keys=$(sudo grep -oE '^[A-Za-z_][A-Za-z0-9_]*=' "$PROD_ENV" 2>/dev/null | tr -d '=' | sort -u)
test_keys=$(sudo grep -oE '^[A-Za-z_][A-Za-z0-9_]*=' "$TEST_ENV" 2>/dev/null | tr -d '=' | sort -u)
if [ -z "$prod_keys" ] || [ -z "$test_keys" ]; then
  echo "读不到 env 文件(需 sudo)"; FAILED=1
else
  only_prod=$(comm -23 <(echo "$prod_keys") <(echo "$test_keys"))
  only_test=$(comm -13 <(echo "$prod_keys") <(echo "$test_keys"))
  [ -n "$only_prod" ] && { echo "仅生产有:"; echo "$only_prod" | sed 's/^/  /'; FAILED=1; }
  [ -n "$only_test" ] && { echo "仅测试有:"; echo "$only_test" | sed 's/^/  /'; FAILED=1; }
  [ -z "$only_prod$only_test" ] && echo "键名一致 ($(echo "$prod_keys" | wc -l) 个)"
fi

section "systemd ExecStart"
for u in bailiankai-api bailiankai-webtest-api; do
  echo "$u: $(systemctl show -p ExecStart "$u.service" 2>/dev/null | cut -d= -f2- | cut -c1-80)"
done

section "goose 迁移版本"
sudo python3 - "$PROD_ENV" "$TEST_ENV" <<'PYEOF'
import re, subprocess, sys
def dsn_of(path):
    env = {}
    with open(path) as f:
        for line in f:
            m = re.match(r'^([A-Za-z_][A-Za-z0-9_]*)=(.*)$', line.strip())
            if m:
                env[m.group(1)] = m.group(2).strip('"').strip("'")
    return env.get("DB_DSN", "")
def max_version(dsn):
    # 解析 user:pass@tcp(host:port)/dbname，不打印 DSN
    m = re.match(r'^(.*?):(.*?)@tcp\((.*?)\)/(.*?)$', dsn)
    if not m:
        return "DSN 格式未知"
    user, pw, host, db = m.groups()
    db = db.split("?")[0]
    import os
    try:
        env = dict(os.environ, MYSQL_PWD=pw)
        out = subprocess.run(
            ["mysql", "-h", host.split(":")[0], "-P", host.split(":")[1] if ":" in host else "3306",
             "-u", user, "-N", "-e",
             "SELECT COALESCE(MAX(version_id),0) FROM goose_db_version WHERE is_applied=1", db],
            capture_output=True, text=True, timeout=15, env=env)
        return out.stdout.strip() if out.returncode == 0 else "查询失败"
    except Exception as e:
        return f"查询异常: {e}"
prod = max_version(dsn_of(sys.argv[1]))
test = max_version(dsn_of(sys.argv[2]))
print(f"生产: {prod} / 测试: {test}")
print("一致" if prod == test and prod not in ("DSN 格式未知", "查询失败") else "不一致或无法比对")
PYEOF
[ "$FAILED" = 0 ] && echo && echo "同构校验通过" || { echo; echo "同构校验发现差异"; exit 1; }
