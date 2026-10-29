#!/bin/bash
# P2 行124: 访问统计。从 nginx access.log 按天汇总（只看汇总不看个人轨迹）。
# 用法: visit-stats.sh [YYYY-MM-DD]   (默认昨天；日志按天轮转)
# 输出: /var/log/bailiankai/visit-daily-<date>.json
set -u
DAY="${1:-$(date -d 'yesterday' '+%F')}"
LOGDIR=/var/log/bailiankai
OUT=$LOGDIR/visit-daily-$DAY.json
mkdir -p "$LOGDIR" 2>/dev/null || { echo "写日志目录失败"; exit 1; }

# nginx 日期格式: 30/Oct/2026
NDAY=$(LC_ALL=C date -d "$DAY" '+%d/%b/%Y' 2>/dev/null) || { echo "日期格式错误"; exit 1; }

python3 - "$NDAY" "$OUT" <<'PYEOF'
import gzip, json, re, sys, glob
nday, out = sys.argv[1], sys.argv[2]
pat = re.compile(r'^(\S+) \S+ \S+ \[([^\]]+)\] "(\S+) (\S+) \S+" (\d+) (\d+|-) "([^"]*)"')
total, by_status, by_path, uniq_ip = 0, {}, {}, set()
for path in ["/var/log/nginx/access.log"] + sorted(glob.glob("/var/log/nginx/access.log.*")):
    opener = gzip.open if path.endswith(".gz") else open
    try:
        with opener(path, "rt", errors="ignore") as f:
            for line in f:
                m = pat.match(line)
                if not m or nday not in m.group(2):
                    continue
                ip, _, method, uri, status = m.group(1), m.group(2), m.group(3), m.group(4), m.group(5)
                total += 1
                uniq_ip.add(ip)
                by_status[status] = by_status.get(status, 0) + 1
                key = method + " " + uri.split("?")[0]
                by_path[key] = by_path.get(key, 0) + 1
    except FileNotFoundError:
        continue
top_paths = sorted(by_path.items(), key=lambda kv: -kv[1])[:20]
json.dump({"date": sys.argv[1], "total": total, "uniq_ip": len(uniq_ip),
           "by_status": by_status, "top_paths": [[k, v] for k, v in top_paths]},
          open(out, "w"), ensure_ascii=False, indent=1)
print(f"{out}: 共 {total} 次访问，去重 IP {len(uniq_ip)}")
PYEOF
